package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/browser"
)

// What the agent's sign-ins put into a person's browser belongs to the agent,
// not to one connection: a browser whose connection drops and comes back finds
// the same tab locked and the same values hidden, and a commit the new
// connection reports (numbered from the start again) is what unlocks it.
func TestAFilledTabStaysLockedWhenTheBrowserConnectsAgain(t *testing.T) {
	worker := &Agent{}
	first := &recordingBrowser{}
	worker.ConnectBrowser("a1", first, "Shop", "https://shop.example/sign-in", true)
	filled := worker.tabFor("a1").Filled()
	filled.Remember("a-password-9")
	filled.Lock("7", browser.Document{ID: "doc-1", Address: "https://shop.example/sign-in", CommitCount: 40})

	worker.DetachTab("a1", first)
	if worker.tabFor("a1") != nil {
		t.Fatal("the tab was not detached")
	}
	second := &recordingBrowser{}
	worker.ConnectBrowser("a1", second, "Shop", "https://shop.example/sign-in", true)
	again := worker.tabFor("a1").Filled()
	if again != filled {
		t.Fatal("a new connection made new values")
	}
	if !again.IsLocked("7") || again.Scrub("a-password-9") != "[hidden]" {
		t.Fatal("the lock or the hiding was lost with the connection")
	}
	// Another person's agent has its own.
	worker.ConnectBrowser("a2", &recordingBrowser{}, "Mail", "https://mail.example/", true)
	if worker.tabFor("a2").Filled() == filled || worker.tabFor("a2").Filled().IsLocked("7") {
		t.Fatal("another agent shares the values")
	}

	// A commit from the old connection is nobody's now.
	worker.TabCommitted("a1", first, "7", browser.Commit{DocumentID: "doc-2", Address: "https://shop.example/account", Count: 1})
	if !again.IsLocked("7") {
		t.Fatal("a commit from a connection that went unlocked the tab")
	}
	// The new one counts from the start, and a new document ends the lock.
	worker.TabCommitted("a1", second, "7", browser.Commit{DocumentID: "doc-2", Address: "https://shop.example/account", Count: 1})
	if again.IsLocked("7") {
		t.Fatal("a new document did not unlock the tab")
	}
	// A commit for a browser that is not connected is dropped.
	worker.TabCommitted("a3", second, "7", browser.Commit{DocumentID: "doc-3", Count: 1})
}

// The tab the actions go to is what the extension last said, and what its
// answers to opening, switching and listing say.
func TestTheCurrentTabIsWhatTheBrowserSaid(t *testing.T) {
	worker := &Agent{}
	worker.ConnectBrowser("a1", &recordingBrowser{}, "Shop", "https://shop.example/", true)
	tab := worker.tabFor("a1")
	if tab.CurrentTabID() != "" {
		t.Fatal("a tab that was not named")
	}
	worker.SetCurrentTab("a1", "11")
	if tab.CurrentTabID() != "11" {
		t.Fatalf("the current tab is %q", tab.CurrentTabID())
	}
	tab.NoteCurrentTab("12")
	if tab.CurrentTabID() != "12" {
		t.Fatalf("the current tab is %q", tab.CurrentTabID())
	}
	// Nothing happens for a browser that is not connected.
	worker.SetCurrentTab("nobody", "1")
}
