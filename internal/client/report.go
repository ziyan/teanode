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

// GetReport returns one report with the original feedback it was parsed
// from, which is what "report get --json" prints in full.
func GetReport(ctx context.Context, connection *Client, reportId string) (*Report, error) {
	var result struct {
		GetReport *Report `json:"GetReport"`
	}
	query := `query ($reportId: String!) {
		GetReport(reportId: $reportId) {
			id createdAt mailId domainId beginAt endAt count ip rdns
			fromDomain senderDomain disposition dkimAligned spfAligned feedback
		}
	}`
	if err := connection.Execute(ctx, query, map[string]any{"reportId": reportId}, &result); err != nil {
		return nil, err
	}
	return result.GetReport, nil
}
