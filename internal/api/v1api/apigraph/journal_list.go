package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

const (
	// journalDaysDefault and journalDaysMost are how many days a timeline
	// page holds when the caller does not say, and at most.
	journalDaysDefault = 14
	journalDaysMost    = 62

	// journalHitsDefault and journalHitsMost are the same for a search.
	journalHitsDefault = 20
	journalHitsMost    = 50

	// journalQueryCharactersMost is the longest search the journal answers.
	journalQueryCharactersMost = 200
)

// requireJournalPerson is the caller, if they may keep a journal. The console
// is not a person and has none.
func (self *graph) requireJournalPerson(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requirePermission(ctx, models.PermissionJournalUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// journalFailure is an error from the database as the API reports it: a
// refusal keeps the words that say what was wrong.
func journalFailure(err error) error {
	return locationFailure(err)
}

// journalPageSize is the size of a page the caller asked for: the default
// when they did not say, and no more than the most.
func journalPageSize(asked, defaultSize, mostSize int) int {
	switch {
	case asked <= 0:
		return defaultSize
	case asked > mostSize:
		return mostSize
	}
	return asked
}

// journalDayBound is a day a call bounds a list by, empty for none.
func journalDayBound(name, day string) error {
	if day == "" {
		return nil
	}
	if _, err := journal.ParseDay(day); err != nil {
		return fmt.Errorf("%w: %s: %s", api.ErrInvalidArguments, name, err)
	}
	return nil
}

func (self *graph) ListJournalDays(ctx context.Context, arguments ListJournalDaysArguments) (*JournalDayPageView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	fromDay, untilDay := strings.TrimSpace(arguments.FromDay), strings.TrimSpace(arguments.UntilDay)
	if err := journalDayBound("fromDay", fromDay); err != nil {
		return nil, err
	}
	if err := journalDayBound("untilDay", untilDay); err != nil {
		return nil, err
	}
	if arguments.Offset < 0 {
		return nil, fmt.Errorf("%w: the offset is not negative", api.ErrInvalidArguments)
	}
	// A day after today has nothing in it yet, whatever the caller bounds by.
	today := journal.Today(principal.User, time.Now())
	if untilDay == "" || untilDay > today {
		untilDay = today
	}
	tx := self.transaction(ctx)
	days, totalCount, err := tx.ListJournalDays(principal.User.ID, fromDay, untilDay, arguments.Offset, journalPageSize(arguments.First, journalDaysDefault, journalDaysMost))
	if err != nil {
		return nil, journalFailure(err)
	}
	firstDay, err := tx.FirstJournalDay(principal.User.ID)
	if err != nil {
		return nil, journalFailure(err)
	}
	options, err := self.journalReadOptions(ctx, tx, principal)
	if err != nil {
		return nil, err
	}
	view := &JournalDayPageView{Days: make([]*JournalDaySummaryView, 0, len(days)), TotalCount: totalCount, Today: today, FirstDay: firstDay}
	for _, day := range days {
		view.Days = append(view.Days, describeJournalDaySummary(journal.ShownDay(day, options), today))
	}
	return view, nil
}

func (self *graph) SearchJournal(ctx context.Context, arguments SearchJournalArguments) (*JournalSearchPageView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(arguments.Query)
	if query == "" || utf8.RuneCountInString(query) > journalQueryCharactersMost {
		return nil, fmt.Errorf("%w: a search is 1 to %d characters", api.ErrInvalidArguments, journalQueryCharactersMost)
	}
	if arguments.Offset < 0 {
		return nil, fmt.Errorf("%w: the offset is not negative", api.ErrInvalidArguments)
	}
	hits, totalCount, err := self.transaction(ctx).SearchJournal(principal.User.ID, query, arguments.Offset, journalPageSize(arguments.First, journalHitsDefault, journalHitsMost))
	if err != nil {
		return nil, journalFailure(err)
	}
	view := &JournalSearchPageView{Hits: make([]*JournalSearchHitView, 0, len(hits)), TotalCount: totalCount}
	for _, hit := range hits {
		hitView := &JournalSearchHitView{Day: hit.Target.Day, Headline: hit.Headline, Snippet: hit.Snippet}
		if hit.Target.IsPeriod() {
			// A period is named by its first day.
			hitView.Day, hitView.PeriodKind = hit.Target.PeriodStart, string(hit.Target.PeriodKind)
		}
		view.Hits = append(view.Hits, hitView)
	}
	return view, nil
}

// journalSettingsView is what the caller decided about their journal and
// what the server adds to it.
func (self *graph) journalSettingsView(ctx context.Context, user *models.User) (*JournalSettingsView, error) {
	tx := self.transaction(ctx)
	settings, err := tx.GetJournalSettings(user.ID)
	if err != nil {
		return nil, translateError(err)
	}
	weekStart, err := journalWeekStartOf(tx, user.ID)
	if err != nil {
		return nil, err
	}
	providerName, err := self.healthModelProviderName(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &JournalSettingsView{
		IsWeatherOn: settings.IsWeatherOn, IsEveningQuestionOn: settings.IsEveningQuestionOn, EveningQuestionTime: settings.EveningQuestionTime,
		WeekStartsOn: weekStart, NarrativeDepthDays: models.JournalNarrativeDepthDays, ModelProviderName: providerName,
	}, nil
}

func (self *graph) ReadJournalSettings(ctx context.Context) (*JournalSettingsView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.journalSettingsView(ctx, principal.User)
}
