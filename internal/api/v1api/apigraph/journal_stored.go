package apigraph

import (
	"errors"
	"regexp"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// The journal's views built from the stored rows alone. A call that changes
// a day or a period answers with the day or the period as the journal keeps
// it (words, mood, weather, numbers, narrative, the records chosen and
// pinned); the sections of records from every source, and the moments, are
// ReadJournalDay's and are left as empty lists.

// journalReferencesMost is how many different records of a narrative are
// found again for one answer.
const journalReferencesMost = 60

// journalReferencePattern is a record in a narrative: [[kind:id]].
var journalReferencePattern = regexp.MustCompile(`\[\[([a-z_]+):([^\[\]]+)\]\]`)

// journalRecordResolver finds the records a journal day or period links to
// and mentions, for the person they belong to. A person without an agent
// reads none: the resolver needs the agent whose conversations and finance
// sources the records are.
type journalRecordResolver struct {
	transaction db.Transaction
	reader      recordlink.Reader
	hasAgent    bool

	// permissions, when set, are the caller's: a record of a source the caller
	// may not read anywhere else is unavailable here too, so that a mention in
	// a narrative cannot show what the caller's role does not reach. Reading a
	// whole day sets it.
	permissions *models.EffectivePermissions
}

func newJournalRecordResolver(tx db.Transaction, userId string) (*journalRecordResolver, error) {
	found, err := tx.GetAgentByUser(userId)
	if err != nil {
		return nil, err
	}
	resolver := &journalRecordResolver{transaction: tx}
	if found != nil {
		resolver.reader, resolver.hasAgent = recordlink.Reader{UserID: userId, AgentID: found.ID}, true
	}
	return resolver, nil
}

// resolve is the record found again. One that cannot be, because it is gone,
// is not the person's, or is of a kind this server does not read yet, reads
// as unavailable: a link or a mention is shown without its title rather than
// failing the whole page.
func (self *journalRecordResolver) resolve(recordKind models.AgentRecordKind, recordId string) *recordlink.Resolved {
	unavailable := &recordlink.Resolved{RecordKind: recordKind, RecordID: recordId, IsUnavailable: true}
	if !self.hasAgent || (self.permissions != nil && !isJournalKindReadable(self.permissions, recordKind)) {
		return unavailable
	}
	resolved, err := recordlink.ResolveRecord(self.transaction, self.reader, recordKind, recordId, models.AgentRecordDetail{}, 0, 0)
	if err != nil {
		log.Warningf("cannot find the %s %q of a journal page again: %s", recordKind, recordId, err)
		return unavailable
	}
	return resolved
}

// references are the records a narrative mentions, each once, in the order
// they first appear.
func (self *journalRecordResolver) references(narrative string) []*JournalRecordView {
	views := []*JournalRecordView{}
	seen := map[string]bool{}
	for _, match := range journalReferencePattern.FindAllStringSubmatch(narrative, -1) {
		reference := match[1] + ":" + match[2]
		if seen[reference] {
			continue
		}
		if len(views) == journalReferencesMost {
			break
		}
		seen[reference] = true
		views = append(views, describeJournalRecord(self.resolve(models.AgentRecordKind(match[1]), match[2])))
	}
	return views
}

// describeLinks splits a day's links into the agent's choices (the hidden ones
// too, marked) and the person's pins, each in the order the page shows them.
func (self *journalRecordResolver) describeLinks(links []*models.JournalLink) (choices, pins []*JournalLinkView) {
	choices, pins = []*JournalLinkView{}, []*JournalLinkView{}
	for _, link := range links {
		view := describeJournalLink(link, self.resolve(link.RecordKind, link.RecordID))
		if link.LinkSource == models.JournalLinkFromPerson {
			pins = append(pins, view)
		} else {
			choices = append(choices, view)
		}
	}
	return choices, pins
}

// journalWeekStartOf is the day the person's weeks start on: what their first
// calendar says, and Monday when it says nothing.
func journalWeekStartOf(tx db.Transaction, userId string) (string, error) {
	return journal.WeekStartOf(tx, userId)
}

// storedJournalDayView is a day as the journal keeps it.
func storedJournalDayView(tx db.Transaction, user *models.User, row *models.JournalDay, now time.Time) (*JournalDayView, error) {
	today := journal.Today(user, now)
	weekStart, err := journalWeekStartOf(tx, user.ID)
	if err != nil {
		return nil, err
	}
	firstOfWeek, _, err := journal.PeriodContaining(models.JournalPeriodWeek, row.Day, weekStart)
	if err != nil {
		return nil, err
	}
	weekday, err := journal.Weekday(row.Day)
	if err != nil {
		return nil, err
	}
	resolver, err := newJournalRecordResolver(tx, user.ID)
	if err != nil {
		return nil, err
	}
	links, err := tx.ListJournalLinks(user.ID, row.ID, "")
	if err != nil {
		return nil, err
	}
	choices, pins := resolver.describeLinks(links)
	narrative := describeJournalDayNarrative(row, today)
	narrative.References = resolver.references(row.Narrative)
	return &JournalDayView{
		Day: row.Day, TimeZone: row.TimeZone, IsToday: row.Day == today, Weekday: weekday, WeekStart: firstOfWeek,
		Words: describeJournalDayWords(row), Mood: describeJournalMood(row), Weather: describeJournalWeather(row.Weather),
		Facts: describeJournalFacts(row.Facts), Narrative: narrative, Choices: choices, Pins: pins, CoverPhotoID: row.CoverPhotoID,
		WorkProjects: []*JournalWorkProjectView{}, CodingSessions: []*JournalCodingSessionView{}, Documents: []*JournalDocumentView{}, Conversations: []*JournalConversationView{},
		Chats: []*JournalChatView{}, Mail: []*JournalMailView{}, Recordings: []*JournalRecordingView{}, Photos: []*JournalPhotoView{},
		Visits: []*JournalVisitView{}, Events: []*JournalEventView{}, Reminders: []*JournalReminderView{},
		Transactions: []*JournalTransactionView{}, ShoppingTrips: []*JournalShoppingTripView{}, Notes: []*JournalRecordView{},
		MemoryPages: []*JournalMemoryPageView{}, UnavailableSections: []string{}, Moments: []*JournalMomentView{},
	}, nil
}

// storedJournalPeriodView is a period as the journal keeps it.
func storedJournalPeriodView(tx db.Transaction, user *models.User, row *models.JournalPeriod, now time.Time) (*JournalPeriodView, error) {
	today := journal.Today(user, now)
	weekStart, err := journalWeekStartOf(tx, user.ID)
	if err != nil {
		return nil, err
	}
	first, last, err := journal.PeriodContaining(row.PeriodKind, row.PeriodStart, weekStart)
	if err != nil {
		return nil, err
	}
	resolver, err := newJournalRecordResolver(tx, user.ID)
	if err != nil {
		return nil, err
	}
	links, err := tx.ListJournalLinks(user.ID, "", row.ID)
	if err != nil {
		return nil, err
	}
	choices, pins := resolver.describeLinks(links)
	narrative := describeJournalPeriodNarrative(row)
	narrative.References = resolver.references(row.Narrative)
	chapters := make([]*JournalChapterView, 0, len(row.Chapters))
	for _, chapter := range row.Chapters {
		chapters = append(chapters, &JournalChapterView{FromDay: chapter.FromDay, UntilDay: chapter.UntilDay, Title: chapter.Title, Detail: chapter.Detail})
	}
	return &JournalPeriodView{
		PeriodKind: string(row.PeriodKind), PeriodStart: row.PeriodStart, PeriodEnd: last, IsCurrent: first <= today && today <= last,
		LookingBack: describeJournalPeriodWords(row), Narrative: narrative,
		Choices: append(choices, pins...), Days: []*JournalDaySummaryView{}, Months: []*JournalMonthSummaryView{},
		Totals: &JournalFactsView{}, MoodCounts: []int{0, 0, 0, 0, 0}, Chapters: chapters,
	}, nil
}

// storedJournalNarrativeView is the narrative of a day or a period, or an
// empty one when nothing was ever kept for it.
func storedJournalNarrativeView(tx db.Transaction, user *models.User, target models.JournalTarget, now time.Time) (*JournalNarrativeView, error) {
	resolver, err := newJournalRecordResolver(tx, user.ID)
	if err != nil {
		return nil, err
	}
	var view *JournalNarrativeView
	var narrative string
	if target.IsPeriod() {
		row, err := tx.GetJournalPeriod(user.ID, target.PeriodKind, target.PeriodStart)
		if err != nil && !isJournalNotFound(err) {
			return nil, err
		}
		view = describeJournalPeriodNarrative(row)
		if row != nil {
			narrative = row.Narrative
		}
	} else {
		row, err := tx.GetJournalDay(user.ID, target.Day)
		if err != nil && !isJournalNotFound(err) {
			return nil, err
		}
		view = describeJournalDayNarrative(row, journal.Today(user, now))
		if row != nil {
			narrative = row.Narrative
		}
	}
	view.References = resolver.references(narrative)
	return view, nil
}

// storedJournalWordsView is the words of a day, or the looking back of a
// period, or an empty one when nothing was ever kept for it.
func storedJournalWordsView(tx db.Transaction, userId string, target models.JournalTarget) (*JournalWordsView, error) {
	if target.IsPeriod() {
		row, err := tx.GetJournalPeriod(userId, target.PeriodKind, target.PeriodStart)
		if err != nil && !isJournalNotFound(err) {
			return nil, err
		}
		return describeJournalPeriodWords(row), nil
	}
	row, err := tx.GetJournalDay(userId, target.Day)
	if err != nil && !isJournalNotFound(err) {
		return nil, err
	}
	return describeJournalDayWords(row), nil
}

// isJournalNotFound is a row the journal never kept; the call that asked
// answers an empty one.
func isJournalNotFound(err error) bool {
	return errors.Is(err, db.ErrNotFound)
}
