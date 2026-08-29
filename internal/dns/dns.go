// Package dns checks that each configured domain publishes the DNS records it
// needs, and reports what is missing.
//
// The check is advisory. It does not decide whether mail is accepted: a
// self-hoster owns the domains in their own configuration file, and refusing
// their mail because a periodic check has not run yet would be a poor first
// hour. Its job is to tell the operator exactly which record is wrong. See
// docs/decisions/20260818-dns-verification-is-advisory.md.
package dns

import (
	"context"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/periodic"
)

var log = logging.MustGetLogger("dns")

type Settings struct {
	// Nameserver to query, as host:port.
	Nameserver string

	// CheckInterval is how often every configured domain is re-checked.
	CheckInterval time.Duration
}

type Verifier interface {
	Close() error

	// Status returns the most recent check for every configured domain,
	// keyed by domain identifier. It never blocks on DNS.
	Status() map[string]*RecordSet

	// StatusFor returns the most recent check for one domain, or nil if it
	// has not been checked yet.
	StatusFor(domainId string) *RecordSet

	// Check resolves one domain's records now, updates the stored status and
	// returns it.
	Check(ctx context.Context, domainId string) (*RecordSet, error)

	// ExternalAddresses returns the addresses the outside world reaches this
	// server on, which is what an MX host's A and AAAA records must point at.
	ExternalAddresses(ctx context.Context) ExternalAddresses

	// CheckAll resolves every configured domain now.
	CheckAll(ctx context.Context) error

	// OutgoingIdentity returns how this server's outgoing mail identifies
	// itself: the address it leaves from, whether that address confirms
	// against its reverse name, and whether the greeting agrees. Cached, and
	// never blocking on the network.
	OutgoingIdentity(ctx context.Context) *OutgoingIdentity
}

type verifier struct {
	config   config.Store
	settings *Settings

	statusMutex sync.RWMutex
	status      map[string]*RecordSet

	// The server's own external addresses, looked up once and then refreshed
	// with each sweep. An address does not change often, and the lookup leaves
	// the machine, so it is not done per request.
	addressMutex     sync.RWMutex
	addresses        ExternalAddresses
	addressesFetched bool

	// How outgoing mail identifies itself. Established on the same schedule
	// as the record checks, because it asks the network — through the proxy,
	// when there is one — and a dashboard must not wait for that.
	outgoingMutex sync.RWMutex
	outgoing      *OutgoingIdentity

	waitGroup sync.WaitGroup
	periodic  periodic.Periodic

	client *dns.Client
}

func Open(configuration config.Store, settings *Settings) (Verifier, error) {
	self := &verifier{
		config:   configuration,
		settings: settings,
		status:   make(map[string]*RecordSet),
		client:   &dns.Client{},
	}
	self.periodic = periodic.New(context.TODO(), &self.waitGroup, self.spinOnce, &periodic.Settings{
		Interval: settings.CheckInterval,
		Name:     "dns",
	})
	self.periodic.Start()
	return self, nil
}

func (self *verifier) Close() error {
	self.periodic.Stop()
	self.waitGroup.Wait()
	return nil
}

func (self *verifier) spinOnce(ctx context.Context) error {
	self.refreshExternalAddresses(ctx)
	self.refreshOutgoingIdentity(ctx)
	return self.CheckAll(ctx)
}

// OutgoingIdentity returns the cached answer, establishing it on the first
// call so that a dashboard opened before the first sweep is not empty.
func (self *verifier) OutgoingIdentity(ctx context.Context) *OutgoingIdentity {
	self.outgoingMutex.RLock()
	identity := self.outgoing
	self.outgoingMutex.RUnlock()

	if identity != nil {
		return identity
	}
	return self.refreshOutgoingIdentity(ctx)
}

func (self *verifier) refreshOutgoingIdentity(ctx context.Context) *OutgoingIdentity {
	identity := self.checkOutgoingIdentity(ctx)

	self.outgoingMutex.Lock()
	defer self.outgoingMutex.Unlock()
	self.outgoing = identity
	return identity
}

// ExternalAddresses returns the cached addresses, looking them up on the first
// call so that a dashboard opened before the first sweep is not empty.
func (self *verifier) ExternalAddresses(ctx context.Context) ExternalAddresses {
	self.addressMutex.RLock()
	fetched := self.addressesFetched
	addresses := self.addresses
	self.addressMutex.RUnlock()

	if fetched {
		return addresses
	}
	return self.refreshExternalAddresses(ctx)
}

func (self *verifier) refreshExternalAddresses(ctx context.Context) ExternalAddresses {
	addresses := discoverExternalAddresses(ctx, self.config.Current().DNS.ExternalAddressServices)

	self.addressMutex.Lock()
	defer self.addressMutex.Unlock()

	// Keep whatever was known if this attempt failed outright, so a momentary
	// loss of connectivity does not blank the guidance the operator is reading.
	if addresses.IPv4 == "" && addresses.IPv6 == "" && self.addressesFetched {
		return self.addresses
	}
	self.addresses = addresses
	self.addressesFetched = true
	return addresses
}

func (self *verifier) CheckAll(ctx context.Context) error {
	configuration := self.config.Current()

	checked := make(map[string]bool, len(configuration.Domains))
	for _, domain := range configuration.Domains {
		if domain == nil || domain.Domain == "" {
			continue
		}
		checked[domain.ID] = true
		self.check(ctx, configuration, domain)
	}

	self.forgetUnconfigured(checked)
	return nil
}

// forgetUnconfigured drops the status of domains that are no longer
// configured, so the dashboard does not show a domain the operator removed.
func (self *verifier) forgetUnconfigured(checked map[string]bool) {
	self.statusMutex.Lock()
	defer self.statusMutex.Unlock()

	for domainId := range self.status {
		if !checked[domainId] {
			delete(self.status, domainId)
		}
	}
}

func (self *verifier) Check(ctx context.Context, domainId string) (*RecordSet, error) {
	configuration := self.config.Current()
	domain := configuration.FindDomainByID(domainId)
	if domain == nil {
		return nil, nil
	}
	return self.check(ctx, configuration, domain), nil
}

func (self *verifier) check(ctx context.Context, configuration *config.Configuration, domain *config.Domain) *RecordSet {
	recordSet := self.resolveDomainRecords(ctx, configuration, domain)

	previous := self.replaceStatus(domain.ID, recordSet)

	// Only say something when the answer changes, otherwise this logs every
	// domain every interval forever.
	if previous == nil || previous.Verified() != recordSet.Verified() {
		if recordSet.Verified() {
			log.Noticef("every DNS record for %q is published correctly", domain.Domain)
		} else {
			for _, record := range recordSet.Records {
				if record.Verified || record.Optional {
					continue
				}
				log.Warningf("%q is missing a %s record at %s: %s", domain.Domain, record.Type, record.Name, record.Purpose)
			}
		}
	}
	return recordSet
}

// replaceStatus stores a fresh result and returns what it replaced.
func (self *verifier) replaceStatus(domainId string, recordSet *RecordSet) *RecordSet {
	self.statusMutex.Lock()
	defer self.statusMutex.Unlock()

	previous := self.status[domainId]
	self.status[domainId] = recordSet
	return previous
}

func (self *verifier) Status() map[string]*RecordSet {
	self.statusMutex.RLock()
	defer self.statusMutex.RUnlock()

	status := make(map[string]*RecordSet, len(self.status))
	for domainId, recordSet := range self.status {
		status[domainId] = recordSet
	}
	return status
}

func (self *verifier) StatusFor(domainId string) *RecordSet {
	self.statusMutex.RLock()
	defer self.statusMutex.RUnlock()
	return self.status[domainId]
}
