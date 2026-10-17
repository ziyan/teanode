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
	Username     string
	User         *models.User
	Impersonator *models.User
}

// isNetworkWaitingQuery is the queries that wait on something outside the
// database, and so read in phases of their own rather than inside one
// transaction held open while they wait: recall for a question embeds it, a
// survey is minutes of model runs, and a search for places waits on the place
// provider, which answers one request a second.
var isNetworkWaitingQuery = map[string]bool{
	"RecallAgentMemory": true, "SurveyAgentMemory": true, "JudgeAgentRetrievalPlan": true,
	"ReadAgentCodingContext": true, "RecallAgentCodingMemory": true,
	"SearchPlaces": true, "ListNearbyPlaces": true,
}

// Queries materialize their results within each root resolver. Keeping the
// transaction there lets resolvers that wait on the network use separate read phases.
// Mutations retain the existing request transaction.
func (self *graph) wrapQueryTransactions() {
	for fieldName, field := range self.schema.QueryType().Fields() {
		resolve := field.Resolve
		if resolve == nil {
			continue
		}
		field.Resolve = func(parameters graphql.ResolveParams) (interface{}, error) {
			execution, isQuery := parameters.Context.Value(queryExecutionKey{}).(*queryExecution)
			if !isQuery || isNetworkWaitingQuery[fieldName] {
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
	principal, err := self.resolvePrincipal(transaction, execution.Username, user)
	if principal != nil {
		principal.Impersonator = execution.Impersonator
	}
	return principal, err
}

// readInPhase runs one short read phase of a query that waits on the network:
// a transaction of its own, with the caller's principal read afresh in it, so
// that a permission taken away while the wait lasted is seen by the phase
// after it.
func (self *graph) readInPhase(ctx context.Context, read func(phaseContext context.Context) error) error {
	return self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		phaseContext := ctx
		if execution, isQuery := ctx.Value(queryExecutionKey{}).(*queryExecution); isQuery {
			principal, err := self.queryPrincipal(transaction, execution)
			if err != nil {
				return err
			}
			phaseContext = api.ContextWithPrincipal(phaseContext, principal)
		}
		return read(api.ContextWithTransaction(phaseContext, transaction))
	})
}
