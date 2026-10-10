package apigraph

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// askWithToken sends GraphQL with a bearer token, the way the app does, and
// returns the data and the error messages.
func (self *signInWorld) askWithToken(t *testing.T, token, query string) (map[string]any, []string) {
	t.Helper()
	body, _ := json.Marshal(graphRequest{Query: query})
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
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

// giveAnAgent lets the person use an agent, which phones belong to.
func (self *signInWorld) giveAnAgent(t *testing.T) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "phone test role", Permissions: []models.Permission{models.PermissionAgentUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "phone test group", UserIDs: []string{self.person.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
		if _, err := tx.CreateAgent(&models.Agent{UserID: self.person.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
}

// A phone is registered by the app with the token it connected with, and
// nothing else: not a browser, not a token somebody made by hand. Removing
// it signs it out, however many times the app has renewed its token since.
func TestOnlyAConnectedAppRegistersAPhoneAndRemovingItSignsItOut(test *testing.T) {
	world := newSignInWorld(test)
	world.giveAnAgent(test)
	appClient := world.register(test, "com.example.app:/oauth")
	_, answer := world.post(test, api.PathOAuthToken, url.Values{
		"grant_type": {"urn:teanode:params:oauth:grant-type:connect-code"}, "client_id": {appClient},
		"connect_code": {world.connectCode(test)}, "device_name": {"A test phone"},
	})
	appToken, _ := answer["access_token"].(string)
	if appToken == "" {
		test.Fatalf("the app could not connect: %v", answer)
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	register := `mutation { RegisterAgentPhone(phoneName: "A test` + "\u202e" + `phone", phoneModel: "iPhone17,1", pushRegistration: "sealed:a-registration", payloadKey: "` + base64.StdEncoding.EncodeToString(key) + `") { id phoneName canBeNotified } }`

	if _, errors := world.ask(test, register); len(errors) == 0 {
		test.Error("a browser registered a phone")
	}
	data, errors := world.askWithToken(test, appToken, register)
	if len(errors) > 0 {
		test.Fatalf("RegisterAgentPhone: %v", errors)
	}
	phone, _ := data["RegisterAgentPhone"].(map[string]any)
	if phone["phoneName"] != "A testphone" || phone["canBeNotified"] != true {
		test.Errorf("the phone registered as %v", phone)
	}
	// The app renews its token, and registers again with the new one, as it
	// does each time it starts: still one phone.
	_, renewed := world.post(test, api.PathOAuthToken, url.Values{
		"grant_type": {"refresh_token"}, "client_id": {appClient}, "refresh_token": {answer["refresh_token"].(string)},
	})
	appToken, _ = renewed["access_token"].(string)
	if appToken == "" {
		test.Fatalf("the app could not renew its token: %v", renewed)
	}
	if _, errors := world.askWithToken(test, appToken, register); len(errors) > 0 {
		test.Fatalf("registering again: %v", errors)
	}
	data, _ = world.ask(test, `{ ListAgentPhones { phones { id } isPushEnabled } }`)
	list, _ := data["ListAgentPhones"].(map[string]any)
	if phones, _ := list["phones"].([]any); len(phones) != 1 {
		test.Fatalf("the person has %d phones, not one", len(phones))
	}

	if _, errors := world.ask(test, `mutation { UpdateAgentPhone(phoneId: "`+phone["id"].(string)+`", notificationKinds: ["approval", "nonsense"]) { id } }`); len(errors) == 0 {
		test.Error("a kind of notification that does not exist was accepted")
	}
	if _, errors := world.ask(test, `mutation { RemoveAgentPhone(phoneId: "`+phone["id"].(string)+`") }`); len(errors) > 0 {
		test.Fatalf("RemoveAgentPhone: %v", errors)
	}
	if world.worksOnTheAPI(appToken) {
		test.Error("a removed phone's renewed token still works")
	}
}
