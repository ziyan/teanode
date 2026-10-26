package apigraph

import (
	"context"
)

// The journal's writing while it is being built. Replaced by the
// implementation.

var _ context.Context

func (self *graph) SetJournalWords(ctx context.Context, arguments SetJournalWordsArguments) (*JournalDayView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) SetJournalMood(ctx context.Context, arguments SetJournalMoodArguments) (*JournalDayView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) SetJournalLookingBack(ctx context.Context, arguments SetJournalLookingBackArguments) (*JournalPeriodView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) TouchUpJournalWords(ctx context.Context, arguments TouchUpJournalWordsArguments) (*JournalTouchUpView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) StopJournalTouchUp(ctx context.Context, arguments JournalTargetArguments) (*JournalTouchUpView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) CorrectJournalNarrative(ctx context.Context, arguments CorrectJournalNarrativeArguments) (*JournalNarrativeView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) RemoveJournalCorrection(ctx context.Context, arguments RemoveJournalCorrectionArguments) (*JournalNarrativeView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) WriteJournalNarrative(ctx context.Context, arguments JournalTargetArguments) (*JournalNarrativeView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) DeleteJournalNarrative(ctx context.Context, arguments JournalTargetArguments) (*JournalNarrativeView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) PinJournalRecord(ctx context.Context, arguments PinJournalRecordArguments) (*JournalLinkView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) RemoveJournalLink(ctx context.Context, arguments RemoveJournalLinkArguments) (bool, error) {
	return false, errJournalUnbuilt
}

func (self *graph) UpdateJournalSettings(ctx context.Context, arguments UpdateJournalSettingsArguments) (*JournalSettingsView, error) {
	return nil, errJournalUnbuilt
}

func (self *graph) EraseJournal(ctx context.Context) (*EraseJournalView, error) {
	return nil, errJournalUnbuilt
}
