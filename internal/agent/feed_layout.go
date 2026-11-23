package agent

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// What a card's layout other than text carries, checked as it is posted so
// that every app can draw it without guessing. The body stays plain Markdown
// whatever the layout, for the command line and for an app that does not know
// a layout yet.

const (
	feedListRowsMost       = 10
	feedHourlyForecastMost = 12
	feedAgendaEntriesMost  = 24
	feedLayoutTextMost     = 300
)

type feedListLayout struct {
	ListRows []feedListRow `json:"listRows"`
}

type feedListRow struct {
	RowTitle           string                 `json:"rowTitle"`
	RowDetail          string                 `json:"rowDetail,omitempty"`
	RowMeta            string                 `json:"rowMeta,omitempty"`
	RowSymbol          string                 `json:"rowSymbol,omitempty"`
	RowURL             string                 `json:"rowUrl,omitempty"`
	RowRecordReference *models.AgentReference `json:"rowRecordReference,omitempty"`
}

type feedWeatherLayout struct {
	PlaceName          string               `json:"placeName"`
	CurrentTemperature *float64             `json:"currentTemperature"`
	TemperatureUnit    string               `json:"temperatureUnit"`
	ConditionText      string               `json:"conditionText"`
	ConditionSymbol    string               `json:"conditionSymbol,omitempty"`
	HighTemperature    *float64             `json:"highTemperature,omitempty"`
	LowTemperature     *float64             `json:"lowTemperature,omitempty"`
	HourlyForecast     []feedHourlyForecast `json:"hourlyForecast,omitempty"`
}

type feedHourlyForecast struct {
	ForecastHour               string   `json:"forecastHour"`
	ForecastTemperature        *float64 `json:"forecastTemperature"`
	ForecastSymbol             string   `json:"forecastSymbol,omitempty"`
	PrecipitationChancePercent *float64 `json:"precipitationChancePercent,omitempty"`
}

type feedAgendaLayout struct {
	AgendaEntries []feedAgendaEntry `json:"agendaEntries"`
}

type feedAgendaEntry struct {
	StartsAt              string                 `json:"startsAt"`
	EndsAt                string                 `json:"endsAt,omitempty"`
	AgendaTitle           string                 `json:"agendaTitle"`
	AgendaEntryKind       string                 `json:"agendaEntryKind,omitempty"`
	AgendaDetail          string                 `json:"agendaDetail,omitempty"`
	AgendaRecordReference *models.AgentReference `json:"agendaRecordReference,omitempty"`
}

// feedAgendaEntryKinds are the kinds of entry of a day plan.
var feedAgendaEntryKinds = []string{"meeting", "focus", "task", "break"}

// decodeLayoutContent reads layout content strictly: a field the layout does
// not have is refused by name, as is a value of the wrong type.
func decodeLayoutContent(content string, target any) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return feedRefusal("layout_content is not what this layout takes: %s", err)
	}
	if decoder.More() {
		return feedRefusal("layout_content must be one JSON object")
	}
	return nil
}

func isFeedTime(text string) bool {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02T15:04:05"} {
		if _, err := time.Parse(layout, text); err == nil {
			return true
		}
	}
	return false
}

func isWebAddress(text string) bool {
	parsed, err := url.Parse(text)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// normalizeFeedLayout checks the layout and its content and writes the content
// back in its one canonical form. Text has none.
func normalizeFeedLayout(item *models.AgentFeedItem) error {
	if item.FeedCardLayout == "" {
		item.FeedCardLayout = models.FeedLayoutText
	}
	if !models.IsFeedCardLayout(item.FeedCardLayout) {
		return feedRefusal("%q is not a card_layout; use text, list, weather or agenda", item.FeedCardLayout)
	}
	content := strings.TrimSpace(item.LayoutContent)
	if item.FeedCardLayout == models.FeedLayoutText {
		if content != "" {
			return feedRefusal("layout_content is for the list, weather and agenda layouts; a text card has none")
		}
		item.LayoutContent = ""
		return nil
	}
	if content == "" {
		return feedRefusal("a %s card needs layout_content", item.FeedCardLayout)
	}
	var normalized any
	switch item.FeedCardLayout {
	case models.FeedLayoutList:
		var list feedListLayout
		if err := decodeLayoutContent(content, &list); err != nil {
			return err
		}
		if len(list.ListRows) == 0 || len(list.ListRows) > feedListRowsMost {
			return feedRefusal("layout_content.listRows has %d rows; a list has from 1 to %d", len(list.ListRows), feedListRowsMost)
		}
		for index := range list.ListRows {
			row := &list.ListRows[index]
			row.RowTitle = cutMarked(strings.TrimSpace(row.RowTitle), feedLayoutTextMost)
			row.RowDetail, row.RowMeta = cutMarked(strings.TrimSpace(row.RowDetail), feedLayoutTextMost*2), cutMarked(strings.TrimSpace(row.RowMeta), feedLayoutTextMost)
			if row.RowTitle == "" {
				return feedRefusal("layout_content.listRows[%d].rowTitle is empty", index)
			}
			if row.RowURL != "" && !isWebAddress(strings.TrimSpace(row.RowURL)) {
				return feedRefusal("layout_content.listRows[%d].rowUrl must be an http or https address", index)
			}
			row.RowURL = strings.TrimSpace(row.RowURL)
		}
		normalized = list
	case models.FeedLayoutWeather:
		var weather feedWeatherLayout
		if err := decodeLayoutContent(content, &weather); err != nil {
			return err
		}
		weather.PlaceName, weather.ConditionText = cutMarked(strings.TrimSpace(weather.PlaceName), feedLayoutTextMost), cutMarked(strings.TrimSpace(weather.ConditionText), feedLayoutTextMost)
		switch {
		case weather.PlaceName == "":
			return feedRefusal("layout_content.placeName is empty")
		case weather.CurrentTemperature == nil:
			return feedRefusal("layout_content.currentTemperature is missing; give a number")
		case strings.TrimSpace(weather.TemperatureUnit) == "":
			return feedRefusal("layout_content.temperatureUnit is missing; give C or F")
		case weather.ConditionText == "":
			return feedRefusal("layout_content.conditionText is empty")
		case len(weather.HourlyForecast) > feedHourlyForecastMost:
			return feedRefusal("layout_content.hourlyForecast has %d hours; at most %d", len(weather.HourlyForecast), feedHourlyForecastMost)
		}
		for index, hour := range weather.HourlyForecast {
			if !isFeedTime(hour.ForecastHour) {
				return feedRefusal("layout_content.hourlyForecast[%d].forecastHour must be an ISO time such as 2026-11-19T09:00", index)
			}
			if hour.ForecastTemperature == nil {
				return feedRefusal("layout_content.hourlyForecast[%d].forecastTemperature is missing; give a number", index)
			}
			if hour.PrecipitationChancePercent != nil && (*hour.PrecipitationChancePercent < 0 || *hour.PrecipitationChancePercent > 100) {
				return feedRefusal("layout_content.hourlyForecast[%d].precipitationChancePercent must be from 0 to 100", index)
			}
		}
		normalized = weather
	case models.FeedLayoutAgenda:
		var agenda feedAgendaLayout
		if err := decodeLayoutContent(content, &agenda); err != nil {
			return err
		}
		if len(agenda.AgendaEntries) == 0 || len(agenda.AgendaEntries) > feedAgendaEntriesMost {
			return feedRefusal("layout_content.agendaEntries has %d entries; a day plan has from 1 to %d", len(agenda.AgendaEntries), feedAgendaEntriesMost)
		}
		for index := range agenda.AgendaEntries {
			entry := &agenda.AgendaEntries[index]
			entry.AgendaTitle, entry.AgendaDetail = cutMarked(strings.TrimSpace(entry.AgendaTitle), feedLayoutTextMost), cutMarked(strings.TrimSpace(entry.AgendaDetail), feedLayoutTextMost*2)
			switch {
			case entry.AgendaTitle == "":
				return feedRefusal("layout_content.agendaEntries[%d].agendaTitle is empty", index)
			case !isFeedTime(entry.StartsAt):
				return feedRefusal("layout_content.agendaEntries[%d].startsAt must be an ISO time such as 2026-11-19T09:00", index)
			case entry.EndsAt != "" && !isFeedTime(entry.EndsAt):
				return feedRefusal("layout_content.agendaEntries[%d].endsAt must be an ISO time such as 2026-11-19T09:30", index)
			}
			if entry.AgendaEntryKind != "" && !containsString(feedAgendaEntryKinds, entry.AgendaEntryKind) {
				return feedRefusal("layout_content.agendaEntries[%d].agendaEntryKind is %q; use meeting, focus, task or break", index, entry.AgendaEntryKind)
			}
		}
		normalized = agenda
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	item.LayoutContent = string(encoded)
	return nil
}

func containsString(list []string, wanted string) bool {
	for _, each := range list {
		if each == wanted {
			return true
		}
	}
	return false
}
