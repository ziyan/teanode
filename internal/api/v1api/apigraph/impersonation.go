package apigraph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/graphql-go/graphql/language/ast"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/web"
)

// An operator who may manage accounts can sign in as one of them, for an
// hour, to see what they see. The session belongs to the person and names
// the operator; every audit row written in it names both; the person sees it
// in their own list of sessions and can end it. docs/planning/
// impersonation-execplan.md has the reasoning.

type ImpersonationMutation interface {
	// StartImpersonation signs you in as another account for an hour, to
	// see what they see: your own session is kept and comes back when it
	// ends. Needs the permission to manage accounts, and every permission
	// that account holds. Not from a token, and not while already signed
	// in as somebody else. It is for seeing what they see: nothing can be
	// changed meanwhile but ending it.
	StartImpersonation(ctx context.Context, arguments StartImpersonationArguments) (*SessionState, error)

	// EndImpersonation ends signing in as somebody else and returns you to
	// your own account.
	EndImpersonation(ctx context.Context) (*SessionState, error)
}

type StartImpersonationArguments struct {
	// ID of the User to sign in as
	UserID string `json:"userId"`
}

func (self *graph) StartImpersonation(ctx context.Context, arguments StartImpersonationArguments) (*SessionState, error) {
	principal, err := self.requirePermission(ctx, models.PermissionUserManage)
	if err != nil {
		return nil, err
	}
	if principal.User == nil || principal.IsImpersonation() {
		return nil, fmt.Errorf("%w: signing in as somebody else needs your own browser session", api.ErrInvalidArguments)
	}
	if arguments.UserID == "" || arguments.UserID == principal.User.ID {
		return nil, api.ErrInvalidArguments
	}
	tx := self.transaction(ctx)
	target, err := tx.GetUser(arguments.UserID)
	if err != nil {
		return nil, translateError(err)
	}
	if target == nil || target.Disabled() {
		return nil, api.ErrNotFound
	}
	// Not somebody who may do more than you. Signing in as an administrator
	// is being one, so the permission to manage accounts would be the
	// permission to hold every other, as with setting a password.
	held, err := tx.EffectivePermissions(target.ID)
	if err != nil {
		return nil, translateError(err)
	}
	if !principal.Permissions.Covers(held) {
		return nil, fmt.Errorf("%w: that account holds permissions you do not, so it is not yours to sign in as",
			api.ErrPermissionDenied)
	}
	request, response := api.ContextRequest(ctx), api.ContextResponse(ctx)
	if request == nil || response == nil {
		return nil, api.ErrInvalidArguments
	}
	session, err := self.authenticator.StartImpersonation(response, request, target.Username)
	if err != nil {
		if errors.Is(err, web.ErrCannotImpersonate) {
			return nil, fmt.Errorf("%w: signing in as somebody else needs your own browser session", api.ErrInvalidArguments)
		}
		return nil, translateError(err)
	}
	log.Noticef("%s signed in as %q until %s", principal.Username(), target.Username, session.ExpiresAt.Format(time.RFC3339))
	state := self.signedInAs(ctx, target.Username)
	state.ImpersonatorUsername = principal.User.Username
	endsAt := session.ExpiresAt
	state.ImpersonationEndsAt = &endsAt
	return state, nil
}

func (self *graph) EndImpersonation(ctx context.Context) (*SessionState, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.IsImpersonation() {
		return nil, fmt.Errorf("%w: you are not signed in as somebody else", api.ErrInvalidArguments)
	}
	request, response := api.ContextRequest(ctx), api.ContextResponse(ctx)
	if request == nil || response == nil {
		return nil, api.ErrInvalidArguments
	}
	restored, err := self.authenticator.EndImpersonation(response, request)
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s stopped signing in as %q", principal.Impersonator.Username, principal.Username())
	if !restored {
		// Their own session ended meanwhile: signed out.
		return &SessionState{
			AuthenticationRequired: true, PasskeysEnabled: self.config.Current().Passkey.Enabled, SSOProviders: self.ssoProviders(),
		}, nil
	}
	return self.signedInAs(ctx, principal.Impersonator.Username), nil
}

// impersonatorOf is the operator behind a request that is an impersonation,
// as the authentication middleware established it, or nil. An operator who
// can no longer be found or sign in is an error: what was done would answer
// to nobody.
func (self *graph) impersonatorOf(request *http.Request, user *models.User) (*models.User, error) {
	name := api.ImpersonatorUsernameFromRequest(request)
	if name == "" || user == nil {
		return nil, nil
	}
	found, err := self.database.GetUserByUsername(name)
	if err != nil {
		return nil, err
	}
	if found == nil || found.Disabled() {
		return nil, api.ErrNotLoggedIn
	}
	// Still allowed, on every request: an operator whose permission to
	// manage accounts was taken away, or who no longer holds everything the
	// person was since given, is not signed in as them any more.
	isAllowed := false
	if err := self.database.TransactionContext(request.Context(), func(tx db.Transaction) error {
		operator, err := tx.EffectivePermissions(found.ID)
		if err != nil {
			return err
		}
		held, err := tx.EffectivePermissions(user.ID)
		if err != nil {
			return err
		}
		isAllowed = operator.Has(models.PermissionUserManage) && operator.Covers(held)
		return nil
	}); err != nil {
		return nil, err
	}
	if !isAllowed {
		return nil, api.ErrNotLoggedIn
	}
	return found, nil
}

// allowedWhileImpersonating are the only mutations an impersonation may run.
// Signed in as somebody else is for seeing what they see, not for acting as
// them: anything changed in the hour would be done in their name, and a list
// of what to refuse kept missing something that lasts -- a token, a rule that
// forwards their mail, a chat linked to their agent, words put in its memory.
var allowedWhileImpersonating = map[string]bool{
	"EndImpersonation": true,
	"Logout":           true,
	"__typename":       true,
}

// refusedWhileImpersonatingName is the first field of a mutation that an
// impersonation may not run, or empty. Fragments are followed, inline and
// named, since a mutation can be asked for through either.
func refusedWhileImpersonatingName(document *ast.Document, operation *ast.OperationDefinition) string {
	fragments := map[string]*ast.FragmentDefinition{}
	if document != nil {
		for _, definition := range document.Definitions {
			if fragment, ok := definition.(*ast.FragmentDefinition); ok && fragment.Name != nil {
				fragments[fragment.Name.Value] = fragment
			}
		}
	}
	followed := map[string]bool{}
	var refused func(selections *ast.SelectionSet) string
	refused = func(selections *ast.SelectionSet) string {
		if selections == nil {
			return ""
		}
		for _, selection := range selections.Selections {
			switch chosen := selection.(type) {
			case *ast.Field:
				if chosen.Name == nil || !allowedWhileImpersonating[chosen.Name.Value] {
					if chosen.Name == nil {
						return "an unnamed field"
					}
					return chosen.Name.Value
				}
			case *ast.InlineFragment:
				if name := refused(chosen.SelectionSet); name != "" {
					return name
				}
			case *ast.FragmentSpread:
				if chosen.Name == nil {
					return "an unnamed fragment"
				}
				if followed[chosen.Name.Value] {
					continue
				}
				followed[chosen.Name.Value] = true
				fragment := fragments[chosen.Name.Value]
				if fragment == nil {
					return chosen.Name.Value
				}
				if name := refused(fragment.SelectionSet); name != "" {
					return name
				}
			default:
				return "a selection of an unknown kind"
			}
		}
		return ""
	}
	if operation == nil {
		return ""
	}
	return refused(operation.SelectionSet)
}
