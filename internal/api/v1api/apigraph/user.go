package apigraph

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

type UserQuery interface {
	// List every User. Needs user:manage.
	ListUsers(ctx context.Context) ([]*User, error)

	// Get the User this request is authenticated as, or null for the
	// console and for a server that has no accounts
	GetCurrentUser(ctx context.Context) (*User, error)
}

type UserMutation interface {
	// Add a User, into the groups named or into Members when none is
	CreateUser(ctx context.Context, arguments CreateUserArguments) (*User, error)

	// Change a User: their name, email address, the username they sign in
	// with, whether they may sign in at all, and which groups they are in
	UpdateUser(ctx context.Context, arguments UpdateUserArguments) (*User, error)

	// Set a User's password. The current password is not required, because
	// the caller manages users; changing your own goes through ChangePassword,
	// which does require it.
	SetUserPassword(ctx context.Context, arguments SetUserPasswordArguments) (*User, error)

	// Remove a User, along with their sessions, tokens, passkeys and
	// memberships. Removing the last one leaves the server unclaimed.
	DeleteUser(ctx context.Context, arguments DeleteUserArguments) error
}

// User is somebody with an account on this server.
type User struct {
	// ID of the User, stable for its lifetime
	ID string `json:"id"`

	// Username they log in with
	Username string `json:"username"`

	// What to call this person, when they have said. Empty otherwise; the
	// web UI falls back to the username.
	Name string `json:"name,omitempty"`

	// Address that receives notifications, such as a domain whose DNS records
	// have stopped resolving
	Email string `json:"email,omitempty"`

	// When this person was disabled, or null while they may sign in
	DisabledAt *time.Time `json:"disabledAt,omitempty"`

	// Whether they have a password at all. One without signs in with a
	// passkey or through an identity provider.
	HasPassword bool `json:"hasPassword"`

	// Locale the web UI greets them in, when they chose one
	Locale string `json:"locale,omitempty"`

	// The groups this person is in
	GroupIDs []string `json:"groupIds"`

	CreatedAt time.Time `json:"createdAt"`
}

func describeUser(user *models.User) *User {
	if user == nil {
		return nil
	}
	groupIds := user.GroupIDs
	if groupIds == nil {
		groupIds = []string{}
	}
	return &User{
		ID:          user.ID,
		Username:    user.Username,
		Name:        user.Name,
		Email:       user.Email,
		DisabledAt:  user.DisabledAt,
		HasPassword: user.PasswordHash != "",
		Locale:      user.Locale,
		GroupIDs:    groupIds,
		CreatedAt:   user.CreatedAt,
	}
}

func (self *graph) ListUsers(ctx context.Context) ([]*User, error) {
	if _, err := self.requirePermission(ctx, models.PermissionUserManage); err != nil {
		return nil, err
	}
	stored, err := self.transaction(ctx).ListUsers()
	if err != nil {
		return nil, err
	}
	users := make([]*User, 0, len(stored))
	for _, user := range stored {
		users = append(users, describeUser(user))
	}
	return users, nil
}

func (self *graph) GetCurrentUser(ctx context.Context) (*User, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	return describeUser(principal.User), nil
}

type CreateUserArguments struct {
	// Username they will log in with
	Username string `json:"username"`

	// Password they will log in with. Empty leaves them without one, for a
	// person who will sign in through an identity provider.
	Password *string `json:"password"`

	// What to call this person
	Name *string `json:"name"`

	// Address that receives notifications
	Email *string `json:"email"`

	// Groups to put them in. Omitted means Members, so that a person made
	// here can read the mailbox they are about to be given.
	GroupIDs *[]string `json:"groupIds"`
}

func (self *graph) CreateUser(ctx context.Context, arguments CreateUserArguments) (*User, error) {
	if _, err := self.requirePermission(ctx, models.PermissionUserManage); err != nil {
		return nil, err
	}
	username := strings.TrimSpace(arguments.Username)
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	created := &models.User{Username: username}
	if arguments.Password != nil && *arguments.Password != "" {
		hash, err := security.HashPassword(*arguments.Password)
		if err != nil {
			return nil, err
		}
		created.PasswordHash = string(hash)
	}
	if arguments.Name != nil {
		created.Name = strings.TrimSpace(*arguments.Name)
	}
	if arguments.Email != nil {
		created.Email = strings.TrimSpace(*arguments.Email)
	}
	tx := self.transaction(ctx)
	if arguments.GroupIDs != nil {
		created.GroupIDs = *arguments.GroupIDs
	} else if members, err := tx.GetGroupByName(models.GroupNameMembers); err != nil {
		return nil, err
	} else if members != nil {
		created.GroupIDs = []string{members.ID}
	}
	stored, err := tx.CreateUser(created)
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s created the account %q", operatorName(ctx), stored.Username)
	return describeUser(stored), nil
}

type UpdateUserArguments struct {
	// ID of the User to change
	UserID string `json:"userId"`

	// What to call this person. Empty clears it.
	Name *string `json:"name"`

	// Address that receives notifications
	Email *string `json:"email"`

	// The username to sign in with from now on. Sessions and API tokens move
	// with the account, so nobody is signed out by their own rename.
	Username *string `json:"username"`

	// Whether this person may sign in. Disabling keeps everything of theirs.
	Disabled *bool `json:"disabled"`

	// The groups this person is in, replacing the current list
	GroupIDs *[]string `json:"groupIds"`

	// Locale the web UI greets them in; empty means the browser's
	Locale *string `json:"locale"`
}

func (self *graph) UpdateUser(ctx context.Context, arguments UpdateUserArguments) (*User, error) {
	principal, err := self.requirePermission(ctx, models.PermissionUserManage)
	if err != nil {
		return nil, err
	}
	if arguments.Username != nil {
		if err := validateUsername(strings.TrimSpace(*arguments.Username)); err != nil {
			return nil, err
		}
	}
	updated, err := self.transaction(ctx).UpdateUser(arguments.UserID, func(user *models.User) error {
		if arguments.Username != nil {
			user.Username = strings.TrimSpace(*arguments.Username)
		}
		if arguments.Name != nil {
			user.Name = strings.TrimSpace(*arguments.Name)
		}
		if arguments.Email != nil {
			user.Email = strings.TrimSpace(*arguments.Email)
		}
		if arguments.Locale != nil {
			user.Locale = strings.TrimSpace(*arguments.Locale)
		}
		if arguments.Disabled != nil {
			if *arguments.Disabled && user.ID == principal.UserID() {
				// Disabling oneself is a lock-out with nobody left to undo
				// it from the same screen.
				return api.ErrInvalidArguments
			}
			if *arguments.Disabled && user.DisabledAt == nil {
				now := time.Now()
				user.DisabledAt = &now
			} else if !*arguments.Disabled {
				user.DisabledAt = nil
			}
		}
		if arguments.GroupIDs != nil {
			user.GroupIDs = *arguments.GroupIDs
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s changed the account %q", operatorName(ctx), updated.Username)
	return describeUser(updated), nil
}

type SetUserPasswordArguments struct {
	// ID of the User whose password to set
	UserID string `json:"userId"`

	// The new password
	Password string `json:"password"`
}

func (self *graph) SetUserPassword(ctx context.Context, arguments SetUserPasswordArguments) (*User, error) {
	if _, err := self.requirePermission(ctx, models.PermissionUserManage); err != nil {
		return nil, err
	}
	if arguments.Password == "" {
		return nil, api.ErrInvalidArguments
	}
	hash, err := security.HashPassword(arguments.Password)
	if err != nil {
		return nil, err
	}
	updated, err := self.transaction(ctx).UpdateUser(arguments.UserID, func(user *models.User) error {
		user.PasswordHash = string(hash)
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s set the password for %q", operatorName(ctx), updated.Username)
	return describeUser(updated), nil
}

type DeleteUserArguments struct {
	// ID of the User to remove
	UserID string `json:"userId"`
}

func (self *graph) DeleteUser(ctx context.Context, arguments DeleteUserArguments) error {
	principal, err := self.requirePermission(ctx, models.PermissionUserManage)
	if err != nil {
		return err
	}
	if arguments.UserID == principal.UserID() {
		return api.ErrInvalidArguments
	}
	tx := self.transaction(ctx)
	user, err := tx.GetUser(arguments.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		return api.ErrNotFound
	}
	if err := tx.DeleteUser(user.ID); err != nil {
		return translateError(err)
	}
	log.Noticef("%s removed the account %q", operatorName(ctx), user.Username)
	if count, err := tx.CountUsers(); err == nil && count == 0 {
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
