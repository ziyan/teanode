package browser

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// FilledValues is what the agent's sign-in put into pages and must not read
// back: passwords and one-time codes, kept in memory only and never written
// anywhere, together with which pages (tabs) are locked because of it.
//
// It does two things for the browser tool.
//
// The lock. A tab that was filled is locked: the tool does not run a script on
// it, take a picture of it, read what it asks of the network or send it a raw
// DevTools command that could read a field, because each of those is a way to
// ask a page for the value of a field. The lock is kept for the tab and for
// its document, not for the tool as a whole:
//
//   - another tab is not locked by it, and switching to or from a tab does not
//     change it;
//   - a change that keeps the document (history.pushState, a fragment) does not
//     end it;
//   - it ends when the tab commits a new document that is not a move through
//     the history (back or forward);
//   - a move through the history that returns to the filled document, or to
//     an address that was filled, locks the tab again.
//
// The scrubbing. For as long as the values are remembered, every result and
// error of the tool has them replaced, in the ways a page or a network log
// writes a value (see writtenForms). Values stay remembered after the page
// moved on, so what an old page left in the results is still hidden. A
// one-time code is forgotten a little after its period ends: it opens
// nothing by then.
//
// The zero value holds nothing and is ready to use; a nil one is safe to ask.
type FilledValues struct {
	mutex  sync.Mutex
	values []filledValue
	tabs   map[string]*filledTab
	// scriptedOrigins are the sites (see OriginOf) the agent ran a script on.
	// A script leaves things behind that outlive its document: a window it
	// opened, a worker, a cookie, a handler another page of the site runs. So
	// the mark is the site's, not the document's, and it lasts as long as
	// this value does (see NoteScriptedOrigin).
	scriptedOrigins map[string]bool
	// now is the clock, for tests.
	now func() time.Time
}

// filledValue is one remembered value and, for a one-time code, when it is
// forgotten.
type filledValue struct {
	text string
	// forgetAt is zero for a value kept for as long as FilledValues lives.
	forgetAt time.Time
}

// filledTab is what is known about the documents of one tab.
type filledTab struct {
	isLocked bool
	// documents are the identifiers of the documents a value was put into.
	documents map[string]bool
	// addresses are the addresses of those documents, without a fragment.
	addresses map[string]bool
	// lastCount is the number of the latest commit counted for the tab, so
	// that a commit report that arrives late, after a newer one, is ignored.
	lastCount int64
	// latest is the newest commit seen, for a fill that is reported after
	// the tab already moved on.
	latest *Commit

	// isScripted says the agent ran a script in the document the tab shows,
	// or changed it with a DevTools command, so that a listener or a form
	// address it set could be there. It is kept the way isLocked is, in the
	// other direction: it ends when the tab commits a new document that is not
	// a move through the history, and comes back with the scripted document.
	isScripted bool
	// scriptedDocuments and scriptedAddresses are where the agent ran script.
	scriptedDocuments map[string]bool
	scriptedAddresses map[string]bool
	// hasScriptOnEveryPage says the tab was given something that runs in every
	// page it loads from now on (a script to run on each new document, a
	// binding, a debugger, a request interception). A navigation does not end
	// it, so the tab is never filled again.
	hasScriptOnEveryPage bool

	// secretHosts are the sites a password or a one-time code was put into
	// in this tab, for the rest of the tab's life: the session that sign-in
	// made is theirs.
	secretHosts map[string]bool
}

// Document is where a value was put: a tab's document.
type Document struct {
	// ID identifies the document (the browser's document id for a tab of the
	// person's browser, the loader id for the headless page). It may be empty
	// when the browser did not say.
	ID string
	// Address is the document's address.
	Address string
	// CommitCount is the number of commits the browser had counted for the tab
	// when the value went in; a report of an earlier commit is not news.
	CommitCount int64
}

// Commit is a tab committing a document: the browser says a main frame now
// shows another document.
type Commit struct {
	DocumentID string
	Address    string
	// IsBackForward says the commit is a move through the history, which does
	// not make a page new: the filled document may be what comes back.
	IsBackForward bool
	// Count numbers the tab's commits in the order they happened.
	Count int64
}

// hiddenWords stand in for a value in what the model reads.
const hiddenWords = "[hidden]"

// filledValuesMost is how many values are kept; the oldest go first.
const filledValuesMost = 256

// oneTimeCodeGrace is how long after its period a one-time code stays
// scrubbed: a page may still show it, or echo it, a little later.
const oneTimeCodeGrace = 60 * time.Second

func (self *FilledValues) clock() time.Time {
	if self.now != nil {
		return self.now()
	}
	return time.Now()
}

// Remember keeps values, to hide them from the model, for as long as this
// value lives. It locks nothing: Lock does.
func (self *FilledValues) Remember(values ...string) {
	self.remember(time.Time{}, values)
}

// RememberOneTimeCode keeps a code, to hide it from the model, until its
// period of the given length has ended and a little longer.
func (self *FilledValues) RememberOneTimeCode(period time.Duration, code string) {
	if self == nil {
		return
	}
	if period <= 0 {
		period = 30 * time.Second
	}
	self.remember(self.clock().Add(period+oneTimeCodeGrace), []string{code})
}

func (self *FilledValues) remember(forgetAt time.Time, values []string) {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.forgetExpiredLocked()
	for _, value := range values {
		if value == "" {
			continue
		}
		known := false
		for index := range self.values {
			if self.values[index].text != value {
				continue
			}
			known = true
			// A value kept for good stays so; a code asked for twice lives
			// until the later of the two.
			if !self.values[index].forgetAt.IsZero() && (forgetAt.IsZero() || forgetAt.After(self.values[index].forgetAt)) {
				self.values[index].forgetAt = forgetAt
			}
		}
		if !known {
			self.values = append(self.values, filledValue{text: value, forgetAt: forgetAt})
		}
	}
	if len(self.values) > filledValuesMost {
		self.values = append([]filledValue{}, self.values[len(self.values)-filledValuesMost:]...)
	}
}

func (self *FilledValues) forgetExpiredLocked() {
	now := self.clock()
	kept := self.values[:0]
	for _, value := range self.values {
		if value.forgetAt.IsZero() || value.forgetAt.After(now) {
			kept = append(kept, value)
		}
	}
	self.values = kept
}

func (self *FilledValues) tabLocked(tab string) *filledTab {
	if self.tabs == nil {
		self.tabs = map[string]*filledTab{}
	}
	found := self.tabs[tab]
	if found == nil {
		found = &filledTab{documents: map[string]bool{}, addresses: map[string]bool{}, scriptedDocuments: map[string]bool{}, scriptedAddresses: map[string]bool{}, secretHosts: map[string]bool{}}
		self.tabs[tab] = found
	}
	return found
}

// Lock says a value was put into a document of the tab, and locks the tab.
// The tab is whatever the browser calls it: an empty name is the one page of
// the headless browser, or a tab of the person's browser whose number the
// extension did not say.
func (self *FilledValues) Lock(tab string, document Document) {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	found := self.tabLocked(tab)
	found.isLocked = true
	if document.ID != "" {
		found.documents[document.ID] = true
	}
	if address := withoutFragment(document.Address); address != "" {
		found.addresses[address] = true
	}
	if document.CommitCount > found.lastCount {
		found.lastCount = document.CommitCount
	}
	// The tab may have moved on before the fill was reported: the newest
	// commit seen says where it is. Without a count there is no telling
	// which came first, and the lock stands.
	if document.CommitCount > 0 && found.latest != nil && found.latest.Count > document.CommitCount {
		found.applyLocked(*found.latest)
	}
}

// IsLocked says whether a script, a picture or a raw protocol command could
// read a value on the tab: it, or a tab that the browser did not name, holds a
// filled document it has not left.
func (self *FilledValues) IsLocked(tab string) bool {
	if self == nil {
		return false
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if found := self.tabs[tab]; found != nil && found.isLocked {
		return true
	}
	// A fill whose tab was not named could be any tab.
	if unnamed := self.tabs[""]; unnamed != nil && unnamed.isLocked {
		return true
	}
	return false
}

// HasFilledDocument says a value was put into some document of the tab, so
// that asking the browser which document it shows now is worth a round trip.
func (self *FilledValues) HasFilledDocument(tab string) bool {
	if self == nil {
		return false
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	found := self.tabs[tab]
	return found != nil && len(found.documents) > 0
}

// NoteCurrentDocument is the answer to asking the browser, right now, which
// document the tab shows. Events say the same thing but can arrive late: a
// move back can restore the filled document from the history's cache before
// the event that reports it, so a command that is about to run asks instead
// of waiting for the event. If the document is one a value was put into, the
// tab is locked (and stays so until a new document is committed).
func (self *FilledValues) NoteCurrentDocument(tab, documentID string) {
	if self == nil || documentID == "" {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if found := self.tabs[tab]; found != nil && found.documents[documentID] {
		found.isLocked = true
	}
}

// Committed is the browser saying the tab committed a document. Reports are
// applied in the order of their counts; a report older than one already seen
// is ignored. A tab that was never filled holds no lock to end.
func (self *FilledValues) Committed(tab string, commit Commit) {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	found := self.tabLocked(tab)
	if commit.Count > 0 && commit.Count <= found.lastCount {
		return
	}
	if commit.Count > found.lastCount {
		found.lastCount = commit.Count
	}
	latest := commit
	found.latest = &latest
	found.applyLocked(commit)
}

func (self *filledTab) applyLocked(commit Commit) {
	// The same rules end the mark of a scripted document as end the lock.
	// A document the browser names is scripted if it is one the agent ran
	// script in, wherever it came from: a page loaded again has lost the
	// script, and one restored from the history's cache has not.
	switch {
	case commit.DocumentID != "":
		self.isScripted = self.scriptedDocuments[commit.DocumentID]
	case commit.IsBackForward:
		if self.scriptedAddresses[withoutFragment(commit.Address)] {
			self.isScripted = true
		}
	default:
		self.isScripted = false
	}
	switch {
	case commit.DocumentID != "" && self.documents[commit.DocumentID]:
		// The filled document is back (restored from the history's cache).
		self.isLocked = true
	case commit.IsBackForward:
		// A move through the history is not a new page. It may bring back a
		// document that looks like the filled one, whose fields the browser
		// restores, so an address that was filled locks again; any other
		// leaves the lock as it was.
		if self.addresses[withoutFragment(commit.Address)] {
			self.isLocked = true
		}
	default:
		self.isLocked = false
	}
}

// ResetCommitCounts forgets the numbering of commits, for a browser that
// connected again: it numbers from the start.
func (self *FilledValues) ResetCommitCounts() {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, found := range self.tabs {
		found.lastCount = 0
		found.latest = nil
	}
}

// NoteScripted says the agent ran script in a document of the tab, or changed
// it with a DevTools command, before anything was filled into it. A page that
// has run the agent's script can hold a listener that sends a field's value
// out or a form that sends it to another site, neither of which the lock or
// the check of the form (which look at the page as it is) can rule out, so it
// is not filled. A document with no id is taken to be the newest one the tab
// committed.
func (self *FilledValues) NoteScripted(tab string, document Document) {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	found := self.tabLocked(tab)
	found.isScripted = true
	if document.ID == "" && found.latest != nil {
		document.ID, document.Address = found.latest.DocumentID, found.latest.Address
	}
	if document.ID != "" {
		found.scriptedDocuments[document.ID] = true
	}
	if address := withoutFragment(document.Address); address != "" {
		found.scriptedAddresses[address] = true
	}
}

// NoteScriptedOrigin says the agent ran a script on a site, or in a window it
// opened on that site, and so on every page of the site from now on. The
// origin is whatever OriginOf gives; anything else is ignored. A fill, a
// one-time code or a passkey is refused on a page of a scripted origin for as
// long as this value lives (the headless page: until it is reset; the
// person's browser: until the person allows it again).
func (self *FilledValues) NoteScriptedOrigin(origin string) {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.noteScriptedOriginLocked(OriginOf(origin))
}

func (self *FilledValues) noteScriptedOriginLocked(origin string) {
	if origin == "" {
		return
	}
	if self.scriptedOrigins == nil {
		self.scriptedOrigins = map[string]bool{}
	}
	self.scriptedOrigins[origin] = true
}

// NoteScriptedOrigins is NoteScriptedOrigin for each address given.
func (self *FilledValues) NoteScriptedOrigins(origins []string) {
	for _, origin := range origins {
		self.NoteScriptedOrigin(origin)
	}
}

// IsOriginScripted says the agent ran a script on the site the address is on.
func (self *FilledValues) IsOriginScripted(address string) bool {
	if self == nil {
		return false
	}
	origin := OriginOf(address)
	if origin == "" {
		return false
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.scriptedOrigins[origin]
}

// ScriptedOrigins are the sites the agent ran a script on, sorted.
func (self *FilledValues) ScriptedOrigins() []string {
	if self == nil {
		return nil
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	origins := make([]string, 0, len(self.scriptedOrigins))
	for origin := range self.scriptedOrigins {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	return origins
}

// ForgetScriptedOrigins lets the agent fill on every site again.
func (self *FilledValues) ForgetScriptedOrigins() {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.scriptedOrigins = nil
}

// ForgetPages forgets what is known of the pages: which are locked, which a
// script ran in, which sites it ran on and where a secret was put. It keeps
// the values to hide (they were typed once and stay hidden from the model),
// for a browser that was reset: it has no page, cookie or storage from before.
func (self *FilledValues) ForgetPages() {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.tabs = nil
	self.scriptedOrigins = nil
}

// OriginOf is the site an address is on, written the one way the scripted
// marks are compared: scheme, lower-case host without a leading "www." (the
// fill treats it as the same site) and the port when it is not the scheme's
// own. It is empty for an address that is not an http or https one.
func OriginOf(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "www.") && strings.Contains(host[4:], ".") {
		host = host[4:]
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := parsed.Port()
	if port == "" || (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}

// NoteScriptOnEveryPage says the tab was given something that runs in every
// page it loads (see filledTab.hasScriptOnEveryPage).
func (self *FilledValues) NoteScriptOnEveryPage(tab string) {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	found := self.tabLocked(tab)
	found.hasScriptOnEveryPage = true
	found.isScripted = true
}

// HasScriptOnEveryPage says the tab, or a tab the browser did not name, was
// given something that runs in every page it loads.
func (self *FilledValues) HasScriptOnEveryPage(tab string) bool {
	if self == nil {
		return false
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, name := range []string{tab, ""} {
		if found := self.tabs[name]; found != nil && found.hasScriptOnEveryPage {
			return true
		}
	}
	return false
}

// HasBeenScripted says the agent ran script in some document of the tab, so
// that asking the browser which document it shows now is worth a round trip.
func (self *FilledValues) HasBeenScripted(tab string) bool {
	if self == nil {
		return false
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if len(self.scriptedOrigins) > 0 {
		return true
	}
	for _, name := range []string{tab, ""} {
		if found := self.tabs[name]; found != nil && (found.isScripted || len(found.scriptedDocuments) > 0 || found.hasScriptOnEveryPage) {
			return true
		}
	}
	return false
}

// IsScripted says the document the tab shows now is one the agent ran script
// in. With the document's id (the headless browser asks the browser for it)
// the answer is exact; without it the answer is the tab's mark, which the
// commits the extension reports keep.
func (self *FilledValues) IsScripted(tab, documentID string) bool {
	if self == nil {
		return false
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, name := range []string{tab, ""} {
		found := self.tabs[name]
		if found == nil {
			continue
		}
		if found.hasScriptOnEveryPage {
			return true
		}
		if documentID != "" {
			if found.scriptedDocuments[documentID] {
				return true
			}
			continue
		}
		if found.isScripted {
			return true
		}
	}
	return false
}

// RememberSecretHost says a password or a one-time code was put into a page
// of this host in the tab. It lasts as long as the tab: the cookies and the
// storage that sign-in made are then the site's session, which the agent does
// not read back.
func (self *FilledValues) RememberSecretHost(tab, host string) {
	if self == nil || host == "" {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.tabLocked(tab).secretHosts[strings.ToLower(host)] = true
}

// HasSecretHost says a secret was put into a page of the host in the tab. An
// empty host asks whether one was put into any host.
func (self *FilledValues) HasSecretHost(tab, host string) bool {
	if self == nil {
		return false
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	host = strings.ToLower(host)
	for _, name := range []string{tab, ""} {
		found := self.tabs[name]
		if found == nil {
			continue
		}
		if (host == "" && len(found.secretHosts) > 0) || found.secretHosts[host] {
			return true
		}
	}
	return false
}

// Scrub replaces every remembered value in the text with [hidden], in every
// form writtenForms lists.
func (self *FilledValues) Scrub(text string) string {
	if self == nil || text == "" {
		return text
	}
	self.mutex.Lock()
	self.forgetExpiredLocked()
	values := make([]string, 0, len(self.values))
	for _, value := range self.values {
		values = append(values, value.text)
	}
	self.mutex.Unlock()
	if len(values) == 0 {
		return text
	}
	var forms []string
	for _, value := range values {
		forms = append(forms, writtenForms(value)...)
	}
	// Longer forms first, so that a form that contains another is replaced
	// whole rather than in two pieces.
	sort.SliceStable(forms, func(left, right int) bool { return len(forms[left]) > len(forms[right]) })
	for _, form := range forms {
		text = strings.ReplaceAll(text, form, hiddenWords)
	}
	return text
}

var percentTriplet = regexp.MustCompile(`%[0-9A-F]{2}`)

// writtenForms are the ways a value appears in text a tool writes or a page
// and a network log carry: itself; as a JSON string (with and without the
// escaping of &, < and >), and as a JSON string inside a JSON string; with
// HTML's escapes; percent-encoded the ways a form or an address does it (a
// space as + and as %20, either case of the hex digits, and twice over);
// base64, standard and URL-safe, with and without padding, wherever in a
// longer string it starts; and as hex.
func writtenForms(value string) []string {
	seen := map[string]bool{}
	var forms []string
	add := func(form string) {
		if form == "" || seen[form] {
			return
		}
		seen[form] = true
		forms = append(forms, form)
	}

	add(value)
	jsonOnce := jsonEscapedForms(value)
	var jsonTwice []string
	for _, once := range jsonOnce {
		add(once)
		jsonTwice = append(jsonTwice, jsonEscapedForms(once)...)
	}
	for _, twice := range jsonTwice {
		add(twice)
	}
	for _, source := range append([]string{value}, jsonOnce...) {
		for _, escaped := range htmlEscapedForms(source) {
			add(escaped)
		}
	}

	for _, encoded := range percentEncodedForms(value) {
		add(encoded)
		add(percentTriplet.ReplaceAllStringFunc(encoded, strings.ToLower))
		for _, again := range percentEncodedForms(encoded) {
			add(again)
		}
	}

	for _, encoded := range base64Forms(value) {
		add(encoded)
	}
	add(hex.EncodeToString([]byte(value)))
	add(strings.ToUpper(hex.EncodeToString([]byte(value))))
	return forms
}

// jsonEscapedForms is the value as JSON writes the inside of a string: with
// the escaping encoding/json applies by default and without it.
func jsonEscapedForms(value string) []string {
	var forms []string
	if escaped, err := json.Marshal(value); err == nil {
		forms = append(forms, strings.Trim(string(escaped), `"`))
	}
	var plain bytes.Buffer
	encoder := json.NewEncoder(&plain)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err == nil {
		forms = append(forms, strings.Trim(strings.TrimSpace(plain.String()), `"`))
	}
	return forms
}

// htmlEscapedForms are the ways a page or a server writes a value into HTML
// text or an attribute.
func htmlEscapedForms(value string) []string {
	named := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	hexQuote := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;")
	apostrophe := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return []string{html.EscapeString(value), named.Replace(value), hexQuote.Replace(value), apostrophe.Replace(value)}
}

// percentEncodedForms are the ways an address or a form body carries a value:
// a form's (a space as +), an address's (a space as %20), what JavaScript's
// encodeURIComponent writes, and every byte as a triplet.
func percentEncodedForms(value string) []string {
	query := url.QueryEscape(value)
	forms := []string{
		query,
		strings.ReplaceAll(query, "+", "%20"),
		url.PathEscape(value),
		percentEncoded(value, func(character byte) bool {
			return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || strings.IndexByte("-_.!~*'()", character) >= 0
		}),
		percentEncoded(value, func(byte) bool { return false }),
	}
	return forms
}

func percentEncoded(value string, isKept func(byte) bool) string {
	const digits = "0123456789ABCDEF"
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		character := value[index]
		if isKept(character) {
			builder.WriteByte(character)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(digits[character>>4])
		builder.WriteByte(digits[character&0x0f])
	}
	return builder.String()
}

// base64Forms are the ways base64 writes a value: alone, and inside a longer
// string, where the value may start at any of the three positions of a group
// of three bytes and so is written three different ways. Only the characters
// that the value alone decides are used for a position other than the first
// (the characters at the edges also depend on the bytes around the value),
// except at the end of the text, where the rest is the value's.
func base64Forms(value string) []string {
	raw := []byte(value)
	if len(raw) == 0 {
		return nil
	}
	var forms []string
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		for offset := 0; offset < 3; offset++ {
			encoded := encoding.EncodeToString(append(make([]byte, offset), raw...))
			unpadded := strings.TrimRight(encoded, "=")
			start := 0
			if offset > 0 {
				start = offset*8/6 + 1
			}
			end := min((offset+len(raw))*8/6, len(unpadded))
			candidates := []string{unpadded[min(start, len(unpadded)):], encoded[min(start, len(encoded)):]}
			if start < end {
				candidates = append(candidates, unpadded[start:end])
			}
			for _, candidate := range candidates {
				// A short run of base64 is in everything.
				if len(candidate) >= 6 {
					forms = append(forms, candidate)
				}
			}
		}
	}
	return forms
}

func withoutFragment(address string) string {
	if index := strings.Index(address, "#"); index >= 0 {
		return address[:index]
	}
	return address
}
