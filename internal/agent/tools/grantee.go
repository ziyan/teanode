package tools

import (
	"context"
	"fmt"
	"strings"
)

// Grantee is who a share is for: one person, or every member of one circle.
type Grantee struct {
	// UserID is set for a person and CircleID for a circle, never both.
	UserID   string
	CircleID string

	// Description says who it is in words for a result: the username, or
	// "everyone in" and the circle's name.
	Description string
}

// ResolveGrantee finds who the with argument of a sharing tool names: the
// username of somebody the person shares a circle with, or the id of a circle
// they are in. A username that is nobody's and an id that is no circle of
// theirs are refused alike, so the answer does not say which accounts exist.
func ResolveGrantee(ctx context.Context, operations Operations, with string) (*Grantee, error) {
	with = strings.TrimSpace(with)
	if with == "" {
		return nil, fmt.Errorf("with whom? give 'with': the username of somebody the person shares a circle with (circle_people lists them), or the id of a circle they are in (circle list gives it)")
	}
	var people struct {
		ListCirclePeople []struct {
			UserID   string `json:"userId"`
			Username string `json:"username"`
		} `json:"ListCirclePeople"`
	}
	if err := operations.Execute(ctx, `query { ListCirclePeople { userId username } }`, nil, &people); err != nil {
		return nil, err
	}
	for _, person := range people.ListCirclePeople {
		if strings.EqualFold(person.Username, with) {
			return &Grantee{UserID: person.UserID, Description: person.Username}, nil
		}
	}
	var circles struct {
		ListCircles []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"ListCircles"`
	}
	if err := operations.Execute(ctx, `query { ListCircles { id name } }`, nil, &circles); err != nil {
		return nil, err
	}
	for _, circle := range circles.ListCircles {
		if circle.ID == with {
			return &Grantee{CircleID: circle.ID, Description: "everyone in " + circle.Name}, nil
		}
	}
	return nil, fmt.Errorf("%q is neither the username of somebody the person shares a circle with nor the id of a circle they are in", with)
}

// GranteeWords says who a with argument names, for a confirmation card: the
// username, or "everyone in" and the circle's name, so that a circle's id
// is never what the person is asked to approve. When it cannot be looked up
// the argument is quoted as it was given.
func GranteeWords(ctx context.Context, with string) string {
	run, err := RunFrom(ctx)
	if err != nil {
		return Named(with, "someone")
	}
	grantee, err := ResolveGrantee(ctx, run.Operations(), with)
	if err != nil {
		return Named(with, "someone")
	}
	return grantee.Description
}
