package client

import "context"

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

// CreatedCredential is a new credential with the settings to enter into a mail
// client.
type CreatedCredential struct {
	Credential *Credential `json:"credential"`
	Host       string      `json:"host"`
	Port       string      `json:"port"`
	Username   string      `json:"username"`
	Password   string      `json:"password"`
}

const credentialFields = `{ id comment alias disabled }`

// ListCredentials returns the credentials of one domain.
func ListCredentials(ctx context.Context, connection *Client, domainId string) ([]*Credential, error) {
	var result struct {
		ListCredentials []*Credential `json:"ListCredentials"`
	}
	query := `query ($domainId: String!) { ListCredentials(domainId: $domainId) ` + credentialFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId}, &result); err != nil {
		return nil, err
	}
	return result.ListCredentials, nil
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

// CredentialParameters are the settings of a credential an operator can set.
type CredentialParameters struct {
	Comment  *string `json:"comment,omitempty"`
	Alias    *string `json:"alias,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
}

// CreateCredential issues a credential for a domain.
func CreateCredential(ctx context.Context, connection *Client, domainId string, parameters *CredentialParameters) (*CreatedCredential, error) {
	var result struct {
		CreateCredential *CreatedCredential `json:"CreateCredential"`
	}
	query := `mutation ($domainId: String!, $credentialParameters: CredentialParametersInput) {
		CreateCredential(domainId: $domainId, credentialParameters: $credentialParameters) {
			credential ` + credentialFields + `
			host
			port
			username
			password
		}
	}`
	variables := map[string]any{"domainId": domainId, "credentialParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateCredential, nil
}

// UpdateCredential changes a credential's comment, alias restriction or
// whether it is refused.
func UpdateCredential(ctx context.Context, connection *Client, credentialId string, parameters *CredentialParameters) (*Credential, error) {
	var result struct {
		UpdateCredential *Credential `json:"UpdateCredential"`
	}
	query := `mutation ($credentialId: String!, $credentialParameters: CredentialParametersInput) {
		UpdateCredential(credentialId: $credentialId, credentialParameters: $credentialParameters) ` + credentialFields + `
	}`
	variables := map[string]any{"credentialId": credentialId, "credentialParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateCredential, nil
}

// DeleteCredential removes a credential.
func DeleteCredential(ctx context.Context, connection *Client, credentialId string) error {
	query := `mutation ($credentialId: String!) { DeleteCredential(credentialId: $credentialId) }`
	return connection.Execute(ctx, query, map[string]any{"credentialId": credentialId}, nil)
}
