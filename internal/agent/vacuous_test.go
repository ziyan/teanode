package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// The sentences a night working from titles alone produces, which are
// twenty-two per cent of what the first real ingest filed, and which say
// nothing the page's own line in the index does not.
func TestAFactThatOnlySaysThePageExistsIsRefused(t *testing.T) {
	for _, row := range []struct {
		page *models.AgentNode
		text string
	}{
		{&models.AgentNode{Path: "projects/formatting", Name: "Formatting"},
			"Formatting is a project or work channel."},
		{&models.AgentNode{Path: "projects/documentation", Name: "Documentation"},
			"The documentation work channel had activity in September 2026."},
		{&models.AgentNode{Path: "projects/calibration", Name: "Calibration"},
			"Calibration is a project or work channel with activity in September 2026."},
		{&models.AgentNode{Path: "projects/backend-private", Name: "backend-private"},
			"backend-private is a private project or work channel."},
		{&models.AgentNode{Path: "projects/frontend", Name: "frontend"},
			"A project or work channel named frontend was active in September 2026."},
		{&models.AgentNode{Path: "projects/ci-cluster-management", Name: "CI Cluster Management"},
			"The CI Cluster Management project was active in September 2026."},
		{&models.AgentNode{Path: "people/grace-hopper", Name: "Grace Hopper"},
			"Grace Hopper is a person."},
	} {
		if saysSomethingNew(row.text, row.page, nil) {
			t.Errorf("%q on %q says nothing the page does not", row.text, row.page.Path)
		}
	}
}

// And what must survive: everything that is actually about the thing
// rather than about it being a page.
func TestAFactAboutTheThingIsKept(t *testing.T) {
	for _, row := range []struct {
		page *models.AgentNode
		text string
	}{
		{&models.AgentNode{Path: "projects/portal", Name: "Portal"},
			"The queue consumer is restarted by hand when it dies."},
		{&models.AgentNode{Path: "projects/calibration", Name: "Calibration"},
			"Calibration is done against the Toyota Boshoku jig."},
		// A date on its own is not substance; a date with something that
		// happened is.
		{&models.AgentNode{Path: "projects/askul-osaka-2018", Name: "Askul Osaka 2018"},
			"Askul Osaka went live in March 2019."},
		{&models.AgentNode{Path: "people/grace-hopper", Name: "Grace Hopper"},
			"Grace Hopper is the customer's site lead."},
		// The page's own words repeated, plus one that is not.
		{&models.AgentNode{Path: "projects/frontend", Name: "frontend"},
			"The frontend project is owned by the Osaka team."},
		{&models.AgentNode{Path: "things/kittiwake", Name: "Kittiwake"},
			"Repainted every spring."},
	} {
		if !saysSomethingNew(row.text, row.page, nil) {
			t.Errorf("%q on %q says something and was refused", row.text, row.page.Path)
		}
	}
}

// A fact with no page to compare against is kept: the rule is about what
// a page already says, and without one there is nothing to say it.
func TestAFactWithNoPageIsKept(t *testing.T) {
	if !saysSomethingNew("anything at all", nil, nil) {
		t.Fatalf("with no page there is nothing for a fact to repeat")
	}
}

func TestMonthSpanSaysOneMonthOnce(t *testing.T) {
	july := time.Date(2026, time.July, 3, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	if got := monthSpan(&july, &later); got != "July 2026" {
		t.Fatalf("one month: %q", got)
	}
	march := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	if got := monthSpan(&march, &later); got != "March 2024 to July 2026" {
		t.Fatalf("two months: %q", got)
	}
}

func TestReviseWordingSaysTheOldLinesTheNewWay(t *testing.T) {
	cases := map[string]string{
		"1 commits by 1 people, August 2015 to August 2015.":     "1 commits by 1 person, August 2015.",
		"63 commits by 4 people, October 2014 to April 2016.":    "63 commits by 4 people, October 2014 to April 2016.",
		"Ziyan wrote 5 of them, November 2012 to November 2012.": "Ziyan wrote 5 of the commits, November 2012.",
		"Ziyan wrote 389 of them, April 2013 to January 2015.":   "Ziyan wrote 389 of the commits, April 2013 to January 2015.",
		"Written in Go.": "Written in Go.",
		"Its readme says: a test, July 2026 to July 2026 it ran.": "Its readme says: a test, July 2026 to July 2026 it ran.",
	}
	for old, want := range cases {
		if got := reviseWording(old); got != want {
			t.Errorf("%q: got %q, want %q", old, got, want)
		}
	}
}

func TestHalfwayIsHalfOfWhatIsLeft(t *testing.T) {
	now := time.Date(2026, time.September, 15, 23, 0, 0, 0, time.UTC)
	if got := halfway(context.Background(), now); !got.IsZero() {
		t.Fatalf("no deadline: %v", got)
	}
	ctx, cancel := context.WithDeadline(context.Background(), now.Add(40*time.Minute))
	defer cancel()
	if got := halfway(ctx, now); got != now.Add(20*time.Minute) {
		t.Fatalf("half of forty minutes: %v", got)
	}
}

func TestSaysNothingOpeningKnowsPadding(t *testing.T) {
	padding := []string{
		"This project matters to Ziyan because they contributed to its development.",
		"This is a software project to which Ziyan contributed; the facts below describe its implementation.",
		"rgmpo is a Python-oriented code project with a recorded repository location. It matters to Ziyan because they were a substantial contributor.",
		"baconator is a work project closely associated with Ziyan.",
	}
	for _, text := range padding {
		if !saysNothingOpening(text) {
			t.Errorf("padding not seen: %q", text)
		}
	}
	real := []string{
		"",
		"Drunkmoon is an early Linux networking prototype intended to provide virtual Ethernet-like interfaces.",
		"A command-line download/upload speed test for the China Telecom Guangdong network.",
		"mussh is a MUJIN internal utility for helping people connect to controllers over SSH.",
		"Kittiwake is the neighbour's sailing boat, repainted every spring.",
	}
	for _, text := range real {
		if saysNothingOpening(text) {
			t.Errorf("real opening taken as padding: %q", text)
		}
	}
}

func TestThePromptsExampleIsNotAFact(t *testing.T) {
	for _, copied := range []string{
		"The queue consumer is restarted by hand when it dies.",
		"Runs the platform team at Acme.",
		"the consumer died. Restarted it.",
	} {
		if !isPromptExample(copied) {
			t.Errorf("copied from the prompt and not seen: %q", copied)
		}
	}
	if isPromptExample("The queue consumer is restarted by cron every night.") {
		t.Errorf("a real line about a queue consumer is not the example")
	}
}

func TestTwoQuoteMarksAreNoOpening(t *testing.T) {
	for _, text := range []string{`""`, `''`, "“”", "null"} {
		if !saysNothingOpening(text) {
			t.Errorf("%q is the prompt's marker for nothing, not an opening", text)
		}
	}
}

// A month page keeps what the record said and loses what the model
// supposed: the guessing sentences go, a guessing list item goes whole,
// and a heading with nothing left under it goes with them. A count that
// was in the record stays, however thin.
func TestAMonthPageLosesItsGuesses(t *testing.T) {
	page := `## The Portal

The queue consumer was moved into the bridge. This suggests the team was under pressure. It restarts itself now.

## Fleet Manager

Two threads on the 3rd. The involvement likely pertains to planning.

Also:
- Threaded in the pico channel on the 4th.
- Engaged with modex, which indicates preparation for the show.
`
	want := `## The Portal

The queue consumer was moved into the bridge. It restarts itself now.

## Fleet Manager

Two threads on the 3rd.

Also:
- Threaded in the pico channel on the 4th.`
	if got := dropGuesses(page); got != want {
		t.Fatalf("dropGuesses:\n%s\n--- wanted ---\n%s", got, want)
	}
	if got := dropGuesses("## June\n\nEverything here probably happened."); got != "" {
		t.Fatalf("a page that was all guesses should be empty, got %q", got)
	}
}
