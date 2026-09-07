package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

type ReportQuery interface {
	// List DMARC Report belonging to a Domain
	ListReports(ctx context.Context, arguments ListReportsArguments) ([]*models.Report, error)

	// Get a particular Report belonging to a Domain
	GetReport(ctx context.Context, arguments GetReportArguments) (*models.Report, error)
}

type ReportMutation interface {
}

type ListReportsArguments struct {
	// ID of the Domain, or empty for every configured domain
	DomainID string `json:"domainId" graphapi:"nullable"`

	*api.Pagination `json:"pagination"`
}

// ListReports returns the DMARC aggregate reports other people have sent about
// mail claiming to be from these domains.
//
// An empty domain means every configured one, the same as the mail list. A
// report is about somebody forging a domain, and the question an operator asks
// is "is anyone forging me", not "is anyone forging this one particular name" —
// so making them pick a domain first hides the answer behind a choice they
// cannot make usefully.
func (self *graph) ListReports(ctx context.Context, arguments ListReportsArguments) ([]*models.Report, error) {
	domainIds, err := self.domainsToList(ctx, models.PermissionMailAudit, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	var reports []*models.Report
	for _, domainId := range domainIds {
		listed, err := api.ContextTransaction(ctx).ListReports(domainId, arguments.Options())
		if err != nil {
			return nil, err
		}
		reports = append(reports, listed...)
	}
	return reports, nil
}

type GetReportArguments struct {
	// ID of the Report to look up
	ReportID string `json:"reportId"`
}

func (self *graph) GetReport(ctx context.Context, arguments GetReportArguments) (*models.Report, error) {
	if _, err := self.requireAnyPermission(ctx, models.PermissionMailAudit); err != nil {
		return nil, err
	}

	report, err := api.ContextTransaction(ctx).GetReport(arguments.ReportID, nil)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, api.ErrNotFound
	}

	// the domain has to still be configured
	if !self.domainStillExists(ctx, report.DomainID) {
		return nil, api.ErrNotFound
	}

	return report, nil
}
