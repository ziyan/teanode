package apigraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/web"
)

// impersonationWorld is a server with an administrator who holds every
// permission, an account manager who holds only user:manage, and a person
// who may manage groups.
type impersonationWorld struct {
	graph         *graph
	authenticator web.Authenticator
	database      db.Database

	administrator, manager, person *models.User
}

func newImpersonationWorld(t *testing.T) *impersonationWorld {
	t.Helper()
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	world := &impersonationWorld{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		account := func(username string, permissions ...models.Permission) *models.User {
			user, err := tx.CreateUser(&models.User{Username: username})
			if err != nil {
				t.Fatalf("CreateUser: %s", err)
			}
			role, err := tx.CreateRole(&models.Role{Name: username + " role", Permissions: permissions})
			if err != nil {
				t.Fatalf("CreateRole: %s", err)
			}
			if _, err := tx.CreateGroup(&models.Group{Name: username + " group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				t.Fatalf("CreateGroup: %s", err)
			}
			return user
		}
		world.administrator = account("fixture-administrator", models.Permissions()...)
		world.manager = account("fixture-manager", models.PermissionUserManage)
		world.person = account("fixture-person", models.PermissionGroupManage)
	})
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "teanode1.key"), []byte("not a real key"), 0o600); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	configuration := config.Example()
	configuration.Server.DataDirectory = directory
	store := config.NewMemoryStore(configuration)
	t.Cleanup(func() { _ = store.Close() })
	if err := config.EnsureSecrets(store); err != nil {
		t.Fatalf("EnsureSecrets: %s", err)
	}
	authenticator, err := web.NewAuthenticator(store, database)
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	component, err := New(database, store, nil, nil, nil, nil, nil, authenticator, nil, &api.Settings{})
	if err != nil {
		t.Fatalf("New: %s", err)
	}
	world.graph, world.authenticator = component.(*graph), authenticator
	return world
}

// ask sends a GraphQL request as the middleware would hand it over, and
// returns the response and its errors.
func (self *impersonationWorld) ask(t *testing.T, query string, username, impersonator string, cookies ...*http.Cookie) (*httptest.ResponseRecorder, []string) {
	t.Helper()
	body, _ := json.Marshal(graphRequest{Query: query})
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(api.AuthenticatedUsernameHeader, username)
	if impersonator != "" {
		request.Header.Set(api.ImpersonatorUsernameHeader, impersonator)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
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
	return response, messages
}

func (self *impersonationWorld) login(t *testing.T, user *models.User) *http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	if err := self.authenticator.StartSession(recorder, httptest.NewRequest(http.MethodPost, "/api/login", nil), user.Username); err != nil {
		t.Fatalf("StartSession: %s", err)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == web.SessionCookieName {
			return cookie
		}
	}
	t.Fatal("no session cookie")
	return nil
}

// Signing in as somebody needs the permission to manage accounts, and every
// permission they hold: an account manager cannot become an administrator by
// signing in as one, and a person who manages groups cannot sign in as
// anybody.
func TestImpersonationNeedsToManageAccountsAndToHoldWhatTheyHold(t *testing.T) {
	world := newImpersonationWorld(t)
	start := func(user *models.User) string {
		return `mutation { StartImpersonation(userId: "` + user.ID + `") { username impersonatorUsername } }`
	}

	_, errors := world.ask(t, start(world.administrator), world.manager.Username, "", world.login(t, world.manager))
	if len(errors) == 0 || !strings.Contains(errors[0], "permissions you do not") {
		t.Fatalf("an account manager is refused an administrator: %v", errors)
	}
	if _, errors := world.ask(t, start(world.manager), world.person.Username, "", world.login(t, world.person)); len(errors) == 0 {
		t.Fatal("a person without user:manage signed in as somebody")
	}

	response, errors := world.ask(t, start(world.person), world.administrator.Username, "", world.login(t, world.administrator))
	if len(errors) != 0 {
		t.Fatalf("the administrator signs in as the person: %v", errors)
	}
	if !strings.Contains(response.Body.String(), `"impersonatorUsername":"fixture-administrator"`) {
		t.Fatalf("the state names the operator: %s", response.Body.String())
	}
	names := map[string]bool{}
	for _, cookie := range response.Result().Cookies() {
		names[cookie.Name] = true
	}
	if !names[web.SessionCookieName] || !names[web.ReturnCookieName] {
		t.Fatalf("both cookies are set: %v", names)
	}
}

// While impersonating, the person's credentials and their agent are off
// limits, and what is done is written under both names.
func TestAnImpersonationIsRefusedCredentialsAndIsAudited(t *testing.T) {
	world := newImpersonationWorld(t)
	for _, refused := range []string{
		`mutation { CreateToken(name: "kept") { secret } }`,
		`mutation { RevokeAllSessions { username } }`,
		`mutation { StartImpersonation(userId: "` + world.manager.ID + `") { username } }`,
	} {
		_, errors := world.ask(t, refused, world.person.Username, world.administrator.Username)
		if len(errors) == 0 || !strings.Contains(errors[0], "signed in as somebody else") {
			t.Fatalf("%s is refused while impersonating: %v", refused, errors)
		}
	}

	if _, errors := world.ask(t, `mutation { CreateGroup(name: "Visited") { id } }`, world.person.Username, world.administrator.Username); len(errors) != 0 {
		t.Fatalf("an ordinary change goes through: %v", errors)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourceGroup), Limit: 10})
		if err != nil {
			t.Fatalf("ListAuditEvents: %s", err)
		}
		for _, event := range events {
			if event.Action == models.AuditActionCreate && strings.Contains(string(event.After), "Visited") {
				if event.ActorUserID != world.person.ID || event.ImpersonatorUserID != world.administrator.ID {
					t.Fatalf("the row names the person and the operator: %+v", event)
				}
				return
			}
		}
		t.Fatal("no audit row for the group")
	})
}
