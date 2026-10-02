package apigraph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/graphql-go/graphql/language/ast"

	"github.com/ziyan/teanode/internal/api"
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
	// in as somebody else. Their credentials and their agent are off limits
	// meanwhile.
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
	return found, nil
}

// refusedWhileImpersonating are the mutations an impersonation may not run,
// by name, compared without regard to case. Checked once, before any resolver runs,
// so that a resolver added later cannot forget it.
//
// The person's credentials: anything minted in the hour outlives it, which
// would turn a visit into a lasting login nobody sees, and anything removed
// is the person locked out of their own account. And the person's agent: it
// learns from what is said to it as them, so an operator talking to it would
// put words in the person's memory.
var refusedWhileImpersonating = map[string]bool{
	"StartImpersonation": true,

	"ChangePassword":            true,
	"SetUserPassword":           true,
	"BeginPasskeyRegistration":  true,
	"FinishPasskeyRegistration": true,
	"RenamePasskey":             true,
	"DeletePasskey":             true,
	"CreateToken":               true,
	"UpdateToken":               true,
	"DeleteToken":               true,
	"CreateMailboxAppPassword":  true,
	"DeleteMailboxAppPassword":  true,
	"CreateCredential":          true,
	"UpdateCredential":          true,
	"DeleteCredential":          true,
	"ApproveOAuthAuthorization": true,
	"RevokeSession":             true,
	"RevokeAllSessions":         true,

	"AskAgent":                 true,
	"StartAgentConversation":   true,
	"ResolveAgentConfirmation": true,
}

// refusedWhileImpersonatingName is the first field of a mutation that an
// impersonation may not run, or empty.
func refusedWhileImpersonatingName(operation *ast.OperationDefinition) string {
	if operation == nil || operation.SelectionSet == nil {
		return ""
	}
	for _, selection := range operation.SelectionSet.Selections {
		field, ok := selection.(*ast.Field)
		if !ok || field.Name == nil {
			continue
		}
		for name := range refusedWhileImpersonating {
			if strings.EqualFold(name, field.Name.Value) {
				return field.Name.Value
			}
		}
	}
	return ""
}
