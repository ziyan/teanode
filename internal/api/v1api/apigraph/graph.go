package apigraph

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/web"
)

type graphRequest struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables"`
	OperationName string                 `json:"operationName"`
}

// maximumRequestSize bounds a GraphQL request body. This endpoint is reached
// before authentication, because logging in is a mutation, so what it reads
// from a stranger has to be bounded; a query is a few kilobytes and a draft
// with files goes through its own route, which has its own cap.
const maximumRequestSize = 1 << 20

// readableAsJSON refuses a request that is not declared as JSON, when the
// thing authorizing it is a cookie.
//
// A form on another site can post to this address with the person's cookie
// attached, and the browser sends it without asking permission first as long
// as the request looks like one a form could make -- which means a content
// type of text/plain, a form encoding, or none. It cannot read the answer,
// but the mutation has already run. Requiring the JSON type takes that shape
// away, because a browser will not let a cross-origin page set it without
// asking this server first.
//
// Only for a cookie, because that is the only credential a browser attaches
// by itself. A request carrying a token is one somebody wrote on purpose, and
// a script posting with no content type at all should keep working.
func readableAsJSON(request *http.Request) error {
	if request.Method != http.MethodPost {
		return nil
	}
	if request.Header.Get("Authorization") != "" {
		return nil
	}
	if _, err := request.Cookie(web.SessionCookieName); err != nil {
		return nil
	}
	kind, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(kind, "application/json") {
		return fmt.Errorf("this address takes application/json")
	}
	return nil
}

func (self *graph) graphView(response http.ResponseWriter, request *http.Request) {
	if err := readableAsJSON(request); err != nil {
		http.Error(response, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	var data graphRequest
	request.Body = http.MaxBytesReader(response, request.Body, maximumRequestSize)
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
			// sign in: the session outlived the account. Neither the name
			// nor the account is kept, so no principal is built from it.
			username = ""
		} else {
			user = found
		}
	}

	ctx := request.Context()
	ctx = api.ContextWithRequest(ctx, request)
	// Logging in and out set a cookie, which is a response header.
	ctx = api.ContextWithResponse(ctx, response)
	ctx = api.ContextWithAuthenticatedUsername(ctx, username)
	ctx = db.ContextWithAuditPrincipal(ctx, self.auditPrincipal(request, user))

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
func (self *graph) auditPrincipal(request *http.Request, user *models.User) db.AuditPrincipal {
	principal := db.AuditPrincipal{ActorKind: models.AuditActorUser, SourceIP: self.remoteAddress(request)}
	if user != nil {
		principal.UserID = user.ID
	}
	return principal
}

// remoteAddress is who asked, through whatever is in front of this server.
func (self *graph) remoteAddress(request *http.Request) string {
	return api.RemoteAddress(request, self.config.Current().Server.TrustedProxies)
}
