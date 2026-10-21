package passwords

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNormalizedHost(test *testing.T) {
	cases := map[string]string{
		"example.com":                         "example.com",
		"https://www.Example.com/sign-in?x=1": "example.com",
		"http://example.com:8080/":            "example.com",
		"example.com/sign-in":                 "example.com",
		"https://user:secret@example.com/":    "example.com",
		"https://example.com@evil.test/":      "evil.test",
		"https://example.com./":               "example.com",
		"https://www.example.com.evil.test/":  "example.com.evil.test",
		"https://login.example.com/":          "login.example.com",
		"www.test":                            "www.test",
		"ftp://example.com/":                  "",
		"javascript:alert(1)":                 "",
		"about:blank":                         "",
		"":                                    "",
		"https://[::1]:8443/":                 "[::1]",
		"https://example.test:notaport/":      "",
		"https://exa mple.test/":              "",
	}
	for site, wanted := range cases {
		if got := NormalizedHost(site); got != wanted {
			test.Errorf("NormalizedHost(%q) = %q, wanted %q", site, got, wanted)
		}
	}
}

func TestIsHostOfSites(test *testing.T) {
	sites := []string{"https://www.example.com/login", "other.example.org"}
	for page, wanted := range map[string]bool{
		"https://example.com/account":    true,
		"https://www.example.com/":       true,
		"https://other.example.org/":     true,
		"https://example.com.evil.test/": false,
		"https://evil.test/example.com":  false,
		"https://login.example.com/":     false,
		"https://example.com@evil.test/": false,
		"https://notexample.test/":       false,
		"about:blank":                    false,
		"":                               false,
	} {
		if got := IsHostOfSites(sites, page); got != wanted {
			test.Errorf("IsHostOfSites(%v, %q) = %t, wanted %t", sites, page, got, wanted)
		}
	}
	if IsHostOfSites(nil, "https://example.com/") {
		test.Error("a login with no sites matched a page")
	}
}

func TestIsRelyingPartyOfHost(test *testing.T) {
	// A suffix with two labels, built here so that no such name is written
	// in the source.
	publicSuffix := strings.Join([]string{"co", "uk"}, ".")
	for _, row := range []struct {
		relyingParty string
		page         string
		wanted       bool
	}{
		{"example.com", "https://example.com/", true},
		{"example.com", "https://www.example.com/", true},
		{"example.com", "https://login.example.com/", true},
		{"login.example.com", "https://deep.login.example.com/", true},
		{"www.example.com", "https://www.example.com/", true},
		{"login.example.com", "https://example.com/", false},
		{"example.com", "https://example.com.evil.test/", false},
		{"example.com", "https://notexample.test/", false},
		{"com", "https://example.com/", false},
		{publicSuffix, "https://shop.example." + publicSuffix + "/", false},
		{"example." + publicSuffix, "https://shop.example." + publicSuffix + "/", true},
		{"0.0.1", "https://127.0.0.1/", false},
		{"127.0.0.1", "https://127.0.0.1/", true},
		{"", "https://example.com/", false},
		{"example.com", "about:blank", false},
	} {
		if got := IsRelyingPartyOfHost(row.relyingParty, row.page); got != row.wanted {
			test.Errorf("IsRelyingPartyOfHost(%q, %q) = %t, wanted %t", row.relyingParty, row.page, got, row.wanted)
		}
	}
}

// The agent's sign-in matches the origin: the port too.
func TestIsOriginOfSitesMatchesThePortToo(test *testing.T) {
	cases := []struct {
		name  string
		sites []string
		page  string
		want  bool
	}{
		{"same host, default port", []string{"example.com"}, "https://example.com/sign-in", true},
		{"www is ignored", []string{"www.example.com"}, "https://example.com/", true},
		{"a site without a port is not another port", []string{"example.com"}, "https://example.com:8443/", false},
		{"a site with a port matches only that port", []string{"https://example.com:8443"}, "https://example.com:8443/x", true},
		{"a site with a port is not the default port", []string{"https://example.com:8443"}, "https://example.com/", false},
		{"a site with a port is not another port", []string{"https://example.com:8443"}, "https://example.com:9443/", false},
		{"the default port may be written", []string{"https://example.com:443"}, "https://example.com/", true},
		{"a written default port matches a page that writes it", []string{"example.com"}, "https://example.com:443/", true},
		{"a loopback page on http needs a site saved as http", []string{"localhost:8080"}, "http://localhost:8080/", false},
		{"a loopback page on http with its port", []string{"http://localhost:8080"}, "http://localhost:8080/", true},
		{"a loopback page on http, another port", []string{"http://localhost:8080"}, "http://localhost:9090/", false},
		{"a loopback page on http, no port named", []string{"http://localhost"}, "http://localhost:8080/", false},
		{"a loopback page on http, default port", []string{"http://localhost"}, "http://localhost/", true},
		{"an unreadable port matches nothing", []string{"https://example.com:99999"}, "https://example.com/", false},
		{"another host", []string{"example.com"}, "https://example.com.evil.test/", false},
		{"an address", []string{"[::1]:8443"}, "https://[::1]:8443/", true},
	}
	for _, each := range cases {
		if got := IsOriginOfSites(each.sites, each.page); got != each.want {
			test.Errorf("%s: IsOriginOfSites(%v, %q) = %v", each.name, each.sites, each.page, got)
		}
	}
	if EffectivePort("https://example.com/") != 443 || EffectivePort("http://example.com/") != 80 || EffectivePort("https://example.com:8443/x") != 8443 {
		test.Error("EffectivePort")
	}
}

// testdata/sitematch.json holds the cases the site rule must answer the same
// way here and in the Chrome extension (web/extension/src/passwords/hosts.ts,
// which reads the same file).
type sharedSiteMatchCases struct {
	NormalizedHostCases []struct {
		Site         string `json:"site"`
		ExpectedHost string `json:"expectedHost"`
	} `json:"normalizedHostCases"`
	OriginCases []struct {
		Name    string   `json:"name"`
		Sites   []string `json:"sites"`
		Page    string   `json:"page"`
		IsMatch bool     `json:"isMatch"`
	} `json:"originCases"`
}

func TestSharedSiteMatchCases(test *testing.T) {
	content, err := os.ReadFile("testdata/sitematch.json")
	if err != nil {
		test.Fatal(err)
	}
	var cases sharedSiteMatchCases
	if err := json.Unmarshal(content, &cases); err != nil {
		test.Fatal(err)
	}
	if len(cases.NormalizedHostCases) == 0 || len(cases.OriginCases) == 0 {
		test.Fatal("testdata/sitematch.json has no cases")
	}
	for _, each := range cases.NormalizedHostCases {
		if got := NormalizedHost(each.Site); got != each.ExpectedHost {
			test.Errorf("NormalizedHost(%q) = %q, wanted %q", each.Site, got, each.ExpectedHost)
		}
	}
	for _, each := range cases.OriginCases {
		if got := IsOriginOfSites(each.Sites, each.Page); got != each.IsMatch {
			test.Errorf("%s: IsOriginOfSites(%q, %q) = %v, wanted %v", each.Name, each.Sites, each.Page, got, each.IsMatch)
		}
	}
}

func TestNormalizedHostRefusesBackslashesAndControlCharacters(test *testing.T) {
	for _, site := range []string{"https://example.com\\@evil.test/", "https://exa\x00mple.test/", "example.com\x7f", "a\\b.test"} {
		if got := NormalizedHost(site); got != "" {
			test.Errorf("NormalizedHost(%q) = %q, wanted nothing", site, got)
		}
	}
}
