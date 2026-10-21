package passwords

import (
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
