package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// Grantee is who a share is for: one person, or every member of one circle.
type Grantee struct {
	// UserID is set for a person and CircleID for a circle, never both.
	UserID   string
	CircleID string

	// Description says who it is in words for a result: the username, or
	// "everyone in" and the circle's name.
	Description string

	// ApprovalDescription is the same for a card the person approves, where a
	// circle also says how many people it is.
	ApprovalDescription string
}

// ResolveGrantee finds who the with argument of a sharing tool names: the
// username of somebody the person shares a circle with, or a circle they are
// in by its id or its name. Circle names are not unique, so a name more than
// one circle has, or that is also a username, is refused with the ids and
// sizes of the circles it could be. A username that is nobody's and a circle
// that is not theirs are refused alike, so the answer does not say which
// accounts exist.
func ResolveGrantee(ctx context.Context, operations Operations, with string) (*Grantee, error) {
	with = strings.TrimSpace(with)
	if with == "" {
		return nil, fmt.Errorf("with whom? give 'with': the username of somebody the person shares a circle with (circle_people lists them), or a circle they are in by its name or id (circle list gives them)")
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
	var circles struct {
		ListCircles []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Members []struct {
				UserID string `json:"userId"`
			} `json:"members"`
		} `json:"ListCircles"`
	}
	if err := operations.Execute(ctx, `query { ListCircles { id name members { userId } } }`, nil, &circles); err != nil {
		return nil, err
	}
	shareablePeople := make([]models.ShareablePerson, 0, len(people.ListCirclePeople))
	for _, person := range people.ListCirclePeople {
		shareablePeople = append(shareablePeople, models.ShareablePerson{UserID: person.UserID, Username: person.Username})
	}
	shareableCircles := make([]models.ShareableCircle, 0, len(circles.ListCircles))
	for _, circle := range circles.ListCircles {
		shareableCircles = append(shareableCircles, models.ShareableCircle{ID: circle.ID, Name: circle.Name, MemberCount: len(circle.Members)})
	}
	target, err := models.ResolveShareTarget(with, shareablePeople, shareableCircles)
	if errors.Is(err, models.ErrNoShareTarget) {
		return nil, fmt.Errorf("%q is neither the username of somebody the person shares a circle with nor the name or id of a circle they are in", with)
	}
	if err != nil {
		return nil, err
	}
	return &Grantee{UserID: target.UserID, CircleID: target.CircleID, Description: target.Description, ApprovalDescription: target.ApprovalDescription}, nil
}

// GranteeWords says who a with argument names, for a confirmation card: the
// username, or "everyone in", the circle's name and how many people it is, so
// that a circle's id is never what the person is asked to approve. When it
// cannot be looked up the argument is quoted as it was given.
func GranteeWords(ctx context.Context, with string) string {
	run, err := RunFrom(ctx)
	if err != nil {
		return Named(with, "someone")
	}
	grantee, err := ResolveGrantee(ctx, run.Operations(), with)
	if err != nil {
		return Named(with, "someone")
	}
	return grantee.ApprovalDescription
}
