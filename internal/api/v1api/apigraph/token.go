package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

type TokenQuery interface {
	// List this account's API tokens. The tokens themselves are not stored
	// and cannot be listed; only what they were issued for, and when each was
	// last used.
	ListTokens(ctx context.Context, arguments ListTokensArguments) ([]*Token, error)
}

type TokenMutation interface {
	// Create an API token and return it. It is shown only in this reply,
	// because only a hash of it is kept.
	CreateToken(ctx context.Context, arguments CreateTokenArguments) (*CreatedToken, error)

	// Revoke an API token. The row is kept for a while, marked revoked, so
	// the list can say so rather than the token quietly disappearing.
	DeleteToken(ctx context.Context, arguments DeleteTokenArguments) error
}

// Token is an issued API token, without the secret.
type Token struct {
	// ID of the Token, which is also the first half of the token string
	ID string `json:"id"`

	// What holds it, for example "laptop"
	Name string `json:"name"`

	// The account it belongs to, and acts as
	Username string `json:"username"`

	// When it was issued
	Created time.Time `json:"created"`

	// When it stops working, or null if it does not expire
	Expires *time.Time `json:"expires,omitempty"`

	// When it was last used, or null if it never has been. Recorded at most
	// once a minute, so it is accurate to about that.
	LastUsed *time.Time `json:"lastUsed,omitempty"`

	// Where it was last used from
	LastUsedIP string `json:"lastUsedIp,omitempty"`

	// When it was revoked, or null while it still works
	Revoked *time.Time `json:"revoked,omitempty"`
}

// CreatedToken is a newly issued Token together with the secret, which is not
// recoverable afterwards.
type CreatedToken struct {
	Token *Token `json:"token"`

	// The token to pass as "Authorization: Bearer". Shown only once.
	Secret string `json:"secret"`
}

func describeToken(token *models.Token) *Token {
	if token == nil {
		return nil
	}
	return &Token{
		ID:         token.ID,
		Name:       token.Name,
		Username:   token.Username,
		Created:    token.CreatedAt,
		Expires:    optionalTime(token.ExpiresAt),
		LastUsed:   optionalTime(token.UsedAt),
		LastUsedIP: token.IP,
		Revoked:    optionalTime(token.RevokedAt),
	}
}

// optionalTime turns the zero time into a null, so that "never" reads as an
// absent field rather than as the first of January in the year one.
func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

type ListTokensArguments struct {
	// Include tokens that have been revoked, which the list shows so that a
	// revocation is visible rather than the row disappearing.
	IncludeRevoked *bool `json:"includeRevoked" graphapi:"nullable"`

	// Whose tokens. Only the console may name somebody else; see owner.
	Username *string `json:"username" graphapi:"nullable"`
}

func (self *graph) ListTokens(ctx context.Context, arguments ListTokensArguments) ([]*Token, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	username, err := self.owner(ctx, arguments.Username)
	if err != nil {
		return nil, err
	}

	stored, err := self.authenticator.ListTokens(username, arguments.IncludeRevoked != nil && *arguments.IncludeRevoked)
	if err != nil {
		return nil, err
	}

	tokens := make([]*Token, 0, len(stored))
	for _, token := range stored {
		tokens = append(tokens, describeToken(token))
	}
	return tokens, nil
}

type CreateTokenArguments struct {
	// What holds it, for example "laptop"
	Name string `json:"name"`

	// Which account it belongs to and acts as. Only the console may name
	// somebody else; see owner.
	Username *string `json:"username" graphapi:"nullable"`

	// How long it lasts, for example "720h". Omit for a token that does not
	// expire.
	Lifetime *string `json:"lifetime" graphapi:"nullable"`
}

func (self *graph) CreateToken(ctx context.Context, arguments CreateTokenArguments) (*CreatedToken, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	username, err := self.owner(ctx, arguments.Username)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(arguments.Name)
	if name == "" {
		return nil, fmt.Errorf("a name is required, so that a token can be recognised later")
	}

	var lifetime time.Duration
	if arguments.Lifetime != nil && strings.TrimSpace(*arguments.Lifetime) != "" {
		parsed, parseError := time.ParseDuration(strings.TrimSpace(*arguments.Lifetime))
		err = parseError
		if err != nil {
			return nil, fmt.Errorf("%q is not a length of time, for example 720h", *arguments.Lifetime)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("a lifetime has to be in the future")
		}
		lifetime = parsed
	}

	token, secret, err := self.authenticator.IssueToken(username, name, lifetime)
	if err != nil {
		return nil, err
	}
	return &CreatedToken{Token: describeToken(token), Secret: secret}, nil
}

type DeleteTokenArguments struct {
	// ID of the Token to revoke
	TokenID string `json:"tokenId"`
}

func (self *graph) DeleteToken(ctx context.Context, arguments DeleteTokenArguments) error {
	if err := self.requireOperator(ctx); err != nil {
		return err
	}

	username, err := self.owner(ctx, nil)
	if err != nil {
		return err
	}
	if err := self.authenticator.RevokeToken(username, arguments.TokenID); err != nil {
		return err
	}
	log.Noticef("%s revoked API token %s", username, arguments.TokenID)
	return nil
}

// owner resolves whose credentials an operation is about.
//
// An account may only ever act on its own: a token acts as the person it
// belongs to, so listing or issuing somebody else's is handing over a way to
// become them, and the dashboard has no reason to offer it.
//
// The console is the exception, and has to be. It authenticates with a token
// minted from the server secret and is not an account at all, so it has no
// tokens of its own — "teanode token create --user ziyan" run on the server is
// how a new operator gets their first one. Whoever can do that can already
// read the configuration, so it grants nothing they did not have.
func (self *graph) owner(ctx context.Context, requested *string) (string, error) {
	authenticated := api.ContextAuthenticatedUsername(ctx)

	if authenticated == config.LocalUsername {
		if requested == nil || strings.TrimSpace(*requested) == "" {
			return "", fmt.Errorf("the console is not an account, so say whose this is with --user")
		}
		named := strings.TrimSpace(*requested)
		if self.config.Current().FindUser(named) == nil {
			return "", fmt.Errorf("there is no account called %q", named)
		}
		return named, nil
	}

	if requested != nil && strings.TrimSpace(*requested) != "" && strings.TrimSpace(*requested) != authenticated {
		return "", fmt.Errorf("an account can only manage its own credentials")
	}
	return authenticated, nil
}
