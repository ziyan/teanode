package client

import "context"

// Alias matches recipient addresses and says where the mail goes.
type Alias struct {
	ID         string      `json:"id"`
	Pattern    string      `json:"pattern"`
	Comment    string      `json:"comment"`
	Kind       string      `json:"kind"`
	Email      string      `json:"email"`
	Webhook    string      `json:"webhook"`
	MailServer *MailServer `json:"mailServer"`
	Disabled   bool        `json:"disabled"`
}

// MailServer is a downstream server an alias relays to. The password is
// never returned.
type MailServer struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Username string `json:"username"`
}

// Destination is where an alias sends mail, as one string for a table.
func (self *Alias) Destination() string {
	switch self.Kind {
	case "email":
		return self.Email
	case "webhook":
		return self.Webhook
	case "mailServer":
		if self.MailServer == nil {
			return ""
		}
		return self.MailServer.Host + ":" + itoa(int(self.MailServer.Port))
	default:
		return "(discarded)"
	}
}

const aliasFields = `{ id pattern comment kind email webhook mailServer { host port username } disabled }`

// ListAliases returns a domain's aliases in the order they are evaluated.
func ListAliases(ctx context.Context, connection *Client, domainId string) ([]*Alias, error) {
	var result struct {
		ListAliases []*Alias `json:"ListAliases"`
	}
	query := `query ($domainId: String!) { ListAliases(domainId: $domainId) ` + aliasFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId}, &result); err != nil {
		return nil, err
	}
	return result.ListAliases, nil
}

// MatchAliases returns the aliases an address would match.
func MatchAliases(ctx context.Context, connection *Client, domainId, address string) ([]*Alias, error) {
	var result struct {
		MatchAliases []*Alias `json:"MatchAliases"`
	}
	query := `query ($domainId: String!, $address: String!) {
		MatchAliases(domainId: $domainId, address: $address) ` + aliasFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId, "address": address}, &result); err != nil {
		return nil, err
	}
	return result.MatchAliases, nil
}

// AliasParameters are the settings of an alias an operator can set. Pattern
// and Kind left empty keep what is stored; the pointers left nil do too.
type AliasParameters struct {
	Pattern    string                `json:"pattern"`
	Comment    *string               `json:"comment,omitempty"`
	Kind       string                `json:"kind"`
	Email      *string               `json:"email,omitempty"`
	Webhook    *string               `json:"webhook,omitempty"`
	MailServer *MailServerParameters `json:"mailServer,omitempty"`
	Disabled   *bool                 `json:"disabled,omitempty"`
}

// MailServerParameters describes a downstream server to relay to.
type MailServerParameters struct {
	Host     string  `json:"host"`
	Port     uint16  `json:"port"`
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}

// CreateAlias adds an alias to a domain.
func CreateAlias(ctx context.Context, connection *Client, domainId string, parameters *AliasParameters) (*Alias, error) {
	var result struct {
		CreateAlias *Alias `json:"CreateAlias"`
	}
	query := `mutation ($domainId: String!, $aliasParameters: AliasParametersInput) {
		CreateAlias(domainId: $domainId, aliasParameters: $aliasParameters) ` + aliasFields + `
	}`
	variables := map[string]any{"domainId": domainId, "aliasParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateAlias, nil
}

// UpdateAlias changes an alias.
func UpdateAlias(ctx context.Context, connection *Client, aliasId string, parameters *AliasParameters) (*Alias, error) {
	var result struct {
		UpdateAlias *Alias `json:"UpdateAlias"`
	}
	query := `mutation ($aliasId: String!, $aliasParameters: AliasParametersInput) {
		UpdateAlias(aliasId: $aliasId, aliasParameters: $aliasParameters) ` + aliasFields + `
	}`
	variables := map[string]any{"aliasId": aliasId, "aliasParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateAlias, nil
}

// DeleteAlias removes an alias.
func DeleteAlias(ctx context.Context, connection *Client, aliasId string) error {
	query := `mutation ($aliasId: String!) { DeleteAlias(aliasId: $aliasId) }`
	return connection.Execute(ctx, query, map[string]any{"aliasId": aliasId}, nil)
}
