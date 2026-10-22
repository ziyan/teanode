package apigraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// askAs sends a GraphQL request as the middleware would hand it over: the
// username header it establishes, plus the credential the caller really sent.
func (self *impersonationWorld) askAs(t *testing.T, query string, user *models.User, cookie *http.Cookie, bearer string) []string {
	t.Helper()
	body, _ := json.Marshal(graphRequest{Query: query})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-TeaNode-Authenticated-User", user.Username)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	self.graph.graphView(response, request)
	var outcome struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &outcome)
	var messages []string
	for _, failure := range outcome.Errors {
		messages = append(messages, failure.Message)
	}
	return messages
}

func (self *impersonationWorld) tokenWorks(token string) bool {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/graphql", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	_, ok := self.authenticator.AuthenticateIdentity(request)
	return ok
}

// Changing a password or signing out everywhere has to end the tokens too: the
// phone app, the extension, the command line and the computer daemons hold a
// token, not a session, so ending sessions alone leaves a stolen device in.
// The token making the call is the one kept, so the caller still gets an
// answer.
func TestEndingSessionsAlsoEndsTokens(t *testing.T) {
	world := newImpersonationWorld(t)
	person := world.person

	const password = "a-password-for-a-test"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %s", err)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.UpdateUser(person.ID, func(user *models.User) error {
			user.PasswordHash = string(hash)
			return nil
		}); err != nil {
			t.Fatalf("UpdateUser: %s", err)
		}
	})

	issue := func() string {
		_, value, err := world.authenticator.IssueToken(person.Username, "fixture", time.Hour)
		if err != nil {
			t.Fatalf("IssueToken: %s", err)
		}
		return value
	}
	// Each call changes the password to the same one, so the next can use it.
	queries := map[string]string{
		"a password change":      `mutation { ChangePassword(currentPassword: "` + password + `", newPassword: "` + password + `") { username } }`,
		"signing out everywhere": `mutation { RevokeAllSessions { authenticationRequired } }`,
	}

	for name, query := range queries {
		t.Run(name+" from the dashboard ends every token", func(t *testing.T) {
			first, second := issue(), issue()
			if errors := world.askAs(t, query, person, world.login(t, person), ""); len(errors) != 0 {
				t.Fatalf("the call failed: %v", errors)
			}
			if world.tokenWorks(first) || world.tokenWorks(second) {
				t.Fatal("a token survived")
			}
		})
		t.Run(name+" from a token keeps only that token", func(t *testing.T) {
			caller, other := issue(), issue()
			if errors := world.askAs(t, query, person, nil, caller); len(errors) != 0 {
				t.Fatalf("the call failed: %v", errors)
			}
			if !world.tokenWorks(caller) {
				t.Fatal("the caller's own token was cut off mid-call")
			}
			if world.tokenWorks(other) {
				t.Fatal("another token survived")
			}
		})
	}
}
