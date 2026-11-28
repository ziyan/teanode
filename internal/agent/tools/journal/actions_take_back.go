package journal

import (
	"context"
	"errors"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
)

// The actions that take something out of the journal or change how it is
// kept. The agent has them because the person has them. The two that cannot
// be undone, deleting a narrative and erasing the journal, are marked
// destructive on their tools, so the person's confirmation card is what
// stands between a request and the deletion.

type targetArguments struct {
	Day        string `json:"day"`
	PeriodKind string `json:"period_kind"`
}

// targetVariables is the day, or the period that contains it, an operation
// is about.
func (self *scope) targetVariables(arguments targetArguments) (string, map[string]any, error) {
	day, err := self.day("day", arguments.Day, 1)
	if err != nil {
		return "", nil, err
	}
	periodKind, err := periodKindOf(arguments.PeriodKind)
	if err != nil {
		return "", nil, err
	}
	variables := map[string]any{"day": day}
	if periodKind != "" {
		variables["periodKind"] = periodKind
	}
	return day, variables, nil
}

func runStopTouchUp(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[targetArguments](call)
	if err != nil {
		return nil, err
	}
	day, variables, err := here.targetVariables(arguments)
	if err != nil {
		return nil, err
	}
	var result struct {
		StopJournalTouchUp *client.JournalTouchUpView `json:"StopJournalTouchUp"`
	}
	if err := here.operations.Execute(ctx, client.DocumentStopJournalTouchUp, variables, &result); err != nil {
		return nil, err
	}
	answer := map[string]any{"day": day, "is_touch_up_on": false, "note": "touching up is off and the touched-up text is deleted; the original words are unchanged"}
	if arguments.PeriodKind != "" {
		answer["period_kind"] = strings.ToLower(strings.TrimSpace(arguments.PeriodKind))
	}
	return tools.JSONResult(answer)
}

type removeCorrectionArguments struct {
	targetArguments
	CorrectionIndex *int `json:"correction_index"`
}

func runRemoveCorrection(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[removeCorrectionArguments](call)
	if err != nil {
		return nil, err
	}
	if arguments.CorrectionIndex == nil || *arguments.CorrectionIndex < 0 {
		return nil, errors.New("correction_index is required: the place of the correction in the narrative's corrections list, counting from 0")
	}
	day, variables, err := here.targetVariables(arguments.targetArguments)
	if err != nil {
		return nil, err
	}
	variables["correctionIndex"] = *arguments.CorrectionIndex
	var result struct {
		RemoveJournalCorrection *client.JournalNarrativeView `json:"RemoveJournalCorrection"`
	}
	if err := here.operations.Execute(ctx, client.DocumentRemoveJournalCorrection, variables, &result); err != nil {
		return nil, err
	}
	corrections := []string{}
	if narrative := result.RemoveJournalCorrection; narrative != nil {
		for _, correction := range narrative.Corrections {
			corrections = append(corrections, correction.CorrectionText)
		}
	}
	return tools.JSONResult(map[string]any{"day": day, "corrections": corrections, "note": "removed; the narrative is written again without it"})
}

func runDeleteNarrative(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[targetArguments](call)
	if err != nil {
		return nil, err
	}
	day, variables, err := here.targetVariables(arguments)
	if err != nil {
		return nil, err
	}
	var result struct {
		DeleteJournalNarrative *client.JournalNarrativeView `json:"DeleteJournalNarrative"`
	}
	if err := here.operations.Execute(ctx, client.DocumentDeleteJournalNarrative, variables, &result); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"day": day, "is_written": false, "note": "the narrative and its headline are deleted"})
}

type removePinArguments struct {
	LinkID string `json:"link_id"`
}

func runRemovePin(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[removePinArguments](call)
	if err != nil {
		return nil, err
	}
	linkId := strings.TrimSpace(arguments.LinkID)
	if linkId == "" {
		return nil, errors.New("link_id is required: the link_id a day's pinned_records or chosen_records gives")
	}
	var result struct {
		RemoveJournalLink bool `json:"RemoveJournalLink"`
	}
	if err := here.operations.Execute(ctx, client.DocumentRemoveJournalLink, map[string]any{"linkId": linkId}, &result); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"link_id": linkId, "is_removed": true})
}

type updateSettingsArguments struct {
	IsWeatherOn         *bool   `json:"is_weather_on"`
	IsEveningQuestionOn *bool   `json:"is_evening_question_on"`
	EveningQuestionTime *string `json:"evening_question_time"`
}

func runUpdateSettings(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[updateSettingsArguments](call)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	if arguments.IsWeatherOn != nil {
		variables["isWeatherOn"] = *arguments.IsWeatherOn
	}
	if arguments.IsEveningQuestionOn != nil {
		variables["isEveningQuestionOn"] = *arguments.IsEveningQuestionOn
	}
	if arguments.EveningQuestionTime != nil {
		variables["eveningQuestionTime"] = *arguments.EveningQuestionTime
	}
	if len(variables) == 0 {
		return nil, errors.New("give at least one of is_weather_on, is_evening_question_on and evening_question_time")
	}
	var result struct {
		UpdateJournalSettings *client.JournalSettingsView `json:"UpdateJournalSettings"`
	}
	if err := here.operations.Execute(ctx, client.DocumentUpdateJournalSettings, variables, &result); err != nil {
		return nil, err
	}
	answer := map[string]any{}
	if settings := result.UpdateJournalSettings; settings != nil {
		answer["is_weather_on"] = settings.IsWeatherOn
		answer["is_evening_question_on"] = settings.IsEveningQuestionOn
		answer["evening_question_time"] = settings.EveningQuestionTime
	}
	return tools.JSONResult(answer)
}

func runErase(ctx context.Context, _ *tools.Call, here *scope) (*tools.Result, error) {
	var result struct {
		EraseJournal *client.EraseJournalView `json:"EraseJournal"`
	}
	if err := here.operations.Execute(ctx, client.DocumentEraseJournal, nil, &result); err != nil {
		return nil, err
	}
	answer := map[string]any{"is_erased": true}
	if erased := result.EraseJournal; erased != nil {
		answer["day_count"], answer["period_count"], answer["link_count"] = erased.DayCount, erased.PeriodCount, erased.LinkCount
	}
	return tools.JSONResult(answer)
}
