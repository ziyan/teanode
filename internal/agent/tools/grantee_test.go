package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// peopleAndCircles answers the two lookups a grantee is resolved from.
type peopleAndCircles struct {
	people  string
	circles string
}

func (self *peopleAndCircles) Permissions() *models.EffectivePermissions { return nil }

func (self *peopleAndCircles) Execute(ctx context.Context, document string, variables map[string]any, result any) error {
	answer := `{}`
	switch {
	case strings.Contains(document, "ListCirclePeople"):
		answer = `{"ListCirclePeople":` + self.people + `}`
	case strings.Contains(document, "ListCircles"):
		answer = `{"ListCircles":` + self.circles + `}`
	}
	return json.Unmarshal([]byte(answer), result)
}

func homeAndBen() *peopleAndCircles {
	return &peopleAndCircles{
		people:  `[{"userId":"user-ben","username":"ben"}]`,
		circles: `[{"id":"circle-home","name":"Home","members":[{"userId":"user-ben"},{"userId":"user-me"},{"userId":"user-ada"},{"userId":"user-cy"}]}]`,
	}
}

// A share is for a username among the people the person shares a circle with,
// or a circle they are in by its id or its name.
func TestAGranteeIsAUsernameOrACircle(test *testing.T) {
	operations := homeAndBen()
	person, err := ResolveGrantee(context.Background(), operations, " Ben ")
	if err != nil || person.UserID != "user-ben" || person.CircleID != "" || person.Description != "ben" {
		test.Fatalf("a person is found by username in any case: %+v %v", person, err)
	}
	for _, with := range []string{"circle-home", "home"} {
		circle, err := ResolveGrantee(context.Background(), operations, with)
		if err != nil || circle.CircleID != "circle-home" || circle.UserID != "" {
			test.Fatalf("a circle is found by %q: %+v %v", with, circle, err)
		}
		if circle.Description != `the circle "Home" (4 people)` {
			test.Errorf("a result and a card say which circle and how many people it is: %q", circle.Description)
		}
	}
	for _, with := range []string{"", "stranger"} {
		if _, err := ResolveGrantee(context.Background(), operations, with); err == nil {
			test.Errorf("%q is neither a username nor a circle", with)
		}
	}
}

// Circle names are not unique: a name two circles have is refused with the
// ids and sizes to choose from, an id still names exactly one, and a username
// wins over a circle of the same name, which is reached by its id.
func TestACircleNameThatIsNotUniqueIsRefused(test *testing.T) {
	operations := &peopleAndCircles{
		people: `[{"userId":"user-ben","username":"ben"}]`,
		circles: `[{"id":"circle-one","name":"Family","members":[{"userId":"a"},{"userId":"b"}]},` +
			`{"id":"circle-two","name":"family","members":[{"userId":"a"}]},` +
			`{"id":"circle-ben","name":"Ben","members":[{"userId":"a"},{"userId":"b"},{"userId":"c"}]}]`,
	}
	_, err := ResolveGrantee(context.Background(), operations, "FAMILY")
	if err == nil || !strings.Contains(err.Error(), "circle-one (2 people)") || !strings.Contains(err.Error(), "circle-two (1 person)") {
		test.Fatalf("two circles with one name are listed with their sizes: %v", err)
	}
	person, err := ResolveGrantee(context.Background(), operations, "ben")
	if err != nil || person.UserID != "user-ben" || person.CircleID != "" {
		test.Fatalf("a username wins over a circle of that name: %+v %v", person, err)
	}
	circle, err := ResolveGrantee(context.Background(), operations, "circle-two")
	if err != nil || circle.CircleID != "circle-two" {
		test.Fatalf("an id is never ambiguous: %+v %v", circle, err)
	}
	circle, err = ResolveGrantee(context.Background(), operations, "circle-ben")
	if err != nil || circle.CircleID != "circle-ben" {
		test.Fatalf("the circle named like a person is reached by its id: %+v %v", circle, err)
	}
}
