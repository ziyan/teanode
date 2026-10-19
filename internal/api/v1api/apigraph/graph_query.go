package apigraph

import (
	"context"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type queryExecutionKey struct{}

type queryExecution struct {
	Username     string
	User         *models.User
	Impersonator *models.User

	// CredentialKind is what the request signed in with, empty for the
	// agent's own operations (see access.Principal.CredentialKind).
	CredentialKind models.CredentialKind

	// PermissionLimit, when set, is what the caller is held to besides its own
	// permissions: the agent's.
	PermissionLimit *models.EffectivePermissions
}

// isModelBackedQuery is the queries that ask a model, or wait on runs
// that do, and so read in phases of their own rather than inside one
// transaction held open while the model answers: recall for a question
// embeds it, and a survey is minutes of runs. A photo search embeds its words,
// and the listing and the count both do, so both are here. A search for
// places waits on the place provider, which answers one request a second.
var isModelBackedQuery = map[string]bool{"RecallAgentMemory": true, "SurveyAgentMemory": true, "JudgeAgentRetrievalPlan": true, "ReadAgentCodingContext": true, "RecallAgentCodingMemory": true, "ListPhotos": true, "CountPhotos": true, "SearchPlaces": true, "SearchAddresses": true, "ListNearbyPlaces": true}

// isTransactionFreeMutation is the mutations that do slow work, such as reading
// and storing a file as large as a video, and so are not run inside the
// request's one transaction (graphView), which that work would hold open. They
// run the way a model-backed query does: with no transaction in the context,
// reading and writing in short transactions of their own through
// inShortTransaction. Everything such a mutation changes it changes in
// transactions it commits itself, so a failure late in it does not undo what it
// did before.
var isTransactionFreeMutation = map[string]bool{"SavePhotoFromAttachment": true, "SavePhotoToLibrary": true}

// Queries materialize their results within each root resolver. Keeping the
// transaction there lets model-backed resolvers use separate read phases.
// Mutations retain the existing request transaction, except those in
// isTransactionFreeMutation.
func (self *graph) wrapQueryTransactions() {
	for fieldName, field := range self.schema.QueryType().Fields() {
		resolve := field.Resolve
		if resolve == nil {
			continue
		}
		field.Resolve = func(parameters graphql.ResolveParams) (interface{}, error) {
			execution, isQuery := parameters.Context.Value(queryExecutionKey{}).(*queryExecution)
			if isQuery && isModelBackedQuery[fieldName] {
				if _, isServiceOperation := serviceOperations()[fieldName]; isServiceOperation {
					// The check that the service is on, wrapped around this
					// resolver, needs to know who is asking. It is worked out
					// in a short transaction that ends before the resolver
					// runs; the resolver works it out again for itself, in
					// the phases it reads in.
					var principal *api.Principal
					if err := self.database.TransactionContext(parameters.Context, func(transaction db.Transaction) (err error) {
						principal, err = self.queryPrincipal(transaction, execution)
						return err
					}); err != nil {
						return nil, err
					}
					parameters.Context = api.ContextWithPrincipal(parameters.Context, principal)
				}
				return resolve(parameters)
			}
			if !isQuery {
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
		principal.CredentialKind = execution.CredentialKind
		if execution.PermissionLimit != nil {
			principal.Permissions = principal.Permissions.Within(execution.PermissionLimit)
		}
	}
	return principal, err
}

// wrapTransactionFreeMutations gives the mutations in isTransactionFreeMutation
// the principal that the request's transaction would have, worked out in a
// short transaction of their own, so that the checks wrapped around them (that
// somebody is signed in, that the service is on) see who is asking. It is
// called after the other wrappers, so that it is the outermost. A mutation
// that finds a transaction in its context, as in a test that calls it
// directly, runs as it always did.
func (self *graph) wrapTransactionFreeMutations() {
	for fieldName := range isTransactionFreeMutation {
		field := self.schema.MutationType().Fields()[fieldName]
		if field == nil {
			continue
		}
		resolve := field.Resolve
		field.Resolve = func(parameters graphql.ResolveParams) (interface{}, error) {
			execution, isFree := parameters.Context.Value(queryExecutionKey{}).(*queryExecution)
			if !isFree {
				return resolve(parameters)
			}
			var principal *api.Principal
			if err := self.database.TransactionContext(parameters.Context, func(transaction db.Transaction) (err error) {
				principal, err = self.queryPrincipal(transaction, execution)
				return err
			}); err != nil {
				return nil, err
			}
			parameters.Context = api.ContextWithPrincipal(parameters.Context, principal)
			return resolve(parameters)
		}
	}
}

// hasOnlyTransactionFreeMutations says the operation is a mutation all of whose
// root fields are in isTransactionFreeMutation. A mutation that also asks for
// anything else runs in the request's transaction as every mutation does.
func hasOnlyTransactionFreeMutations(operation *ast.OperationDefinition) bool {
	if operation == nil || operation.Operation != "mutation" || operation.SelectionSet == nil || len(operation.SelectionSet.Selections) == 0 {
		return false
	}
	for _, selection := range operation.SelectionSet.Selections {
		field, isField := selection.(*ast.Field)
		if !isField || field.Name == nil || !isTransactionFreeMutation[field.Name.Value] {
			return false
		}
	}
	return true
}

// inShortTransaction runs read with a transaction and a principal in its
// context. Where the request has its own transaction (a mutation like any
// other, or a test that calls the resolver directly) read runs in it. Where it
// has none (isTransactionFreeMutation, isModelBackedQuery) read runs in a
// transaction of its own that ends when it returns, and what the caller does
// after is outside any transaction.
func (self *graph) inShortTransaction(ctx context.Context, read func(ctx context.Context) error) error {
	execution, isFree := ctx.Value(queryExecutionKey{}).(*queryExecution)
	if !isFree || self.transaction(ctx) != nil {
		return read(ctx)
	}
	return self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		principal, err := self.queryPrincipal(transaction, execution)
		if err != nil {
			return err
		}
		return read(api.ContextWithPrincipal(api.ContextWithTransaction(ctx, transaction), principal))
	})
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
