package mx

import (
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The domain table, as the mail path reads it: a row per lookup, inside the
// transaction the message is being handled in, plus the two caches that keep
// a message from recompiling an alias pattern or reparsing a signing key.
//
// The caches are keyed by what they were built from — the pattern, the key —
// so an alias whose pattern is edited or a domain whose key is regenerated
// misses at once, without anybody invalidating anything; a stale entry is
// merely one nobody asks for again.

// directory holds the per-process caches, and where the domains come from.
type directory struct {
	patterns sync.Map // pattern -> *regexp.Regexp
	signers  sync.Map // sha256 of the private key -> crypto.Signer

	domainsMutex     sync.Mutex
	domains          []*models.Domain
	domainsFetchedAt time.Time

	// source answers the lookups outside a transaction. Nil means the
	// database; a test supplies a list.
	source domainSource
}

// domainSource is where the domains are read from outside a transaction.
type domainSource interface {
	domainByName(name string) (*models.Domain, error)
	listDomains() ([]*models.Domain, error)
}

// staticDomains is a domainSource over a list, for tests.
type staticDomains []*models.Domain

func (self staticDomains) domainByName(name string) (*models.Domain, error) {
	for _, domain := range self {
		if strings.EqualFold(domain.Domain, name) {
			return domain, nil
		}
	}
	return nil, nil
}

func (self staticDomains) listDomains() ([]*models.Domain, error) {
	return self, nil
}

// databaseDomains is the domainSource the server runs with.
type databaseDomains struct {
	database db.Database
}

func (self databaseDomains) domainByName(name string) (*models.Domain, error) {
	var domain *models.Domain
	err := self.database.Transaction(func(tx db.Transaction) error {
		var err error
		domain, err = tx.GetDomainByName(name)
		return err
	})
	return domain, err
}

func (self databaseDomains) listDomains() ([]*models.Domain, error) {
	var domains []*models.Domain
	err := self.database.Transaction(func(tx db.Transaction) error {
		var err error
		domains, err = tx.ListDomains()
		return err
	})
	return domains, err
}

func (self *exchange) domainSource() domainSource {
	if self.directory.source != nil {
		return self.directory.source
	}
	return databaseDomains{database: self.database}
}

// domainListTTL bounds how stale the list of every domain may be. It is only
// asked for to decide which domain owns the server's own name, a question
// whose answer changes when a domain is added, which is rare.
const domainListTTL = 5 * time.Second

// matchingAliases returns the enabled aliases of a domain that should receive
// mail for a local part, in the order the operator arranged them.
//
// A catch-all is a fallback, not an addition. If any alias with a pattern
// matches, only those are returned; the catch-alls are used only when nothing
// specific matched. Without that rule, an address covered by both a specific
// alias and a catch-all would be delivered twice, which is not what an
// operator who set up a catch-all as a safety net expects.
func (self *exchange) matchingAliases(domain *models.Domain, localPart string) []*models.Alias {
	if domain == nil {
		return nil
	}
	var matched []*models.Alias
	var catchAll []*models.Alias
	for _, alias := range domain.Aliases {
		if alias == nil || alias.Disabled {
			continue
		}
		if alias.IsCatchAll() {
			catchAll = append(catchAll, alias)
			continue
		}
		pattern := self.pattern(alias)
		if pattern == nil {
			continue
		}
		if pattern.MatchString(localPart) {
			matched = append(matched, alias)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return catchAll
}

// pattern is the alias's compiled expression, or nil when it does not
// compile, in which case it matches nothing and the writer reported why.
func (self *exchange) pattern(alias *models.Alias) *regexp.Regexp {
	if cached, ok := self.directory.patterns.Load(alias.Pattern); ok {
		return cached.(*regexp.Regexp)
	}
	// Matching is case insensitive: the local part of an address is not
	// required to be, and a recipient who capitalises their own address must
	// still reach the alias they were given.
	compiled, err := regexp.Compile("(?i)" + alias.Pattern)
	if err != nil {
		log.Errorf("alias %q has an invalid pattern %q and will match nothing: %s", alias.ID, alias.Pattern, err)
		return nil
	}
	self.directory.patterns.Store(alias.Pattern, compiled)
	return compiled
}

// signerFor returns the parsed signing key for a domain, and whether it has
// one. A domain with no key can still receive mail; what it sends is simply
// unsigned, which receivers are entitled to treat with suspicion.
func (self *exchange) signerFor(domain *models.Domain) (crypto.Signer, string, bool) {
	if domain == nil || domain.DKIM.PrivateKey == "" {
		return nil, "", false
	}
	digest := sha256.Sum256([]byte(domain.DKIM.PrivateKey))
	key := hex.EncodeToString(digest[:])
	if cached, ok := self.directory.signers.Load(key); ok {
		return cached.(crypto.Signer), domain.DKIM.Selector, true
	}
	signer, err := domain.DKIM.Signer()
	if err != nil {
		log.Errorf("domain %q has an unusable signing key, so its outgoing mail will not be signed: %s", domain.Domain, err)
		return nil, "", false
	}
	self.directory.signers.Store(key, signer)
	return signer, domain.DKIM.Selector, true
}

// domainByName reads one domain outside any transaction, for the places that
// have none: naming the host a sender reached, in a header.
func (self *exchange) domainByName(name string) *models.Domain {
	domain, err := self.domainSource().domainByName(name)
	if err != nil {
		log.Errorf("could not read the domain %q: %s", name, err)
		return nil
	}
	return domain
}

// allDomains is every domain, a few seconds old at most.
func (self *exchange) allDomains() []*models.Domain {
	self.directory.domainsMutex.Lock()
	defer self.directory.domainsMutex.Unlock()
	if time.Since(self.directory.domainsFetchedAt) < domainListTTL {
		return self.directory.domains
	}
	domains, err := self.domainSource().listDomains()
	if err != nil {
		log.Errorf("could not list the domains: %s", err)
		return self.directory.domains
	}
	self.directory.domains = domains
	self.directory.domainsFetchedAt = time.Now()
	return domains
}
