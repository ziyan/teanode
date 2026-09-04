package client

import (
	"context"
	"strings"
	"time"
)

// Domain is a mail domain the server accepts mail for, as the dashboard sees
// it: the configured settings plus the live state of its DNS records.
type Domain struct {
	ID                       string        `json:"id"`
	Domain                   string        `json:"domain"`
	Subdomain                string        `json:"subdomain"`
	Comment                  string        `json:"comment"`
	SpamFilterScoreThreshold float64       `json:"spamFilterScoreThreshold"`
	MailServers              []string      `json:"mailServers"`
	MailHosts                []string      `json:"mailHosts"`
	LinkHost                 string        `json:"linkHost"`
	LinkHostname             string        `json:"linkHostname"`
	DKIMSelector             string        `json:"dkimSelector"`
	HasDKIMKey               bool          `json:"hasDkimKey"`
	Aliases                  []*Alias      `json:"aliases"`
	Credentials              []*Credential `json:"credentials"`
	Records                  *RecordSet    `json:"records"`
}

// RecordSet is the DNS a domain needs, and what is published now.
type RecordSet struct {
	Domain    string    `json:"domain"`
	Records   []*Record `json:"records"`
	CheckedAt time.Time `json:"checkedAt"`
	Error     string    `json:"error"`
}

// Record is one DNS record a domain needs.
type Record struct {
	Type     string   `json:"type"`
	Name     string   `json:"name"`
	Expected string   `json:"expected"`
	Found    []string `json:"found"`
	Verified bool     `json:"verified"`
	Priority uint16   `json:"priority"`
	Optional bool     `json:"optional"`
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

// Published counts the records that are verified, and the ones that are
// required, so a list can say "3 of 5".
func (self *RecordSet) Published() (verified, required int) {
	if self == nil {
		return 0, 0
	}
	for _, record := range self.Records {
		if record.Optional {
			continue
		}
		required++
		if record.Verified {
			verified++
		}
	}
	return verified, required
}

func trimDot(name string) string {
	return strings.TrimSuffix(strings.TrimSpace(name), ".")
}

// domainFields is the selection every domain query asks for, so that the
// Domain struct above and the queries cannot drift apart.
const domainFields = `{
	id
	domain
	subdomain
	comment
	spamFilterScoreThreshold
	mailServers
	mailHosts
	linkHost
	linkHostname
	dkimSelector
	hasDkimKey
	aliases ` + aliasFields + `
	credentials ` + credentialFields + `
	records { domain checkedAt error records { type name expected found verified priority optional purpose } }
}`

// ListDomains returns the configured domains.
func ListDomains(ctx context.Context, connection *Client) ([]*Domain, error) {
	var result struct {
		ListDomains []*Domain `json:"ListDomains"`
	}
	if err := connection.Execute(ctx, `query { ListDomains `+domainFields+` }`, nil, &result); err != nil {
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

// DomainParameters are the settings of a domain an operator can change. A nil
// field is left alone, so an update carries only what it changes.
type DomainParameters struct {
	Domain                   *string   `json:"domain,omitempty"`
	Subdomain                *string   `json:"subdomain,omitempty"`
	Comment                  *string   `json:"comment,omitempty"`
	SpamFilterScoreThreshold *float64  `json:"spamFilterScoreThreshold,omitempty"`
	MailServers              *[]string `json:"mailServers,omitempty"`
	LinkHost                 *string   `json:"linkHost,omitempty"`
	DKIMSelector             *string   `json:"dkimSelector,omitempty"`
}

// CreateDomain adds a domain, which the server gives a signing key.
func CreateDomain(ctx context.Context, connection *Client, parameters *DomainParameters) (*Domain, error) {
	var result struct {
		CreateDomain *Domain `json:"CreateDomain"`
	}
	query := `mutation ($domainParameters: DomainParametersInput) {
		CreateDomain(domainParameters: $domainParameters) ` + domainFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"domainParameters": parameters}, &result); err != nil {
		return nil, err
	}
	return result.CreateDomain, nil
}

// UpdateDomain changes a domain.
func UpdateDomain(ctx context.Context, connection *Client, domainId string, parameters *DomainParameters) (*Domain, error) {
	var result struct {
		UpdateDomain *Domain `json:"UpdateDomain"`
	}
	query := `mutation ($domainId: String!, $domainParameters: DomainParametersInput) {
		UpdateDomain(domainId: $domainId, domainParameters: $domainParameters) ` + domainFields + `
	}`
	variables := map[string]any{"domainId": domainId, "domainParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateDomain, nil
}

// DeleteDomain removes a domain with its aliases and credentials. Mail
// already received for it is kept.
func DeleteDomain(ctx context.Context, connection *Client, domainId string) error {
	query := `mutation ($domainId: String!) { DeleteDomain(domainId: $domainId) }`
	return connection.Execute(ctx, query, map[string]any{"domainId": domainId}, nil)
}

// CheckDomain checks a domain's DNS records now.
func CheckDomain(ctx context.Context, connection *Client, domainId string) (*Domain, error) {
	var result struct {
		CheckDomain *Domain `json:"CheckDomain"`
	}
	query := `mutation ($domainId: String!) { CheckDomain(domainId: $domainId) ` + domainFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId}, &result); err != nil {
		return nil, err
	}
	return result.CheckDomain, nil
}

// RegenerateDomainKey replaces a domain's signing key.
func RegenerateDomainKey(ctx context.Context, connection *Client, domainId string) (*Domain, error) {
	var result struct {
		RegenerateDomainKey *Domain `json:"RegenerateDomainKey"`
	}
	query := `mutation ($domainId: String!) { RegenerateDomainKey(domainId: $domainId) ` + domainFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId}, &result); err != nil {
		return nil, err
	}
	return result.RegenerateDomainKey, nil
}

// ExternalAddresses is how the outside world reaches this server.
type ExternalAddresses struct {
	IPv4  string `json:"ipv4"`
	IPv6  string `json:"ipv6"`
	Error string `json:"error"`
}

// GetServerAddresses returns the addresses the DNS records have to point at.
func GetServerAddresses(ctx context.Context, connection *Client) (*ExternalAddresses, error) {
	var result struct {
		GetServerAddresses *ExternalAddresses `json:"GetServerAddresses"`
	}
	if err := connection.Execute(ctx, `query { GetServerAddresses { ipv4 ipv6 error } }`, nil, &result); err != nil {
		return nil, err
	}
	return result.GetServerAddresses, nil
}

// OutgoingIdentity is how outgoing mail identifies itself, and whether the
// receivers that check will believe it.
type OutgoingIdentity struct {
	Address          string   `json:"address"`
	Via              string   `json:"via"`
	ReverseName      string   `json:"reverseName"`
	ForwardAddresses []string `json:"forwardAddresses"`
	Confirmed        bool     `json:"confirmed"`
	HelloName        string   `json:"helloName"`
	HelloAddresses   []string `json:"helloAddresses"`
	HelloMatches     bool     `json:"helloMatches"`
}

// GetOutgoingIdentity returns how this server's outgoing mail identifies
// itself.
func GetOutgoingIdentity(ctx context.Context, connection *Client) (*OutgoingIdentity, error) {
	var result struct {
		GetOutgoingIdentity *OutgoingIdentity `json:"GetOutgoingIdentity"`
	}
	query := `query { GetOutgoingIdentity { address via reverseName forwardAddresses confirmed helloName helloAddresses helloMatches } }`
	if err := connection.Execute(ctx, query, nil, &result); err != nil {
		return nil, err
	}
	return result.GetOutgoingIdentity, nil
}

// equalFold compares domain names, which are case insensitive.
func equalFold(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}
