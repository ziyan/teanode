package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// ScriptedPageWords is what the model is told when a page is not filled
// because the agent ran script in it and the site it is on could not be told,
// here and by the tab target.
const ScriptedPageWords = "This page has run the agent's script, so it is not filled. Sign in before you run any script on a site."

// ScriptedSiteWords is what the model is told when the headless browser does
// not fill a page because the agent ran a script on its site.
const ScriptedSiteWords = "The agent ran a script on this site in this browser, and what a script leaves behind (a window, a worker, a handler on another page of the site) outlives its page, so nothing is filled on this site any more. Use reset to start a fresh browser, then sign in before you run any script."

// ScriptedSiteInTabWords is what the model is told when the person's browser
// does not fill a page because the agent ran a script on its site.
const ScriptedSiteInTabWords = "The agent ran a script on this site in the person's browser, and what a script leaves behind (a window, a worker, a handler on another page of the site) outlives its page, so nothing is filled on this site. The person can turn it back on with \"Allow the agent to fill on these sites again\" in the TeaNode extension's options. Sign in before you run any script."

// ScriptOnEveryPageWords is what the model is told when a tab was given a
// script that runs in every page it loads.
const ScriptOnEveryPageWords = "This tab was given a script that runs in every page it loads, so nothing is filled in it. Open a new tab (open) and fill the sign-in page there before running any script. In the agent's own browser, reset starts a fresh one."

// wordsError is an error that is a sentence for the model: it ends with a full
// stop, which an error string should not, so it is its own type.
type wordsError string

func (self wordsError) Error() string { return string(self) }

// currentDocument is the document the main frame shows now, as the browser says.
func (self *Context) currentDocument(ctx context.Context) (Document, error) {
	var tree struct {
		FrameTree struct {
			Frame struct {
				LoaderID string `json:"loaderId"`
				URL      string `json:"url"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := self.call(ctx, "Page.getFrameTree", nil, &tree); err != nil || tree.FrameTree.Frame.LoaderID == "" {
		return Document{}, errors.New("browser: the browser did not say which page this is")
	}
	return Document{ID: tree.FrameTree.Frame.LoaderID, Address: tree.FrameTree.Frame.URL}, nil
}

// noteScript records that a script is running in the page, or has just run, in
// the document it shows now (see FilledValues.NoteScripted), and on that
// document's site (see FilledValues.NoteScriptedOrigin). It is called before
// the script and again after, because the page can move to another document
// in between: the one the script ran in is the one shown at the start, or at
// the end, or one the page passed through (see noteScriptedNavigations). When
// the browser does not say which document that is, the page is taken to be
// scripted for good: a script that cannot be placed cannot be ruled out later.
func (self *Context) noteScript(ctx context.Context) {
	document, err := self.currentDocument(ctx)
	if err != nil {
		self.filled.NoteScriptOnEveryPage("")
		return
	}
	self.filled.NoteScripted("", document)
	self.filled.NoteScriptedOrigin(document.Address)
	self.noteScriptedTargets(ctx)
}

// noteScriptedNavigations marks every main frame document the page committed
// since the events were first listened to: a script that waits for a promise
// can run through several.
func (self *Context) noteScriptedNavigations(events <-chan *message) {
	for {
		select {
		case event := <-events:
			if event.Method != "Page.frameNavigated" {
				continue
			}
			var navigated struct {
				Frame struct {
					ParentID string `json:"parentId"`
					LoaderID string `json:"loaderId"`
					URL      string `json:"url"`
				} `json:"frame"`
			}
			if json.Unmarshal(event.Params, &navigated) != nil || navigated.Frame.ParentID != "" {
				continue
			}
			self.filled.NoteScripted("", Document{ID: navigated.Frame.LoaderID, Address: navigated.Frame.URL})
			self.filled.NoteScriptedOrigin(navigated.Frame.URL)
		default:
			return
		}
	}
}

// pageTarget is one target of the browser context: a page or a window the
// page's script opened, and the like.
type pageTarget struct {
	TargetID         string `json:"targetId"`
	Type             string `json:"type"`
	URL              string `json:"url"`
	BrowserContextID string `json:"browserContextId"`
}

// otherTargets are the targets of this page's browser context besides the
// page itself: windows a script opened, workers, frames of other processes.
func (self *Context) otherTargets(ctx context.Context) ([]pageTarget, error) {
	var listed struct {
		TargetInfos []pageTarget `json:"targetInfos"`
	}
	if err := self.browser.connection.call(ctx, "", "Target.getTargets", nil, &listed); err != nil {
		return nil, err
	}
	self.mutex.Lock()
	contextID, targetID := self.contextId, self.targetId
	self.mutex.Unlock()
	var others []pageTarget
	for _, target := range listed.TargetInfos {
		if target.BrowserContextID == contextID && target.TargetID != targetID {
			others = append(others, target)
		}
	}
	return others, nil
}

// isWindow says a target is a page or a window of the web, which a script can
// open and the browser can close; the browser's own interface targets (an
// address bar's pop-up) and workers are not.
func (self pageTarget) isWindow() bool {
	return self.Type == "page"
}

// noteScriptedTargets marks the site of every other target of the browser
// context: a window the script opened is the script's too. When the browser
// does not list them the page is taken to be scripted for good.
func (self *Context) noteScriptedTargets(ctx context.Context) {
	others, err := self.otherTargets(ctx)
	if err != nil {
		self.filled.NoteScriptOnEveryPage("")
		return
	}
	for _, target := range others {
		self.filled.NoteScriptedOrigin(target.URL)
	}
}

// closeOtherTargets closes every target of the browser context but the page,
// so that no window a script opened is alive while a secret is typed. It
// fails when the browser does not say which targets there are or one stays.
func (self *Context) closeOtherTargets(ctx context.Context) error {
	others, err := self.otherTargets(ctx)
	if err != nil {
		return errors.New("browser: the browser did not say which windows are open")
	}
	for _, target := range others {
		if !target.isWindow() {
			continue
		}
		if err := self.browser.connection.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": target.TargetID}, nil); err != nil {
			return errors.New("browser: another window is open and could not be closed")
		}
	}
	return nil
}

// CheckNotScripted says the page can be filled: the agent has run no script on
// the site it shows now, and none in the document. It also closes every
// window but the page, which a script may have opened (a window outlives the
// document that opened it and can reach back into the site's pages). The error
// is the words for the model. The browser is asked which document the page
// shows only if a script was ever run, and when it does not answer the page is
// refused.
func (self *Context) CheckNotScripted(ctx context.Context) error {
	if self.filled.HasScriptOnEveryPage("") {
		return wordsError(ScriptOnEveryPageWords)
	}
	if self.filled.HasBeenScripted("") {
		document, err := self.currentDocument(ctx)
		if err != nil {
			return err
		}
		if self.filled.IsOriginScripted(document.Address) {
			return wordsError(ScriptedSiteWords)
		}
		if self.filled.IsScripted("", document.ID) {
			return wordsError(ScriptedPageWords)
		}
	}
	return self.closeOtherTargets(ctx)
}

// Filled is what a sign-in put into this page: the values to hide from the
// model and whether the page is locked.
func (self *Context) Filled() *FilledValues {
	return &self.filled
}

// historyEntries is the page's navigation history as the browser keeps it.
type historyEntries struct {
	currentID    int
	currentIndex int
	ids          []int
	address      string
}

// history reads the navigation history, which a script in the page cannot
// change.
func (self *Context) history(ctx context.Context) (*historyEntries, error) {
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := self.call(ctx, "Page.getNavigationHistory", nil, &history); err != nil {
		return nil, err
	}
	if history.CurrentIndex < 0 || history.CurrentIndex >= len(history.Entries) {
		return nil, errors.New("browser: the browser did not say where the page is")
	}
	entries := &historyEntries{currentID: history.Entries[history.CurrentIndex].ID, currentIndex: history.CurrentIndex, address: history.Entries[history.CurrentIndex].URL}
	for _, entry := range history.Entries {
		entries.ids = append(entries.ids, entry.ID)
	}
	return entries, nil
}

// PrepareToFill is called before a value goes into the page. It stops the
// browser from keeping what the page asks of the network (a request's body is
// where a form's password goes), locks the page against what could read a
// field back (see FilledValues), and watches the page's main frame so that
// the lock ends only when it commits a new document. The values themselves
// are given to Filled().Remember, before this.
func (self *Context) PrepareToFill(ctx context.Context) {
	_ = self.call(ctx, "Network.disable", nil, nil)
	document := Document{}
	var tree struct {
		FrameTree struct {
			Frame struct {
				LoaderID string `json:"loaderId"`
				URL      string `json:"url"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := self.call(ctx, "Page.getFrameTree", nil, &tree); err == nil {
		document.ID, document.Address = tree.FrameTree.Frame.LoaderID, tree.FrameTree.Frame.URL
	}
	entries, entriesErr := self.history(ctx)

	self.mutex.Lock()
	if entriesErr == nil {
		self.currentEntryID = entries.currentID
		if self.knownEntryIDs == nil {
			self.knownEntryIDs = map[int]bool{}
		}
		for _, identifier := range entries.ids {
			self.knownEntryIDs[identifier] = true
		}
		if document.Address == "" {
			document.Address = entries.address
		}
	}
	document.CommitCount = self.commitCount
	isWatching := self.isWatching
	self.isWatching = true
	watch := self.filledWatch
	sessionId := self.sessionId
	self.mutex.Unlock()

	self.filled.Lock("", document)
	if isWatching || watch == nil {
		return
	}
	events, stopListening := self.browser.connection.listen(sessionId)
	go func() {
		defer stopListening()
		for {
			select {
			case <-watch:
				return
			case event := <-events:
				if event.Method != "Page.frameNavigated" {
					continue
				}
				var navigated struct {
					Frame struct {
						ParentID string `json:"parentId"`
						LoaderID string `json:"loaderId"`
						URL      string `json:"url"`
					} `json:"frame"`
				}
				if json.Unmarshal(event.Params, &navigated) != nil || navigated.Frame.ParentID != "" {
					continue
				}
				self.committed(navigated.Frame.LoaderID, navigated.Frame.URL, watch)
			}
		}
	}()
}

// CheckCurrentDocument asks the browser which document the page shows right
// now and locks the page if a value was put into that document. Going back can
// restore the filled document from the history's cache a moment before the
// event that reports it reaches the lock (and before the server's own history
// read finishes), so a command that could read a field back asks first
// instead of trusting the events alone. It does nothing for a page nothing
// was filled into. The error says the browser did not answer; the caller then
// refuses the command rather than guess.
func (self *Context) CheckCurrentDocument(ctx context.Context) error {
	if !self.filled.HasFilledDocument("") {
		return nil
	}
	var tree struct {
		FrameTree struct {
			Frame struct {
				LoaderID string `json:"loaderId"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := self.call(ctx, "Page.getFrameTree", nil, &tree); err != nil {
		return errors.New("browser: the browser did not say which page this is")
	}
	self.filled.NoteCurrentDocument("", tree.FrameTree.Frame.LoaderID)
	return nil
}

// historyAfterCommitWait is how long a commit waits for the navigation history
// to be readable: while a page swaps in the browser answers "Not attached to an
// active page", and answers properly a moment later.
var historyAfterCommitWait = 3 * time.Second

// readHistoryAfterCommit reads the navigation history, trying again for a short
// while when the browser says the page is not attached yet.
func (self *Context) readHistoryAfterCommit(stop <-chan struct{}) (*historyEntries, error) {
	wait := historyAfterCommitWait
	if self.historyWait > 0 {
		wait = self.historyWait
	}
	deadline := time.Now().Add(wait)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		entries, err := self.history(ctx)
		cancel()
		if err == nil || time.Now().After(deadline) {
			return entries, err
		}
		select {
		case <-stop:
			return nil, err
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// committed is the page's main frame committing a document. Whether it is a
// move through the history is told by the navigation history: a new page
// makes a new entry, and back and forward land on an entry that was already
// there. When the history cannot be read the commit is taken to be a move
// through it, which leaves the lock as it is.
func (self *Context) committed(loaderID, address string, stop <-chan struct{}) {
	entries, err := self.readHistoryAfterCommit(stop)
	isBackForward := true
	self.mutex.Lock()
	self.commitCount++
	count := self.commitCount
	if err == nil {
		isBackForward = self.knownEntryIDs[entries.currentID] && entries.currentID != self.currentEntryID
		self.currentEntryID = entries.currentID
		if self.knownEntryIDs == nil {
			self.knownEntryIDs = map[int]bool{}
		}
		for _, identifier := range entries.ids {
			self.knownEntryIDs[identifier] = true
		}
	}
	self.mutex.Unlock()
	self.filled.Committed("", Commit{DocumentID: loaderID, Address: address, IsBackForward: isBackForward, Count: count})
}

// FieldKind is what a field is to hold, which the field must be able to hold.
type FieldKind string

const (
	// FieldKindUserName is a text, email or telephone input, or one whose
	// autocomplete says username or email.
	FieldKindUserName FieldKind = "userName"
	// FieldKindPassword is a password input, or one whose autocomplete says
	// current-password or new-password.
	FieldKindPassword FieldKind = "password"
	// FieldKindOneTimeCode is a text, number or telephone input, or one whose
	// autocomplete says one-time-code.
	FieldKindOneTimeCode FieldKind = "oneTimeCode"
)

// FillRequest is one value to put into one field.
type FillRequest struct {
	// Ref is the field, by the ref of the last snapshot.
	Ref int
	// Host is the host the page must be on (lower case, without a leading
	// "www."), and Port the port, which is checked when it is not zero.
	Host string
	Port int
	// Kind is what the field must be able to hold.
	Kind FieldKind
	// Text is the value.
	Text string
}

// fillScript is the whole of a fill, done in the page in one step: the page's
// host and port are checked, the field is found by its ref and checked to be
// the kind of field asked for, visible and enabled, and the value is put in
// the way the page's own typing would (the browser's own setter of the input's
// value, then input and change events), so there is no gap between the check
// and the value going in. A field whose form sends to another site than the
// page is not filled either. A password or a code is also noted as filled by the
// agent, so that a snapshot never shows it, whatever the field's type becomes.
const fillScript = `(function (host, port, ref, kind, text, isSecret) {
  let pageHost = location.hostname.toLowerCase().replace(/\.+$/, '');
  if (pageHost.startsWith('www.') && pageHost.slice(4).includes('.')) pageHost = pageHost.slice(4);
  const pagePort = Number(location.port || (location.protocol === 'https:' ? 443 : 80));
  if (pageHost !== host || (port !== 0 && pagePort !== port)) return {isFilled: false, problem: 'the page is no longer on the site this was meant for'};
  // Whether the form the field is in sends to another site than this page:
  // the form's action and the formaction of every control that submits it,
  // including an image input and a control outside the form that names it
  // with form=. The attributes are read with the browser's own getters, not
  // from the properties of the form, which a field named "action" or
  // "elements" replaces. An address that does not resolve to http or https is
  // not a place to send to, and a secure page sending to a plain-text address
  // counts as sending elsewhere.
  const isFormSendingElsewhere = (field) => {
    const getAttribute = Element.prototype.getAttribute;
    const inputForm = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'form').get;
    const buttonForm = Object.getOwnPropertyDescriptor(HTMLButtonElement.prototype, 'form').get;
    const form = inputForm.call(field);
    if (!form) return false;
    const attributes = [getAttribute.call(form, 'action')];
    for (const control of Array.from(Document.prototype.querySelectorAll.call(document, '[formaction]'))) {
      let owner = null;
      if (control instanceof HTMLButtonElement) owner = buttonForm.call(control);
      else if (control instanceof HTMLInputElement) owner = inputForm.call(control);
      if (owner === form) attributes.push(getAttribute.call(control, 'formaction'));
    }
    return attributes.some((attribute) => {
      if (attribute === null || attribute.trim() === '') return false;
      try {
        const target = new URL(attribute, document.baseURI);
        if (target.protocol !== 'http:' && target.protocol !== 'https:') return false;
        return target.host !== location.host || (location.protocol === 'https:' && target.protocol === 'http:');
      } catch (error) {
        return false;
      }
    });
  };
  const node = %s(ref, '');
  if (!node || !node.isConnected || node.ownerDocument !== document) return {isFilled: false, problem: 'no field has that ref; take a snapshot first'};
  if (!(node instanceof HTMLInputElement)) return {isFilled: false, problem: 'that ref is not an input; a textarea, a select and an editable region are never filled'};
  const type = (node.type || 'text').toLowerCase();
  const tokens = (node.getAttribute('autocomplete') || '').toLowerCase().split(/\s+/);
  const has = (...names) => names.some((name) => tokens.includes(name));
  const isTextual = ['text', 'email', 'tel', 'number', 'search', 'url', 'password'].includes(type);
  let isRightKind = false;
  let wanted = '';
  if (kind === 'password') {
    isRightKind = type === 'password' || (isTextual && has('current-password', 'new-password'));
    wanted = 'a password input, or one whose autocomplete says current-password or new-password';
  } else if (kind === 'userName') {
    isRightKind = ['text', 'email', 'tel'].includes(type) || (isTextual && type !== 'password' && has('username', 'email'));
    wanted = 'a text, email or telephone input, or one whose autocomplete says username or email';
  } else if (kind === 'oneTimeCode') {
    isRightKind = ['text', 'number', 'tel'].includes(type) || (isTextual && has('one-time-code'));
    wanted = 'a text, number or telephone input, or one whose autocomplete says one-time-code';
  }
  if (!isRightKind) return {isFilled: false, problem: 'that field is not ' + wanted};
  if (node.disabled || node.readOnly || type === 'hidden') return {isFilled: false, problem: 'that field cannot be typed into'};
  let opacity = 1;
  for (let ancestor = node; ancestor; ancestor = ancestor.parentElement) {
    const style = getComputedStyle(ancestor);
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return {isFilled: false, problem: 'that field cannot be typed into'};
    opacity *= Number.parseFloat(style.opacity === '' ? '1' : style.opacity);
  }
  const rect = node.getBoundingClientRect();
  if (opacity < 0.1 || rect.width < 5 || rect.height < 5) return {isFilled: false, problem: 'that field cannot be typed into'};
  if (isFormSendingElsewhere(node)) return {isFilled: false, problem: 'the form around that field sends to another site than this page'};
  if (isSecret) {
    if (!window.__teanodeFilled) window.__teanodeFilled = new WeakSet();
    window.__teanodeFilled.add(node);
  }
  node.scrollIntoView({block: 'center', inline: 'center'});
  node.focus();
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
  setter.call(node, text);
  node.dispatchEvent(new Event('input', {bubbles: true}));
  node.dispatchEvent(new Event('change', {bubbles: true}));
  return {isFilled: true, problem: ''};
})`

// FillSecret puts text into the field named by the request, replacing what is
// in it, in one step in the page (see fillScript). Only a ref from the last
// snapshot names a field, and only a field of the kind asked for takes the
// text. The error says what was wrong and never repeats the text.
func (self *Context) FillSecret(ctx context.Context, request FillRequest) error {
	self.touch()
	if request.Ref <= 0 {
		return errors.New("browser: say which field, by the ref a snapshot gives")
	}
	if request.Text == "" {
		return errors.New("browser: there is nothing to put into the field")
	}
	if request.Host == "" {
		return errors.New("browser: no site to fill for")
	}
	switch request.Kind {
	case FieldKindUserName, FieldKindPassword, FieldKindOneTimeCode:
	default:
		return errors.New("browser: say what kind of field this is")
	}
	// A page the agent's script ran in could send the value anywhere.
	if err := self.CheckNotScripted(ctx); err != nil {
		return err
	}
	arguments := make([]string, 0, 6)
	for _, argument := range []any{request.Host, request.Port, request.Ref, string(request.Kind), request.Text, request.Kind != FieldKindUserName} {
		encoded, err := json.Marshal(argument)
		if err != nil {
			return errors.New("browser: the field could not be prepared")
		}
		arguments = append(arguments, string(encoded))
	}
	script := fmt.Sprintf(fillScript, element) + "(" + strings.Join(arguments, ", ") + ")"
	var filled struct {
		IsFilled bool   `json:"isFilled"`
		Problem  string `json:"problem"`
	}
	// An error from the page is not passed on: it could quote the script.
	if err := self.evaluate(ctx, script, &filled); err != nil {
		return errors.New("browser: the field could not be filled")
	}
	if !filled.IsFilled {
		return fmt.Errorf("browser: %s", filled.Problem)
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
	// A page the agent's script ran in could wrap the sign-in and send what the
	// authenticator signs anywhere.
	if err := self.CheckNotScripted(ctx); err != nil {
		return err
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
	events, stopListening := self.browser.connection.listen(self.session())
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
