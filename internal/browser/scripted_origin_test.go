package browser

import "testing"

// A path, query or fragment that is not valid on its own (a stray "%") is a
// page of the site all the same, so a script there marks the site.
func TestTheOriginOfAnAddressSurvivesAMalformedPathQueryOrFragment(test *testing.T) {
	for _, address := range []string{
		"https://example.com/login",
		"https://example.com/100%/",
		"https://example.com/a%zz",
		"https://example.com/?q=%zz",
		"https://example.com/#%zz",
		"https://example.com?q=%zz",
		"https://WWW.example.com:443/x%",
	} {
		if got := OriginOf(address); got != "https://example.com" {
			test.Errorf("OriginOf(%q) = %q, expected %q", address, got, "https://example.com")
		}
	}
	for address, expected := range map[string]string{
		"http://example.com:8080/%zz":         "http://example.com:8080",
		"about:blank":                         "",
		"ftp://example.com/":                  "",
		"https://":                            "",
		"https://example.com\\@evil.example/": "",
	} {
		if got := OriginOf(address); got != expected {
			test.Errorf("OriginOf(%q) = %q, expected %q", address, got, expected)
		}
	}

	var filled FilledValues
	filled.NoteScriptedOrigin("https://example.com/100%/")
	if !filled.IsOriginScripted("https://example.com/login") {
		test.Error("a script on /100%/ did not mark the rest of the site")
	}
}

// When the site of a document the script ran in cannot be read, nothing says
// which pages it was on: every page of the tab is taken to be scripted. A page
// that belongs to no site is not a reason for that.
func TestAScriptOnADocumentWhoseSiteCannotBeReadIsOnEveryPage(test *testing.T) {
	var filled FilledValues
	filled.NoteScriptedAddress("7", "https://shop%zz.example.test/")
	if !filled.HasScriptOnEveryPage("7") || filled.HasScriptOnEveryPage("8") {
		test.Error("a script on an address with an unreadable host was not taken to be on every page of the tab")
	}

	var other FilledValues
	other.NoteScriptedAddress("7", "https://example.com\\@evil.example/")
	if !other.HasScriptOnEveryPage("7") {
		test.Error("a script on an address with a backslash was not taken to be on every page of the tab")
	}

	var blank FilledValues
	blank.NoteScriptedAddress("7", "about:blank")
	if blank.HasScriptOnEveryPage("7") || len(blank.ScriptedOrigins()) != 0 {
		test.Error("a page that belongs to no site was taken as a script on every page")
	}

	var readable FilledValues
	readable.NoteScriptedAddress("7", "https://example.com/100%/")
	if readable.HasScriptOnEveryPage("7") || !readable.IsOriginScripted("https://example.com/") {
		test.Error("a readable address marked more or less than its site")
	}
}
