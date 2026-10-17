package models

import (
	"errors"
	"fmt"
	"strings"
)

// ShareablePerson is somebody the signed-in account shares a circle with, as
// far as naming a share's target goes.
type ShareablePerson struct {
	UserID   string
	Username string
}

// ShareableCircle is a circle the signed-in account is in.
type ShareableCircle struct {
	ID          string
	Name        string
	MemberCount int
}

// ShareTarget is who a share is for: one person or one circle, never both.
type ShareTarget struct {
	UserID   string
	CircleID string

	// Description says who it is in a result: the username, or "everyone in"
	// and the circle's name.
	Description string

	// ApprovalDescription is the same for a card somebody approves, where a
	// circle also says how many people it is: "everyone in Family (4 people)".
	ApprovalDescription string
}

// ErrNoShareTarget is what ResolveShareTarget answers when the words name
// nobody and no circle. The caller words the refusal for its own surface,
// because what to run next differs; a name that matches several things is
// worded here, once, so every surface says the same.
var ErrNoShareTarget = errors.New("neither a person nor a circle")

// PeopleCountWords says how many people a circle has: "1 person", "4 people".
func PeopleCountWords(count int) string {
	if count == 1 {
		return "1 person"
	}
	return fmt.Sprintf("%d people", count)
}

// DescribeCircleChoices lists circles by id and size, which is what somebody
// needs to pick one of several that are called the same.
func DescribeCircleChoices(circles []ShareableCircle) string {
	choices := make([]string, 0, len(circles))
	for _, circle := range circles {
		choices = append(choices, fmt.Sprintf("%s (%s)", circle.ID, PeopleCountWords(circle.MemberCount)))
	}
	return strings.Join(choices, ", ")
}

// AmbiguousCircleNameError is the refusal for a circle name that more than
// one circle has.
func AmbiguousCircleNameError(typed string, circles []ShareableCircle) error {
	return fmt.Errorf("%q is the name of more than one circle you are in: %s; give the id of the one you mean",
		typed, DescribeCircleChoices(circles))
}

// ResolveShareTarget finds who typed words name: the id of a circle the
// account is in, the username of somebody it shares a circle with, or the
// name of a circle. Circle names are not unique, so a name that matches more
// than one circle, or that is both a username and a circle's name, is refused
// with the ids and sizes of the circles it could be; an id never is
// ambiguous.
func ResolveShareTarget(typed string, people []ShareablePerson, circles []ShareableCircle) (*ShareTarget, error) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return nil, ErrNoShareTarget
	}
	for _, circle := range circles {
		if circle.ID == typed {
			return circleShareTarget(circle), nil
		}
	}
	var namedPerson *ShareablePerson
	for _, person := range people {
		if strings.EqualFold(person.Username, typed) {
			namedPerson = &person
			break
		}
	}
	var namedCircles []ShareableCircle
	for _, circle := range circles {
		if strings.EqualFold(circle.Name, typed) {
			namedCircles = append(namedCircles, circle)
		}
	}
	switch {
	case namedPerson != nil && len(namedCircles) > 0:
		return nil, fmt.Errorf("%q is both the username of somebody you share a circle with and the name of %s: %s; "+
			"give the circle's id to mean the circle, and rename the circle to name the person",
			typed, circleOrCircles(len(namedCircles)), DescribeCircleChoices(namedCircles))
	case len(namedCircles) > 1:
		return nil, AmbiguousCircleNameError(typed, namedCircles)
	case namedPerson != nil:
		return &ShareTarget{UserID: namedPerson.UserID, Description: namedPerson.Username, ApprovalDescription: namedPerson.Username}, nil
	case len(namedCircles) == 1:
		return circleShareTarget(namedCircles[0]), nil
	}
	return nil, ErrNoShareTarget
}

func circleOrCircles(count int) string {
	if count == 1 {
		return "a circle"
	}
	return "circles"
}

func circleShareTarget(circle ShareableCircle) *ShareTarget {
	return &ShareTarget{
		CircleID:            circle.ID,
		Description:         "everyone in " + circle.Name,
		ApprovalDescription: fmt.Sprintf("everyone in %s (%s)", circle.Name, PeopleCountWords(circle.MemberCount)),
	}
}
