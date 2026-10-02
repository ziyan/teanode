package apigraph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/web"
)

// An operator who may manage accounts can sign in as one of them, for an
// hour, to help them: whatever the person can do, the operator can do, and
// nothing more, since every request is the person's with the person's
// permissions. The session belongs to the person and names the operator;
// every audit row written in it names both; the person sees it in their own
// list of sessions and can end it. docs/planning/impersonation-execplan.md
// has the reasoning.

type ImpersonationMutation interface {
	// StartImpersonation signs you in as another account for an hour, to
	// help them: you can do what they can, and nothing more. Your own
	// session is kept and comes back when it ends. Needs the permission to
	// manage accounts, and every permission that account holds. Not from a
	// token, and not while already signed in as somebody else. Everything
	// changed meanwhile is audited under both names.
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
