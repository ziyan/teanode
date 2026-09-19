package cmd

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// The grading of a question set, over a carried set written by hand.
//
// The grading is where the rules are -- every word of a claim, on the
// page the claim names, and an abstain question that passes by carrying
// nothing -- and it is the part that would be wrong in a way nobody
// notices, because a set that grades itself generously reads as a graph
// that remembers. So it is a function of what was carried rather than of
// a connection, and this test needs no server and no database.

// carriedFor is what recall would have put in front of the model, as the
// evaluation receives it.
func carriedFor(pages ...*client.AgentRecalledPage) []*client.AgentRecalledPage {
	return pages
}

func recalledPage(path string, facts ...*client.AgentRecalledFact) *client.AgentRecalledPage {
	return &client.AgentRecalledPage{Path: path, Facts: facts}
}

func recalledFact(number int, text string) *client.AgentRecalledFact {
	return &client.AgentRecalledFact{Number: number, Text: text}
}

func TestGradingAQuestionSet(t *testing.T) {
	t.Parallel()

	// One small graph, carried for every question below.
	alice := recalledPage("people/alice-chen",
		recalledFact(1, "Runs the platform team at Example Ltd"),
		recalledFact(2, "Works from Lisbon since March 2026"),
	)
	portal := recalledPage("projects/portal",
		recalledFact(1, "Runs on the Frankfurt cluster"),
		recalledFact(3, "Alice Chen leads the controls work"),
	)
	stale := recalledPage("people/alice-chen",
		recalledFact(2, "Works from Berlin"),
	)

	for name, testCase := range map[string]struct {
		question evaluationQuestion
		carried  []*client.AgentRecalledPage
		hit      bool
		failed   string
	}{
		"direct": {
			question: evaluationQuestion{ID: "d1", Kind: questionDirect, Question: "what does Alice do?",
				Expects: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"platform"}}}},
			carried: carriedFor(alice, portal),
			hit:     true,
		},
		"directMissing": {
			question: evaluationQuestion{ID: "d2", Kind: questionDirect, Question: "what does Alice do?",
				Expects: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"accounts"}}}},
			carried: carriedFor(alice, portal),
			failed:  `did not carry people/alice-chen saying "accounts"`,
		},
		// Every word has to be in one fact: two words from two different
		// facts on the page are not the fact the question is about.
		"directAcrossTwoFacts": {
			question: evaluationQuestion{ID: "d3", Kind: questionDirect, Question: "where does Alice work?",
				Expects: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"platform", "lisbon"}}}},
			carried: carriedFor(alice),
			failed:  `did not carry people/alice-chen saying "platform lisbon"`,
		},
		// Case is not what a question is about, and neither is the page a
		// fact happens to also be true of.
		"paraphrase": {
			question: evaluationQuestion{ID: "p1", Kind: questionParaphrase, Question: "who is her employer?",
				Expects: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"EXAMPLE LTD"}}}},
			carried: carriedFor(alice),
			hit:     true,
		},
		"paraphraseOnTheWrongPage": {
			question: evaluationQuestion{ID: "p2", Kind: questionParaphrase, Question: "who leads the controls work?",
				Expects: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"controls"}}}},
			carried: carriedFor(alice, portal),
			failed:  `did not carry people/alice-chen saying "controls"`,
		},
		"changed": {
			question: evaluationQuestion{ID: "c1", Kind: questionChanged, Question: "where does Alice live?",
				Expects: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"lisbon"}}},
				Forbids: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"berlin"}}}},
			carried: carriedFor(alice),
			hit:     true,
		},
		// The old statement coming back with the new one is the failure
		// the changed kind exists to catch.
		"changedCarriesTheOldOne": {
			question: evaluationQuestion{ID: "c2", Kind: questionChanged, Question: "where does Alice live?",
				Expects: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"lisbon"}}},
				Forbids: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"berlin"}}}},
			carried: carriedFor(alice, stale),
			failed:  `carried people/alice-chen saying "berlin"`,
		},
		"multihop": {
			question: evaluationQuestion{ID: "m1", Kind: questionMultihop, Question: "who runs the Frankfurt cluster work?",
				Expects: []evaluationClaim{
					{Path: "projects/portal", Words: []string{"frankfurt"}},
					{Path: "people/alice-chen", Words: []string{"platform"}},
				}},
			carried: carriedFor(alice, portal),
			hit:     true,
		},
		"multihopOnlyOneHop": {
			question: evaluationQuestion{ID: "m2", Kind: questionMultihop, Question: "who runs the Frankfurt cluster work?",
				Expects: []evaluationClaim{
					{Path: "projects/portal", Words: []string{"frankfurt"}},
					{Path: "people/alice-chen", Words: []string{"platform"}},
				}},
			carried: carriedFor(portal),
			failed:  `did not carry people/alice-chen saying "platform"`,
		},
		// A claim with no words is about the page: nothing from it at all
		// is what an abstain question asks for.
		"abstain": {
			question: evaluationQuestion{ID: "a1", Kind: questionAbstain, Question: "what is Alice's brother called?",
				Forbids: []evaluationClaim{{Path: "people/alice-chen"}}},
			carried: carriedFor(portal),
			hit:     true,
		},
		"abstainAnsweredAnyway": {
			question: evaluationQuestion{ID: "a2", Kind: questionAbstain, Question: "what is Alice's brother called?",
				Forbids: []evaluationClaim{{Path: "people/alice-chen"}}},
			carried: carriedFor(alice),
			failed:  "carried anything on people/alice-chen",
		},
		"abstainWithNothingCarried": {
			question: evaluationQuestion{ID: "a3", Kind: questionAbstain, Question: "what is Alice's brother called?",
				Forbids: []evaluationClaim{{Path: "people/alice-chen"}}},
			carried: nil,
			hit:     true,
		},
		// A night divides a page that has grown too long, and the fact
		// the question is about moves to a page under it. The question
		// set names the parent, because it was written before the
		// division; the fact has not been lost and the question has not
		// failed.
		"underAPageTheNightDivided": {
			question: evaluationQuestion{ID: "d4", Kind: questionDirect, Question: "which cluster?",
				Expects: []evaluationClaim{{Path: "projects/portal", Words: []string{"frankfurt"}}}},
			carried: carriedFor(recalledPage("projects/portal/deployments",
				recalledFact(3, "Runs on the Frankfurt cluster"))),
			hit: true,
		},
		// A page whose path merely begins with the same letters is not
		// under it: projects/portal-old is another project.
		"besideAPageIsNotUnderIt": {
			question: evaluationQuestion{ID: "d5", Kind: questionDirect, Question: "which cluster?",
				Expects: []evaluationClaim{{Path: "projects/portal", Words: []string{"frankfurt"}}}},
			carried: carriedFor(recalledPage("projects/portal-old",
				recalledFact(3, "Runs on the Frankfurt cluster"))),
			failed: `did not carry projects/portal saying "frankfurt"`,
		},
		// The same reach on the negative side: an abstain question is
		// answered anyway if the page it forbids was divided.
		"abstainAnsweredFromUnderThePage": {
			question: evaluationQuestion{ID: "a5", Kind: questionAbstain, Question: "what is Alice's brother called?",
				Forbids: []evaluationClaim{{Path: "people/alice-chen"}}},
			carried: carriedFor(recalledPage("people/alice-chen/family",
				recalledFact(1, "Her brother is called Tom"))),
			failed: "carried anything on people/alice-chen",
		},
		// A set whose abstain question expects something is a mistake in
		// the file, and saying so beats grading it.
		"abstainThatExpects": {
			question: evaluationQuestion{ID: "a4", Kind: questionAbstain, Question: "what is Alice's brother called?",
				Expects: []evaluationClaim{{Path: "people/alice-chen", Words: []string{"brother"}}}},
			carried: carriedFor(alice),
			failed:  "an abstain question expects nothing; drop its expects or change its kind",
		},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			outcome := gradeRecall(testCase.question, testCase.carried)
			if outcome.Hit != testCase.hit {
				t.Fatalf("%s: hit is %v, wanted %v (%s)", testCase.question.ID, outcome.Hit, testCase.hit, outcome.Failed)
			}
			if outcome.Failed != testCase.failed {
				t.Fatalf("%s: it failed with %q, wanted %q", testCase.question.ID, outcome.Failed, testCase.failed)
			}
		})
	}
}

// The set that ships with the program is read by the same reader the
// command uses, so that a set edited by hand — which is how every real
// one is written — is found to be wrong here rather than on the server.
func TestTheExampleQuestionSetIsReadable(t *testing.T) {
	t.Parallel()

	questions, err := readQuestionSet("../../docs/evaluation/memory-questions.json")
	if err != nil {
		t.Fatalf("the example set does not read: %s", err)
	}
	kinds := map[string]int{}
	for _, question := range questions {
		kinds[question.Kind]++
	}
	// Two of each: the file is there to show the shape of all five kinds.
	for _, kind := range questionKinds {
		if kinds[kind] < 2 {
			t.Errorf("the example set has %d %s questions; it is meant to show every kind", kinds[kind], kind)
		}
	}
}

// The totals are per kind, in the order the kinds are declared, and a
// kind nothing asked about is left out rather than printed as 0 of 0.
func TestTheTotalsAreCountedPerKind(t *testing.T) {
	t.Parallel()

	totals := totalsByKind([]*evaluationResult{
		{ID: "a1", Kind: questionAbstain, Hit: true},
		{ID: "d1", Kind: questionDirect, Hit: true},
		{ID: "d2", Kind: questionDirect},
		{ID: "d3", Kind: questionDirect, Hit: true},
	})
	if len(totals) != 2 {
		t.Fatalf("two kinds were asked about, and %d are counted", len(totals))
	}
	if totals[0].Kind != questionDirect || totals[0].Hits != 2 || totals[0].Asked != 3 {
		t.Fatalf("direct is %+v", totals[0])
	}
	if totals[1].Kind != questionAbstain || totals[1].Hits != 1 || totals[1].Asked != 1 {
		t.Fatalf("abstain is %+v", totals[1])
	}
}

// What cannot be undone asks first, and these two did not.
//
// `memory forget` with no --number deletes a page and everything filed
// under it; `knowledge remove` throws away every document and chunk a
// source ever produced, which for a checkout or a chat archive is hours
// of reading and the embedding bill that went with it. Both went
// straight through on a mistyped path or name.
//
// The forget is checked by running it. Standard input is not a terminal
// under `go test`, which is the case confirm refuses outright, so a
// refusal naming --force proves the question is asked -- and it is asked
// before any connection is opened, which is why this needs no server.
func TestForgettingAPageAndRemovingASourceAskFirst(t *testing.T) {
	t.Parallel()

	forget := commandNamed(t, commandNamed(t, NewAgentCommand(), "memory"), "forget")
	if flagNamed(forget, "force") == nil {
		t.Errorf("memory forget offers --force, so a script that has already decided can say so")
	}
	remove := commandNamed(t, commandNamed(t, NewAgentCommand(), "knowledge"), "remove")
	if flagNamed(remove, "force") == nil {
		t.Errorf("knowledge remove offers --force")
	}

	err := forget.Run(t.Context(), []string{"forget", "people/alice-chen"})
	if err == nil {
		t.Fatalf("forgetting a whole page without confirmation is refused")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("the refusal says how to proceed: %s", err)
	}
	if !strings.Contains(err.Error(), "people/alice-chen") {
		t.Errorf("and says what would have gone: %s", err)
	}
}

// commandNamed and flagNamed reach into the command tree the way a person
// reaches the command: by the words they type.
func commandNamed(t *testing.T, parent *cli.Command, name string) *cli.Command {
	t.Helper()
	for _, command := range parent.Commands {
		if command.Name == name {
			return command
		}
	}
	t.Fatalf("%s has no %q subcommand", parent.Name, name)
	return nil
}

func flagNamed(command *cli.Command, name string) cli.Flag {
	for _, flag := range command.Flags {
		for _, named := range flag.Names() {
			if named == name {
				return flag
			}
		}
	}
	return nil
}

// A page's aliases are the other names words may find it by, and a merge
// folds the page it swallowed into one. They could be read from here but
// not written, so a name the agent had wrong stayed wrong.
func TestAPageTakesItsAliases(test *testing.T) {
	test.Parallel()

	page := commandNamed(test, commandNamed(test, NewAgentCommand(), "memory"), "page")
	alias := flagNamed(page, "alias")
	if alias == nil {
		test.Fatal("memory page takes --alias")
	}
	// Repeatable, because the names a page goes by are a list and a
	// person gives them one at a time.
	if _, repeatable := alias.(*cli.StringSliceFlag); !repeatable {
		test.Errorf("--alias is a list flag, so that it can be given more than once")
	}
}
