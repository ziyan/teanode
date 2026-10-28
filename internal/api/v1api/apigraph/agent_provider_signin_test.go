package apigraph

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A sign-in begun on one instance is finished on another: the page asks
// again on whichever instance its next request reaches, and the code waiting
// for the person is in the database, not in the instance that began it.
func TestASignInBegunOnOneInstanceIsFinishedOnAnother(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	if err := database.SetSecret([]byte("a-server-secret-for-the-test")); err != nil {
		t.Fatalf("SetSecret: %s", err)
	}

	var mutex sync.Mutex
	polls := 0
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.example/auth":{"chatgpt_account_id":"an-account","chatgpt_plan_type":"a-plan"}}`))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		body, _ := io.ReadAll(request.Body)
		switch request.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = io.WriteString(writer, `{"device_auth_id":"a-device-id","user_code":"ABCD-1234","interval":1}`)
		case "/api/accounts/deviceauth/token":
			polls++
			if !strings.Contains(string(body), `"device_auth_id":"a-device-id"`) {
				t.Errorf("the service was polled with %s", body)
			}
			_, _ = io.WriteString(writer, `{"authorization_code":"a-code","code_challenge":"x","code_verifier":"a-verifier"}`)
		case "/oauth/token":
			_, _ = io.WriteString(writer, `{"refresh_token":"a-refresh-token","id_token":"header.`+claims+`.signature"}`)
		default:
			t.Errorf("asked for %s", request.URL.Path)
		}
	}))
	defer server.Close()
	previous := deviceSignInEndpointsFor
	deviceSignInEndpointsFor = func(string) (*llm.DeviceSignInEndpoints, error) {
		return &llm.DeviceSignInEndpoints{Issuer: server.URL, ClientId: "a-client", TokenUrl: server.URL + "/oauth/token", Client: server.Client()}, nil
	}
	defer func() { deviceSignInEndpointsFor = previous }()

	var operator *models.User
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if operator, err = tx.CreateUser(&models.User{Username: "sign-in-operator", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
	})
	ctx := api.ContextWithPrincipal(context.Background(), &api.Principal{User: operator, Permissions: models.NewEffectivePermissions([]models.Grant{
		{Permission: models.PermissionServerManage},
	})})
	// Two instances: a graph and a configuration each, and one database.
	newInstance := func() *graph {
		configuration := config.Default()
		configuration.Server.Name = "assistant.example.com"
		configuration.Listen.HTTPS = ""
		configuration.TLS.ACME.Enabled = false
		return &graph{database: database, config: config.NewMemoryStore(configuration)}
	}
	beginning, finishing := newInstance(), newInstance()

	begun, err := beginning.BeginAgentProviderSignIn(ctx, BeginAgentProviderSignInArguments{Provider: "plan"})
	if err != nil {
		t.Fatalf("BeginAgentProviderSignIn: %s", err)
	}
	if begun.UserCode != "ABCD-1234" || begun.VerificationAddress != server.URL+"/codex/device" {
		t.Fatalf("it began as %+v", begun)
	}

	finished, err := finishing.FinishAgentProviderSignIn(ctx, FinishAgentProviderSignInArguments{SignInID: begun.SignInID})
	if err != nil {
		t.Fatalf("FinishAgentProviderSignIn: %s", err)
	}
	if !finished.IsSignedIn || finished.Account != "an-account" || finished.Plan != "a-plan" || finished.Provider != "plan" {
		t.Errorf("it finished as %+v", finished)
	}
	declared := finishing.config.Current().Agent.Provider("plan")
	if declared == nil || declared.RefreshToken != "a-refresh-token" || declared.Kind != config.AgentProviderKindCodex {
		t.Errorf("the provider was saved as %+v", declared)
	}

	// Finished, it is not waiting on either instance any more.
	for _, resolver := range []*graph{beginning, finishing} {
		if _, err := resolver.FinishAgentProviderSignIn(ctx, FinishAgentProviderSignInArguments{SignInID: begun.SignInID}); err == nil {
			t.Errorf("a finished sign-in was finished again")
		}
	}
}
