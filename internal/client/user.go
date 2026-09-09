package client

import (
	"context"
	"fmt"
)

// User is an account that may administer a server.
type User struct {
	// ID is what other rows hold a user by: a group's membership, an audit
	// event's actor.
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

const userFields = `{ id username name email }`

// ListUsers returns the accounts configured on the server.
func ListUsers(ctx context.Context, connection *Client) ([]*User, error) {
	var result struct {
		ListUsers []*User `json:"ListUsers"`
	}
	if err := connection.Execute(ctx, `query { ListUsers `+userFields+` }`, nil, &result); err != nil {
		return nil, err
	}
	return result.ListUsers, nil
}

// GetCurrentUser returns the account this connection authenticates as, or nil
// when it authenticates as something that is not an account — which is what a
// locally minted token does.
func GetCurrentUser(ctx context.Context, connection *Client) (*User, error) {
	var result struct {
		GetCurrentUser *User `json:"GetCurrentUser"`
	}
	if err := connection.Execute(ctx, `query { GetCurrentUser `+userFields+` }`, nil, &result); err != nil {
		return nil, err
	}
	return result.GetCurrentUser, nil
}

// CreateUser adds an account.
func CreateUser(ctx context.Context, connection *Client, username, password, email string) (*User, error) {
	var result struct {
		CreateUser *User `json:"CreateUser"`
	}
	query := `mutation ($username: String!, $password: String!, $email: String) {
		CreateUser(username: $username, password: $password, email: $email) ` + userFields + `
	}`
	variables := map[string]any{"username": username, "password": password}
	if email != "" {
		variables["email"] = email
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateUser, nil
}

// userIdFor turns the username the command line speaks in into the identifier
// the schema takes. There is no lookup by name in the schema, so this asks
// the two questions that exist.
//
// The signed-in account first, because changing your own name and password is
// allowed without user:manage and ListUsers is not: asking the other way round
// would refuse an operation the server permits.
func userIdFor(ctx context.Context, connection *Client, username string) (string, error) {
	current, err := GetCurrentUser(ctx, connection)
	if err != nil {
		return "", err
	}
	if current != nil && current.Username == username {
		return current.ID, nil
	}

	users, err := ListUsers(ctx, connection)
	if err != nil {
		return "", err
	}
	for _, user := range users {
		if user.Username == username {
			return user.ID, nil
		}
	}
	return "", fmt.Errorf("client: there is no account called %q", username)
}

// UserParameters are what UpdateUser can change. A nil field is left alone.
type UserParameters struct {
	Name        *string
	Email       *string
	NewUsername *string
}

// UpdateUser changes an account's name, address or username.
func UpdateUser(ctx context.Context, connection *Client, username string, parameters *UserParameters) (*User, error) {
	var result struct {
		UpdateUser *User `json:"UpdateUser"`
	}
	userId, err := userIdFor(ctx, connection, username)
	if err != nil {
		return nil, err
	}
	query := `mutation ($userId: String!, $name: String, $email: String, $username: String) {
		UpdateUser(userId: $userId, name: $name, email: $email, username: $username) ` + userFields + `
	}`
	variables := map[string]any{"userId": userId}
	if parameters.Name != nil {
		variables["name"] = *parameters.Name
	}
	if parameters.Email != nil {
		variables["email"] = *parameters.Email
	}
	if parameters.NewUsername != nil {
		variables["username"] = *parameters.NewUsername
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateUser, nil
}

// SetUserPassword replaces an account's password.
func SetUserPassword(ctx context.Context, connection *Client, username, password string) (*User, error) {
	var result struct {
		SetUserPassword *User `json:"SetUserPassword"`
	}
	userId, err := userIdFor(ctx, connection, username)
	if err != nil {
		return nil, err
	}
	query := `mutation ($userId: String!, $password: String!) {
		SetUserPassword(userId: $userId, password: $password) ` + userFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"userId": userId, "password": password}, &result); err != nil {
		return nil, err
	}
	return result.SetUserPassword, nil
}

// DeleteUser removes an account and the tokens issued to it.
func DeleteUser(ctx context.Context, connection *Client, username string) error {
	userId, err := userIdFor(ctx, connection, username)
	if err != nil {
		return err
	}
	query := `mutation ($userId: String!) { DeleteUser(userId: $userId) }`
	return connection.Execute(ctx, query, map[string]any{"userId": userId}, nil)
}
