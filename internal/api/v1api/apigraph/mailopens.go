package apigraph

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// MailOpens is what is known about a sent message having been looked at.
//
// It is a floor with false positives in it, not a measurement, and every
// caller has to be told so. A fetch of one of the addresses put in the message
// is all this knows about. Apple's Mail Privacy Protection fetches every
// picture before the recipient has seen anything, so a message can be reported
// opened that nobody read. Most mail programs refuse to load pictures until
// the reader asks, so a message that was read can be reported unopened for
// ever. Gmail fetches through its own cache, so the address is Google's and
// the time may be delivery rather than reading.
//
// Trackable says whether the question can be asked at all: a message with no
// picture in it carries no address to fetch, and "not opened" for such a
// message means nothing.
type MailOpens struct {
	// The Mail this is about. Set when several are asked for at once, so the
	// caller can match an answer to the row it asked about.
	MailID string `json:"mailId,omitempty"`

	// Whether this message carries any address that could be fetched. False
	// for a message with no pictures, and for everything that arrived rather
	// than being sent.
	Trackable bool `json:"trackable"`

	// Whether one has been fetched.
	Opened bool `json:"opened"`

	// When the first fetch happened, and the most recent.
	OpenedAt     *time.Time `json:"openedAt,omitempty"`
	LastOpenedAt *time.Time `json:"lastOpenedAt,omitempty"`

	// How many fetches there have been across every address in the message.
	// Two pictures opened once is two.
	OpenCount int64 `json:"openCount,omitempty"`

	// Where the most recent fetch came from, and what it said it was. For
	// most mail this is a proxy rather than the reader.
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
}

type GetMailOpensArguments struct {
	// ID of the Mail to ask about
	MailID string `json:"mailId"`
}

// GetMailOpens reports whether a sent message has been looked at, as far as
// that can be known. Read the warning on MailOpens before showing the number
// to anybody.
func (self *graph) GetMailOpens(ctx context.Context, arguments GetMailOpensArguments) (*MailOpens, error) {
	if _, err := self.requireAnyPermission(ctx, models.PermissionMailAudit); err != nil {
		return nil, err
	}

	mail, err := api.ContextTransaction(ctx).GetMail(arguments.MailID, nil)
	if err != nil {
		return nil, err
	}
	if mail == nil {
		return nil, api.ErrNotFound
	}

	// The addresses were recorded against the envelope, which is the
	// identifier the message had while it was being composed.
	links, err := self.database.ListMediaLinksForEnvelope(mail.EnvelopeID)
	if err != nil {
		return nil, err
	}

	return summariseOpens(links), nil
}

// summariseOpens folds every address in one message into one answer.
//
// The first fetch of any of them is the message's first, the most recent of
// any of them its last, and the count is every fetch of every address added
// up — two pictures each fetched once is two. That is deliberately not a
// number of readings, and the page that shows it says so.
func summariseOpens(links []*models.MediaLink) *MailOpens {
	opens := &MailOpens{Trackable: len(links) > 0}
	for _, link := range links {
		opens.OpenCount += link.OpenCount
		if link.OpenedAt == nil {
			continue
		}
		opens.Opened = true
		if opens.OpenedAt == nil || link.OpenedAt.Before(*opens.OpenedAt) {
			opens.OpenedAt = link.OpenedAt
		}
		if link.LastOpenedAt != nil && (opens.LastOpenedAt == nil || link.LastOpenedAt.After(*opens.LastOpenedAt)) {
			opens.LastOpenedAt = link.LastOpenedAt
			opens.IP = link.IP
			opens.UserAgent = link.UserAgent
		}
	}
	return opens
}

type ListMailOpensArguments struct {
	// IDs of the Mail to ask about
	MailIDs []string `json:"mailIds"`
}

// ListMailOpens answers the same question as GetMailOpens for a page of
// messages, in two queries rather than two per message. The mail list asks it
// about every row it is showing.
//
// A message with nothing to fetch is still in the answer, with trackable
// false, because "no picture in it" and "not fetched" are different things and
// the list has to be able to tell them apart.
//
// A message the page asked about that no longer exists is left out of the
// answer entirely, which the dashboard reads as "nothing known".

// existingMails drops the ones that are not there any more.
//
// GetMails answers positionally: one entry per identifier asked for, nil in
// the place of any that names no mail. The identifiers come from a page
// somebody is looking at and retention deletes underneath it, so a nil here
// is the ordinary case rather than an impossible one.
//
// Filtered once, here, rather than checked at each use. Checking at each use
// is how the first repair of this missed the second loop and moved the panic
// four lines down instead of removing it: a message that no longer exists has
// no open count, so it should leave the answer at the top rather than be
// stepped around all the way through.
func existingMails(mails []*models.Mail) []*models.Mail {
	existing := make([]*models.Mail, 0, len(mails))
	for _, mail := range mails {
		if mail != nil {
			existing = append(existing, mail)
		}
	}
	return existing
}

func (self *graph) ListMailOpens(ctx context.Context, arguments ListMailOpensArguments) ([]*MailOpens, error) {
	if _, err := self.requireAnyPermission(ctx, models.PermissionMailAudit); err != nil {
		return nil, err
	}
	if len(arguments.MailIDs) == 0 {
		return nil, nil
	}

	mails, err := api.ContextTransaction(ctx).GetMails(arguments.MailIDs, nil)
	if err != nil {
		return nil, err
	}

	mails = existingMails(mails)

	envelopeIds := make([]string, 0, len(mails))
	for _, mail := range mails {
		if mail.EnvelopeID != "" {
			envelopeIds = append(envelopeIds, mail.EnvelopeID)
		}
	}
	links, err := self.database.ListMediaLinksForEnvelopes(envelopeIds)
	if err != nil {
		return nil, err
	}

	byEnvelope := make(map[string][]*models.MediaLink, len(envelopeIds))
	for _, link := range links {
		byEnvelope[link.EnvelopeID] = append(byEnvelope[link.EnvelopeID], link)
	}

	answers := make([]*MailOpens, 0, len(mails))
	for _, mail := range mails {
		opens := summariseOpens(byEnvelope[mail.EnvelopeID])
		opens.MailID = mail.ID
		answers = append(answers, opens)
	}
	return answers, nil
}
