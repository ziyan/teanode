package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	passwordstool "github.com/ziyan/teanode/internal/agent/tools/passwords"
	devtools "github.com/ziyan/teanode/internal/browser"
	"github.com/ziyan/teanode/internal/passwords"
)

// Signing in with Passwords: fill_login, fill_one_time_code and
// passkey_sign_in. The model names an item by its id and the fields by the
// refs of a snapshot. It never sees a value: the password, the code and the
// passkey go from the item straight into the page, and what comes back says
// only what was filled.
//
// Three things stand between an item and a page, and none of them is the
// model's word:
//
//   - the item must be one the person let the agent use, a login of their own;
//   - the page's origin is read from the browser (the headless page's
//     navigation history, or the address the person's tab reported), never
//     from the arguments, and must be one of the item's sites, host for host
//     and port for port ("example.com" is not "example.com.evil.test", and a
//     site without a port is not the same site on port 8443);
//   - the page must be https (or a loopback address), so that a secret does
//     not cross the network in the clear.
//
// The use is recorded on the item, with the host, before a secret is put
// anywhere.

// browserPasswordActions are the actions that sign in with an item of
// Passwords.
var browserPasswordActions = map[string]bool{"fill_login": true, "fill_one_time_code": true, "passkey_sign_in": true}

const (
	// passkeyOnlyHeadlessWords is the answer to passkey_sign_in on the
	// person's own tab.
	passkeyOnlyHeadlessWords = "Passkey sign-in with the agent works only in the agent's own browser (target headless)"

	// oldExtensionWords is the answer when the person's extension does not fill
	// passwords yet.
	oldExtensionWords = "The TeaNode extension in that browser needs updating to fill passwords"
)

// refusal is the model's answer to an action that is not done, in a sentence of
// its own rather than a fragment of an error.
func refusal(words string) error {
	return errors.New(words)
}

// scrubbed hides what the agent's sign-in put into the page from an action's
// answer and from its error, so that a snapshot, a script's result or an
// extension's reply that repeats a value says [hidden]. The answer's words and
// note are scrubbed; a picture is not text.
func scrubbed(filled *devtools.FilledValues, result *tools.Result, err error) (*tools.Result, error) {
	if err != nil {
		if hidden := filled.Scrub(err.Error()); hidden != err.Error() {
			return result, errors.New(hidden)
		}
		return result, err
	}
	if result != nil {
		result.Content = filled.Scrub(result.Content)
		result.Note = filled.Scrub(result.Note)
	}
	return result, nil
}

// signInSecrets are the values of one sign-in. They are kept apart so that an
// error that echoes one can be cleaned before the model reads it.
type signInSecrets []string

// hide replaces every secret in the error's words, in case the extension or
// the browser repeated one.
func (self signInSecrets) hide(err error) error {
	if err == nil {
		return nil
	}
	words := err.Error()
	for _, secret := range self {
		if secret != "" {
			words = strings.ReplaceAll(words, secret, "[hidden]")
		}
	}
	return errors.New(words)
}

// pageHostOf is the host a page's address is for, after the checks that stand
// before any secret: there is an address, and it is https (or loopback).
func pageHostOf(pageURL string) (string, error) {
	host := passwords.NormalizedHost(pageURL)
	if host == "" {
		return "", errors.New("the page has no site to sign in to; open the site's sign-in page first")
	}
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return "", errors.New("the page's address cannot be read")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		hostname := strings.ToLower(parsed.Hostname())
		if hostname != "localhost" && hostname != "127.0.0.1" && hostname != "::1" {
			return "", fmt.Errorf("%s is not an https page, and a password is not typed into a page that is not; nothing was filled", host)
		}
	default:
		return "", errors.New("the page is not a web page to sign in to; open the site's sign-in page first")
	}
	return host, nil
}

// signInTarget is what a sign-in checked before it filled anything.
type signInTarget struct {
	login *passwords.LoginFields
	host  string
	// port is the port the page is on.
	port int
}

// prepareSignIn opens the item and checks the page's origin against its sites.
// pageURL is what the browser said, and is read after the item is checked so
// that a refused item costs the page nothing. When shouldMatchSites is false
// the caller checks the host another way (a passkey's relying party).
func prepareSignIn(ctx context.Context, run tools.Run, itemId, pageURL string, shouldMatchSites bool) (*signInTarget, error) {
	login, err := passwordstool.OpenLoginToSignIn(ctx, run, itemId)
	if err != nil {
		return nil, err
	}
	host, err := pageHostOf(pageURL)
	if err != nil {
		return nil, err
	}
	if shouldMatchSites && !passwords.IsOriginOfSites(login.Sites, pageURL) {
		return nil, fmt.Errorf("the page is on %s, which is not one of that login's sites (the same host and the same port), so nothing was filled; a page that asks for this login anywhere else is not the site it is for", host)
	}
	return &signInTarget{login: login, host: host, port: passwords.EffectivePort(pageURL)}, nil
}

// loginName is how the result names the item.
func loginName(login *passwords.LoginFields) string {
	if name := strings.TrimSpace(login.Name); name != "" {
		return name
	}
	return "the login"
}

// codeOf is the current one-time code of a login, and how long a code of that
// login lasts. The address holds the secret, so no error from reading it is
// passed on.
func codeOf(login *passwords.LoginFields) (string, time.Duration, error) {
	if strings.TrimSpace(login.OneTimeCodeURI) == "" {
		return "", 0, errors.New("that login has no one-time code saved")
	}
	oneTimeCode, err := passwords.ParseOneTimeCodeURI(login.OneTimeCodeURI)
	if err != nil {
		return "", 0, errors.New("the one-time code saved on that login cannot be read")
	}
	code, err := oneTimeCode.CodeAt(time.Now())
	if err != nil {
		return "", 0, errors.New("the one-time code saved on that login cannot be made")
	}
	return code, time.Duration(oneTimeCode.PeriodSeconds) * time.Second, nil
}

// whatIsFilled says which of the login's values go into the page, by the
// refs given, and refuses a value the login does not have.
func (self *signInTarget) whatIsFilled(arguments *browserArguments) (isUserName, isPassword bool, err error) {
	isUserName, isPassword = arguments.UserNameRef > 0, arguments.PasswordRef > 0
	if !isUserName && !isPassword {
		return false, false, errors.New("say which fields, by the refs of a snapshot: user_name_ref for the user name, password_ref for the password")
	}
	if isUserName && self.login.UserName == "" {
		return false, false, errors.New("that login has no user name saved, so leave user_name_ref out")
	}
	if isPassword && self.login.Password == "" {
		return false, false, errors.New("that login has no password saved, so leave password_ref out")
	}
	return isUserName, isPassword, nil
}

// filledWords is the whole answer to a fill: what was filled, where, from
// which item, never a value.
func filledWords(what, host string, login *passwords.LoginFields) *tools.Result {
	return tools.TextResult("Filled the %s for %s from %s", what, host, loginName(login))
}

// userNameAndPassword says "user name", "password" or "user name and
// password".
func userNameAndPassword(isUserName, isPassword bool) string {
	switch {
	case isUserName && isPassword:
		return "user name and password"
	case isUserName:
		return "user name"
	}
	return "password"
}

// runPasswordAction is a sign-in action in the headless browser.
func runPasswordAction(ctx context.Context, run tools.Run, page *devtools.Context, arguments *browserArguments) (*tools.Result, error) {
	pageURL, err := page.PageURL(ctx)
	if err != nil {
		return nil, err
	}
	// A page the agent ran script in is not filled: the script may have left
	// a listener or changed a form that sends what is typed elsewhere.
	if err := page.CheckNotScripted(ctx); err != nil {
		return nil, refusal(err.Error())
	}
	switch arguments.Action {
	case "fill_login":
		target, err := prepareSignIn(ctx, run, arguments.ItemID, pageURL, true)
		if err != nil {
			return nil, err
		}
		isUserName, isPassword, err := target.whatIsFilled(arguments)
		if err != nil {
			return nil, err
		}
		secrets := signInSecrets{target.login.UserName, target.login.Password}
		if err := passwordstool.RecordSignIn(ctx, run, arguments.ItemID, target.host); err != nil {
			return nil, err
		}
		// The password is hidden and the page locked, with the network log
		// off, before the first value goes in.
		if isPassword {
			page.Filled().Remember(target.login.Password)
			page.Filled().RememberSecretHost("", target.host)
			page.PrepareToFill(ctx)
		}
		if isUserName {
			if err := page.FillSecret(ctx, devtools.FillRequest{Ref: arguments.UserNameRef, Host: target.host, Port: target.port, Kind: devtools.FieldKindUserName, Text: target.login.UserName}); err != nil {
				return nil, secrets.hide(fmt.Errorf("the user name was not filled: %w", err))
			}
		}
		if isPassword {
			if err := page.FillSecret(ctx, devtools.FillRequest{Ref: arguments.PasswordRef, Host: target.host, Port: target.port, Kind: devtools.FieldKindPassword, Text: target.login.Password}); err != nil {
				return nil, secrets.hide(fmt.Errorf("the password was not filled: %w", err))
			}
		}
		return filledWords(userNameAndPassword(isUserName, isPassword), target.host, target.login), nil
	case "fill_one_time_code":
		target, err := prepareSignIn(ctx, run, arguments.ItemID, pageURL, true)
		if err != nil {
			return nil, err
		}
		if arguments.Ref <= 0 {
			return nil, errors.New("say which field, by the ref of a snapshot")
		}
		code, period, err := codeOf(target.login)
		if err != nil {
			return nil, err
		}
		if err := passwordstool.RecordSignIn(ctx, run, arguments.ItemID, target.host); err != nil {
			return nil, err
		}
		page.Filled().RememberOneTimeCode(period, code)
		page.Filled().RememberSecretHost("", target.host)
		page.PrepareToFill(ctx)
		if err := page.FillSecret(ctx, devtools.FillRequest{Ref: arguments.Ref, Host: target.host, Port: target.port, Kind: devtools.FieldKindOneTimeCode, Text: code}); err != nil {
			return nil, signInSecrets{code}.hide(fmt.Errorf("the one-time code was not filled: %w", err))
		}
		return filledWords("one-time code", target.host, target.login), nil
	case "passkey_sign_in":
		return runPasskeySignIn(ctx, run, page, arguments, pageURL)
	}
	return nil, fmt.Errorf("%q is not a way to sign in", arguments.Action)
}

// runPasskeySignIn gives the headless page a virtual authenticator that holds
// the item's passkey for the page's relying party. The agent then clicks the
// site's sign-in button with the usual actions; the authenticator is removed
// after the page's next navigation or after a minute.
func runPasskeySignIn(ctx context.Context, run tools.Run, page *devtools.Context, arguments *browserArguments, pageURL string) (*tools.Result, error) {
	// A passkey is bound to its relying party by the browser itself, so the
	// rule here is the relying party's, not the item's list of sites.
	target, err := prepareSignIn(ctx, run, arguments.ItemID, pageURL, false)
	if err != nil {
		return nil, err
	}
	if len(target.login.Passkeys) == 0 {
		return nil, errors.New("that login has no passkey saved")
	}
	var chosen *passwords.LoginPasskey
	for index := range target.login.Passkeys {
		candidate := &target.login.Passkeys[index]
		if !passwords.IsRelyingPartyOfHost(candidate.RelyingPartyID, pageURL) {
			continue
		}
		// The newest passkey for this page: an RFC 3339 time sorts as text.
		if chosen == nil || candidate.CreatedAt > chosen.CreatedAt {
			chosen = candidate
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("that login has no passkey for %s, so nothing was set up", target.host)
	}
	if err := passwordstool.RecordSignIn(ctx, run, arguments.ItemID, target.host); err != nil {
		return nil, err
	}
	if err := page.AddPasskey(ctx, devtools.Passkey{
		CredentialID: chosen.CredentialID, RelyingPartyID: chosen.RelyingPartyID, UserHandle: chosen.UserHandle, PrivateKey: chosen.PrivateKey,
	}); err != nil {
		return nil, signInSecrets{string(chosen.PrivateKey)}.hide(fmt.Errorf("the passkey could not be set up: %w", err))
	}
	return tools.TextResult("Ready to sign in to %s with the passkey from %s. Click the site's sign-in button now (or the passkey option on its sign-in page) and the page signs in; the passkey is taken away after the page's next navigation, or in a minute.", target.host, loginName(target.login)), nil
}

// tabFillLoginArguments are what the extension is sent to fill a login in the
// person's tab: the server builds them, the host so that the extension can
// check it against the tab again, the values so that they never pass through
// the model. A value is empty when its ref is.
type tabFillLoginArguments struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	ExpectedTab string `json:"expectedTab,omitempty"`
	UserNameRef int    `json:"userNameRef"`
	PasswordRef int    `json:"passwordRef"`
	UserName    string `json:"userName"`
	Password    string `json:"password"`
}

// tabFillOneTimeCodeArguments are what the extension is sent to fill a code.
type tabFillOneTimeCodeArguments struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	ExpectedTab string `json:"expectedTab,omitempty"`
	Ref         int    `json:"ref"`
	Code        string `json:"code"`
}

// runPasswordActionOnTab is a sign-in action in the person's own tab. The
// caller has already checked that the person has not taken the tab over and
// that a tab is there.
func runPasswordActionOnTab(ctx context.Context, run tools.Run, tab tools.Tab, arguments *browserArguments) (*tools.Result, error) {
	if arguments.Action == "passkey_sign_in" {
		return nil, refusal(passkeyOnlyHeadlessWords)
	}
	if !tab.CanFillPasswords() {
		return nil, refusal(oldExtensionWords)
	}
	if err := refuseScriptedTab(tab.Filled(), tab.CurrentTabID()); err != nil {
		return nil, err
	}
	target, err := prepareSignIn(ctx, run, arguments.ItemID, tab.URL(), true)
	if err != nil {
		return nil, err
	}
	switch arguments.Action {
	case "fill_login":
		isUserName, isPassword, err := target.whatIsFilled(arguments)
		if err != nil {
			return nil, err
		}
		tabKey := tab.CurrentTabID()
		sent := tabFillLoginArguments{Host: target.host, Port: target.port, ExpectedTab: tabKey, UserNameRef: arguments.UserNameRef, PasswordRef: arguments.PasswordRef}
		if isUserName {
			sent.UserName = target.login.UserName
		} else {
			sent.UserNameRef = 0
		}
		if isPassword {
			sent.Password = target.login.Password
		} else {
			sent.PasswordRef = 0
		}
		// The password is hidden and the tab locked before the extension is
		// asked; the answer says which document it went into.
		if isPassword {
			tab.Filled().Remember(target.login.Password)
			tab.Filled().RememberSecretHost(tabKey, target.host)
			tab.Filled().Lock(tabKey, devtools.Document{Address: tab.URL()})
		}
		if err := askTabToFill(ctx, run, tab, arguments, target, sent, signInSecrets{sent.UserName, sent.Password}, isPassword); err != nil {
			return nil, err
		}
		return filledWords(userNameAndPassword(isUserName, isPassword), target.host, target.login), nil
	case "fill_one_time_code":
		if arguments.Ref <= 0 {
			return nil, errors.New("say which field, by the ref of a snapshot")
		}
		code, period, err := codeOf(target.login)
		if err != nil {
			return nil, err
		}
		tabKey := tab.CurrentTabID()
		sent := tabFillOneTimeCodeArguments{Host: target.host, Port: target.port, ExpectedTab: tabKey, Ref: arguments.Ref, Code: code}
		tab.Filled().RememberOneTimeCode(period, code)
		tab.Filled().RememberSecretHost(tabKey, target.host)
		tab.Filled().Lock(tabKey, devtools.Document{Address: tab.URL()})
		if err := askTabToFill(ctx, run, tab, arguments, target, sent, signInSecrets{code}, true); err != nil {
			return nil, err
		}
		return filledWords("one-time code", target.host, target.login), nil
	}
	return nil, fmt.Errorf("%q is not a way to sign in", arguments.Action)
}

// askTabToFill records the use, sends the extension the arguments and checks
// that it says it filled. Whatever it answers, no value is passed on. When a
// secret went in, the answer's tab and document are what the lock is kept for
// from then on (the tab was locked as soon as the extension was asked, in case
// the answer never comes).
func askTabToFill(ctx context.Context, run tools.Run, tab tools.Tab, arguments *browserArguments, target *signInTarget, sent any, secrets signInSecrets, isSecret bool) error {
	if err := passwordstool.RecordSignIn(ctx, run, arguments.ItemID, target.host); err != nil {
		return err
	}
	data, err := askTab(ctx, run, tab, arguments.Action, sent)
	if err != nil {
		return secrets.hide(err)
	}
	var answer struct {
		IsFilled    bool   `json:"isFilled"`
		Tab         *int64 `json:"tab"`
		DocumentID  string `json:"documentId"`
		CommitCount int64  `json:"commitCount"`
	}
	if json.Unmarshal(data, &answer) != nil || !answer.IsFilled {
		return errors.New("the TeaNode extension did not fill the fields; take a snapshot to see the page and try again")
	}
	if isSecret {
		tabKey := tab.CurrentTabID()
		if answer.Tab != nil {
			tabKey = strconv.FormatInt(*answer.Tab, 10)
			tab.NoteCurrentTab(tabKey)
		}
		tab.Filled().RememberSecretHost(tabKey, target.host)
		tab.Filled().Lock(tabKey, devtools.Document{ID: answer.DocumentID, Address: tab.URL(), CommitCount: answer.CommitCount})
	}
	return nil
}
