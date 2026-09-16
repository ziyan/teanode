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
