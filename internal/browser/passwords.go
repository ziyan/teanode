package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The pieces the browser tool needs to sign in with an item of Passwords:
// where the page is, to put a secret into a field of a page whose host is the
// expected one, and to hold one passkey for a page's relying party.
//
// Nothing here logs, returns or formats the text it is given: an error says
// what failed and never what was being typed.

// passkeyLifetime is how long a passkey stays in the browser when the page
// does not navigate: long enough to click the sign-in button, not long enough
// to be left behind.
var passkeyLifetime = 60 * time.Second

// passkeyLifetimeForTests sets how long a passkey stays and answers the
// previous time. Tests only.
func passkeyLifetimeForTests(lifetime time.Duration) time.Duration {
	previous := passkeyLifetime
	passkeyLifetime = lifetime
	return previous
}

// PageURL is the address of the page as the browser itself reports it, from
// its navigation history, not from a script the page could answer for itself.
func (self *Context) PageURL(ctx context.Context) (string, error) {
	self.touch()
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := self.call(ctx, "Page.getNavigationHistory", nil, &history); err != nil {
		return "", err
	}
	if history.CurrentIndex < 0 || history.CurrentIndex >= len(history.Entries) {
		return "", errors.New("browser: the browser did not say where the page is")
	}
	return history.Entries[history.CurrentIndex].URL, nil
}

// Filled is what a sign-in put into this page: the values to hide from the
// model and whether scripts may run on the page.
func (self *Context) Filled() *FilledValues {
	return &self.filled
}

// KeepFilled remembers values that are about to be put into the page, locks the
// page against scripts, and watches for the page's main frame to move on to
// another document, which unlocks it. Call it before the first value goes in.
func (self *Context) KeepFilled(values ...string) {
	self.filled.Remember("", values...)
	self.mutex.Lock()
	isWatching := self.isWatching
	self.isWatching = true
	self.mutex.Unlock()
	if isWatching || self.filledWatch == nil {
		return
	}
	events, stopListening := self.browser.connection.listen(self.sessionId)
	go func() {
		defer stopListening()
		for {
			select {
			case <-self.filledWatch:
				return
			case event := <-events:
				if event.Method != "Page.frameNavigated" {
					continue
				}
				var navigated struct {
					Frame struct {
						ParentID string `json:"parentId"`
					} `json:"frame"`
				}
				if json.Unmarshal(event.Params, &navigated) == nil && navigated.Frame.ParentID == "" {
					self.filled.Unlock()
				}
			}
		}
	}()
}

// FillSecret puts text into the field named by ref, replacing what is in it,
// the way a person's typing would: the field is focused and the text is
// inserted, so the page sees the same input events. The same script that finds
// the field checks that the page is still on expectedHost (lower case, without
// a leading "www."), so a page that navigated away between the caller's check
// and this call gets nothing. Only a ref from the last snapshot names a field.
func (self *Context) FillSecret(ctx context.Context, ref int, expectedHost, text string) error {
	self.touch()
	if ref <= 0 {
		return errors.New("browser: say which field, by the ref a snapshot gives")
	}
	if text == "" {
		return errors.New("browser: there is nothing to put into the field")
	}
	if expectedHost == "" {
		return errors.New("browser: no site to fill for")
	}
	expected, err := json.Marshal(expectedHost)
	if err != nil {
		return err
	}
	var prepared struct {
		IsFocused bool   `json:"isFocused"`
		Problem   string `json:"problem"`
	}
	script := fmt.Sprintf(`(function () {
  let host = location.hostname.toLowerCase().replace(/\.+$/, '');
  if (host.startsWith('www.') && host.slice(4).includes('.')) host = host.slice(4);
  if (host !== %s) return {isFocused: false, problem: 'the page is no longer on the site this was meant for'};
  const node = %s(%d, '');
  if (!node || !node.isConnected || node.ownerDocument !== document) return {isFocused: false, problem: 'no field has that ref; take a snapshot first'};
  const tag = node.tagName.toLowerCase();
  if (tag !== 'input' && tag !== 'textarea' && !node.isContentEditable) return {isFocused: false, problem: 'that ref is not a field to type into'};
  if (node.disabled || node.readOnly) return {isFocused: false, problem: 'that field cannot be typed into'};
  node.scrollIntoView({block: 'center', inline: 'center'});
  node.focus();
  if (document.activeElement !== node) return {isFocused: false, problem: 'the field would not take focus'};
  if (typeof node.select === 'function') node.select();
  return {isFocused: true, problem: ''};
})()`, expected, element, ref)
	if err := self.evaluate(ctx, script, &prepared); err != nil {
		return errors.New("browser: the field could not be prepared")
	}
	if !prepared.IsFocused {
		return fmt.Errorf("browser: %s", prepared.Problem)
	}
	if err := self.call(ctx, "Input.insertText", map[string]any{"text": text}, nil); err != nil {
		return errors.New("browser: the text could not be put into the field")
	}
	return nil
}

// Passkey is one passkey as a virtual authenticator holds it. The byte
// values are sent to the browser as base64, which is how the protocol takes
// them; PrivateKey is an ES256 key as PKCS#8 DER.
type Passkey struct {
	CredentialID   []byte
	RelyingPartyID string
	UserHandle     []byte
	PrivateKey     []byte
}

// AddPasskey gives the page a virtual authenticator that holds only this
// passkey, answers for the person without asking, and is removed after the
// page's next navigation or after a minute, whichever is first, and when the
// context closes. A second call replaces the first.
func (self *Context) AddPasskey(ctx context.Context, passkey Passkey) error {
	self.touch()
	if len(passkey.CredentialID) == 0 || len(passkey.PrivateKey) == 0 || passkey.RelyingPartyID == "" {
		return errors.New("browser: the passkey is incomplete")
	}
	self.RemovePasskey()
	if err := self.call(ctx, "WebAuthn.enable", map[string]any{"enableUI": false}, nil); err != nil {
		return err
	}
	var added struct {
		AuthenticatorID string `json:"authenticatorId"`
	}
	if err := self.call(ctx, "WebAuthn.addVirtualAuthenticator", map[string]any{"options": map[string]any{
		"protocol":                    "ctap2",
		"transport":                   "internal",
		"hasResidentKey":              true,
		"hasUserVerification":         true,
		"isUserVerified":              true,
		"automaticPresenceSimulation": true,
	}}, &added); err != nil {
		_ = self.call(ctx, "WebAuthn.disable", nil, nil)
		return err
	}
	credential := map[string]any{
		"credentialId":         passkey.CredentialID,
		"isResidentCredential": true,
		"rpId":                 passkey.RelyingPartyID,
		"privateKey":           passkey.PrivateKey,
		"signCount":            0,
	}
	if len(passkey.UserHandle) > 0 {
		credential["userHandle"] = passkey.UserHandle
	}
	if err := self.call(ctx, "WebAuthn.addCredential", map[string]any{"authenticatorId": added.AuthenticatorID, "credential": credential}, nil); err != nil {
		_ = self.call(ctx, "WebAuthn.removeVirtualAuthenticator", map[string]any{"authenticatorId": added.AuthenticatorID}, nil)
		_ = self.call(ctx, "WebAuthn.disable", nil, nil)
		return errors.New("browser: the browser did not take the passkey")
	}
	lifetime := passkeyLifetime
	events, stopListening := self.browser.connection.listen(self.sessionId)
	held := &heldPasskey{done: make(chan struct{}), authenticatorID: added.AuthenticatorID}
	self.mutex.Lock()
	self.heldPasskey = held
	self.mutex.Unlock()
	go func() {
		defer stopListening()
		timer := time.NewTimer(lifetime)
		defer timer.Stop()
		for {
			select {
			case <-held.done:
				return
			case <-timer.C:
				self.removePasskeyOf(held)
				return
			case event := <-events:
				if event.Method != "Page.frameNavigated" {
					continue
				}
				var navigated struct {
					Frame struct {
						ParentID string `json:"parentId"`
					} `json:"frame"`
				}
				if json.Unmarshal(event.Params, &navigated) == nil && navigated.Frame.ParentID == "" {
					self.removePasskeyOf(held)
					return
				}
			}
		}
	}()
	return nil
}

// RemovePasskey takes the virtual authenticator away, if there is one.
func (self *Context) RemovePasskey() {
	self.mutex.Lock()
	held := self.heldPasskey
	self.mutex.Unlock()
	if held != nil {
		self.removePasskeyOf(held)
	}
}

// HasPasskey says whether a virtual authenticator is in the page.
func (self *Context) HasPasskey() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.heldPasskey != nil
}

// heldPasskey is the virtual authenticator a passkey sign-in put in the page.
type heldPasskey struct {
	// done is closed when the authenticator is gone, to end the watch.
	done            chan struct{}
	authenticatorID string
}

// removePasskeyOf removes the authenticator once, however many of the
// navigation, the timer and a newer passkey ask.
func (self *Context) removePasskeyOf(held *heldPasskey) {
	self.mutex.Lock()
	if self.heldPasskey != held {
		self.mutex.Unlock()
		return
	}
	self.heldPasskey = nil
	isClosed := self.closed
	self.mutex.Unlock()
	close(held.done)
	if isClosed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = self.call(ctx, "WebAuthn.removeVirtualAuthenticator", map[string]any{"authenticatorId": held.authenticatorID}, nil)
	_ = self.call(ctx, "WebAuthn.disable", nil, nil)
}
