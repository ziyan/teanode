package apigraph

import (
	"context"
	"net/url"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// An app is a program somebody authorized to act as them, over OAuth. It
// holds a token of theirs, which it renews by itself: each renewal retires
// the token and issues another, so the app, not the token, is what a person
// recognizes and manages. What makes it theirs is the tokens it holds for
// them; an app registered by somebody else is none of their business.

type AppQuery interface {
	// List the apps that hold a token of this account's, one row an app
	// however many tokens it holds.
	ListApps(ctx context.Context, arguments ListAppsArguments) ([]*App, error)
}

type AppMutation interface {
	// Rename an app, on the tokens it holds for this account and on each one
	// it renews into.
	RenameApp(ctx context.Context, arguments RenameAppArguments) error

	// Disconnect an app: revoke every token it holds for this account, so it
	// has nothing left to renew and has to be authorized again.
	DisconnectApp(ctx context.Context, arguments DisconnectAppArguments) error
}

// App is a program holding a token of the person's.
type App struct {
	// ClientID is the app's registration
	ClientID string `json:"clientId"`

	// What the person calls it: the name on its tokens
	Name string `json:"name"`

	// The name it gave itself when it registered
	RegisteredName string `json:"registeredName"`

	// Where it asked to be sent after an approval: the host of each address
	// it registered
	RedirectHosts []string `json:"redirectHosts"`

	// When it registered
	Registered *time.Time `json:"registered,omitempty"`

	// When it last used a token, or null if it never has
	LastUsed *time.Time `json:"lastUsed,omitempty"`

	// Where it last used one from
	LastUsedIP string `json:"lastUsedIp,omitempty"`

	// When its current token stops working; it renews before then
	Expires *time.Time `json:"expires,omitempty"`

	// The last moment it can renew. Unused past this, it is disconnected.
	RenewableUntil *time.Time `json:"renewableUntil,omitempty"`

	// TokenCount is how many tokens it holds for the person: more than one
	// when it was authorized more than once.
	TokenCount int `json:"tokenCount"`
}

type ListAppsArguments struct {
	// Whose apps. Only the console may name somebody else; see owner.
	Username *string `json:"username" graphapi:"nullable"`
}

func (self *graph) ListApps(ctx context.Context, arguments ListAppsArguments) ([]*App, error) {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return nil, err
	}
	username, err := self.owner(ctx, arguments.Username)
	if err != nil {
		return nil, err
	}
	tokens, err := self.authenticator.ListTokens(username, false)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	apps := map[string]*App{}
	for _, token := range tokens {
		if token.ClientID == "" || !token.Refreshable(now) {
			continue
		}
		app := apps[token.ClientID]
		if app == nil {
			app = &App{ClientID: token.ClientID, Name: token.Name, RedirectHosts: []string{}}
			apps[token.ClientID] = app
		}
		app.TokenCount++
		// The newest token speaks for the app: its name, and when it
		// runs out.
		if expires := optionalTime(token.ExpiresAt); expires != nil && (app.Expires == nil || expires.After(*app.Expires)) {
			app.Expires = expires
			app.Name = token.Name
			renewable := expires.Add(models.RefreshWindow)
			app.RenewableUntil = &renewable
		}
		if used := optionalTime(token.UsedAt); used != nil && (app.LastUsed == nil || used.After(*app.LastUsed)) {
			app.LastUsed = used
			app.LastUsedIP = token.IP
		}
	}
	listed := make([]*App, 0, len(apps))
	for _, app := range apps {
		client, err := self.database.GetOAuthClient(app.ClientID)
		if err != nil {
			return nil, err
		}
		if client != nil {
			app.RegisteredName = client.Name
			app.Registered = optionalTime(client.CreatedAt)
			for _, address := range client.RedirectURIs {
				if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
					app.RedirectHosts = append(app.RedirectHosts, parsed.Host)
				}
			}
		}
		listed = append(listed, app)
	}
	sort.Slice(listed, func(left, right int) bool {
		return timeOf(listed[left].LastUsed).After(timeOf(listed[right].LastUsed))
	})
	return listed, nil
}

// timeOf is a time that may be null, as the zero time.
func timeOf(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

type RenameAppArguments struct {
	ClientID string `json:"clientId"`
	Name     string `json:"name"`

	// Whose app. Only the console may name somebody else; see owner.
	Username *string `json:"username" graphapi:"nullable"`
}

func (self *graph) RenameApp(ctx context.Context, arguments RenameAppArguments) error {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return err
	}
	username, err := self.owner(ctx, arguments.Username)
	if err != nil {
		return err
	}
	return self.authenticator.RenameApp(username, arguments.ClientID, arguments.Name)
}

type DisconnectAppArguments struct {
	ClientID string `json:"clientId"`

	// Whose app. Only the console may name somebody else; see owner.
	Username *string `json:"username" graphapi:"nullable"`
}

func (self *graph) DisconnectApp(ctx context.Context, arguments DisconnectAppArguments) error {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return err
	}
	username, err := self.owner(ctx, arguments.Username)
	if err != nil {
		return err
	}
	return self.authenticator.DisconnectApp(username, arguments.ClientID)
}
