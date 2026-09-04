package client

import "context"

// User is an account that may administer a server.
type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

const userFields = `{ username name email }`

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
	query := `mutation ($username: String!, $name: String, $email: String, $newUsername: String) {
		UpdateUser(username: $username, name: $name, email: $email, newUsername: $newUsername) ` + userFields + `
	}`
	variables := map[string]any{"username": username}
	if parameters.Name != nil {
		variables["name"] = *parameters.Name
	}
	if parameters.Email != nil {
		variables["email"] = *parameters.Email
	}
	if parameters.NewUsername != nil {
		variables["newUsername"] = *parameters.NewUsername
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
	query := `mutation ($username: String!, $password: String!) {
		SetUserPassword(username: $username, password: $password) ` + userFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"username": username, "password": password}, &result); err != nil {
		return nil, err
	}
	return result.SetUserPassword, nil
}

// DeleteUser removes an account and the tokens issued to it.
func DeleteUser(ctx context.Context, connection *Client, username string) error {
	query := `mutation ($username: String!) { DeleteUser(username: $username) }`
	return connection.Execute(ctx, query, map[string]any{"username": username}, nil)
}
