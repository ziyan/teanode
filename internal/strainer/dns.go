package strainer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/spamfilter"
)

// Reputation lookups in public block lists.
//
// A block list is answered by ordinary DNS, which is why this needs no
// service of its own. To ask a list about 198.51.100.4, reverse the octets
// and append the list's zone — 4.100.51.198.zen.example.org — and look it up.
// An answer means listed; a name that does not exist means not listed. A
// domain is asked about without the reversal: example.com.dbl.example.org.
//
// The distinction that matters is between "not listed" and "could not ask".
// A resolver outage answers every query with an error, and scoring those as
// misses would quietly make every sender on the internet look reputable at
// the moment the filter is least able to tell.

// listedCacheDuration is how long one answer is reused.
//
// The resolver interface does not hand back the record's time to live, so
// this is a fixed span rather than the one the zone asked for. It is short
// because a listing changes when a host is cleaned up or newly compromised,
// and long enough that a sender opening thirty connections is asked about
// once.
const listedCacheDuration = 10 * time.Minute

// maximumDomainsDefault bounds how many domains one message can ask about
// when the setting is unset. A message with a thousand links must not become
// a thousand queries.
const maximumDomainsDefault = 10

// dnsTimeoutDefault bounds the whole set of lookups for one message.
const dnsTimeoutDefault = 5 * time.Second

// linkPattern finds the host of an http or https URL in a message body.
var linkPattern = regexp.MustCompile(`(?i)https?://([a-z0-9._~-]+)`)

// listing is one cached answer.
type listing struct {
	listed  bool
	expires time.Time
}

// listCache remembers answers for a short while.
type listCache struct {
	mutex   sync.Mutex
	entries map[string]listing
}

func newListCache() *listCache {
	return &listCache{entries: make(map[string]listing)}
}

func (self *listCache) get(key string) (bool, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	entry, ok := self.entries[key]
	if !ok || time.Now().After(entry.expires) {
		return false, false
	}
	return entry.listed, true
}

func (self *listCache) put(key string, listed bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	// Bounded rather than grown without limit: this is a cache in a
	// long-running server, and a busy relay would otherwise hold every
	// address it has ever seen. Dropping the lot is crude and costs one
	// round of lookups.
	if len(self.entries) > 8192 {
		self.entries = make(map[string]listing)
	}
	self.entries[key] = listing{listed: listed, expires: time.Now().Add(listedCacheDuration)}
}

// dnsChecks asks every configured list about this message.
func (self *Strainer) dnsChecks(ctx context.Context, message *spamfilter.Message) []check {
	settings := &self.settings.DNS
	if self.resolver == nil {
		return nil
	}

	timeout := time.Duration(settings.Timeout)
	if timeout <= 0 {
		timeout = dnsTimeoutDefault
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type query struct {
		list    config.AntispamList
		subject string
		symbol  string
	}
	queries := make([]query, 0, 8)

	if message.RemoteAddress.IsValid() {
		if reversed := reverseAddress(message.RemoteAddress); reversed != "" {
			for _, list := range settings.AddressLists {
				queries = append(queries, query{
					list:    list,
					subject: reversed,
					symbol:  "DNSBL_" + zoneSymbol(list.Zone),
				})
			}
		}
	}
	if len(settings.DomainLists) > 0 {
		maximumDomains := settings.MaximumDomains
		if maximumDomains <= 0 {
			maximumDomains = maximumDomainsDefault
		}
		for _, domain := range messageDomains(message, maximumDomains) {
			for _, list := range settings.DomainLists {
				queries = append(queries, query{
					list:    list,
					subject: domain,
					symbol:  "URIBL_" + zoneSymbol(list.Zone),
				})
			}
		}
	}

	// Asked together: one message's lookups are independent, and doing them
	// in turn would add every list's latency to every delivery.
	results := make([]bool, len(queries))
	var waitGroup sync.WaitGroup
	for index, asked := range queries {
		waitGroup.Add(1)
		go func(index int, asked query) {
			defer waitGroup.Done()

			listed, err := self.listed(ctx, asked.list.Zone, asked.subject)
			if err != nil {
				log.Debugf("block list %s could not be asked about %s: %s", asked.list.Zone, asked.subject, err)
				return
			}
			results[index] = listed
		}(index, asked)
	}
	waitGroup.Wait()

	// One symbol per zone, however many domains in the message it matched:
	// a message with six listed links is not six times as spammy, and
	// scoring it that way would let one message reach any threshold.
	fired := make(map[string]bool, len(queries))
	checks := make([]check, 0, 4)
	for index, asked := range queries {
		if !results[index] || fired[asked.symbol] {
			continue
		}
		fired[asked.symbol] = true
		checks = append(checks, check{
			symbol:      asked.symbol,
			score:       asked.list.Weight,
			description: fmt.Sprintf("listed in the public block list %s", asked.list.Zone),
		})
	}
	return checks
}

// listed asks one list about one subject, through the cache.
func (self *Strainer) listed(ctx context.Context, zone, subject string) (bool, error) {
	name := subject + "." + strings.TrimSuffix(zone, ".")
	if answer, ok := self.cache.get(name); ok {
		return answer, nil
	}

	_, err := self.resolver.LookupIPAddr(ctx, name)
	if err == nil {
		self.cache.put(name, true)
		return true, nil
	}

	// A name that does not exist is the answer "not listed", and is the
	// common case. Every other error means the question could not be asked,
	// and must not be recorded as a miss.
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) && dnsError.IsNotFound {
		self.cache.put(name, false)
		return false, nil
	}
	return false, err
}

// reverseAddress builds the name a block list is asked at.
//
// IPv4 reverses the four octets. IPv6 reverses every nibble, which is the
// same shape as a PTR lookup without the ip6.arpa suffix.
func reverseAddress(address netip.Addr) string {
	address = address.Unmap()
	if address.Is4() {
		octets := address.As4()
		return fmt.Sprintf("%d.%d.%d.%d", octets[3], octets[2], octets[1], octets[0])
	}
	if !address.Is6() {
		return ""
	}
	octets := address.As16()
	nibbles := make([]string, 0, 32)
	for index := len(octets) - 1; index >= 0; index-- {
		nibbles = append(nibbles, fmt.Sprintf("%x", octets[index]&0x0f), fmt.Sprintf("%x", octets[index]>>4))
	}
	return strings.Join(nibbles, ".")
}

// zoneSymbol turns a zone into something readable in a symbol name.
func zoneSymbol(zone string) string {
	trimmed := strings.TrimSuffix(zone, ".")
	// The interesting part is the organisation, not the label in front of it
	// or the public suffix behind: zen.spamhaus.org reads as SPAMHAUS.
	labels := strings.Split(trimmed, ".")
	if len(labels) >= 2 {
		trimmed = labels[len(labels)-2]
	}
	return strings.ToUpper(strings.ReplaceAll(trimmed, "-", "_"))
}

// messageDomains collects the domains worth asking about: the envelope
// sender's, and the hosts of links in the body.
func messageDomains(message *spamfilter.Message, maximum int) []string {
	seen := make(map[string]bool, maximum)
	domains := make([]string, 0, maximum)

	add := func(candidate string) bool {
		candidate = strings.ToLower(strings.Trim(strings.TrimSpace(candidate), "."))
		if candidate == "" || seen[candidate] || !strings.Contains(candidate, ".") {
			return true
		}
		// An address literal is not a domain and there is nothing to ask.
		if _, err := netip.ParseAddr(candidate); err == nil {
			return true
		}
		seen[candidate] = true
		domains = append(domains, candidate)
		return len(domains) < maximum
	}

	if message.Authentication != nil && message.Authentication.SPF != nil {
		if !add(message.Authentication.SPF.Domain) {
			return domains
		}
	}
	for _, match := range linkPattern.FindAllSubmatch(message.Body, -1) {
		if len(match) < 2 {
			continue
		}
		if !add(string(match[1])) {
			break
		}
	}
	return domains
}
