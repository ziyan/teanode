package cmd

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/client"
)

// A condition is written the way a person would say it: what to look at, how
// to compare it, and what to. The parser is what stands between that and the
// shape the server takes.
func TestParseConditionReadsWhatAPersonWrites(test *testing.T) {
	test.Parallel()

	cases := []struct {
		written string
		want    client.MailboxRuleCondition
	}{
		{"from:contains:@github.com", client.MailboxRuleCondition{Field: "from", Operator: "contains", Value: "@github.com"}},
		{"subject:equals:Re: hello", client.MailboxRuleCondition{Field: "subject", Operator: "equals", Value: "Re: hello"}},
		{"score:above:5", client.MailboxRuleCondition{Field: "score", Operator: "above", Value: "5"}},
		{"header:List-Id:contains:golang", client.MailboxRuleCondition{Field: "header", Header: "List-Id", Operator: "contains", Value: "golang"}},
		{"sender-known", client.MailboxRuleCondition{Field: "sender-known"}},
		{"any", client.MailboxRuleCondition{Field: "any"}},
	}
	for _, test_case := range cases {
		got, err := parseCondition(test_case.written)
		if err != nil {
			test.Errorf("parseCondition(%q): %s", test_case.written, err)
			continue
		}
		if got != test_case.want {
			test.Errorf("parseCondition(%q) = %+v, want %+v", test_case.written, got, test_case.want)
		}
	}
}

// And what is not a condition says so, naming what may be written instead.
func TestParseConditionRefusesWhatIsNotOne(test *testing.T) {
	test.Parallel()

	cases := map[string]string{
		"nonsense:contains:x":     "not a field",
		"from:starts-with:ada":    "not an operator",
		"from:contains":           "not a condition",
		"header:List-Id:contains": "not a header condition",
		"any:contains:x":          "takes nothing after it",
	}
	for written, wanted := range cases {
		_, err := parseCondition(written)
		if err == nil {
			test.Errorf("parseCondition(%q) was accepted", written)
			continue
		}
		if !strings.Contains(err.Error(), wanted) {
			test.Errorf("parseCondition(%q) said %q, want it to mention %q", written, err, wanted)
		}
	}
}

// A folder is named by a person, so it is found by name as well as by
// identifier; two folders of the same name under different parents are
// ambiguous, and the answer names both rather than picking one.
func TestRequireFolderFindsByNameAndSaysWhenTwoMatch(test *testing.T) {
	test.Parallel()

	view := &client.MailboxView{
		Mailbox: &client.Mailbox{ID: "m1", Name: "Personal"},
		Folders: []*client.MailboxFolder{
			{ID: "f1", Name: "Inbox", Kind: "inbox"},
			{ID: "f2", Name: "Work"},
			{ID: "f3", Name: "Receipts", ParentID: "f2"},
			{ID: "f4", Name: "Receipts"},
		},
	}

	found, err := requireFolder(view, "work")
	if err != nil || found.ID != "f2" {
		test.Errorf("requireFolder by name = %+v, %v; want the Work folder", found, err)
	}
	found, err = requireFolder(view, "f3")
	if err != nil || found.ID != "f3" {
		test.Errorf("requireFolder by identifier = %+v, %v; want f3", found, err)
	}
	if _, err := requireFolder(view, "Receipts"); err == nil || !strings.Contains(err.Error(), "f3") || !strings.Contains(err.Error(), "f4") {
		test.Errorf("an ambiguous name said %v, want both identifiers", err)
	}
	if _, err := requireFolder(view, "Nothing"); err == nil {
		test.Error("a folder that does not exist was found")
	}
}

// The path is what the rail draws as a tree, written on one line.
func TestFolderPathNamesTheParents(test *testing.T) {
	test.Parallel()

	view := &client.MailboxView{
		Folders: []*client.MailboxFolder{
			{ID: "f1", Name: "Work"},
			{ID: "f2", Name: "Projects", ParentID: "f1"},
			{ID: "f3", Name: "Travel", ParentID: "f2"},
		},
	}
	if path := folderPath(view, view.Folders[2]); path != "Work/Projects/Travel" {
		test.Errorf("folderPath = %q, want Work/Projects/Travel", path)
	}
}

// Adding to a group's list does not repeat what is already there, and
// removing leaves the rest in order.
func TestChangeListAddsWithoutRepeatingAndRemoves(test *testing.T) {
	test.Parallel()

	current := []string{"a", "b", "c"}
	if got := strings.Join(changeList(current, []string{"b", "d"}, true), ","); got != "a,b,c,d" {
		test.Errorf("adding = %q, want a,b,c,d", got)
	}
	if got := strings.Join(changeList(current, []string{"b"}, false), ","); got != "a,c" {
		test.Errorf("removing = %q, want a,c", got)
	}
}
