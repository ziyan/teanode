package apigraph

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"

	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
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

	var result *graphql.Result
	if err := self.database.Transaction(func(tx db.Transaction) error {
		ctx := request.Context()
		ctx = api.ContextWithRequest(ctx, request)
		// Logging in and out set a cookie, which is a response header.
		ctx = api.ContextWithResponse(ctx, response)
		ctx = api.ContextWithTransaction(ctx, tx)

		// TODO: rate limit

		// Authentication is handled by the session middleware in internal/web
		// rather than here; the username it established is passed through so
		// resolvers can attribute a change.
		ctx = api.ContextWithAuthenticatedUsername(ctx, api.UsernameFromRequest(request))

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
