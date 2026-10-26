package apigraph

import (
	"context"
)

// The journal's reading while it is being built. Replaced by the
// implementation.

var _ context.Context

func (self *graph) ListJournalDays(ctx context.Context, arguments ListJournalDaysArguments) (*JournalDayPageView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) SearchJournal(ctx context.Context, arguments SearchJournalArguments) (*JournalSearchPageView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) ReadJournalSettings(ctx context.Context) (*JournalSettingsView, error) {
	return nil, errJournalUnbuilt
}
