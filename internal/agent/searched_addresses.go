package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// searchedAddressKey is an address as two spellings of the same page agree on
// it: the scheme and host in lower case, no fragment, no trailing slash.
func searchedAddressKey(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Host == "" {
		return strings.TrimSpace(address)
	}
	key := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + strings.TrimRight(parsed.Path, "/")
	if parsed.RawQuery != "" {
		key += "?" + parsed.RawQuery
	}
	return key
}

// noteSearchedAddresses remembers the addresses a search in this turn
// returned, for a turn held to fetching only those (see
// AskSettings.IsFetchLimitedToSearched).
func (self *AskRun) noteSearchedAddresses(addresses []string) {
	if !self.settings.IsFetchLimitedToSearched || len(addresses) == 0 {
		return
	}
	self.searchedAddressesMutex.Lock()
	defer self.searchedAddressesMutex.Unlock()
	if self.searchedAddresses == nil {
		self.searchedAddresses = map[string]bool{}
	}
	for _, address := range addresses {
		self.searchedAddresses[searchedAddressKey(address)] = true
	}
}

// checkFetchAllowed says why a web_fetch call may not run in a turn held to
// fetching only what a search returned, or nil. An address built by the model
// can carry whatever the turn has read to whoever owns the server, so only an
// address nobody in the turn wrote is fetched.
func (self *AskRun) checkFetchAllowed(arguments json.RawMessage) error {
	if !self.settings.IsFetchLimitedToSearched {
		return nil
	}
	var asked struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(arguments, &asked); err != nil {
		return fmt.Errorf("the address is needed")
	}
	self.searchedAddressesMutex.Lock()
	defer self.searchedAddressesMutex.Unlock()
	if !self.searchedAddresses[searchedAddressKey(asked.URL)] {
		return fmt.Errorf("this run fetches only a page that a web_search in it returned; search for the page, then fetch the address the search gave")
	}
	return nil
}
