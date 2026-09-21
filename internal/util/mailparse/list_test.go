package mailparse_test

import (
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

// What a message says about the list it came from, which is the whole of what
// the subscriptions page is built on: get this wrong and a newsletter either
// never appears or appears twice.
func TestParseList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		headers     []string
		from        string
		key         string
		listName    string
		unsubscribe []string
		oneClick    bool
		stripped    bool
	}{
		{
			// Apple's private relay rewrites the sender and takes the
			// unsubscribe address out, leaving the promise about it behind.
			// A sender never writes this header alone, so finding it alone
			// says the address was removed rather than never offered.
			name:     "an unsubscribe removed in transit, which is still a subscription",
			headers:  []string{"List-Unsubscribe-Post: List-Unsubscribe=One-Click"},
			from:     `"South China Morning Post" <news_at_e_scmp_com_trd2wntfxk@privaterelay.example.com>`,
			key:      "news_at_e_scmp_com_trd2wntfxk@privaterelay.example.com",
			listName: "South China Morning Post",
			stripped: true,
			// Nothing to post to, so nothing is promised that can be acted on.
			oneClick: false,
		},
		{
			name: "the same, from a list that publishes its own identity",
			headers: []string{
				"List-Id: Example Weekly <weekly.news.example.com>",
				"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
			},
			from:     "Example Weekly <news@example.com>",
			key:      "weekly.news.example.com",
			listName: "Example Weekly",
			stripped: true,
		},
		{
			// Bulk mail that offers no way out is not the same thing as mail
			// whose way out was removed, and neither is a subscription here.
			name:    "bulk, with nothing said about a list",
			headers: []string{"Precedence: bulk", "X-Auto-Response-Suppress: All"},
			from:    "Trending on Nextdoor <no-reply@rs.email.example.com>",
		},
		{
			name: "a list that publishes its own identity",
			headers: []string{
				"List-Id: Example Weekly <weekly.news.example.com>",
				"List-Unsubscribe: <https://news.example.com/u/abc>, <mailto:leave@example.com>",
				"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
			},
			from:        "Example Weekly <news@example.com>",
			key:         "weekly.news.example.com",
			listName:    "Example Weekly",
			unsubscribe: []string{"https://news.example.com/u/abc", "mailto:leave@example.com"},
			oneClick:    true,
		},
		{
			name:        "no identity of its own, so the sender is the subscription",
			headers:     []string{"List-Unsubscribe: <https://shop.example.com/leave>"},
			from:        "Example Shop <offers@shop.example.com>",
			key:         "offers@shop.example.com",
			listName:    "Example Shop",
			unsubscribe: []string{"https://shop.example.com/leave"},
		},
		{
			name:        "the sender's address when there is no display name either",
			headers:     []string{"List-Unsubscribe: <mailto:leave@shop.example.com>"},
			from:        "offers@shop.example.com",
			key:         "offers@shop.example.com",
			listName:    "offers@shop.example.com",
			unsubscribe: []string{"mailto:leave@shop.example.com"},
		},
		{
			// Long headers arrive written over several lines. Read line by
			// line, the second URL is a line of its own that begins with a
			// space.
			name: "a header folded onto a second line",
			headers: []string{
				"List-Unsubscribe: <https://news.example.com/u/abc>,\r\n <mailto:leave@example.com>",
			},
			from:        "news@example.com",
			key:         "news@example.com",
			listName:    "news@example.com",
			unsubscribe: []string{"https://news.example.com/u/abc", "mailto:leave@example.com"},
		},
		{
			name: "one-click promised, but only an address to write to",
			headers: []string{
				"List-Unsubscribe: <mailto:leave@example.com>",
				"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
			},
			from:        "news@example.com",
			key:         "news@example.com",
			listName:    "news@example.com",
			unsubscribe: []string{"mailto:leave@example.com"},
			oneClick:    false,
		},
		{
			// http is not https. A POST that anybody on the path can read is
			// not what RFC 8058 undertakes.
			name: "one-click promised over plain http",
			headers: []string{
				"List-Unsubscribe: <http://news.example.com/u/abc>",
				"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
			},
			from:        "news@example.com",
			key:         "news@example.com",
			listName:    "news@example.com",
			unsubscribe: []string{"http://news.example.com/u/abc"},
			oneClick:    false,
		},
		{
			name:        "a scheme this will not act on",
			headers:     []string{"List-Unsubscribe: <javascript:alert(1)>, <https://news.example.com/u>"},
			from:        "news@example.com",
			key:         "news@example.com",
			listName:    "news@example.com",
			unsubscribe: []string{"https://news.example.com/u"},
		},
		{
			name:     "an identifier written without a description",
			headers:  []string{"List-Id: <weekly.news.example.com>"},
			from:     "news@example.com",
			key:      "weekly.news.example.com",
			listName: "weekly.news.example.com",
		},
		{
			name:    "ordinary mail",
			headers: []string{"From: Ada <ada@example.com>", "Subject: lunch"},
			from:    "Ada <ada@example.com>",
		},
		{
			// Bulk on its own is not a subscription: a bounce notice carries
			// it and nobody signed up for one.
			name:    "bulk, but nothing to leave",
			headers: []string{"Precedence: bulk"},
			from:    "mailer-daemon@example.com",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			info := mailparse.ParseList(test.headers, test.from)
			if info.Key != test.key {
				t.Errorf("Key = %q, want %q", info.Key, test.key)
			}
			if info.Name != test.listName {
				t.Errorf("Name = %q, want %q", info.Name, test.listName)
			}
			if info.OneClick != test.oneClick {
				t.Errorf("OneClick = %v, want %v", info.OneClick, test.oneClick)
			}
			if info.Stripped != test.stripped {
				t.Errorf("Stripped = %v, want %v", info.Stripped, test.stripped)
			}
			if len(info.Unsubscribe) != len(test.unsubscribe) ||
				(len(test.unsubscribe) > 0 && !reflect.DeepEqual(info.Unsubscribe, test.unsubscribe)) {
				t.Errorf("Unsubscribe = %q, want %q", info.Unsubscribe, test.unsubscribe)
			}
			if info.Subscription() != (test.key != "") {
				t.Errorf("Subscription() = %v, want %v", info.Subscription(), test.key != "")
			}
		})
	}
}

// Which address the three ways of leaving pick out of the list.
func TestListInfoAddresses(t *testing.T) {
	t.Parallel()

	info := mailparse.ParseList([]string{
		"List-Unsubscribe: <mailto:leave@example.com>, <http://news.example.com/u>, <https://news.example.com/u>",
	}, "news@example.com")

	if got := info.HTTPSUnsubscribe(); got != "https://news.example.com/u" {
		t.Errorf("HTTPSUnsubscribe() = %q", got)
	}
	// The first web address the sender offered, whichever scheme it uses: a
	// person opening it in a browser is not making the promise RFC 8058 is.
	if got := info.WebUnsubscribe(); got != "http://news.example.com/u" {
		t.Errorf("WebUnsubscribe() = %q", got)
	}
	if got := info.MailUnsubscribe(); got != "mailto:leave@example.com" {
		t.Errorf("MailUnsubscribe() = %q", got)
	}

	empty := mailparse.ParseList([]string{"List-Id: <weekly.example.com>"}, "news@example.com")
	if empty.HTTPSUnsubscribe() != "" || empty.WebUnsubscribe() != "" || empty.MailUnsubscribe() != "" {
		t.Errorf("a list with no way to leave offered one: %+v", empty)
	}
}
