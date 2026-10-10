package api

import "testing"

// An app's own return address is a reverse domain name with a path, and
// nothing a browser or an operating system already means something by.
func TestOnlyAnAppsOwnAddressIsAPrivateUseRedirect(test *testing.T) {
	for _, address := range []string{
		"com.example.notes:/oauth",
		"com.example.app:/oauth/callback",
		"org.example.reader:/signed-in",
	} {
		if !IsPrivateUseRedirect(address) {
			test.Errorf("%q should be an app's own address", address)
		}
	}
	for _, address := range []string{
		"",
		"teanode:/oauth",                  // no dot: any app could claim it
		"javascript:alert(1)",             // opaque, and a browser's
		"file:///etc/passwd",              // a host-less file, no dot
		"com.example.app://host/oauth",    // a host
		"com.example.app:oauth",           // opaque
		"com.example.app:/",               // no path
		"com.example.app:/oauth#fragment", // a fragment never reaches anybody
		".example.notes:/oauth",
		"com..example:/oauth",
		"https://example.com/callback",
		"http://127.0.0.1:7391/callback",
	} {
		if IsPrivateUseRedirect(address) {
			test.Errorf("%q should not be an app's own address", address)
		}
	}
}

// A program is an installed app only when every address it registered is an
// app's own, so a web page cannot slip one in beside its own.
func TestANativeAppRegisteredOnlyAppAddresses(test *testing.T) {
	if !IsNativeAppClient([]string{"com.example.app:/oauth"}) {
		test.Error("an app with its own address was not taken for one")
	}
	if IsNativeAppClient([]string{"com.example.app:/oauth", "https://example.com/callback"}) {
		test.Error("a program with a web address too was taken for an app")
	}
	if IsNativeAppClient(nil) {
		test.Error("a program with no addresses was taken for an app")
	}
}

func TestADisplayNameHidesNothing(test *testing.T) {
	for given, shown := range map[string]string{
		"TeaNode":            "TeaNode",
		"  a   test\nphone ": "a test phone",
		"Tea\u202eedoN":      "TeaedoN",
		"Tea\u200bNode":      "TeaNode",
		"a\x00b\x1bc":        "abc",
		"名前":                 "名前",
		"一二三四五六七八九十一二三四五六七八九十一二三": "一二三四五六七八九十一二三四五六七八九十",
	} {
		if got := DisplayName(given, 20); got != shown {
			test.Errorf("%q was shown as %q, not %q", given, got, shown)
		}
	}
}
