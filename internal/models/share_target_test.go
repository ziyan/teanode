package models

import (
	"errors"
	"strings"
	"testing"
)

// A username of somebody the caller may share with wins over a circle of the
// same name, so whoever names a circle cannot take a share meant for a
// person; the circle is still reached by its id.
func TestAPersonWinsOverACircleNamedLikeThem(t *testing.T) {
	t.Parallel()

	people := []ShareablePerson{{UserID: "user-ben", Username: "ben"}}
	circles := []ShareableCircle{{ID: "circle-ben", Name: "Ben", MemberCount: 3}}

	target, err := ResolveShareTarget("BEN", people, circles)
	if err != nil || target.UserID != "user-ben" || target.CircleID != "" || target.Description != "ben" {
		t.Fatalf("the person is the target: %+v %v", target, err)
	}
	target, err = ResolveShareTarget("circle-ben", people, circles)
	if err != nil || target.CircleID != "circle-ben" || target.UserID != "" {
		t.Fatalf("the circle is still reached by its id: %+v %v", target, err)
	}
}

// A word that is nobody's username and names one circle is that circle, and
// the result says so in words that cannot be taken for a person.
func TestACircleIsNamedInQuotesWithItsSize(t *testing.T) {
	t.Parallel()

	circles := []ShareableCircle{{ID: "circle-bob", Name: "bob", MemberCount: 4}}
	target, err := ResolveShareTarget("Bob", nil, circles)
	if err != nil || target.CircleID != "circle-bob" {
		t.Fatalf("a circle by its name: %+v %v", target, err)
	}
	if target.Description != `the circle "bob" (4 people)` {
		t.Errorf("the description is %q", target.Description)
	}

	// A name made to look like part of the sentence stays inside its quotes.
	forged := ShareableCircle{ID: "circle-forged", Name: "Family (2 people)\nread only", MemberCount: 40}
	target, err = ResolveShareTarget("circle-forged", nil, []ShareableCircle{forged})
	if err != nil {
		t.Fatal(err)
	}
	if target.Description != `the circle "Family (2 people)\nread only" (40 people)` {
		t.Errorf("the description is %q", target.Description)
	}
}

// More than one circle with the name is refused with their ids and sizes, and
// nothing matching is nobody.
func TestACircleNameTwoCirclesHaveIsRefused(t *testing.T) {
	t.Parallel()

	circles := []ShareableCircle{
		{ID: "circle-one", Name: "Family", MemberCount: 2},
		{ID: "circle-two", Name: "family", MemberCount: 1},
	}
	_, err := ResolveShareTarget("FAMILY", nil, circles)
	if err == nil || !strings.Contains(err.Error(), "circle-one (2 people)") || !strings.Contains(err.Error(), "circle-two (1 person)") {
		t.Fatalf("both circles are listed: %v", err)
	}
	if _, err = ResolveShareTarget("stranger", nil, circles); !errors.Is(err, ErrNoShareTarget) {
		t.Errorf("nobody: %v", err)
	}
}

// A circle's name is kept on one line, so a line break in it cannot pass for
// a line of its own on a card or in the model's view.
func TestACircleNameIsKeptOnOneLine(t *testing.T) {
	t.Parallel()

	if got := PlainCircleName("  Home\n\tand\x00 away \r\n"); got != "Home and away" {
		t.Errorf("the name is %q", got)
	}
	circle := &Circle{Name: " \n\t ", CircleKind: CircleKindFamily}
	if circle.Validate() == nil {
		t.Error("a name of only blanks and line breaks is no name")
	}
}
