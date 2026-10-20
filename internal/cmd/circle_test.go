package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/models"
)

// Circles are on the API, the command line and the agent's circle tool under
// the same names: nothing exists on one that the others lack. Putting an
// account in a circle without an invitation is the operator's, and the tool
// offers it only to somebody who manages accounts.
// The dashboard and the iPhone app call the same operations.
func TestCircleParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.CircleQuery](), reflect.TypeFor[apigraph.CircleMutation](), reflect.TypeFor[apigraph.PersonCardQuery]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	// operation, then the subcommands of teanode circle that do it and the
	// actions of the agent's circle tool, which are named as the subcommands
	// are.
	table := map[string]struct {
		subcommands []string
		actions     []string
	}{
		"ListCircles":              {[]string{"list"}, []string{"list"}},
		"ReadCircle":               {[]string{"show"}, []string{"show"}},
		"ListCircleInvitations":    {[]string{"invitations"}, []string{"list"}},
		"ListCirclePeople":         {[]string{"people"}, []string{"people"}},
		"ReadPersonCard":           {[]string{"person"}, []string{"person"}},
		"CreateCircle":             {[]string{"create"}, []string{"create"}},
		"RenameCircle":             {[]string{"rename"}, []string{"rename"}},
		"DeleteCircle":             {[]string{"delete"}, []string{"delete"}},
		"InviteToCircle":           {[]string{"invite"}, []string{"invite"}},
		"AnswerCircleInvitation":   {[]string{"accept", "decline"}, []string{"accept", "decline"}},
		"WithdrawCircleInvitation": {[]string{"withdraw"}, []string{"withdraw"}},
		"SetCircleMemberRole":      {[]string{"role"}, []string{"role"}},
		"RemoveCircleMember":       {[]string{"remove"}, []string{"remove"}},
		"LeaveCircle":              {[]string{"leave"}, []string{"leave"}},
		"AddCircleMember":          {[]string{"add"}, []string{"add"}},

		// What somebody is to the person: the words are listed by
		// "teanode circle relate --help" and offered by the tool's
		// relationship argument, both from models.RelationshipTerms.
		"ListRelationshipTerms":    {nil, nil},
		"SetPersonRelationship":    {[]string{"relate"}, []string{"relate"}},
		"RemovePersonRelationship": {[]string{"unrelate"}, []string{"unrelate"}},
	}

	commands := map[string]bool{}
	for _, subcommand := range NewCircleCommand().Commands {
		commands[subcommand.Name] = true
	}
	circleActions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "circle" {
			continue
		}
		properties, _ := tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				circleActions[name] = true
			}
		case []any:
			for _, name := range enum {
				circleActions[name.(string)] = true
			}
		}
	}
	if len(circleActions) == 0 {
		test.Fatal("the agent has no circle tool")
	}

	listed := 0
	toolActions := map[string]bool{}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		for _, subcommand := range names.subcommands {
			listed++
			if !commands[subcommand] {
				test.Errorf("teanode circle has no %s", subcommand)
			}
		}
		for _, action := range names.actions {
			toolActions[action] = true
			if !circleActions[action] {
				test.Errorf("the circle tool has no action %s", action)
			}
		}
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}
	if len(commands) != listed {
		test.Errorf("teanode circle has %d subcommands, the table lists %d", len(commands), listed)
	}
	for action := range circleActions {
		if !toolActions[action] {
			test.Errorf("the circle tool has the action %s, which the table does not list", action)
		}
	}
}

// Joining a family circle points at the commands that share the calendar and
// where the person is, colleagues at the calendar's, and friends at nothing.
func TestCircleSharingHint(test *testing.T) {
	test.Parallel()

	for circleKind, wanted := range map[string]string{
		"family":     "you can share your calendar and where you are with it, if your account may use them: teanode calendar share, teanode location share",
		"colleagues": "you can share your calendar with it, if your account may use it: teanode calendar share",
		"friends":    "",
		"":           "",
	} {
		if got := circleSharingHint(circleKind); got != wanted {
			test.Errorf("%q says %q, want %q", circleKind, got, wanted)
		}
	}
}

// relationshipServer answers the documents the relationship commands send:
// robin is the caller, described as pronoun, and shares a circle with sam
// (whom robin calls father) and alex (nothing said). Each document is
// recorded by operation name.
func relationshipServer(test *testing.T, pronoun string) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	answers := map[string]string{
		"ListCirclePeople": `[
			{"userId":"u-sam","username":"sam","name":"Sam Lee","memberRole":"","joinedAt":"2030-01-01T00:00:00Z","relationshipKind":"parent","relationshipTermGender":"masculine","relationshipWord":"father"},
			{"userId":"u-alex","username":"alex","name":"","memberRole":"","joinedAt":"2030-01-01T00:00:00Z","relationshipKind":"","relationshipTermGender":"","relationshipWord":""}]`,
		"SetPersonRelationship":    `{"userId":"u-sam","username":"sam","name":"Sam Lee","relationshipKind":"parent","relationshipTermGender":"masculine","relationshipWord":"father"}`,
		"RemovePersonRelationship": `true`,
		"GetCurrentUser":           `{"id":"u-robin","username":"robin","name":"Robin Park","pronoun":"` + pronoun + `"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		lock.Lock()
		defer lock.Unlock()
		for operation, answer := range answers {
			if strings.Contains(document.Query, operation) {
				calls[operation] = document.Variables
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"data":{"` + operation + `":` + answer + `}}`))
				return
			}
		}
		response.WriteHeader(http.StatusBadRequest)
	}))
	test.Cleanup(server.Close)
	return server, func(operation string) map[string]any {
		lock.Lock()
		defer lock.Unlock()
		return calls[operation]
	}
}

func runCircleAgainst(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewCircleCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "circle"}, arguments...))
	})
	return output, err
}

// "circle relate" finds the account among the people the caller shares a
// circle with, sends the word as typed, and says what each is to the other,
// the other's word for the caller worded by the caller's own pronoun.
func TestCircleRelateSaysWhatThePersonAndTheOtherAreToEachOther(test *testing.T) {
	for pronoun, wanted := range map[string]string{
		"she": "Sam Lee is your father; they will see you as their daughter.\n",
		"he":  "Sam Lee is your father; they will see you as their son.\n",
		"":    "Sam Lee is your father; they will see you as their child.\n",
	} {
		server, recorded := relationshipServer(test, pronoun)
		output, err := runCircleAgainst(test, server, "relate", "SAM", "father")
		if err != nil {
			test.Fatal(err)
		}
		if output != wanted {
			test.Errorf("with the pronoun %q it printed %q, want %q", pronoun, output, wanted)
		}
		if variables := recorded("SetPersonRelationship"); variables["userId"] != "u-sam" || variables["relationshipTerm"] != "father" {
			test.Errorf("the relationship was set with %v", variables)
		}
	}
}

// With --json it prints the person as the server answered, and no sentence.
func TestCircleRelateWithJSONPrintsThePerson(test *testing.T) {
	server, _ := relationshipServer(test, "she")
	output, err := runCircleAgainst(test, server, "relate", "sam", "father", "--json")
	if err != nil {
		test.Fatal(err)
	}
	var person map[string]any
	if err := json.Unmarshal([]byte(output), &person); err != nil || person["username"] != "sam" || person["relationshipWord"] != "father" {
		test.Errorf("it printed %q: %v", output, err)
	}
}

// Somebody who shares no circle with the caller is refused by name, before
// any relationship is set, and the arguments are both needed.
func TestCircleRelateRefusesAStrangerAndMissingArguments(test *testing.T) {
	server, recorded := relationshipServer(test, "")
	if _, err := runCircleAgainst(test, server, "relate", "stranger", "father"); err == nil || !strings.Contains(err.Error(), "stranger") {
		test.Errorf("a stranger was not refused by name: %v", err)
	}
	if _, err := runCircleAgainst(test, server, "relate", "sam"); err == nil || !strings.Contains(err.Error(), "usage: teanode circle relate") {
		test.Errorf("a missing relationship was not refused with the usage: %v", err)
	}
	if recorded("SetPersonRelationship") != nil {
		test.Error("a relationship was set")
	}
}

// "circle unrelate" removes it by account id and says whether there was one.
func TestCircleUnrelateTakesAwayWhatWasSaid(test *testing.T) {
	server, recorded := relationshipServer(test, "")
	output, err := runCircleAgainst(test, server, "unrelate", "sam")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "took away how you and sam are related") {
		test.Errorf("it printed %q", output)
	}
	if variables := recorded("RemovePersonRelationship"); variables["userId"] != "u-sam" {
		test.Errorf("the removal was %v", variables)
	}
}

// The people table has a Relationship column: the word, blank for somebody
// nothing was said about.
func TestCirclePeopleShowsWhatEachIsToThePerson(test *testing.T) {
	server, _ := relationshipServer(test, "")
	output, err := runCircleAgainst(test, server, "people")
	if err != nil {
		test.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 || strings.Fields(lines[0])[2] != "RELATIONSHIP" {
		test.Fatalf("the table is %q", output)
	}
	if !strings.Contains(lines[1], "Sam Lee") || !strings.HasSuffix(lines[1], "father") || strings.Contains(lines[2], "father") {
		test.Errorf("the rows are %q", lines[1:])
	}
}

// Every word the server offers is in the help of "circle relate", which is
// built from the same list in models.
func TestCircleRelateOffersEveryTerm(test *testing.T) {
	help := relationshipTermsHelp()
	for _, term := range models.RelationshipTerms() {
		if !strings.Contains(help, term.Term) {
			test.Errorf("circle relate --help does not list %q", term.Term)
		}
	}
	for _, name := range []string{"wife", "father_in_law", "aunt_or_uncle"} {
		if _, err := models.ParseRelationshipTerm(name); err != nil {
			test.Errorf("%q is listed but not understood: %s", name, err)
		}
	}
}

// "user update --pronoun" sends he, she or they, and none sends the empty
// word that clears it; anything else is refused before the server is asked.
func TestUserUpdatePronoun(test *testing.T) {
	for given, wanted := range map[string]string{"she": "she", "He": "he", "they": "they", "none": ""} {
		var sent map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			var document struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			_ = json.NewDecoder(request.Body).Decode(&document)
			response.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(document.Query, "GetCurrentUser"):
				_, _ = response.Write([]byte(`{"data":{"GetCurrentUser":{"id":"u-robin","username":"robin"}}}`))
			case strings.Contains(document.Query, "UpdateUser"):
				sent = document.Variables
				_, _ = response.Write([]byte(`{"data":{"UpdateUser":{"id":"u-robin","username":"robin"}}}`))
			default:
				response.WriteHeader(http.StatusBadRequest)
			}
		}))
		command := &cli.Command{
			Name:     "fixture",
			Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
			Commands: []*cli.Command{NewUserCommand()},
		}
		var err error
		output := captureStandardOutput(test, func() {
			err = command.Run(test.Context(), []string{"fixture", "user", "update", "robin", "--pronoun", given})
		})
		server.Close()
		if err != nil {
			test.Fatal(err)
		}
		if pronoun, isSent := sent["pronoun"]; !isSent || pronoun != wanted || sent["userId"] != "u-robin" {
			test.Errorf("--pronoun %s sent %v", given, sent)
		}
		if output != "changed robin\n" {
			test.Errorf("it printed %q", output)
		}
	}

	if _, err := pronounFlag("hers"); err == nil || !strings.Contains(err.Error(), "he, she, they or none") {
		test.Errorf("hers was not refused: %v", err)
	}
}
