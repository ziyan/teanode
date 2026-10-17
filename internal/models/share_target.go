package models

import (
	"errors"
	"fmt"
	"strconv"
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

	// Description says who it is, in a result and on a card somebody approves:
	// the username, or the circle with its name in quotes and how many people
	// it is, `the circle "Family" (4 people)`.
	Description string
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

// DescribeCircle says which circle a result or a card means: the circle, with
// its name in quotes, so that a name typed by whoever made the circle cannot
// read as part of the sentence, and how many people it is.
func DescribeCircle(name string, memberCount int) string {
	return fmt.Sprintf("%s (%s)", DescribeCircleByName(name), PeopleCountWords(memberCount))
}

// DescribeCircleByName is DescribeCircle for a line that does not know the
// circle's size, such as a share in a list.
func DescribeCircleByName(name string) string {
	return "the circle " + strconv.Quote(name)
}

// ResolveShareTarget finds who typed words name: the id of a circle the
// account is in, the username of somebody it shares a circle with, or the
// name of a circle. A username of somebody the account may share with wins
// over a circle of the same name, so that whoever names a circle cannot take
// a share meant for a person; the circle is still reached by its id. Circle
// names are not unique, so a name that is nobody's username and matches more
// than one circle is refused with the ids and sizes of the circles it could
// be; an id never is ambiguous.
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
	for _, person := range people {
		if strings.EqualFold(person.Username, typed) {
			return &ShareTarget{UserID: person.UserID, Description: person.Username}, nil
		}
	}
	var namedCircles []ShareableCircle
	for _, circle := range circles {
		if strings.EqualFold(circle.Name, typed) {
			namedCircles = append(namedCircles, circle)
		}
	}
	switch len(namedCircles) {
	case 0:
		return nil, ErrNoShareTarget
	case 1:
		return circleShareTarget(namedCircles[0]), nil
	}
	return nil, AmbiguousCircleNameError(typed, namedCircles)
}

func circleShareTarget(circle ShareableCircle) *ShareTarget {
	return &ShareTarget{CircleID: circle.ID, Description: DescribeCircle(circle.Name, circle.MemberCount)}
}
