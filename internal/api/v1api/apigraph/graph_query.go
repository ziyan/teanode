package apigraph

import (
	"context"

	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type queryExecutionKey struct{}

type queryExecution struct {
	Username string
	User     *models.User
}

// Queries materialize their results within each root resolver. Keeping the
// transaction there lets model-backed resolvers use separate read phases.
// Mutations retain the existing request transaction.
func (self *graph) wrapQueryTransactions() {
	for fieldName, field := range self.schema.QueryType().Fields() {
		resolve := field.Resolve
		if resolve == nil {
			continue
		}
		field.Resolve = func(parameters graphql.ResolveParams) (interface{}, error) {
			execution, isQuery := parameters.Context.Value(queryExecutionKey{}).(*queryExecution)
			if !isQuery || fieldName == "RecallAgentMemory" {
				return resolve(parameters)
			}
			var resolved interface{}
			err := self.database.TransactionContext(parameters.Context, func(transaction db.Transaction) error {
				principal, err := self.queryPrincipal(transaction, execution)
				if err != nil {
					return err
				}
				parameters.Context = api.ContextWithPrincipal(api.ContextWithTransaction(parameters.Context, transaction), principal)
				resolved, err = resolve(parameters)
				return err
			})
			if err != nil {
				return nil, err
			}
			return resolved, nil
		}
	}
}

// requireRecallPerson checks permission and the active agent inside a short read phase.
// It is used again after model work so a revoked grant cannot release results.
func (self *graph) requireRecallPerson(ctx context.Context) (*api.Principal, *models.Agent, error) {
	var principal *api.Principal
	var person *models.Agent
	err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if execution, isQuery := ctx.Value(queryExecutionKey{}).(*queryExecution); isQuery {
			var err error
			principal, err = self.queryPrincipal(transaction, execution)
			if err != nil {
				return err
			}
			ctx = api.ContextWithPrincipal(ctx, principal)
		}
		var err error
		principal, person, err = self.requireAgentPerson(api.ContextWithTransaction(ctx, transaction))
		return err
	})
	return principal, person, err
}

func (self *graph) queryPrincipal(transaction db.Transaction, execution *queryExecution) (*api.Principal, error) {
	user := execution.User
	if user != nil {
		var err error
		user, err = transaction.GetUser(user.ID)
		if err != nil {
			return nil, err
		}
		if user == nil || user.Disabled() {
			return nil, nil
		}
	}
	return self.resolvePrincipal(transaction, execution.Username, user)
}
