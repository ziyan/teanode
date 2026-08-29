package apigraph

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/security"
)

type UserQuery interface {
	// List the people who may administer this server
	ListUsers(ctx context.Context) ([]*User, error)

	// Get the User this request is authenticated as, or null when the server
	// has no accounts and is open to whoever can reach it
	GetCurrentUser(ctx context.Context) (*User, error)
}

type UserMutation interface {
	// Add a User who may administer this server
	CreateUser(ctx context.Context, arguments CreateUserArguments) (*User, error)

	// Change a User's name, email address, or the username they sign in with
	UpdateUser(ctx context.Context, arguments UpdateUserArguments) (*User, error)

	// Set a User's password. The current password is not required, because
	// the caller is already an operator; changing your own from the dashboard
	// goes through /api/password, which does require it.
	SetUserPassword(ctx context.Context, arguments SetUserPasswordArguments) (*User, error)

	// Remove a User, along with the API tokens issued to them. Removing the
	// last one leaves the server unclaimed, so the next visitor is asked to
	// create an account.
	DeleteUser(ctx context.Context, arguments DeleteUserArguments) error
}

// User is somebody who may administer this server.
type User struct {
	// Username they log in with
	Username string `json:"username"`

	// What to call this person, when they have said. Empty otherwise; the
	// dashboard falls back to the username.
	Name string `json:"name,omitempty"`

	// Address that receives notifications, such as a domain whose DNS records
	// have stopped resolving
	Email string `json:"email,omitempty"`
}

func describeUser(user *config.User) *User {
	if user == nil {
		return nil
	}
	return &User{Username: user.Username, Name: user.Name, Email: user.Email}
}

func (self *graph) ListUsers(ctx context.Context) ([]*User, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	users := make([]*User, 0, len(configuration.Users))
	for _, user := range configuration.Users {
		users = append(users, describeUser(user))
	}
	return users, nil
}

func (self *graph) GetCurrentUser(ctx context.Context) (*User, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	username := api.ContextAuthenticatedUsername(ctx)
	if username == "" || username == config.LocalUsername {
		return nil, nil
	}
	return describeUser(self.config.Current().FindUser(username)), nil
}

type CreateUserArguments struct {
	// Username they will log in with
	Username string `json:"username"`

	// Password they will log in with
	Password string `json:"password"`

	// Address that receives notifications
	Email *string `json:"email"`
}

func (self *graph) CreateUser(ctx context.Context, arguments CreateUserArguments) (*User, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	username := strings.TrimSpace(arguments.Username)
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if arguments.Password == "" {
		return nil, api.ErrInvalidArguments
	}

	hash, err := security.HashPassword(arguments.Password)
	if err != nil {
		return nil, err
	}

	// The identifier is minted here rather than when the row is written, so
	// that the account has one the moment anything can refer to it.
	created := &config.User{ID: config.NewID(), Username: username, PasswordHash: string(hash)}
	if arguments.Email != nil {
		created.Email = strings.TrimSpace(*arguments.Email)
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		if configuration.FindUser(username) != nil {
			return api.ErrAlreadyExists
		}
		configuration.Users = append(configuration.Users, created)
		return nil
	}); err != nil {
		return nil, err
	}

	log.Noticef("%s created the account %q", api.ContextAuthenticatedUsername(ctx), username)
	return describeUser(created), nil
}

type UpdateUserArguments struct {
	// Username of the User to change
	Username string `json:"username"`

	// What to call this person. Empty clears it.
	Name *string `json:"name"`

	// Address that receives notifications
	Email *string `json:"email"`

	// The username to sign in with from now on. Sessions and API tokens move
	// with the account, so nobody is signed out by their own rename.
	NewUsername *string `json:"newUsername"`
}

func (self *graph) UpdateUser(ctx context.Context, arguments UpdateUserArguments) (*User, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	renamed := ""
	if arguments.NewUsername != nil {
		candidate := strings.TrimSpace(*arguments.NewUsername)
		if candidate != arguments.Username {
			if err := validateUsername(candidate); err != nil {
				return nil, err
			}
			renamed = candidate
		}
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		user := configuration.FindUser(arguments.Username)
		if user == nil {
			return api.ErrNotFound
		}
		if renamed != "" {
			// Case-insensitively too: two accounts differing only in case are
			// two accounts nobody can tell apart on a sign-in form.
			for _, other := range configuration.Users {
				if other != nil && other != user && strings.EqualFold(other.Username, renamed) {
					return api.ErrAlreadyExists
				}
			}
			user.Username = renamed
		}
		if arguments.Name != nil {
			user.Name = strings.TrimSpace(*arguments.Name)
		}
		if arguments.Email != nil {
			user.Email = strings.TrimSpace(*arguments.Email)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	username := arguments.Username
	if renamed != "" {
		username = renamed
		// Nothing else to move. Sessions, tokens and passkeys name the
		// account by its identifier, which a rename does not touch — which is
		// the whole reason the account has one.
		log.Noticef("%s renamed the account %q to %q", api.ContextAuthenticatedUsername(ctx), arguments.Username, renamed)
	}

	return describeUser(self.config.Current().FindUser(username)), nil
}

type SetUserPasswordArguments struct {
	// Username of the User whose password to set
	Username string `json:"username"`

	// The new password
	Password string `json:"password"`
}

func (self *graph) SetUserPassword(ctx context.Context, arguments SetUserPasswordArguments) (*User, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	if arguments.Password == "" {
		return nil, api.ErrInvalidArguments
	}

	hash, err := security.HashPassword(arguments.Password)
	if err != nil {
		return nil, err
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		user := configuration.FindUser(arguments.Username)
		if user == nil {
			return api.ErrNotFound
		}
		user.PasswordHash = string(hash)
		return nil
	}); err != nil {
		return nil, err
	}

	log.Noticef("%s set the password for %q", api.ContextAuthenticatedUsername(ctx), arguments.Username)
	return describeUser(self.config.Current().FindUser(arguments.Username)), nil
}

type DeleteUserArguments struct {
	// Username of the User to remove
	Username string `json:"username"`
}

func (self *graph) DeleteUser(ctx context.Context, arguments DeleteUserArguments) error {
	if err := self.requireOperator(ctx); err != nil {
		return err
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		if configuration.FindUser(arguments.Username) == nil {
			return api.ErrNotFound
		}
		remaining := make([]*config.User, 0, len(configuration.Users))
		for _, user := range configuration.Users {
			if user != nil && user.Username != arguments.Username {
				remaining = append(remaining, user)
			}
		}
		// The account's tokens go with it, because they live inside it.
		configuration.Users = remaining
		return nil
	}); err != nil {
		return err
	}

	log.Noticef("%s removed the account %q", api.ContextAuthenticatedUsername(ctx), arguments.Username)
	if len(self.config.Current().Users) == 0 {
		log.Warningf("no accounts remain; this server is unclaimed and the next visitor can take it")
	}
	return nil
}

// validateUsername applies the same rule the first-run setup does, so an
// account added later cannot be one that could not have been created first.
func validateUsername(username string) error {
	if username == "" || len(username) > 64 || strings.ContainsAny(username, " \t\r\n") {
		return api.ErrInvalidArguments
	}
	return nil
}
