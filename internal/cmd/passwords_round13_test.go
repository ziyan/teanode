package cmd

import (
	"strings"
	"testing"
)

// On a computer that never saw a marked item, the server shows the item as it
// was while Ben and the agent had it. rekey seals for everyone the record
// reaches, so it says who that is before it seals anything, and without a
// terminal to ask on it refuses unless --yes was given. list and show name
// the same people under the item.
func TestRekeyNamesWhoGetsTheKeyAndAsksFirst(test *testing.T) {
	fake, _, zID := staleMarkedItems(test)
	rewriteRememberedItem(test, zID, func(known *passwordsKnownItem) { *known = passwordsKnownItem{} })

	for _, command := range [][]string{{"list"}, {"show", "zzz.example.com"}} {
		output := fake.mustRun(testToken, "", command...)
		if !strings.Contains(output, "run teanode passwords rekey "+zID+" (the new key goes to ben; the agent gets a copy)") {
			test.Errorf("%v did not name who rekey gives the key to:\n%s", command, output)
		}
	}

	item := fake.items[zID]
	countBefore := fake.rekeyCount
	output, err := fake.run(testToken, "", "rekey", "zzz.example.com")
	if err == nil || !strings.Contains(err.Error(), "the new key goes to ben; the agent gets a copy") || !strings.Contains(err.Error(), "--yes") {
		test.Fatalf("rekey without a terminal or --yes: %q, %v", output, err)
	}
	if fake.rekeyCount != countBefore {
		test.Fatalf("rekey sealed something before it was agreed to")
	}

	output = fake.mustRun(testToken, "", "rekey", "zzz.example.com", "--yes")
	if !strings.Contains(output, "the new key goes to ben; the agent gets a copy") || !strings.Contains(output, "has a new key") {
		test.Errorf("rekey --yes did not say who got the key:\n%s", output)
	}
	if fake.rekeyCount != countBefore+1 || item.itemKeyByAgentKey == "" {
		test.Errorf("rekey --yes did not seal: count %d, agent copy %q", fake.rekeyCount, item.itemKeyByAgentKey)
	}
}

// With --json the sentence goes to standard error, so standard output is JSON.
func TestRekeyWithJSONKeepsTheWordsOffStandardOutput(test *testing.T) {
	fake, _, zID := staleMarkedItems(test)
	rewriteRememberedItem(test, zID, func(known *passwordsKnownItem) { *known = passwordsKnownItem{} })
	output := fake.mustRun(testToken, "", "rekey", "zzz.example.com", "--yes", "--json")
	if strings.Contains(output, "the new key goes to") || !strings.HasPrefix(strings.TrimSpace(output), "{") {
		test.Errorf("rekey --yes --json wrote: %s", output)
	}
}

func TestARekeyPlanNamesTheNobodyCase(test *testing.T) {
	plan := passwordsRekeyPlan{}
	if words := plan.words(); words != "the new key goes to nobody but you; the agent gets none" {
		test.Errorf("an empty plan says %q", words)
	}
	plan = passwordsRekeyPlan{SealedForUserNames: []string{"ben", "cora"}, WaitingUserNames: []string{"dave"}, IsAgentCopyMade: true}
	if words := plan.words(); words != "the new key goes to ben, cora; the agent gets a copy; waiting for them to set up Passwords: dave" {
		test.Errorf("a plan says %q", words)
	}
}
