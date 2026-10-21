package browser

import (
	"context"
	"strings"
	"testing"
)

// The mark of a scripted document is kept the way the lock is, in the other
// direction: a new document ends it, a move through the history to the
// scripted document or its address brings it back, and something that runs in
// every page is never undone by a navigation.
func TestScriptedDocumentsEndWithANewDocumentAndComeBackWithTheOldOne(test *testing.T) {
	var filled FilledValues
	if filled.IsScripted("7", "") || filled.HasBeenScripted("7") {
		test.Fatal("a tab nothing ran in is scripted")
	}
	filled.Committed("7", Commit{DocumentID: "doc-1", Address: "https://shop.example.test/sign-in", Count: 1})
	// A script with no document named runs in the newest one committed.
	filled.NoteScripted("7", Document{})
	if !filled.IsScripted("7", "") || !filled.IsScripted("7", "doc-1") || filled.IsScripted("7", "doc-2") || filled.IsScripted("8", "") {
		test.Fatal("the scripted document")
	}
	// A new page ends it, and going back to the old page brings it back.
	filled.Committed("7", Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 2})
	if filled.IsScripted("7", "") {
		test.Fatal("a new document is scripted")
	}
	filled.Committed("7", Commit{DocumentID: "doc-1", Address: "https://shop.example.test/sign-in", IsBackForward: true, Count: 3})
	if !filled.IsScripted("7", "") {
		test.Fatal("the scripted document came back unmarked")
	}
	filled.Committed("7", Commit{DocumentID: "doc-3", Address: "https://shop.example.test/other", IsBackForward: true, Count: 4})
	if filled.IsScripted("7", "") {
		test.Fatal("a move to another page left the mark")
	}
	// A reload is a new document at the same address.
	filled.NoteScripted("7", Document{ID: "doc-3", Address: "https://shop.example.test/other"})
	filled.Committed("7", Commit{DocumentID: "doc-4", Address: "https://shop.example.test/other", Count: 5})
	if filled.IsScripted("7", "") {
		test.Fatal("a reload is scripted")
	}

	// Something that runs in every page is never undone.
	filled.NoteScriptOnEveryPage("7")
	filled.Committed("7", Commit{DocumentID: "doc-5", Address: "https://shop.example.test/", Count: 6})
	if !filled.IsScripted("7", "") || !filled.IsScripted("7", "doc-5") || !filled.HasScriptOnEveryPage("7") || filled.HasScriptOnEveryPage("8") {
		test.Fatal("a script on every page was undone by a navigation")
	}
	// A script in a tab the browser did not name could be in any.
	filled.NoteScripted("", Document{Address: "https://shop.example.test/"})
	if !filled.IsScripted("9", "") {
		test.Fatal("a script in an unnamed tab is not in any")
	}
	var none *FilledValues
	none.NoteScripted("", Document{})
	if none.IsScripted("", "") || none.HasSecretHost("", "") {
		test.Fatal("a nil value is scripted")
	}
}

// The hosts a secret was put into are kept for the life of the tab, whichever
// document it shows.
func TestSecretHostsAreKeptForTheLifeOfTheTab(test *testing.T) {
	var filled FilledValues
	if filled.HasSecretHost("7", "") || filled.HasSecretHost("7", "shop.example.test") {
		test.Fatal("a secret host on a tab nothing was filled into")
	}
	filled.RememberSecretHost("7", "Shop.Example.test")
	filled.Committed("7", Commit{DocumentID: "doc-9", Address: "https://other.example.test/", Count: 1})
	if !filled.HasSecretHost("7", "shop.example.test") || !filled.HasSecretHost("7", "") || filled.HasSecretHost("7", "other.example.test") || filled.HasSecretHost("8", "") {
		test.Fatal("the secret hosts")
	}
	filled.RememberSecretHost("", "unnamed.example.test")
	if !filled.HasSecretHost("8", "unnamed.example.test") {
		test.Fatal("a fill in an unnamed tab could be any")
	}
}

// A script run in the headless page marks the site of the document the browser
// says it is in; the fill, the one-time code and the passkey are refused on
// any page of the site, with no command sent to the page, until the browser is
// reset.
func TestEvaluateMarksTheDocumentAndAFillIsRefusedOnIt(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	signIn := "https://shop.example.test/sign-in"
	chrome.setHistory([]string{"about:blank", signIn}, nil, -1, "loader-1")
	chrome.evaluate = func(string) any { return map[string]any{"isFilled": true, "problem": ""} }
	request := FillRequest{Ref: 4, Host: "shop.example.test", Port: 443, Kind: FieldKindPassword, Text: "an-invented-secret-value"}
	ctx := context.Background()

	// The snapshot and the fill are the tool's own scripts, not the agent's.
	if _, err := page.Snapshot(ctx, "interactive", 0); err != nil {
		test.Fatal(err)
	}
	if err := page.FillSecret(ctx, request); err != nil {
		test.Fatalf("a fill before any script: %s", err)
	}
	if _, err := page.Evaluate(ctx, "document.title", 100); err != nil {
		test.Fatal(err)
	}
	evaluations := chrome.sent("Runtime.evaluate")
	for name, fill := range map[string]func() error{
		"a password": func() error { return page.FillSecret(ctx, request) },
		"a code": func() error {
			request := request
			request.Kind = FieldKindOneTimeCode
			return page.FillSecret(ctx, request)
		},
		"a user name": func() error {
			request := request
			request.Kind = FieldKindUserName
			return page.FillSecret(ctx, request)
		},
		"a passkey": func() error {
			return page.AddPasskey(ctx, Passkey{CredentialID: []byte("c"), RelyingPartyID: "shop.example.test", PrivateKey: []byte("k")})
		},
		"a passkey twice": func() error {
			return page.AddPasskey(ctx, Passkey{CredentialID: []byte("c"), RelyingPartyID: "shop.example.test", PrivateKey: []byte("k")})
		},
	} {
		err := fill()
		if err == nil || err.Error() != ScriptedSiteWords || strings.Contains(err.Error(), request.Text) {
			test.Errorf("%s on a scripted document: %v", name, err)
		}
	}
	if chrome.sent("Runtime.evaluate") != evaluations || chrome.sent("WebAuthn.addVirtualAuthenticator") != 0 {
		test.Fatalf("something was sent to a scripted page: %v", chrome.commands)
	}
	// A reload, or a new page, is a new document of the same site, which the
	// script may have left things on.
	chrome.setHistory([]string{"about:blank", signIn}, nil, -1, "loader-2")
	if err := page.FillSecret(ctx, request); err == nil || err.Error() != ScriptedSiteWords {
		test.Fatalf("a fill on a new document of the site: %v", err)
	}
	// Another site is not marked.
	chrome.setHistory([]string{"about:blank", "https://bank.example.test/sign-in"}, nil, -1, "loader-3")
	if err := page.FillSecret(ctx, request); err != nil {
		test.Fatalf("a fill on another site: %s", err)
	}
	// A reset page has nothing from before.
	chrome.setHistory([]string{"about:blank", signIn}, nil, -1, "loader-4")
	if err := page.Reset(ctx); err != nil {
		test.Fatal(err)
	}
	if err := page.FillSecret(ctx, request); err != nil {
		test.Fatalf("a fill after a reset: %s", err)
	}
}

// A script that cannot be placed in a document, because the browser did not
// say which one the page shows, cannot be ruled out later: the page is not
// filled at all.
func TestAScriptThatCannotBePlacedMarksThePageForGood(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	chrome.setHistory([]string{"about:blank", "https://shop.example.test/sign-in"}, nil, -1, "")
	chrome.evaluate = func(string) any { return map[string]any{"isFilled": true, "problem": ""} }
	ctx := context.Background()
	if _, err := page.Evaluate(ctx, "1", 10); err != nil {
		test.Fatal(err)
	}
	chrome.setHistory([]string{"about:blank", "https://shop.example.test/sign-in"}, nil, -1, "loader-2")
	err := page.FillSecret(ctx, FillRequest{Ref: 4, Host: "shop.example.test", Port: 443, Kind: FieldKindPassword, Text: "an-invented-secret-value"})
	if err == nil || err.Error() != ScriptOnEveryPageWords {
		test.Fatalf("a page a script could not be placed in: %v", err)
	}
}

// The check of a form is in the script that fills: the script carries it.
func TestTheFillScriptChecksWhereTheFormSends(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	var script string
	chrome.evaluate = func(expression string) any {
		script = expression
		return map[string]any{"isFilled": true, "problem": ""}
	}
	if err := page.FillSecret(context.Background(), FillRequest{Ref: 4, Host: "example.test", Kind: FieldKindPassword, Text: "an-invented-secret-value"}); err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"isFormSendingElsewhere(node)", "'[formaction]'", "getAttribute.call(form, 'action')", "HTMLButtonElement.prototype, 'form'", "HTMLInputElement.prototype, 'form'", "target.host !== location.host", "location.protocol === 'https:' && target.protocol === 'http:'", "document.baseURI"} {
		if !strings.Contains(script, wanted) {
			test.Errorf("the script lacks %s", wanted)
		}
	}
	if strings.Contains(script, "form.elements") || strings.Contains(script, "form.action") {
		test.Error("the script trusts a property of the form that a field replaces")
	}
}
