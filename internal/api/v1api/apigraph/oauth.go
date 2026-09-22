package apigraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// How long somebody has to collect an approval.
//
// Minutes rather than hours. The program asking is waiting on the answer with
// a request already open; anything longer is a window in which a stolen
// approval is still worth something.
const authorizationLifetime = 5 * time.Minute

// How long a token issued by approval lasts before it has to be refreshed.
//
// A month, matching what a person gets when they mint one by hand and say
// nothing about a lifetime. A program that is still in use refreshes without
// anybody noticing; one that was forgotten stops working, which is the point.
const authorizedTokenLifetime = 30 * 24 * time.Hour

type OAuthQuery interface {
	// Describe the program asking to be authorized, for the page that asks
	// somebody whether to allow it.
	ReadOAuthAuthorizationRequest(ctx context.Context, arguments ReadOAuthAuthorizationRequestArguments) (*OAuthAuthorizationRequest, error)
}

type OAuthMutation interface {
	// Approve a program, returning the address to send the person back to.
	ApproveOAuthAuthorization(ctx context.Context, arguments ApproveOAuthAuthorizationArguments) (*OAuthApproval, error)
}

type ReadOAuthAuthorizationRequestArguments struct {
	// ClientID of the program, as it registered
	ClientID string `json:"clientId"`

	// RedirectURI it asked to be sent back to
	RedirectURI string `json:"redirectUri"`
}

// OAuthAuthorizationRequest is what the page shows.
type OAuthAuthorizationRequest struct {
	// ClientName is what the program calls itself. Chosen by whoever
	// registered it and shown as such: the page says the name is claimed
	// rather than verified, because anybody may register under any name.
	ClientName string `json:"clientName"`

	// RedirectHost is where approving would send the person, shown because
	// it is the one part of this a reader can actually judge.
	RedirectHost string `json:"redirectHost"`

	// Username is the account that would be acted as.
	Username string `json:"username"`

	// Registered is when the program introduced itself. A registration made
	// seconds ago is the one you just started; an old one you do not
	// recognize is worth a second look.
	Registered time.Time `json:"registered"`
}

type ApproveOAuthAuthorizationArguments struct {
	ClientID      string `json:"clientId"`
	RedirectURI   string `json:"redirectUri"`
	CodeChallenge string `json:"codeChallenge"`

	// State is handed back untouched, which is how the program recognizes
	// its own request.
	State *string `json:"state" graphapi:"nullable"`

	// Resource is what the token will be good for, and is why a token from
	// this flow cannot be spent against the rest of the API.
	Resource *string `json:"resource" graphapi:"nullable"`
}

// OAuthApproval is where to send the person next.
type OAuthApproval struct {
	// RedirectURL carries the approval and the state, ready to open.
	RedirectURL string `json:"redirectUrl"`
}

// requireClient is the registered program an argument names, with its
// redirect address checked.
//
// The address is checked here rather than trusted because this is the check
// that stops an approval being sent somewhere the program never registered,
// which is the whole of what somebody attacking this would want.
func (self *graph) requireClient(clientId, redirectURI string) (*models.OAuthClient, error) {
	client, err := self.database.GetOAuthClient(strings.TrimSpace(clientId))
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("%w: no program is registered under that identifier", api.ErrNotFound)
	}
	if !client.AllowsRedirect(redirectURI) {
		return nil, fmt.Errorf("%w: that program did not register this address", api.ErrInvalidArguments)
	}
	return client, nil
}

func (self *graph) ReadOAuthAuthorizationRequest(ctx context.Context, arguments ReadOAuthAuthorizationRequestArguments) (*OAuthAuthorizationRequest, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	// The console is the command line run on the server itself, which is not
	// an account. There is nobody for a program to act as, so there is
	// nothing to approve.
	if principal.User == nil {
		return nil, fmt.Errorf("%w: a program is authorized to act as an account", api.ErrInvalidArguments)
	}
	client, err := self.requireClient(arguments.ClientID, arguments.RedirectURI)
	if err != nil {
		return nil, err
	}
	host := arguments.RedirectURI
	if parsed, parseError := url.Parse(arguments.RedirectURI); parseError == nil && parsed.Host != "" {
		host = parsed.Host
	}
	return &OAuthAuthorizationRequest{
		ClientName:   client.Name,
		RedirectHost: host,
		Username:     principal.User.Username,
		Registered:   client.CreatedAt,
	}, nil
}

func (self *graph) ApproveOAuthAuthorization(ctx context.Context, arguments ApproveOAuthAuthorizationArguments) (*OAuthApproval, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, fmt.Errorf("%w: a program is authorized to act as an account", api.ErrInvalidArguments)
	}
	client, err := self.requireClient(arguments.ClientID, arguments.RedirectURI)
	if err != nil {
		return nil, err
	}

	// A challenge is required, and only the hashing method is accepted. A
	// flow without one is a flow where an approval captured in transit is
	// enough to collect the token, and this server publishes S256 as the
	// only method it supports.
	challenge := strings.TrimSpace(arguments.CodeChallenge)
	if !usableChallenge(challenge) {
		return nil, fmt.Errorf("%w: this server authorizes only with a PKCE challenge, hashed with S256", api.ErrInvalidArguments)
	}

	// The secret half of the approval. The program collects by presenting it
	// alongside the verifier, and only the hash is kept here.
	key := security.GenerateRandomString(32, security.LowerAlphaNumeric)
	authorization, err := self.database.CreateOAuthAuthorization(&models.OAuthAuthorization{
		ID:            security.NewULID(),
		ClientID:      client.ID,
		UserID:        principal.User.ID,
		RedirectURI:   arguments.RedirectURI,
		CodeChallenge: challenge,
		Resource:      valueOrEmpty(arguments.Resource),
		ExpiresAt:     time.Now().Add(authorizationLifetime),
	}, hashOf(key))
	if err != nil {
		return nil, err
	}
	if err := self.database.TouchOAuthClientApproval(client.ID, time.Now()); err != nil {
		// Worth a line and not worth refusing over: this only decides when a
		// registration nobody used is swept.
		log.Warningf("could not record that %s was approved: %s", client.ID, err)
	}

	redirect, err := url.Parse(arguments.RedirectURI)
	if err != nil {
		return nil, fmt.Errorf("%w: that address cannot be returned to", api.ErrInvalidArguments)
	}
	query := redirect.Query()
	query.Set("code", authorization.ID+"."+key)
	if arguments.State != nil && *arguments.State != "" {
		query.Set("state", *arguments.State)
	}
	redirect.RawQuery = query.Encode()

	log.Noticef("%s authorized the client %s (%q)", principal.User.Username, client.ID, client.Name)
	return &OAuthApproval{RedirectURL: redirect.String()}, nil
}

// usableChallenge is a PKCE challenge this server will accept.
//
// The length range is what a base64url encoding of a SHA-256 digest comes to,
// and the character set is that alphabet. Checking it here means a malformed
// challenge is refused while somebody is watching, rather than at collection
// when only the program would see it.
func usableChallenge(challenge string) bool {
	if len(challenge) < 43 || len(challenge) > 128 {
		return false
	}
	for _, character := range challenge {
		switch {
		case character >= 'A' && character <= 'Z':
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '-' || character == '_':
		default:
			return false
		}
	}
	return true
}

// hashOf is how a secret is written down here: hex, like every other hashed
// secret in this server, so the column and the comparison mean the same thing
// wherever they are read.
func hashOf(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
