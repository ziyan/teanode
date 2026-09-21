package client

import (
	"context"
	"encoding/json"
	"time"
)

// Report is a DMARC aggregate report somebody sent about mail claiming to be
// from one of this server's domains.
type Report struct {
	ID           string          `json:"id"`
	CreatedAt    time.Time       `json:"createdAt"`
	MailID       string          `json:"mailId"`
	DomainID     string          `json:"domainId"`
	BeginAt      time.Time       `json:"beginAt"`
	EndAt        time.Time       `json:"endAt"`
	Count        uint64          `json:"count"`
	IP           string          `json:"ip"`
	RDNS         string          `json:"rdns"`
	FromDomain   string          `json:"fromDomain"`
	SenderDomain string          `json:"senderDomain"`
	Disposition  string          `json:"disposition"`
	DKIMAligned  bool            `json:"dkimAligned"`
	SPFAligned   bool            `json:"spfAligned"`
	Feedback     json.RawMessage `json:"feedback"`
}

const reportFields = `{
	id createdAt mailId domainId beginAt endAt count ip rdns
	fromDomain senderDomain disposition dkimAligned spfAligned
}`

// ListReports returns the reports for one domain, or every domain when
// domainId is empty.
func ListReports(ctx context.Context, connection *Client, domainId string, first int) ([]*Report, error) {
	var result struct {
		ListReports []*Report `json:"ListReports"`
	}
	query := `query ($domainId: String, $pagination: PaginationInput) {
		ListReports(domainId: $domainId, pagination: $pagination) ` + reportFields + `
	}`
	variables := map[string]any{"pagination": pagination(first)}
	if domainId != "" {
		variables["domainId"] = domainId
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ListReports, nil
}

// feedbackFields is the parsed report itself, spelled out. It is a structure
// rather than a scalar, so a selection is not optional: asking for "feedback"
// on its own is not a query the server will run, which is what this asked for
// until now.
const feedbackFields = `{
	organizationName email extraContactInfo reportId begin end errors
	domain dkimAlignment spfAlignment policy subdomainPolicy percent failureOptions
	records {
		sourceIp count disposition dkim spf reasonType reasonComment
		headerFrom envelopeFrom envelopeTo
		dkims { domain selector result humanResult }
		spfs { domain scope result }
	}
}`

// GetReport returns one report with the original feedback it was parsed
// from, which is what "report get --json" prints in full.
func GetReport(ctx context.Context, connection *Client, reportId string) (*Report, error) {
	var result struct {
		GetReport *Report `json:"GetReport"`
	}
	query := `query ($reportId: String!) {
		GetReport(reportId: $reportId) {
			id createdAt mailId domainId beginAt endAt count ip rdns
			fromDomain senderDomain disposition dkimAligned spfAligned
			feedback ` + feedbackFields + `
		}
	}`
	if err := connection.Execute(ctx, query, map[string]any{"reportId": reportId}, &result); err != nil {
		return nil, err
	}
	return result.GetReport, nil
}
