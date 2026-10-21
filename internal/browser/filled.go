package browser

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
)

// FilledValues is what the agent's sign-in put into a page and must not read
// back: a password or a one-time code, kept in memory for as long as the page
// (or the person's tab) lives, and never written anywhere.
//
// It does two things for the browser tool. While a page holds a filled secret
// the tool does not run a script on it or send it a raw DevTools command, which
// is the one way to ask a page for the value of a field; the page is locked
// until its main frame moves on to another document. And for as long as the
// page lives, every result and error of the tool for that page has the exact
// values replaced, so a snapshot, a script result or an extension's answer that
// repeats one says [hidden] instead.
//
// The zero value holds nothing and is ready to use; a nil one is safe to ask.
type FilledValues struct {
	mutex    sync.Mutex
	values   []string
	isLocked bool
	// address is the page's address when it was locked, for a page whose
	// documents cannot be watched (the person's tab): the lock ends when the
	// tab reports another address.
	address string
}

// hiddenWords stand in for a value in what the model reads.
const hiddenWords = "[hidden]"

// Remember keeps the values and locks the page. The address is the page's
// address when the browser cannot say when the document changes, and empty
// when it can.
func (self *FilledValues) Remember(address string, values ...string) {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, value := range values {
		if value == "" {
			continue
		}
		known := false
		for _, existing := range self.values {
			if existing == value {
				known = true
			}
		}
		if !known {
			self.values = append(self.values, value)
		}
	}
	self.isLocked = true
	self.address = withoutFragment(address)
}

// IsLocked says the page holds a filled secret and has not moved on.
func (self *FilledValues) IsLocked() bool {
	if self == nil {
		return false
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.isLocked
}

// Unlock ends the lock: the page moved on to another document. The values stay
// remembered, so what a page that is gone left in the tab's results is still
// hidden.
func (self *FilledValues) Unlock() {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.isLocked = false
}

// UnlockIfMoved ends the lock when the page is now at another address than the
// one it was filled at, ignoring a fragment. It is how a tab whose documents
// cannot be watched is seen to move on. It is conservative in one direction
// only: a page that changes its address without a new document (a script
// calling history.pushState) also ends the lock.
func (self *FilledValues) UnlockIfMoved(address string) {
	if self == nil {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.isLocked && self.address != "" && withoutFragment(address) != self.address {
		self.isLocked = false
	}
}

// Scrub replaces every remembered value in the text with [hidden], as written
// and as JSON writes it.
func (self *FilledValues) Scrub(text string) string {
	if self == nil || text == "" {
		return text
	}
	self.mutex.Lock()
	values := append([]string{}, self.values...)
	self.mutex.Unlock()
	for _, value := range values {
		for _, form := range writtenForms(value) {
			text = strings.ReplaceAll(text, form, hiddenWords)
		}
	}
	return text
}

// writtenForms are the ways a value appears in text a tool writes: itself, as
// a JSON string with HTML characters escaped (what encoding/json writes by
// default), and as a JSON string without that escaping.
func writtenForms(value string) []string {
	forms := []string{value}
	escaped, err := json.Marshal(value)
	if err == nil {
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

func withoutFragment(address string) string {
	if index := strings.Index(address, "#"); index >= 0 {
		return address[:index]
	}
	return address
}
