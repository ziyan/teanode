package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// peopleAndCircles answers the two lookups a grantee is resolved from.
type peopleAndCircles struct{}

func (self *peopleAndCircles) Permissions() *models.EffectivePermissions { return nil }

func (self *peopleAndCircles) Execute(ctx context.Context, document string, variables map[string]any, result any) error {
	answer := `{}`
	switch {
	case strings.Contains(document, "ListCirclePeople"):
		answer = `{"ListCirclePeople":[{"userId":"user-ben","username":"ben"}]}`
	case strings.Contains(document, "ListCircles"):
		answer = `{"ListCircles":[{"id":"circle-home","name":"Home"}]}`
	}
	return json.Unmarshal([]byte(answer), result)
}

// A share is for a username among the people the person shares a circle with,
// or the id of one of their circles: never a circle's name, which could be the
// same as somebody's username.
func TestAGranteeIsAUsernameOrACircleID(test *testing.T) {
	operations := &peopleAndCircles{}
	person, err := ResolveGrantee(context.Background(), operations, " Ben ")
	if err != nil || person.UserID != "user-ben" || person.CircleID != "" || person.Description != "ben" {
		test.Fatalf("a person is found by username in any case: %+v %v", person, err)
	}
	circle, err := ResolveGrantee(context.Background(), operations, "circle-home")
	if err != nil || circle.CircleID != "circle-home" || circle.UserID != "" || circle.Description != "everyone in Home" {
		test.Fatalf("a circle is found by its id: %+v %v", circle, err)
	}
	for _, with := range []string{"", "Home", "stranger"} {
		if _, err := ResolveGrantee(context.Background(), operations, with); err == nil {
			test.Errorf("%q is neither a username nor a circle id", with)
		}
	}
}
