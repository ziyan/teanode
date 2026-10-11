package apigraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/api/v1api/apioauth"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/web"
)

// signInWorld is a server with one person, its OAuth endpoints and its graph,
// for following a program from registering to holding a token.
type signInWorld struct {
	graph         *graph
	authenticator web.Authenticator
	database      db.Database
	router        *mux.Router
	person        *models.User

	// host is what the dashboard was reached as: loopback unless a test
	// says otherwise, since a connect code is made only over https or on
	// the server's own machine.
	host string
}

func newSignInWorld(t *testing.T) *signInWorld {
	t.Helper()
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	world := &signInWorld{host: "127.0.0.1:10081", database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "fixture-pairing-person"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		world.person = user
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
	worker := agent.New(&agent.Settings{Database: database, Configuration: store.Current, Instance: "test", Tick: time.Hour})
	component, err := New(database, store, nil, nil, nil, nil, nil, authenticator, nil, &api.Settings{Agent: worker})
	if err != nil {
		t.Fatalf("New: %s", err)
	}
	discovery, err := apioauth.New(store, database, authenticator)
	if err != nil {
		t.Fatalf("apioauth.New: %s", err)
	}
	world.router = mux.NewRouter()
	if err := discovery.AddRoutes(world.router); err != nil {
		t.Fatalf("AddRoutes: %s", err)
	}
	world.graph, world.authenticator = component.(*graph), authenticator
	return world
}

// post sends a form to an OAuth endpoint and reads the JSON answer.
func (self *signInWorld) post(t *testing.T, path string, form url.Values) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "192.0.2.10:4000"
	response := httptest.NewRecorder()
	self.router.ServeHTTP(response, request)
	answer := map[string]any{}
	_ = json.Unmarshal(response.Body.Bytes(), &answer)
	return response.Code, answer
}

// register registers a program with these return addresses.
func (self *signInWorld) register(t *testing.T, redirects ...string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"client_name": "a test app", "redirect_uris": redirects})
	request := httptest.NewRequest(http.MethodPost, api.PathOAuthRegister, strings.NewReader(string(body)))
	request.RemoteAddr = "192.0.2.10:4000"
	response := httptest.NewRecorder()
	self.router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("registering %v: %d %s", redirects, response.Code, response.Body.String())
	}
	var registered struct {
		ClientID string `json:"client_id"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &registered)
	return registered.ClientID
}

// ask sends GraphQL as the signed-in person and returns the data and the
// error messages.
func (self *signInWorld) ask(t *testing.T, query string) (map[string]any, []string) {
	t.Helper()
	body, _ := json.Marshal(graphRequest{Query: query})
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(api.AuthenticatedUsernameHeader, self.person.Username)
	request.Host = self.host
	response := httptest.NewRecorder()
	self.graph.graphView(response, request)
	var outcome struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &outcome)
	var messages []string
	for _, failure := range outcome.Errors {
		messages = append(messages, failure.Message)
	}
	return outcome.Data, messages
}

// worksOnTheAPI says whether a token is taken at the GraphQL endpoint.
func (self *signInWorld) worksOnTheAPI(token string) bool {
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	username, ok := self.authenticator.Authenticate(request)
	return ok && username == self.person.Username
}

// connectCode makes a connect code as the person and returns the secret the
// link carries.
func (self *signInWorld) connectCode(t *testing.T) string {
	t.Helper()
	data, errors := self.ask(t, `mutation { CreateOAuthConnectCode { connectLink qrCodeSvg expiresAt } }`)
	if len(errors) > 0 {
		t.Fatalf("CreateOAuthConnectCode: %v", errors)
	}
	made, _ := data["CreateOAuthConnectCode"].(map[string]any)
	link, _ := made["connectLink"].(string)
	if !strings.HasPrefix(link, api.AppConnectLink+"#") || !strings.HasPrefix(made["qrCodeSvg"].(string), "<svg") {
		t.Fatalf("the code came back as %v", made)
	}
	// The secret and the server travel after the "#", which no browser sends.
	fragment, err := url.ParseQuery(strings.TrimPrefix(link, api.AppConnectLink+"#"))
	if err != nil || fragment.Get("server") == "" || len(fragment.Get("code")) < 32 {
		t.Fatalf("the link carries %v (%v)", fragment, err)
	}
	return fragment.Get("code")
}

// A phone app scans a code made where the person is signed in, and gets a
// token for the whole API as them, once.
func TestAnAppConnectsWithACodeAndGetsTheWholeAPI(test *testing.T) {
	world := newSignInWorld(test)
	appClient := world.register(test, "com.example.app:/oauth")
	webClient := world.register(test, "https://app.example.com/callback")
	code := world.connectCode(test)

	exchange := func(clientId string) map[string]any {
		_, answer := world.post(test, api.PathOAuthToken, url.Values{
			"grant_type": {"urn:teanode:params:oauth:grant-type:connect-code"}, "client_id": {clientId},
			"connect_code": {code}, "device_name": {"a test\u202ephone"},
		})
		return answer
	}

	// A web page cannot use it, and trying does not spend it.
	if answer := exchange(webClient); answer["error"] != "unauthorized_client" {
		test.Errorf("a web program presenting the code was answered %v", answer)
	}
	answer := exchange(appClient)
	token, _ := answer["access_token"].(string)
	if token == "" || answer["scope"] != "api" || answer["refresh_token"] == "" {
		test.Fatalf("the app was answered %v", answer)
	}
	if !world.worksOnTheAPI(token) {
		test.Error("the connected token is not taken by the API")
	}
	if again := exchange(appClient); again["error"] != "invalid_grant" {
		test.Errorf("the same code was accepted twice: %v", again)
	}
}

// A code would travel in clear over plain http, so it is made only over https
// or on the server's own machine.
func TestNoCodeIsMadeOverPlainHTTP(test *testing.T) {
	world := newSignInWorld(test)
	world.host = "mail.example.com"
	if _, errors := world.ask(test, `mutation { CreateOAuthConnectCode { connectLink } }`); len(errors) == 0 {
		test.Error("a code was made for a dashboard reached over plain http")
	}
}

// The consent page grants the agent tools to a program that names no
// resource, whatever it registered as: a page anybody can send a link to is
// where phishing aims. An app approved there gets the agent tools, which
// GraphQL refuses. (An installed app that names the API is the one exception;
// see TestAnInstalledAppNamingTheAPISignsInToTheWholeAccount.)
func TestTheConsentPageNeverGrantsTheWholeAPI(test *testing.T) {
	world := newSignInWorld(test)
	appClient := world.register(test, "com.example.app:/oauth")
	const challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	data, errors := world.ask(test, `mutation { ApproveOAuthAuthorization(clientId: "`+appClient+`", redirectUri: "com.example.app:/oauth", codeChallenge: "`+challenge+`") { redirectUrl } }`)
	if len(errors) > 0 {
		test.Fatalf("ApproveOAuthAuthorization: %v", errors)
	}
	approved, _ := data["ApproveOAuthAuthorization"].(map[string]any)
	redirect, _ := url.Parse(approved["redirectUrl"].(string))
	_, answer := world.post(test, api.PathOAuthToken, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {appClient}, "redirect_uri": {"com.example.app:/oauth"},
		"code": {redirect.Query().Get("code")}, "code_verifier": {"dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
	})
	token, _ := answer["access_token"].(string)
	if token == "" || answer["scope"] != "mcp" {
		test.Fatalf("the app's consent was answered %v", answer)
	}
	if world.worksOnTheAPI(token) {
		test.Error("a token from the consent page was taken by the whole API")
	}
}

// An app installed on a device that names the API signs in to the whole
// account through the consent page, as a connect code would sign it in, and
// a program on the web asking the same is refused before anybody is asked.
// docs/decisions/20261011-an-installed-app-may-sign-in-with-a-password.md
func TestAnInstalledAppNamingTheAPISignsInToTheWholeAccount(test *testing.T) {
	world := newSignInWorld(test)
	appClient := world.register(test, "com.example.app:/oauth")
	const challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	resource := "https://" + world.host + api.Prefix
	read, errors := world.ask(test, `query { ReadOAuthAuthorizationRequest(clientId: "`+appClient+`", redirectUri: "com.example.app:/oauth", resource: "`+resource+`") { isWholeAccount } }`)
	if len(errors) > 0 || read["ReadOAuthAuthorizationRequest"].(map[string]any)["isWholeAccount"] != true {
		test.Fatalf("the page must say it is the whole account: %v %v", read, errors)
	}
	data, errors := world.ask(test, `mutation { ApproveOAuthAuthorization(clientId: "`+appClient+`", redirectUri: "com.example.app:/oauth", codeChallenge: "`+challenge+`", resource: "`+resource+`") { redirectUrl } }`)
	if len(errors) > 0 {
		test.Fatalf("ApproveOAuthAuthorization: %v", errors)
	}
	approved, _ := data["ApproveOAuthAuthorization"].(map[string]any)
	redirect, _ := url.Parse(approved["redirectUrl"].(string))
	if redirect.Scheme != "com.example.app" {
		test.Fatalf("the approval must return to the app on this device, not %q", redirect)
	}
	_, answer := world.post(test, api.PathOAuthToken, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {appClient}, "redirect_uri": {"com.example.app:/oauth"},
		"code": {redirect.Query().Get("code")}, "code_verifier": {"dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
	})
	token, _ := answer["access_token"].(string)
	if token == "" || answer["scope"] != "api" || !world.worksOnTheAPI(token) {
		test.Fatalf("the app's sign-in was answered %v", answer)
	}

	webClient := world.register(test, "https://harness.example.com/callback")
	if _, errors := world.ask(test, `mutation { ApproveOAuthAuthorization(clientId: "`+webClient+`", redirectUri: "https://harness.example.com/callback", codeChallenge: "`+challenge+`", resource: "`+resource+`") { redirectUrl } }`); len(errors) == 0 {
		test.Error("a program on the web was given the whole account")
	}
}
