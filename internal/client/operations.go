package client

import (
	"context"
	"strings"
	"time"
)

// The queries and mutations the command line client uses. They are written out
// rather than generated, because there are few of them and a handwritten query
// asks for exactly the fields the command prints.

// User is an account that may administer a server.
type User struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

// ListUsers returns the accounts configured on the server.
func ListUsers(ctx context.Context, connection *Client) ([]*User, error) {
	var result struct {
		ListUsers []*User `json:"ListUsers"`
	}
	if err := connection.Execute(ctx, `query { ListUsers { username email } }`, nil, &result); err != nil {
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
	if err := connection.Execute(ctx, `query { GetCurrentUser { username email } }`, nil, &result); err != nil {
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
		CreateUser(username: $username, password: $password, email: $email) { username email }
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

// SetUserPassword replaces an account's password.
func SetUserPassword(ctx context.Context, connection *Client, username, password string) (*User, error) {
	var result struct {
		SetUserPassword *User `json:"SetUserPassword"`
	}
	query := `mutation ($username: String!, $password: String!) {
		SetUserPassword(username: $username, password: $password) { username email }
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

// Token is an issued API token, without its secret.
type Token struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Username string     `json:"username"`
	Created  time.Time  `json:"created"`
	Expires  *time.Time `json:"expires"`
	LastUsed *time.Time `json:"lastUsed"`
	Revoked  *time.Time `json:"revoked"`
}

const tokenFields = `id name username created expires lastUsed revoked`

// ListTokens returns the tokens belonging to the account this client is
// authenticated as.
//
// There is no listing somebody else's: a token acts as the person it belongs
// to, so a list of theirs is a list of ways to become them.
func ListTokens(ctx context.Context, connection *Client, username string, includeRevoked bool) ([]*Token, error) {
	var result struct {
		ListTokens []*Token `json:"ListTokens"`
	}
	query := `query ($username: String, $includeRevoked: Boolean) {
		ListTokens(username: $username, includeRevoked: $includeRevoked) { ` + tokenFields + ` }
	}`
	variables := map[string]any{"includeRevoked": includeRevoked}
	if username != "" {
		variables["username"] = username
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ListTokens, nil
}

// CreateToken issues a token and returns it. The secret is not recoverable
// afterwards.
func CreateToken(ctx context.Context, connection *Client, name, username, lifetime string) (*Token, string, error) {
	var result struct {
		CreateToken struct {
			Token  *Token `json:"token"`
			Secret string `json:"secret"`
		} `json:"CreateToken"`
	}
	query := `mutation ($name: String!, $username: String, $lifetime: String) {
		CreateToken(name: $name, username: $username, lifetime: $lifetime) {
			token { ` + tokenFields + ` }
			secret
		}
	}`
	variables := map[string]any{"name": name}
	if username != "" {
		variables["username"] = username
	}
	if lifetime != "" {
		variables["lifetime"] = lifetime
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, "", err
	}
	return result.CreateToken.Token, result.CreateToken.Secret, nil
}

// DeleteToken revokes a token.
func DeleteToken(ctx context.Context, connection *Client, id string) error {
	query := `mutation ($tokenId: String!) { DeleteToken(tokenId: $tokenId) }`
	return connection.Execute(ctx, query, map[string]any{"tokenId": id}, nil)
}

// Domain is a mail domain the server accepts mail for.
type Domain struct {
	ID           string     `json:"id"`
	Domain       string     `json:"domain"`
	Subdomain    string     `json:"subdomain"`
	Comment      string     `json:"comment"`
	DKIMSelector string     `json:"dkimSelector"`
	HasDKIMKey   bool       `json:"hasDkimKey"`
	Records      *RecordSet `json:"records"`
}

// RecordSet is the DNS a domain needs, and what is published now.
type RecordSet struct {
	Records []*Record `json:"records"`
}

// Record is one DNS record a domain needs.
type Record struct {
	Type     string   `json:"type"`
	Name     string   `json:"name"`
	Expected string   `json:"expected"`
	Found    []string `json:"found"`
	Verified bool     `json:"verified"`
	Purpose  string   `json:"purpose"`
}

// FindRecord returns the record of a type whose name matches, or nil.
//
// The trailing dot is ignored on both sides: the server reports fully
// qualified names, and a caller building one from a domain and a selector will
// not have written it.
func (self *RecordSet) FindRecord(recordType, name string) *Record {
	if self == nil {
		return nil
	}
	for _, record := range self.Records {
		if record.Type == recordType && equalFold(trimDot(record.Name), trimDot(name)) {
			return record
		}
	}
	return nil
}

func trimDot(name string) string {
	return strings.TrimSuffix(strings.TrimSpace(name), ".")
}

// ListDomains returns the configured domains.
func ListDomains(ctx context.Context, connection *Client) ([]*Domain, error) {
	var result struct {
		ListDomains []*Domain `json:"ListDomains"`
	}
	query := domainFields("query { ListDomains %s }")
	if err := connection.Execute(ctx, query, nil, &result); err != nil {
		return nil, err
	}
	return result.ListDomains, nil
}

// FindDomain returns the configured domain with this name, matched case
// insensitively, or nil.
func FindDomain(ctx context.Context, connection *Client, name string) (*Domain, error) {
	domains, err := ListDomains(ctx, connection)
	if err != nil {
		return nil, err
	}
	for _, domain := range domains {
		if equalFold(domain.Domain, name) {
			return domain, nil
		}
	}
	return nil, nil
}

// RegenerateDomainKey replaces a domain's signing key.
func RegenerateDomainKey(ctx context.Context, connection *Client, domainId string) (*Domain, error) {
	var result struct {
		RegenerateDomainKey *Domain `json:"RegenerateDomainKey"`
	}
	query := domainFields("mutation ($domainId: String!) { RegenerateDomainKey(domainId: $domainId) %s }")
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId}, &result); err != nil {
		return nil, err
	}
	return result.RegenerateDomainKey, nil
}

// Credential may send mail as a domain.
type Credential struct {
	ID       string `json:"id"`
	Comment  string `json:"comment"`
	Alias    string `json:"alias"`
	Disabled bool   `json:"disabled"`
}

// CredentialSettings is what to enter into a mail client.
type CredentialSettings struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ListCredentials returns the credentials of one domain.
func ListCredentials(ctx context.Context, connection *Client, domainId string) ([]*Credential, error) {
	var result struct {
		ListCredentials []*Credential `json:"ListCredentials"`
	}
	query := `query ($domainId: String!) {
		ListCredentials(domainId: $domainId) { id comment alias disabled }
	}`
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId}, &result); err != nil {
		return nil, err
	}
	return result.ListCredentials, nil
}

// CreatedCredential is a new credential with the settings to enter into a mail
// client.
type CreatedCredential struct {
	Credential *Credential `json:"credential"`
	Host       string      `json:"host"`
	Port       string      `json:"port"`
	Username   string      `json:"username"`
	Password   string      `json:"password"`
}

// GetCredentialSettings returns the SMTP settings for an existing credential,
// including its password.
func GetCredentialSettings(ctx context.Context, connection *Client, domainId, credentialId string) (*CredentialSettings, error) {
	var result struct {
		GetCredentialSettings *CredentialSettings `json:"GetCredentialSettings"`
	}
	query := `query ($domainId: String!, $credentialId: String!) {
		GetCredentialSettings(domainId: $domainId, credentialId: $credentialId) { host port username password }
	}`
	variables := map[string]any{"domainId": domainId, "credentialId": credentialId}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.GetCredentialSettings, nil
}

// CreateCredential issues a credential for a domain.
func CreateCredential(ctx context.Context, connection *Client, domainId, comment, alias string) (*CreatedCredential, error) {
	var result struct {
		CreateCredential *CreatedCredential `json:"CreateCredential"`
	}
	query := `mutation ($domainId: String!, $credentialParameters: CredentialParametersInput) {
		CreateCredential(domainId: $domainId, credentialParameters: $credentialParameters) {
			credential { id comment alias disabled }
			host
			port
			username
			password
		}
	}`
	parameters := map[string]any{}
	if comment != "" {
		parameters["comment"] = comment
	}
	if alias != "" {
		parameters["alias"] = alias
	}
	variables := map[string]any{"domainId": domainId, "credentialParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateCredential, nil
}

// DeleteCredential removes a credential.
func DeleteCredential(ctx context.Context, connection *Client, domainId, credentialId string) error {
	query := `mutation ($domainId: String!, $credentialId: String!) {
		DeleteCredential(domainId: $domainId, credentialId: $credentialId)
	}`
	return connection.Execute(ctx, query, map[string]any{"domainId": domainId, "credentialId": credentialId}, nil)
}

// equalFold compares domain names, which are case insensitive.
func equalFold(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

// domainFields fills in the selection every domain query asks for, so that the
// Domain struct above and the queries cannot drift apart.
func domainFields(query string) string {
	return strings.Replace(query, "%s", `{
		id
		domain
		subdomain
		comment
		dkimSelector
		hasDkimKey
		records { records { type name expected found verified purpose } }
	}`, 1)
}
