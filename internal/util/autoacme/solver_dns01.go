package autoacme

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
)

const (
	// challengeRecordPrefix is the label a dns-01 challenge value is published
	// under, fixed by RFC 8555 section 8.4.
	challengeRecordPrefix = "_acme-challenge."

	// challengeRecordTTL is deliberately short so that a stale value from a
	// previous order expires quickly.
	challengeRecordTTL = 30

	// propagationPollInterval is how often the challenge record is re-checked
	// against the zone's own nameservers.
	propagationPollInterval = 5 * time.Second
)

// route53Solver answers the dns-01 challenge by publishing a TXT record in an
// AWS Route53 hosted zone.
//
// This is the only solver that can obtain a wildcard certificate, because a
// certificate authority will not issue "*.example.com" against an HTTP or TLS
// challenge. It is also the only one that needs cloud credentials, so it is
// optional and off by default; an operator who does not need a wildcard should
// use http-01 or tls-alpn-01 and give this server no AWS access at all.
//
// Unlike the other two solvers, this one batches: every value for an order
// goes into one record set per name, and propagation is waited for once.
type route53Solver struct {
	client      *route53.Client
	zoneID      string
	nameservers []string
	hosts       []string
}

// allHosts is every name across every certificate. The dns-01 solver works
// out which zone to write in from the names it may be asked about, and it can
// be asked about any of them.
func allHosts(settings *Settings) []string {
	var hosts []string
	for _, request := range settings.Certificates {
		hosts = append(hosts, request.Hosts...)
	}
	return hosts
}

func newRoute53Solver(settings *Settings) *route53Solver {
	return &route53Solver{
		client:      route53.NewFromConfig(settings.AWSConfig),
		zoneID:      settings.Route53ZoneID,
		nameservers: settings.Route53Nameservers,
		hosts:       allHosts(settings),
	}
}

func (self *route53Solver) Type() string {
	return "dns-01"
}

func (self *route53Solver) Present(ctx context.Context, client acmeClient, challenges []Challenge) error {
	values := make([]string, 0, len(challenges))
	for _, challenge := range challenges {
		value, err := client.DNS01ChallengeRecord(challenge.Challenge.Token)
		if err != nil {
			return fmt.Errorf("autoacme: cannot build the dns-01 record for %q: %w", challenge.Domain, err)
		}
		values = append(values, value)
	}

	names := self.challengeRecordNames()
	for _, name := range names {
		if err := self.replaceRecordValues(ctx, name, values); err != nil {
			return err
		}
	}
	for _, name := range names {
		nameservers, err := self.nameserversFor(ctx, name)
		if err != nil {
			return err
		}
		if err := self.waitForRecordValues(ctx, name, nameservers, values); err != nil {
			return err
		}
	}
	return nil
}

// nameserversFor returns the addresses to poll while waiting for a challenge
// record to appear.
//
// Configured nameservers win. Otherwise the zone's own are looked up, which
// matters more than it sounds: an empty list used to mean "do not wait at
// all", because the loop below simply had nothing to iterate over. The
// certificate authority was then asked to validate a record published a
// fraction of a second earlier, failed every time, and — with the retry around
// the order — failed in a tight loop.
func (self *route53Solver) nameserversFor(ctx context.Context, name string) ([]string, error) {
	if len(self.nameservers) > 0 {
		return self.nameservers, nil
	}

	zone := strings.TrimPrefix(name, challengeRecordPrefix)
	records, err := net.DefaultResolver.LookupNS(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("autoacme: cannot find the nameservers for %q, so cannot tell when %q has propagated: %w", zone, name, err)
	}

	nameservers := make([]string, 0, len(records))
	for _, record := range records {
		if host := strings.TrimSuffix(record.Host, "."); host != "" {
			nameservers = append(nameservers, net.JoinHostPort(host, "53"))
		}
	}
	if len(nameservers) == 0 {
		return nil, fmt.Errorf("autoacme: %q has no nameservers, so there is nothing to wait on", zone)
	}
	log.Debugf("waiting on the nameservers for %q: %v", zone, nameservers)
	return nameservers, nil
}

func (self *route53Solver) CleanUp(ctx context.Context, challenges []Challenge) error {
	// The record is left in place with a short TTL. Deleting it needs the
	// exact current value set, which a concurrent order may already have
	// replaced, and an orphaned _acme-challenge TXT record is harmless.
	return nil
}

// challengeRecordNames returns the record to publish for each configured host.
// A wildcard host is authorized under its bare domain, and a bare domain plus
// its wildcard therefore share one record.
func (self *route53Solver) challengeRecordNames() []string {
	seen := make(map[string]bool, len(self.hosts))
	names := make([]string, 0, len(self.hosts))
	for _, host := range self.hosts {
		name := challengeRecordPrefix + strings.TrimPrefix(host, "*.")
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func (self *route53Solver) replaceRecordValues(ctx context.Context, name string, values []string) error {
	resourceRecords := make([]types.ResourceRecord, 0, len(values))
	for _, value := range values {
		resourceRecords = append(resourceRecords, types.ResourceRecord{
			Value: aws.String(fmt.Sprintf("%q", value)),
		})
	}

	if _, err := self.client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &types.ChangeBatch{
			Changes: []types.Change{
				{
					Action: types.ChangeActionUpsert,
					ResourceRecordSet: &types.ResourceRecordSet{
						Name:            aws.String(name),
						Type:            types.RRTypeTxt,
						ResourceRecords: resourceRecords,
						TTL:             aws.Int64(challengeRecordTTL),
					},
				},
			},
		},
		HostedZoneId: aws.String(self.zoneID),
	}); err != nil {
		return fmt.Errorf("autoacme: cannot publish the dns-01 record %q: %w", name, err)
	}
	log.Debugf("published dns-01 challenge record %q", name)
	return nil
}

// waitForRecordValues blocks until every expected value is visible on every
// configured nameserver, or the context is done. Asking the zone's own
// nameservers rather than a recursive resolver avoids waiting for caches that
// are not in the certificate authority's path anyway.
func (self *route53Solver) waitForRecordValues(ctx context.Context, name string, nameservers []string, expectedValues []string) error {
	for _, nameserver := range nameservers {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				dialer := net.Dialer{Timeout: 5 * time.Second}
				return dialer.DialContext(ctx, "udp", nameserver)
			},
		}

		for {
			select {
			case <-ctx.Done():
				return fmt.Errorf("autoacme: gave up waiting for %q to propagate to %s: %w", name, nameserver, ctx.Err())
			case <-time.After(propagationPollInterval):
			}

			// A lookup failure here is expected while the record is still
			// propagating: the name does not exist yet. Keep waiting rather
			// than failing the order.
			values, err := resolver.LookupTXT(ctx, name)
			if err != nil {
				log.Debugf("%q not resolvable on %s yet: %s", name, nameserver, err)
				continue
			}
			if containsAll(values, expectedValues) {
				log.Debugf("%q has propagated to %s", name, nameserver)
				break
			}
			log.Debugf("%q has not fully propagated to %s yet: %v", name, nameserver, values)
		}
	}
	return nil
}

func containsAll(values, expected []string) bool {
	present := make(map[string]bool, len(values))
	for _, value := range values {
		present[value] = true
	}
	for _, value := range expected {
		if !present[value] {
			return false
		}
	}
	return true
}
