package apigraph

import (
	"context"
	"errors"
)

// The journal's reading while it is being built. Replaced by the
// implementation.

var errJournalUnbuilt = errors.New("the journal is not built yet")

var _ context.Context

func (self *graph) ListJournalDays(ctx context.Context, arguments ListJournalDaysArguments) (*JournalDayPageView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) ReadJournalDay(ctx context.Context, arguments ReadJournalDayArguments) (*JournalDayView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) ReadJournalPeriod(ctx context.Context, arguments ReadJournalPeriodArguments) (*JournalPeriodView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) SearchJournal(ctx context.Context, arguments SearchJournalArguments) (*JournalSearchPageView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) ReadJournalSettings(ctx context.Context) (*JournalSettingsView, error) {
	return nil, errJournalUnbuilt
}
