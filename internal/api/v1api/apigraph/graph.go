package apigraph

import (
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"

	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type graphRequest struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables"`
	OperationName string                 `json:"operationName"`
}

func (self *graph) graphView(response http.ResponseWriter, request *http.Request) {
	var data graphRequest
	if err := json.NewDecoder(request.Body).Decode(&data); err != nil {
		http.Error(response, fmt.Sprintf("failed to decode request: %s", err), http.StatusBadRequest)
		return
	}

	// Authentication is handled by the session middleware in internal/web
	// rather than here; the username it established is what the principal is
	// built from. The account is read outside the transaction, like every
	// authenticated request reads it; what it may do is resolved inside, so
	// that a role change committed a moment ago is what this request sees.
	username := api.UsernameFromRequest(request)
	var user *models.User
	if username != "" && username != config.LocalUsername {
		found, err := self.database.GetUserByUsername(username)
		if err != nil {
			log.Errorf("failed to read the account %q: %s", username, err)
			http.Error(response, "failed to execute request", http.StatusInternalServerError)
			return
		}
		if found == nil || found.Disabled() {
			// Signed in as somebody who no longer exists, or may no longer
			// sign in: the session outlived the account.
			username = ""
		}
		user = found
	}

	ctx := request.Context()
	ctx = api.ContextWithRequest(ctx, request)
	// Logging in and out set a cookie, which is a response header.
	ctx = api.ContextWithResponse(ctx, response)
	ctx = api.ContextWithAuthenticatedUsername(ctx, username)
	ctx = db.ContextWithAuditPrincipal(ctx, auditPrincipal(request, user))

	var result *graphql.Result
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		ctx := api.ContextWithTransaction(ctx, tx)

		principal, err := self.resolvePrincipal(tx, username, user)
		if err != nil {
			return err
		}
		ctx = api.ContextWithPrincipal(ctx, principal)

		result = graphql.Do(graphql.Params{
			Schema:         self.schema,
			RequestString:  data.Query,
			VariableValues: data.Variables,
			OperationName:  data.OperationName,
			Context:        ctx,
		})
		return nil
	}); err != nil {
		log.Errorf("failed to execute request: %s", err)
		http.Error(response, fmt.Sprintf("failed to execute request: %s", err), http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", mime.FormatMediaType("application/json", map[string]string{"charset": "utf-8"}))
	response.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(response).Encode(result); err != nil {
		log.Errorf("failed to encode response: %s", err)
		return
	}
}

// localUsername is what the authentication middleware calls the console.
const localUsername = config.LocalUsername

// resolvePrincipal is who this request is, and what it may do.
//
// The console — the command line run on the server itself with the local
// token — is not an account and may do everything: whoever can read the
// server secret holds the database anyway.
func (self *graph) resolvePrincipal(tx db.Transaction, username string, user *models.User) (*api.Principal, error) {
	if username == localUsername {
		grants := make([]models.Grant, 0, len(models.Permissions()))
		for _, permission := range models.Permissions() {
			grants = append(grants, models.Grant{Permission: permission})
		}
		return &api.Principal{Console: true, Permissions: models.NewEffectivePermissions(grants)}, nil
	}
	if user == nil {
		return nil, nil
	}
	permissions, err := tx.EffectivePermissions(user.ID)
	if err != nil {
		return nil, err
	}
	return &api.Principal{User: user, Permissions: permissions}, nil
}

// auditPrincipal is who the audit rows this request writes will name.
func auditPrincipal(request *http.Request, user *models.User) db.AuditPrincipal {
	principal := db.AuditPrincipal{ActorKind: models.AuditActorUser, SourceIP: remoteAddress(request)}
	if user != nil {
		principal.UserID = user.ID
	}
	return principal
}

// remoteAddress is who asked, without the port.
func remoteAddress(request *http.Request) string {
	if request == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		return host
	}
	return request.RemoteAddr
}
