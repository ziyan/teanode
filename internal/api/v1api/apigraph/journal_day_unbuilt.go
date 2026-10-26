package apigraph

import (
	"context"
	"errors"
)

// Reading a whole day or period while it is being built. Replaced by the
// implementation; errJournalUnbuilt goes with the last placeholder.

var errJournalUnbuilt = errors.New("the journal is not built yet")

var _ context.Context

func (self *graph) ReadJournalDay(ctx context.Context, arguments ReadJournalDayArguments) (*JournalDayView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) ReadJournalPeriod(ctx context.Context, arguments ReadJournalPeriodArguments) (*JournalPeriodView, error) {
	return nil, errJournalUnbuilt
}
