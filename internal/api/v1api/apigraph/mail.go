package apigraph

import (
	"context"
	"sort"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/aggregate"
)

type MailQuery interface {
	// Count Mail by the values of one field, for a filter menu
	CountMailsBy(ctx context.Context, arguments CountMailsByArguments) ([]*Facet, error)

	// List Mail belonging to a Domain
	ListMails(ctx context.Context, arguments ListMailsArguments) ([]*models.Mail, error)

	// Get a particular Mail belonging to a Domain
	GetMail(ctx context.Context, arguments GetMailArguments) (*models.Mail, error)

	// Ask whether a sent Mail has been looked at, as far as a fetched picture
	// can say. Read what MailOpens means before showing it to anybody.
	GetMailOpens(ctx context.Context, arguments GetMailOpensArguments) (*MailOpens, error)

	// Ask the same about a page of Mail at once, for a list
	ListMailOpens(ctx context.Context, arguments ListMailOpensArguments) ([]*MailOpens, error)
}

type MailMutation interface {
}

// mailColumns are the fields a caller may filter, sort or group a mail list
// by, and the columns they mean.
//
// An allow list because a field name reaches SQL as an identifier and cannot
// be parameterised. It is also the API's promise: these are the columns that
// keep working, rather than whatever the table happens to be called today.
var mailColumns = aggregate.Columns{
	"domainId":   `"domain_id"`,
	"sender":     `"sender"`,
	"from":       `"from"`,
	"subject":    `"subject"`,
	"status":     `"status"`,
	"kind":       `"kind"`,
	"size":       `"size"`,
	"receivedAt": `"received_at"`,
	"ip":         `"ip"`,
	"rdns":       `"rdns"`,
	// The two identifiers a message carries: its own, and the envelope it
	// arrived in. Sending from the dashboard finds what it sent by the
	// second, which is the one both sides hold.
	"messageId":  `"message_id"`,
	"envelopeId": `"envelope_id"`,
}

type CountMailsByArguments struct {
	// ID of the Domain, or empty for every configured Domain
	DomainID string `json:"domainId"`

	// Field to group by, for example "status" or "kind"
	Field string `json:"field"`

	// Narrowing applied before counting, so the numbers describe what the
	// other filters have already left
	Aggregations api.Aggregations `json:"aggregations" graphapi:"nullable"`
}

// Facet is one value a column takes, and how many rows carry it.
type Facet struct {
	// The value itself
	Value string `json:"value"`

	// How many rows would remain if this were the only thing selected
	Count int `json:"count"`
}

// CountMailsBy fills a filter menu: the values a column actually takes, with
// a number beside each.
//
// Counted in the database over everything, not in the browser over the page
// it fetched — "which domains have mail" is a question about all of it, and
// an answer computed from the most recent five hundred is a different
// question wearing the same words.
func (self *graph) CountMailsBy(ctx context.Context, arguments CountMailsByArguments) ([]*Facet, error) {
	domainIds, err := self.domainsToList(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	if len(domainIds) == 0 {
		return []*Facet{}, nil
	}

	counted, err := api.ContextTx(ctx).CountMailsBy(domainIds, arguments.Field,
		&db.Options{Aggregations: arguments.Aggregations, Columns: mailColumns})
	if err != nil {
		return nil, err
	}

	facets := make([]*Facet, 0, len(counted))
	for _, facet := range counted {
		facets = append(facets, &Facet{Value: facet.Value, Count: facet.Count})
	}
	return facets, nil
}

type ListMailsArguments struct {
	// ID of the Domain, or empty for every configured Domain
	DomainID string `json:"domainId"`

	// Filtering, sorting and grouping, applied in the database
	Aggregations api.Aggregations `json:"aggregations" graphapi:"nullable"`

	*api.Pagination `json:"pagination"`
}

// ListMails returns received mail, newest first. An empty domainId means every
// configured domain, which is what somebody opening the dashboard to see "did
// my mail arrive" actually wants.
func (self *graph) ListMails(ctx context.Context, arguments ListMailsArguments) ([]*models.Mail, error) {
	domainIds, err := self.domainsToList(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	var mails []*models.Mail
	for _, domainId := range domainIds {
		listed, err := api.ContextTx(ctx).ListMails(domainId, arguments.OptionsWith(arguments.Aggregations, mailColumns))
		if err != nil {
			return nil, err
		}
		mails = append(mails, listed...)
	}

	// The rows come back one domain at a time and have to be put back in
	// order across them. Identifiers sort by time, so ordering by them puts
	// the newest first without another query — but only when the caller did
	// not ask for an order of their own, which the database has already
	// applied and this would otherwise undo.
	if !sortsExplicitly(arguments.Aggregations) {
		sort.Slice(mails, func(first, second int) bool {
			return mails[first].ID > mails[second].ID
		})
	}
	if limit := arguments.Limit(); limit > 0 && len(mails) > limit {
		mails = mails[:limit]
	}

	// list associated deliveries
	mailIds := make([]string, 0, len(mails))
	for _, mail := range mails {
		mailIds = append(mailIds, mail.ID)
	}
	deliveries, err := api.ContextTx(ctx).ListDeliveries(mailIds, nil)
	if err != nil {
		return nil, err
	}

	// add deliveries into each mail
	mailsMap := make(map[string]*models.Mail)
	for _, mail := range mails {
		mailsMap[mail.ID] = mail
	}
	for _, delivery := range deliveries {
		mail, ok := mailsMap[delivery.MailID]
		if ok {
			mail.Deliveries = append(mail.Deliveries, delivery)
		}
	}
	return mails, nil
}

type GetMailArguments struct {
	// ID of the Mail to look up
	MailID string `json:"mailId"`
}

func (self *graph) GetMail(ctx context.Context, arguments GetMailArguments) (*models.Mail, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	mail, err := api.ContextTx(ctx).GetMail(arguments.MailID, nil)
	if err != nil {
		return nil, err
	}
	if mail == nil {
		return nil, api.ErrNotFound
	}

	// the domain has to still be configured
	if self.config.Current().FindDomainByID(mail.DomainID) == nil {
		return nil, api.ErrNotFound
	}

	return mail, nil
}

// sortsExplicitly reports whether a pipeline orders the rows itself.
//
// A merge across domains has to re-order what it merged, and doing that on
// top of a requested sort silently replaces it — the query is right, the
// database is right, and the answer comes back in the wrong order.
func sortsExplicitly(aggregations api.Aggregations) bool {
	for _, stage := range aggregations {
		if stage != nil && len(stage.Sort) > 0 {
			return true
		}
	}
	return false
}
