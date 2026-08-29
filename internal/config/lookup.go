package config

import (
	"crypto"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// index holds the derived lookup tables for one configuration snapshot, so
// that the mail path never recompiles a regular expression or scans a slice
// per message.
//
// It is built lazily and can be invalidated. Lazily, because most snapshots
// are never queried; invalidated, because Store.Update hands the mutation
// function a snapshot it may both read and change, and a read before a change
// would otherwise leave the tables permanently missing whatever was added
// after it. That is not hypothetical: it made a domain created through the
// dashboard invisible to every request until the process restarted.
type index struct {
	mutex sync.RWMutex
	built bool

	domainsByName map[string]*Domain
	domainsByID   map[string]*Domain
	aliasesByID   map[string]*Alias

	// credentials maps a credential identifier to the domain that owns it,
	// because the mail path needs both.
	credentialsByID map[string]*credentialOwner

	// patterns holds one compiled expression per alias, keyed by alias
	// identifier. An alias whose pattern does not compile is absent, which
	// makes it match nothing; Validate reports it separately.
	patterns map[string]*regexp.Regexp

	// signers holds one parsed signing key per domain. Parsing PEM for every
	// outgoing message would be wasteful, and a domain with no usable key is
	// simply absent here.
	signers map[string]crypto.Signer
}

type credentialOwner struct {
	domain     *Domain
	credential *Credential
}

func (self *Configuration) indexIsBuilt() bool {
	self.index.mutex.RLock()
	defer self.index.mutex.RUnlock()
	return self.index.built
}

// InvalidateIndex marks the lookup tables as needing to be rebuilt.
//
// Every Store implementation has to call this after running a mutation. The
// tables are built on first read, and a mutation that reads before it writes
// — which is what a create does when it checks for a duplicate first — builds
// them from the state before the change. Without this the new domain, alias
// or credential cannot be found by anything, and the symptom is remote from
// the cause: mail is refused with "Invalid credentials" using a credential
// that is plainly there in the configuration.
func (self *Configuration) InvalidateIndex() {
	self.invalidateIndex()
}

func (self *Configuration) invalidateIndex() {
	self.index.mutex.Lock()
	defer self.index.mutex.Unlock()
	self.index.built = false
}

func (self *Configuration) buildIndex() {
	if self.indexIsBuilt() {
		return
	}

	self.index.mutex.Lock()
	defer self.index.mutex.Unlock()
	if self.index.built {
		return
	}

	func() {
		self.index.domainsByName = make(map[string]*Domain, len(self.Domains))
		self.index.domainsByID = make(map[string]*Domain, len(self.Domains))
		self.index.aliasesByID = make(map[string]*Alias)
		self.index.credentialsByID = make(map[string]*credentialOwner)
		self.index.patterns = make(map[string]*regexp.Regexp)
		self.index.signers = make(map[string]crypto.Signer)

		for _, domain := range self.Domains {
			if domain == nil {
				continue
			}
			if domain.Domain != "" {
				self.index.domainsByName[strings.ToLower(domain.Domain)] = domain
			}
			if domain.ID != "" {
				self.index.domainsByID[domain.ID] = domain
			}
			for _, alias := range domain.Aliases {
				if alias == nil {
					continue
				}
				if alias.ID != "" {
					self.index.aliasesByID[alias.ID] = alias
				}
				if alias.IsCatchAll() {
					continue
				}
				// Matching is case insensitive: the local part of an address
				// is not required to be, and a recipient who capitalises their
				// own address must still reach the alias they were given.
				compiled, err := regexp.Compile("(?i)" + alias.Pattern)
				if err != nil {
					log.Errorf("alias %q of domain %q has an invalid pattern %q and will match nothing: %s", alias.ID, domain.Domain, alias.Pattern, err)
					continue
				}
				self.index.patterns[alias.ID] = compiled
			}
			if domain.DKIM.PrivateKey != "" {
				signer, err := domain.DKIM.Signer()
				if err != nil {
					log.Errorf("domain %q has an unusable signing key, so its outgoing mail will not be signed: %s", domain.Domain, err)
				} else {
					self.index.signers[domain.ID] = signer
				}
			}
			for _, credential := range domain.Credentials {
				if credential == nil || credential.ID == "" {
					continue
				}
				self.index.credentialsByID[credential.ID] = &credentialOwner{domain: domain, credential: credential}
			}
		}
	}()
	self.index.built = true
}

// FindDomain returns the configured domain with this name, or nil. Matching is
// case insensitive because domain names are.
func (self *Configuration) FindDomain(name string) *Domain {
	self.buildIndex()
	return self.index.domainsByName[strings.ToLower(name)]
}

// FindDomainByID returns the configured domain with this identifier, or nil.
// Stored mail references domains by identifier, and the domain may since have
// been deleted, so callers must handle nil.
func (self *Configuration) FindDomainByID(id string) *Domain {
	self.buildIndex()
	return self.index.domainsByID[id]
}

// FindAliasByID returns the configured alias with this identifier, or nil.
// Stored deliveries reference aliases by identifier, and the alias may since
// have been deleted, so callers must handle nil.
func (self *Configuration) FindAliasByID(id string) *Alias {
	self.buildIndex()
	return self.index.aliasesByID[id]
}

// FindCredential returns the credential with this identifier together with the
// domain that owns it. Both are nil when the credential is unknown.
func (self *Configuration) FindCredential(id string) (*Domain, *Credential) {
	self.buildIndex()
	owner, ok := self.index.credentialsByID[id]
	if !ok {
		return nil, nil
	}
	return owner.domain, owner.credential
}

// MatchAliases returns the enabled aliases of this domain that should receive
// mail for a local part, in configuration order.
//
// A catch-all is a fallback, not an addition. If any alias with a pattern
// matches, only those are returned; the catch-alls are used only when nothing
// specific matched. Without that rule, an address covered by both a specific
// alias and a catch-all would be delivered twice, which is not what an
// operator who set up a catch-all as a safety net expects.
//
// Several specific aliases can still match at once, and each produces its own
// delivery. That is how one address forwards to two places.
func (self *Configuration) MatchAliases(domain *Domain, localPart string) []*Alias {
	if domain == nil {
		return nil
	}
	self.buildIndex()

	var matched []*Alias
	var catchAll []*Alias
	for _, alias := range domain.Aliases {
		if alias == nil || alias.Disabled {
			continue
		}
		if alias.IsCatchAll() {
			catchAll = append(catchAll, alias)
			continue
		}
		pattern, ok := self.index.patterns[alias.ID]
		if !ok {
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

// SignerFor returns the parsed signing key for a domain, and whether it has
// one. A domain with no key can still receive mail; what it sends is simply
// unsigned, which receivers are entitled to treat with suspicion.
func (self *Configuration) SignerFor(domain *Domain) (crypto.Signer, string, bool) {
	if domain == nil {
		return nil, "", false
	}
	self.buildIndex()
	signer, ok := self.index.signers[domain.ID]
	if !ok {
		return nil, "", false
	}
	return signer, domain.DKIM.Selector, true
}

// Hostname returns the fully qualified name that mail for this domain arrives
// at, for example "mail.example.com", used as the bounce and DMARC report
// domain. It falls back to the bare domain when no subdomain is configured.
func (self *Domain) Hostname() string {
	if self.Subdomain == "" {
		return self.Domain
	}
	return self.Subdomain + "." + self.Domain
}

// FindUser returns the account with this username, or nil.
func (self *Configuration) FindUser(username string) *User {
	if username == "" {
		return nil
	}
	for _, user := range self.Users {
		if user != nil && user.Username == username {
			return user
		}
	}
	return nil
}

// MailServers returns the hosts every domain's MX records should name, in
// order of preference.
//
// Empty configuration means the server itself, which is what a single-host
// deployment wants and what the getting-started guide tells people to publish.
// Reading it through here rather than at each call site means the two cases
// are the same shape everywhere.
func (self *Configuration) MailServers() []string {
	hosts := make([]string, 0, len(self.Server.MailServers))
	for _, host := range self.Server.MailServers {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 && self.Server.Name != "" {
		return []string{self.Server.Name}
	}
	return hosts
}

// MailHostsFor returns the names this domain's mail arrives at, which is what
// its MX records must point at.
//
// The domain the server is named under uses the server's own names: they are
// already in its zone, and a second name for the same host there buys nothing.
//
// Every other domain gets one name of its own, "mx." in front of the domain.
// It could be told to point at the server's name instead, and used to be, but
// then every domain published the name of a different one — look up the MX of
// any of them and you have the set they belong to. One name rather than one
// per name the server answers on, because a pair only means something when it
// is a pair of addresses: here both resolve to the same host, so the second is
// a record to publish and a certificate name to keep renewed, for nothing.
//
// The cost is that the server's address is written down once per domain rather
// than once, so a server that changes address has one record per domain to
// update. That is the same trade the signing keys make.
func (self *Configuration) MailHostsFor(domain *Domain) []string {
	if domain == nil || domain.Domain == "" {
		return self.MailServers()
	}

	// What the domain says, when it says anything. One deployment wants a
	// pair of names because that is what it has always published, another
	// wants one, and a third wants a particular domain to keep pointing at
	// the server's own name. None of those is wrong, so none of them is
	// derived.
	if configured := trimmedHosts(domain.MailServers); len(configured) > 0 {
		return configured
	}

	servers := self.MailServers()
	owned := make([]string, 0, len(servers))
	for _, host := range servers {
		if !self.ownsServerName(domain, host) {
			return []string{"mx." + domain.Domain}
		}
		owned = append(owned, host)
	}
	if len(owned) == 0 {
		return []string{"mx." + domain.Domain}
	}
	return owned
}

// trimmedHosts drops the blanks and the spaces, so a list typed into a form
// with a trailing comma means what it looks like it means.
func trimmedHosts(hosts []string) []string {
	trimmed := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(host), "."))); host != "" {
			trimmed = append(trimmed, host)
		}
	}
	return trimmed
}

// InThisDomain reports whether a name belongs to this domain's own zone, and
// so whether its address record is this operator's to publish and its
// certificate this server's to obtain.
func (self *Domain) InThisDomain(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	name := strings.ToLower(self.Domain)
	return host == name || strings.HasSuffix(host, "."+name)
}

// MailHostFor is the one name to use where only one is wanted — naming the
// host a sender reached, in a header. The first, which is the one an MX record
// prefers.
func (self *Configuration) MailHostFor(domain *Domain) string {
	hosts := self.MailHostsFor(domain)
	if len(hosts) == 0 {
		return self.Server.Name
	}
	return hosts[0]
}

// LinkHostFor is the name to write into an address this server puts in mail it
// sends: a picture in a template, and whatever else a recipient's program
// later fetches.
//
// The domain's own, when it has one. Otherwise the name its mail arrives at,
// which is the right guess and is sometimes wrong for a reason that has
// nothing to do with mail: the host it resolves to may answer HTTPS with
// something else. That is what LinkHost is for, and why this is a separate
// question from where the mail goes.
func (self *Configuration) LinkHostFor(domain *Domain) string {
	if domain != nil {
		if host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain.LinkHost), ".")); host != "" {
			return host
		}
	}
	return self.MailHostFor(domain)
}

// ownsServerName reports whether this domain is the one a name belonging to
// the server sits under, and so the one whose zone that name is published in.
//
// When no configured domain owns it — the server is called mail.example.net
// while serving example.com — nobody would be shown the records for it at all,
// so every domain is treated as the owner and the Setup page carries the
// address as well.
func (self *Configuration) ownsServerName(domain *Domain, serverName string) bool {
	owner := ""
	for _, candidate := range self.Domains {
		if candidate == nil || candidate.Domain == "" {
			continue
		}
		name := strings.ToLower(candidate.Domain)
		if strings.EqualFold(serverName, name) || strings.HasSuffix(strings.ToLower(serverName), "."+name) {
			// The longest match wins, so a server called mail.a.example.com
			// is owned by a.example.com rather than example.com when both are
			// configured.
			if len(name) > len(owner) {
				owner = name
			}
		}
	}
	if owner == "" {
		return true
	}
	return strings.EqualFold(owner, domain.Domain)
}

// SubmissionHost is the host a mail client should be told to connect to.
//
// The configured one when there is one, and the server's own name otherwise —
// which is right whenever the server is reachable at the name it announces,
// and that is most deployments.
func (self *Configuration) SubmissionHost() string {
	if host := strings.TrimSpace(self.SMTP.Submission.Host); host != "" {
		return host
	}
	return self.Server.Name
}

// SubmissionPort is the port a mail client should be told to connect to.
//
// The configured one when there is one, and otherwise the port this server
// listens on — which is wrong exactly when something forwards a different one,
// which is what the setting is for.
func (self *Configuration) SubmissionPort() string {
	if port := self.SMTP.Submission.Port; port != 0 {
		return strconv.Itoa(int(port))
	}
	return portOf(self.Listen.SMTPOutgoing)
}

// portOf extracts the port from a listen address such as ":587" or
// "127.0.0.1:10587".
func portOf(address string) string {
	if index := strings.LastIndex(address, ":"); index >= 0 {
		return address[index+1:]
	}
	return address
}
