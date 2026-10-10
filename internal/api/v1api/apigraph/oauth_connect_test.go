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

	"github.com/gorilla/mux"

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
	router        *mux.Router
	person        *models.User
}

func newSignInWorld(t *testing.T) *signInWorld {
	t.Helper()
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	world := &signInWorld{}
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
	component, err := New(database, store, nil, nil, nil, nil, nil, authenticator, nil, &api.Settings{})
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

// Only an app installed on a device may be given the whole API: a program
// with a web address is refused by the consent page.
func TestOnlyAnInstalledAppMayAskForTheWholeAPI(test *testing.T) {
	world := newSignInWorld(test)
	webClient := world.register(test, "https://app.example.com/callback")
	const challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if _, errors := world.ask(test, `mutation { ApproveOAuthAuthorization(clientId: "`+webClient+`", redirectUri: "https://app.example.com/callback", codeChallenge: "`+challenge+`", scope: "api") { redirectUrl } }`); len(errors) == 0 {
		test.Error("a web program was approved for the whole API")
	}

	appClient := world.register(test, "com.example.app:/oauth")
	data, errors := world.ask(test, `{ ReadOAuthAuthorizationRequest(clientId: "`+appClient+`", redirectUri: "com.example.app:/oauth", scope: "api") { isFullAccess } }`)
	if len(errors) > 0 {
		test.Fatalf("ReadOAuthAuthorizationRequest: %v", errors)
	}
	if read, _ := data["ReadOAuthAuthorizationRequest"].(map[string]any); read["isFullAccess"] != true {
		test.Errorf("the consent page was told %v", read)
	}
	data, errors = world.ask(test, `mutation { ApproveOAuthAuthorization(clientId: "`+appClient+`", redirectUri: "com.example.app:/oauth", codeChallenge: "`+challenge+`", scope: "api") { redirectUrl } }`)
	if len(errors) > 0 {
		test.Fatalf("ApproveOAuthAuthorization: %v", errors)
	}
	approved, _ := data["ApproveOAuthAuthorization"].(map[string]any)
	redirect, _ := url.Parse(approved["redirectUrl"].(string))
	if redirect.Scheme != "com.example.app" {
		test.Fatalf("the approval went to %v", redirect)
	}
	_, answer := world.post(test, api.PathOAuthToken, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {appClient}, "redirect_uri": {"com.example.app:/oauth"},
		"code": {redirect.Query().Get("code")}, "code_verifier": {"dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
	})
	token, _ := answer["access_token"].(string)
	if answer["scope"] != "api" || !world.worksOnTheAPI(token) {
		test.Errorf("the app's sign-in was answered %v", answer)
	}
}
