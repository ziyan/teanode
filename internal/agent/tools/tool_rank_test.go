package tools

import (
	"fmt"
	"strings"
	"testing"
)

// rankCatalog is a catalog shaped like a real one: a few tools the model
// holds, a few that wait, and many more that wait and share the common
// words ("get", "list", "search", "read") with everything else.
func rankCatalog() (held, deferred []*Tool) {
	held = []*Tool{
		{Name: "memory", Family: FamilyGeneral, Description: "Read and write the pages of what you know: get a page, search pages, add facts to a page."},
		{Name: "search", Family: FamilyGeneral, Description: "Search everything the person has at once: mail, notes, files and pages."},
		{Name: "tool_search", Family: FamilyGeneral, Description: "Search for more tools by what they do."},
	}
	deferred = []*Tool{
		{Name: "knowledge", Family: FamilyGeneral, Description: "Read the sources the agent knows: a document whole, a passage, a checkout."},
		{Name: "calendar", Family: FamilyPeople, Description: "The person's calendars: add, edit and remove events."},
		{Name: "calendar_agenda", Family: FamilyPeople, Description: "The events of a day or a range of days, in order."},
		{Name: "contact_book", Family: FamilyPeople, Description: "The person's address books and the contacts in them."},
		{Name: "brokerage_filing_facts", Family: FamilyServers, Description: "Get the facts of a company's filing: revenue, income, the figures reported."},
		{Name: "brokerage_accounts", Family: FamilyServers, Description: "List the brokerage accounts and their balances."},
		{Name: "skill__mailapp__mailapp_search", Family: FamilySkills, Description: "Search the messages of the person's mail app account."},
		{Name: "skill__mailapp__mailapp_read", Family: FamilySkills, Description: "Read one message of the person's mail app account by its id."},
		{Name: "skill__mailapp__mailapp_new", Family: FamilySkills, Description: "List the new messages of the person's mail app account."},
		{Name: "dns_check", Family: FamilyDomains, Description: "Check the DNS records of a domain."},
	}
	for index := 0; index < DeferralThreshold; index++ {
		deferred = append(deferred, &Tool{
			Name: fmt.Sprintf("other_%d", index), Family: FamilyServers,
			Description: fmt.Sprintf("Get, list, read and search the items of service %d.", index),
		})
	}
	return held, deferred
}

func namesOf(tools []*Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// Common words load nothing on their own, and a query about a tool the
// model holds names that tool back rather than loading whatever shares a
// word with it: "facts" is in a brokerage tool's description too.
func TestSearchToolsDoesNotLoadOnCommonWords(t *testing.T) {
	held, deferred := rankCatalog()
	found := SearchTools(held, deferred, "memory get search pages facts", 0)
	if len(found.Loadable) != 0 {
		t.Errorf("a query for the memory tool held loaded %v", namesOf(found.Loadable))
	}
	if names := namesOf(found.Held); len(names) == 0 || names[0] != "memory" {
		t.Errorf("the memory tool held is named back first: %v", names)
	}
}

// A query naming a waiting tool loads it, beside the one held, and puts it
// first.
func TestSearchToolsLoadsAToolTheQueryNames(t *testing.T) {
	held, deferred := rankCatalog()
	found := SearchTools(held, deferred, "knowledge memory read sources", 0)
	if names := namesOf(found.Loadable); len(names) == 0 || names[0] != "knowledge" {
		t.Errorf("knowledge is named and loaded first: %v", names)
	}
	found = SearchTools(held, deferred, "+contact_book", 0)
	if names := namesOf(found.Loadable); len(names) != 1 || names[0] != "contact_book" {
		t.Errorf("a whole name loads that tool alone: %v", names)
	}
}

// Every word of a longer name puts it before the tool named by one of
// them, and a plural finds its singular.
func TestSearchToolsPrefersTheToolEveryWordOfWhoseNameIsAsked(t *testing.T) {
	held, deferred := rankCatalog()
	found := SearchTools(held, deferred, "calendar agenda upcoming events", 0)
	if names := namesOf(found.Loadable); len(names) == 0 || names[0] != "calendar_agenda" {
		t.Errorf("calendar_agenda first: %v", names)
	}
	found = SearchTools(held, deferred, "mailapp search messages", 0)
	if names := namesOf(found.Loadable); len(names) == 0 || names[0] != "skill__mailapp__mailapp_search" {
		t.Errorf("the skill's search first: %v", names)
	}
	// "search" is also the name of a tool held, but the skill matches
	// better, so the held one is not named back.
	if len(found.Held) != 0 {
		t.Errorf("a held tool that matches less well is not named: %v", namesOf(found.Held))
	}
}

// select: loads the tools it names, in its order, within the limit, and
// names back the ones held; tool_search is never named.
func TestSearchToolsSelectsByName(t *testing.T) {
	held, deferred := rankCatalog()
	found := SearchTools(held, deferred, "select:dns_check, calendar ,memory,tool_search,missing", 0)
	if names := strings.Join(namesOf(found.Loadable), ","); names != "dns_check,calendar" {
		t.Errorf("select loads what it names, in order: %s", names)
	}
	if names := strings.Join(namesOf(found.Held), ","); names != "memory" {
		t.Errorf("select names back what is held, never tool_search: %s", names)
	}
	found = SearchTools(held, deferred, "select:dns_check,calendar,knowledge", 2)
	if len(found.Loadable) != 2 {
		t.Errorf("select keeps to the limit: %v", namesOf(found.Loadable))
	}
}

// A search loads at most the limit asked, five when none is, and never
// more than ten.
func TestSearchToolsKeepsToItsLimits(t *testing.T) {
	held, deferred := rankCatalog()
	var close []*Tool
	for index := 0; index < 20; index++ {
		close = append(close, &Tool{Name: fmt.Sprintf("zone_tool_%d", index), Family: FamilyDomains, Description: "Change a zone of a domain."})
	}
	deferred = append(deferred, close...)
	if found := SearchTools(held, deferred, "zone domain change", 0); len(found.Loadable) != SearchLoadedLimit {
		t.Errorf("five by default: %d", len(found.Loadable))
	}
	if found := SearchTools(held, deferred, "zone domain change", 50); len(found.Loadable) != SearchLoadedMost {
		t.Errorf("ten at most: %d", len(found.Loadable))
	}
}

// What the search compares: lowercase words, split in camelCase and at
// punctuation, without the words that say nothing, with plurals made
// singular.
func TestSearchWords(t *testing.T) {
	for _, testCase := range []struct{ text, words string }{
		{"List the Events of calendarAgenda", "list event calendar agenda"},
		{"entries, searches; boxes", "entry search box"},
		{"address status shared", "address status shared"},
		{"a b to of", ""},
	} {
		if got := strings.Join(searchWords(testCase.text), " "); got != testCase.words {
			t.Errorf("searchWords(%q) = %q, want %q", testCase.text, got, testCase.words)
		}
	}
}
