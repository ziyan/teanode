package api_test

import (
	"net/http"
	"testing"

	"github.com/ziyan/teanode/internal/api"
)

// Who a request came from, which is what every audit row and every "last used
// from" records.
//
// Two ways to get it wrong, and this server had both. Ignoring
// X-Forwarded-For records the CDN's address for everybody, so the log says a
// hundred things happened from one address in Virginia. Believing it without
// asking who sent it lets anybody who can reach the server directly write
// their own address into the audit trail.
func TestRemoteAddressReadsForwardedHeadersOnlyFromAProxy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		remote    string
		forwarded string
		trusted   []string
		want      string
	}{
		{
			name:   "no proxy configured: the connection is the answer",
			remote: "203.0.113.7:44321",
			want:   "203.0.113.7",
		},
		{
			name:      "a forged header from a stranger is ignored",
			remote:    "203.0.113.7:44321",
			forwarded: "198.51.100.9",
			want:      "203.0.113.7",
		},
		{
			name:      "behind a trusted proxy, the client is the answer",
			remote:    "15.158.27.49:41000",
			forwarded: "198.51.100.9",
			trusted:   []string{"15.158.0.0/16"},
			want:      "198.51.100.9",
		},
		{
			name:      "a single trusted address, written without a range",
			remote:    "15.158.27.49:41000",
			forwarded: "198.51.100.9",
			trusted:   []string{"15.158.27.49"},
			want:      "198.51.100.9",
		},
		{
			name:      "what the client wrote before the proxy is not believed",
			remote:    "15.158.27.49:41000",
			forwarded: "10.0.0.1, 198.51.100.9",
			trusted:   []string{"15.158.0.0/16"},
			want:      "198.51.100.9",
		},
		{
			name:      "two of our proxies, and the client before them",
			remote:    "15.158.27.49:41000",
			forwarded: "198.51.100.9, 15.158.27.50",
			trusted:   []string{"15.158.0.0/16"},
			want:      "198.51.100.9",
		},
		{
			name:      "a proxy that gives the port too",
			remote:    "15.158.27.49:41000",
			forwarded: "198.51.100.9:52000",
			trusted:   []string{"15.158.0.0/16"},
			want:      "198.51.100.9",
		},
		{
			name:      "a header of nonsense falls back to the connection",
			remote:    "15.158.27.49:41000",
			forwarded: "not-an-address",
			trusted:   []string{"15.158.0.0/16"},
			want:      "15.158.27.49",
		},
		{
			name:    "an address the operator wrote wrongly is not a wildcard",
			remote:  "203.0.113.7:44321",
			trusted: []string{"not-a-range"},
			want:    "203.0.113.7",
		},
		{
			name:      "IPv6, bracketed as it arrives",
			remote:    "[2001:db8:1::1]:41000",
			forwarded: "[2001:db8:2::99]:5000",
			trusted:   []string{"2001:db8:1::/48"},
			want:      "2001:db8:2::99",
		},
		{
			// Deliberate: an address inside the trusted range is one of ours,
			// wherever it appears. A proxy that forwards to another proxy
			// writes its own address into the header, and that is not who
			// asked.
			name:      "an address inside the trusted range is a proxy, not a client",
			remote:    "15.158.27.49:41000",
			forwarded: "15.158.27.60",
			trusted:   []string{"15.158.0.0/16"},
			want:      "15.158.27.49",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := &http.Request{RemoteAddr: test.remote, Header: http.Header{}}
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-For", test.forwarded)
			}
			if got := api.RemoteAddress(request, test.trusted); got != test.want {
				t.Errorf("RemoteAddress() = %q, want %q", got, test.want)
			}
		})
	}
}

// A request that is not there at all — the first-user claim reads one out of
// a context that may not carry it.
func TestRemoteAddressWithoutARequest(t *testing.T) {
	t.Parallel()
	if got := api.RemoteAddress(nil, []string{"10.0.0.0/8"}); got != "" {
		t.Errorf("RemoteAddress(nil) = %q, want the empty string", got)
	}
}
