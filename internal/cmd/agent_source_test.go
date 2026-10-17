package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// The reminders list is a calendar too. "allow calendar" with no name means the
// person's diary, and asks which only when there are two of those.
func TestChoosingACalendarWithNoNameLeavesTheRemindersListOut(test *testing.T) {
	test.Parallel()

	diary := &client.AgentCollection{ID: "c1", Name: "Calendar", Kind: "calendar", CalendarKind: "events"}
	family := &client.AgentCollection{ID: "c2", Name: "Family", Kind: "calendar", CalendarKind: "events"}
	reminders := &client.AgentCollection{ID: "c3", Name: "Reminders", Kind: "calendar", CalendarKind: "reminders"}
	book := &client.AgentCollection{ID: "b1", Name: "Contacts", Kind: "addressBook"}
	older := &client.AgentCollection{ID: "c4", Name: "Calendar", Kind: "calendar"}

	for name, example := range map[string]struct {
		collections []*client.AgentCollection
		kind, named string
		wantID      string
		wantError   string
	}{
		"a diary and a reminders list":      {[]*client.AgentCollection{diary, reminders, book}, "calendar", "", "c1", ""},
		"the reminders list first":          {[]*client.AgentCollection{reminders, diary}, "calendar", "", "c1", ""},
		"two diaries":                       {[]*client.AgentCollection{diary, family, reminders}, "calendar", "", "", "more than one (Calendar, Family)"},
		"two diaries, one named":            {[]*client.AgentCollection{diary, family, reminders}, "calendar", "family", "c2", ""},
		"the reminders list by name":        {[]*client.AgentCollection{diary, reminders}, "calendar", "Reminders", "c3", ""},
		"only a reminders list":             {[]*client.AgentCollection{reminders, book}, "calendar", "", "c3", ""},
		"a server that does not say a kind": {[]*client.AgentCollection{older}, "calendar", "", "c4", ""},
		"an address book":                   {[]*client.AgentCollection{diary, reminders, book}, "addressBook", "", "b1", ""},
		"no calendar":                       {[]*client.AgentCollection{book}, "calendar", "", "", "no calendar"},
		"no such name":                      {[]*client.AgentCollection{diary}, "calendar", "Work", "", `no calendar called "Work"`},
	} {
		found, err := chooseCollection(example.collections, example.kind, example.named)
		switch {
		case example.wantError != "":
			if err == nil || !strings.Contains(err.Error(), example.wantError) {
				test.Errorf("%s: the error is %v, want one with %q", name, err, example.wantError)
			}
		case err != nil || found.ID != example.wantID:
			test.Errorf("%s: chose %+v, %v; want %s", name, found, err, example.wantID)
		}
	}
}

// "agent source allow calendar" on a person who has a reminders list gives
// the agent the diary, in one call, and does not ask which.
func TestAllowingTheCalendarWhenThereIsARemindersListGrantsTheDiary(test *testing.T) {
	test.Parallel()

	view := `{"agent":{"id":"a1","name":"Agent","enabled":true},"sources":[],"collections":[` +
		`{"id":"reminders-id","name":"Reminders","kind":"calendar","calendarKind":"reminders","granted":false,"items":3},` +
		`{"id":"diary-id","name":"Calendar","kind":"calendar","calendarKind":"events","granted":false,"items":12}]}`
	var granted map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		if strings.Contains(body.Query, "GrantAgentSource") {
			granted = body.Variables
			_, _ = response.Write([]byte(`{"data":{"GrantAgentSource":` + view + `}}`))
			return
		}
		_, _ = response.Write([]byte(`{"data":{"ReadAgent":` + view + `}}`))
	}))
	defer server.Close()
	connection, err := client.New(client.Options{URL: server.URL, Token: "tnt_x"})
	if err != nil {
		test.Fatal(err)
	}
	allow := func(arguments ...string) error {
		command := &cli.Command{Name: "allow", Writer: &bytes.Buffer{}, Action: func(ctx context.Context, command *cli.Command) error {
			return grantCollection(ctx, command, connection, true)
		}}
		return command.Run(test.Context(), append([]string{"allow"}, arguments...))
	}
	if err := allow("calendar"); err != nil {
		test.Fatalf("allow calendar: %s", err)
	}
	if granted["id"] != "diary-id" || granted["kind"] != "calendar" || granted["granted"] != true {
		test.Errorf("the agent was given %v; want the diary", granted)
	}
	// The reminders list is still there to be named.
	if err := allow("calendar", "Reminders"); err != nil || granted["id"] != "reminders-id" {
		test.Errorf("allow calendar Reminders: %v, gave %v", err, granted)
	}
	if err := allow("diary"); err == nil || !strings.Contains(err.Error(), "calendar or addressbook") {
		test.Errorf("allow diary answers %v", err)
	}
}
