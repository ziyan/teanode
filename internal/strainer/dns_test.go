package strainer_test

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/spamfilter"
	"github.com/ziyan/teanode/internal/strainer"
)

// fakeResolver answers the names it was given and reports the rest as not
// existing, unless it was told to fail, which is the case that matters most.
type fakeResolver struct {
	listed  map[string]bool
	broken  bool
	queries atomic.Int64
}

func (self *fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	self.queries.Add(1)
	if self.broken {
		// What a resolver outage looks like: an error that is not "no such
		// name". Scoring this as "not listed" would make every sender on the
		// internet look reputable exactly when the filter cannot tell.
		return nil, &net.DNSError{Err: "server misbehaving", Name: host, IsTemporary: true}
	}
	if self.listed[host] {
		return []net.IPAddr{{IP: net.IPv4(127, 0, 0, 2)}}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func (self *fakeResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return nil, nil
}
func (self *fakeResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return nil, nil
}
func (self *fakeResolver) LookupAddr(ctx context.Context, address string) ([]string, error) {
	return nil, nil
}

func dnsSettings() *config.AntispamBuiltin {
	return &config.AntispamBuiltin{
		DNS: config.AntispamDNS{
			Enabled:      true,
			AddressLists: []config.AntispamList{{Zone: "zen.example.org", Weight: 3.0}},
			DomainLists:  []config.AntispamList{{Zone: "dbl.example.org", Weight: 2.5}},
		},
	}
}

func fired(t *testing.T, result *models.SpamFilterResult) map[string]float64 {
	t.Helper()
	out := make(map[string]float64, len(result.Checks))
	for _, check := range result.Checks {
		out[check.Symbol] = check.Score
	}
	return out
}

// A listed address scores, and the symbol names the list so a reader can see
// who said so.
func TestAListedAddressScores(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{listed: map[string]bool{"4.100.51.198.zen.example.org": true}}
	result, err := strainer.New(dnsSettings(), resolver, nil).Check(context.Background(), &spamfilter.Message{
		RemoteAddress: netip.MustParseAddr("198.51.100.4"),
	})
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if score := fired(t, result)["DNSBL_EXAMPLE"]; score != 3.0 {
		t.Errorf("DNSBL_EXAMPLE scored %v, want 3.0; got %v", score, result.Checks)
	}
}

// A resolver that cannot answer must not be read as a clean bill of health.
// This is the failure that would silently disable the whole check.
func TestABrokenResolverScoresNothing(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{broken: true}
	result, err := strainer.New(dnsSettings(), resolver, nil).Check(context.Background(), &spamfilter.Message{
		RemoteAddress: netip.MustParseAddr("198.51.100.4"),
	})
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if result.Score != 0 {
		t.Errorf("score = %v with a broken resolver, want 0; got %v", result.Score, result.Checks)
	}
}

// A message full of links to the same listed domain is not many times as
// spammy as one link, and must not be scored as though it were.
func TestManyListedLinksScoreOnce(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("visit https://bad.example.net/offer now. ", 40)
	resolver := &fakeResolver{listed: map[string]bool{"bad.example.net.dbl.example.org": true}}
	result, err := strainer.New(dnsSettings(), resolver, nil).Check(context.Background(), &spamfilter.Message{
		Body: []byte(body),
	})
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if result.Score != 2.5 {
		t.Errorf("score = %v for one listed domain repeated, want 2.5; got %v", result.Score, result.Checks)
	}
}

// A message with a thousand links must not become a thousand queries.
func TestTheNumberOfDomainsIsCapped(t *testing.T) {
	t.Parallel()

	builder := strings.Builder{}
	for index := 0; index < 500; index++ {
		builder.WriteString("https://host")
		builder.WriteString(string(rune('a' + index%26)))
		builder.WriteString(strings.Repeat("x", index%7))
		builder.WriteString(".example.net/ ")
	}
	settings := dnsSettings()
	settings.DNS.MaximumDomains = 5

	resolver := &fakeResolver{}
	if _, err := strainer.New(settings, resolver, nil).Check(context.Background(), &spamfilter.Message{
		Body: []byte(builder.String()),
	}); err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if queries := resolver.queries.Load(); queries > 5 {
		t.Errorf("made %d queries for a message capped at 5 domains", queries)
	}
}

// The same address arriving again is not asked about again; a bulk sender
// opens many connections.
func TestAnswersAreCached(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{listed: map[string]bool{"4.100.51.198.zen.example.org": true}}
	filter := strainer.New(dnsSettings(), resolver, nil)
	message := &spamfilter.Message{RemoteAddress: netip.MustParseAddr("198.51.100.4")}

	for index := 0; index < 5; index++ {
		if _, err := filter.Check(context.Background(), message); err != nil {
			t.Fatalf("Check() = %v", err)
		}
	}
	if queries := resolver.queries.Load(); queries != 1 {
		t.Errorf("asked the block list %d times for the same address, want 1", queries)
	}
}
