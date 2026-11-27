package agent

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/linkpreview"
	"github.com/ziyan/teanode/internal/models"
)

// What a card is held to beyond its fields: plain punctuation, text cut to
// length with a note instead of a refusal, charts that say where their numbers
// came from, and camera stills. See docs/subsystems/feed.md.

const (
	feedChartTitleCharacters = 80
	feedChartLabelCharacters = 40
	feedChartUnitCharacters  = 40
	feedCameraNameCharacters = 80
	feedFactRowsAsSeries     = 3
)

// feedDashPattern is an em dash or an en dash with the blanks around it.
var feedDashPattern = regexp.MustCompile("[ \\t]*[\u2014\u2013][ \\t]*")

// rewriteFeedDashes writes the dashes of a text the plain way: a spaced dash
// becomes a comma, an unspaced one a hyphen, and one at the edge of the text
// goes. A spaced dash that opens a line is a list bullet.
func rewriteFeedDashes(text string) string {
	if !strings.ContainsAny(text, "\u2014\u2013") {
		return text
	}
	var written strings.Builder
	last := 0
	for _, bounds := range feedDashPattern.FindAllStringIndex(text, -1) {
		written.WriteString(text[last:bounds[0]])
		last = bounds[1]
		isSpaced := strings.ContainsAny(text[bounds[0]:bounds[1]], " \t")
		switch {
		case !isSpaced:
			written.WriteString("-")
		case bounds[0] == 0 || bounds[1] == len(text):
		case text[bounds[0]-1] == '\n':
			written.WriteString("- ")
		default:
			written.WriteString(", ")
		}
	}
	written.WriteString(text[last:])
	return written.String()
}

// feedText is a card's text as it is kept: its dashes written the plain way,
// trimmed, and cut to length. A text that was cut is named in the card's post
// notes, which the poster is answered with.
func feedText(item *models.AgentFeedItem, fieldName, text string, characters int) string {
	text = strings.TrimSpace(rewriteFeedDashes(text))
	length := len([]rune(text))
	// A text cut earlier ends with the mark and is one character past the
	// limit; cutting it again would say so each time the card is changed.
	if length <= characters || (length == characters+1 && strings.HasSuffix(text, "…")) {
		return text
	}
	item.PostNotes = append(item.PostNotes, fmt.Sprintf("%s was cut to %d characters", fieldName, characters))
	return cutMarked(text, characters)
}

// feedLine is feedText on one line: its line breaks are blanks.
func feedLine(item *models.AgentFeedItem, fieldName, text string, characters int) string {
	return feedText(item, fieldName, strings.Join(strings.Fields(text), " "), characters)
}

// feedTimeLabelPatterns are the labels of a row that name a time or a date.
var feedTimeLabelPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\d{4}-\d{2}(-\d{2})?([ T]\d{1,2}:\d{2}(:\d{2})?)?$`),
	regexp.MustCompile(`(?i)^\d{1,2}:\d{2}(:\d{2})?\s*([ap]\.?m\.?)?$`),
	regexp.MustCompile(`^\d{1,2}/\d{1,2}(/\d{2,4})?$`),
	regexp.MustCompile(`(?i)^(mon|tue|wed|thu|fri|sat|sun)[a-z]*\.?(,?\s+[a-z0-9, ]+)?$`),
	regexp.MustCompile(`(?i)^(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?(\s*\d{1,2})?(,?\s*\d{4})?$`),
	regexp.MustCompile(`(?i)^\d{1,2}\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?(\s+\d{4})?$`),
}

// feedNumberValue is a value that is a number, with a sign or a currency
// symbol before it and a unit after it.
var feedNumberValue = regexp.MustCompile(`^[^\d\-+.]{0,3}[-+]?\d[\d,]*(\.\d+)?\s*[%A-Za-z$\x{20AC}\x{00A3}\x{00A5}\x{00B0}/ ]{0,16}$`)

func isFeedTimeLabel(label string) bool {
	label = strings.TrimSpace(label)
	for _, pattern := range feedTimeLabelPatterns {
		if pattern.MatchString(label) {
			return true
		}
	}
	return false
}

// refuseFeedFactSeries refuses a card whose fact rows are three or more times
// or dates with a number each: that is a series, and a chart draws it.
func refuseFeedFactSeries(rows []models.FeedFactRow) error {
	var found []string
	for index, row := range rows {
		if isFeedTimeLabel(row.FactLabel) && feedNumberValue.MatchString(strings.TrimSpace(row.FactValue)) {
			found = append(found, fmt.Sprint(index+1))
		}
	}
	if len(found) < feedFactRowsAsSeries {
		return nil
	}
	return feedRefusal("fact_rows rows %s are each a time or a date with a number, which is a series: send them as one chart_blocks entry (chartKind series, xLabels, series values and a provenance) instead of rows", strings.Join(found, ", "))
}

// normalizeFeedCharts checks a card's charts and writes them back in their
// one form: text tidied, a missing role measured, bars largest first, and no
// list left nil. A chart that cannot be drawn is refused with its field named.
func normalizeFeedCharts(item *models.AgentFeedItem) error {
	if len(item.ChartBlocks) > models.FeedChartsMost {
		return feedRefusal("chartBlocks has %d charts; a card has at most %d", len(item.ChartBlocks), models.FeedChartsMost)
	}
	if item.ChartBlocks == nil {
		item.ChartBlocks = []models.FeedChartBlock{}
	}
	for index := range item.ChartBlocks {
		if err := normalizeFeedChart(item, &item.ChartBlocks[index], fmt.Sprintf("chartBlocks[%d]", index)); err != nil {
			return err
		}
	}
	return nil
}

func normalizeFeedChart(item *models.AgentFeedItem, chart *models.FeedChartBlock, path string) error {
	chart.ChartKind = models.FeedChartKind(strings.ToLower(strings.TrimSpace(string(chart.ChartKind))))
	chart.ChartTitle = feedLine(item, path+".chartTitle", chart.ChartTitle, feedChartTitleCharacters)
	chart.Unit = feedLine(item, path+".unit", chart.Unit, feedChartUnitCharacters)
	chart.Provenance = feedLine(item, path+".provenance", chart.Provenance, models.FeedProvenanceMost)
	if chart.Provenance == "" {
		return feedRefusal("%s.provenance is missing: say in one line where and when the numbers were read, such as \"Read from the bank at 09:40 EST\"", path)
	}
	if chart.XLabels == nil {
		chart.XLabels = []string{}
	}
	if chart.Series == nil {
		chart.Series = []models.FeedChartSeries{}
	}
	if chart.Markers == nil {
		chart.Markers = []models.FeedChartMarker{}
	}
	if chart.Bars == nil {
		chart.Bars = []models.FeedChartBar{}
	}
	switch chart.ChartKind {
	case models.FeedChartKindSeries:
		if len(chart.Bars) > 0 {
			return feedRefusal("%s.bars is for a bars chart; a series chart has xLabels, series and markers", path)
		}
		return normalizeFeedSeriesChart(item, chart, path)
	case models.FeedChartKindBars:
		if len(chart.XLabels) > 0 || len(chart.Series) > 0 || len(chart.Markers) > 0 {
			return feedRefusal("%s.xLabels, series and markers are for a series chart; a bars chart has bars", path)
		}
		return normalizeFeedBarsChart(item, chart, path)
	}
	return feedRefusal("%s.chartKind is %q; use series or bars", path, chart.ChartKind)
}

// feedChartValueLargest is the largest size a number of a chart may have.
// Past it the drawing loses the small differences, and the apps' arithmetic
// can overflow.
const feedChartValueLargest = 1e15

func isFeedChartValueTooLarge(value float64) bool {
	return math.Abs(value) > feedChartValueLargest || math.IsNaN(value)
}

func normalizeFeedSeriesChart(item *models.AgentFeedItem, chart *models.FeedChartBlock, path string) error {
	if len(chart.XLabels) < models.FeedChartLabelsLeast || len(chart.XLabels) > models.FeedChartLabelsMost {
		return feedRefusal("%s.xLabels has %d labels; a series has from %d to %d, in order", path, len(chart.XLabels), models.FeedChartLabelsLeast, models.FeedChartLabelsMost)
	}
	labels := map[string]bool{}
	for index := range chart.XLabels {
		chart.XLabels[index] = feedLine(item, fmt.Sprintf("%s.xLabels[%d]", path, index), chart.XLabels[index], feedChartLabelCharacters)
		if labels[chart.XLabels[index]] {
			return feedRefusal("%s.xLabels[%d] is %q, which an earlier label already is; each label names one place on the axis", path, index, chart.XLabels[index])
		}
		labels[chart.XLabels[index]] = true
	}
	if len(chart.Series) == 0 || len(chart.Series) > models.FeedChartSeriesMost {
		return feedRefusal("%s.series has %d lines; a chart has from 1 to %d", path, len(chart.Series), models.FeedChartSeriesMost)
	}
	for index := range chart.Series {
		line := &chart.Series[index]
		linePath := fmt.Sprintf("%s.series[%d]", path, index)
		line.SeriesName = feedLine(item, linePath+".seriesName", line.SeriesName, feedChartLabelCharacters)
		line.SeriesRole = models.FeedSeriesRole(strings.ToLower(strings.TrimSpace(string(line.SeriesRole))))
		switch line.SeriesRole {
		case "":
			line.SeriesRole = models.FeedSeriesMeasured
		case models.FeedSeriesMeasured, models.FeedSeriesReference, models.FeedSeriesProjection:
		default:
			return feedRefusal("%s.seriesRole is %q; use measured, reference or projection", linePath, line.SeriesRole)
		}
		if len(line.Values) != len(chart.XLabels) {
			return feedRefusal("%s.values has %d entries; give one number for each of the %d xLabels, or null for none", linePath, len(line.Values), len(chart.XLabels))
		}
		isNumbered := false
		for valueIndex, value := range line.Values {
			if value != nil && isFeedChartValueTooLarge(*value) {
				return feedRefusal("%s.values[%d] is %v; a value is at most %v in size", linePath, valueIndex, *value, feedChartValueLargest)
			}
			isNumbered = isNumbered || value != nil
		}
		if !isNumbered {
			return feedRefusal("%s.values has no number in it", linePath)
		}
	}
	if len(chart.Markers) > models.FeedChartMarkersMost {
		return feedRefusal("%s.markers has %d markers; at most %d", path, len(chart.Markers), models.FeedChartMarkersMost)
	}
	for index := range chart.Markers {
		marker := &chart.Markers[index]
		markerPath := fmt.Sprintf("%s.markers[%d]", path, index)
		marker.MarkerAt = feedLine(item, markerPath+".markerAt", marker.MarkerAt, feedChartLabelCharacters)
		marker.MarkerLabel = feedLine(item, markerPath+".markerLabel", marker.MarkerLabel, feedChartLabelCharacters)
		if !labels[marker.MarkerAt] {
			return feedRefusal("%s.markerAt is %q, which is not one of the xLabels", markerPath, marker.MarkerAt)
		}
	}
	return nil
}

func normalizeFeedBarsChart(item *models.AgentFeedItem, chart *models.FeedChartBlock, path string) error {
	if len(chart.Bars) == 0 || len(chart.Bars) > models.FeedChartBarsMost {
		return feedRefusal("%s.bars has %d bars; a chart has from 1 to %d", path, len(chart.Bars), models.FeedChartBarsMost)
	}
	for index := range chart.Bars {
		bar := &chart.Bars[index]
		barPath := fmt.Sprintf("%s.bars[%d]", path, index)
		bar.BarLabel = feedLine(item, barPath+".barLabel", bar.BarLabel, feedChartLabelCharacters)
		if isFeedChartValueTooLarge(bar.BarValue) {
			return feedRefusal("%s.barValue is %v; a value is at most %v in size", barPath, bar.BarValue, feedChartValueLargest)
		}
		if bar.BarValue < 0 {
			return feedRefusal("%s.barValue is %v; a bar is never negative", barPath, bar.BarValue)
		}
	}
	sort.SliceStable(chart.Bars, func(first, second int) bool { return chart.Bars[first].BarValue > chart.Bars[second].BarValue })
	return nil
}

// isFeedLinkAddress says an address is https or the app's own scheme, for the
// links of a camera still.
func isFeedLinkAddress(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return parsed.Host != ""
	case "teanode":
		return true
	}
	return false
}

// normalizeFeedStill checks the words of a camera still. Where its picture
// comes from is settled as the card is stored.
func normalizeFeedStill(item *models.AgentFeedItem, entry *models.FeedMedia, path string) error {
	entry.CameraName = feedLine(item, path+".cameraName", entry.CameraName, feedCameraNameCharacters)
	entry.StillReason = feedLine(item, path+".stillReason", entry.StillReason, models.FeedStillReasonMost)
	entry.CapturedAt = strings.TrimSpace(entry.CapturedAt)
	entry.PlaybackURL, entry.LiveURL = strings.TrimSpace(entry.PlaybackURL), strings.TrimSpace(entry.LiveURL)
	entry.AttachmentID = strings.TrimSpace(entry.AttachmentID)
	switch {
	case entry.CameraName == "":
		return feedRefusal("%s.cameraName is empty: say which camera the still is from", path)
	case entry.StillReason == "":
		return feedRefusal("%s.stillReason is empty: say why this camera, such as \"the camera at the front door, where the parcel was left\"", path)
	case entry.PhotoID != "" && entry.AttachmentID != "":
		return feedRefusal("%s takes either photoId or attachmentId, not both", path)
	case entry.PlaybackURL != "" && !isFeedLinkAddress(entry.PlaybackURL):
		return feedRefusal("%s.playbackUrl must be an https address or one of the app's own (teanode://)", path)
	case entry.LiveURL != "" && !isFeedLinkAddress(entry.LiveURL):
		return feedRefusal("%s.liveUrl must be an https address or one of the app's own (teanode://)", path)
	}
	if _, err := time.Parse(time.RFC3339, entry.CapturedAt); err != nil {
		return feedRefusal("%s.capturedAt must be the camera's own time with its offset, such as 2026-11-19T14:02:11-05:00", path)
	}
	return nil
}

// carryFeedStills gives a still its picture back from the card it replaces or
// changes when it names the same camera and moment: a new moment needs a new
// still, so a still of another moment with no picture of its own is dropped
// later, with a note.
func carryFeedStills(previous []models.FeedMedia, item *models.AgentFeedItem) {
	for index := range item.FeedMedia {
		entry := &item.FeedMedia[index]
		if entry.FeedMediaKind != models.FeedMediaCameraStill || entry.PhotoID != "" || entry.AttachmentID != "" || entry.ImageFileID != "" {
			continue
		}
		for _, earlier := range previous {
			if earlier.FeedMediaKind == models.FeedMediaCameraStill && earlier.ImageFileID != "" && earlier.CameraName == entry.CameraName && earlier.CapturedAt == entry.CapturedAt {
				entry.ImageFileID, entry.WidthPixels, entry.HeightPixels, entry.MediaKey = earlier.ImageFileID, earlier.WidthPixels, earlier.HeightPixels, earlier.MediaKey
				break
			}
		}
	}
}

// keepFeedStills makes each camera still of the card ready to show: a photo
// of the person's is kept as it is, a still the card already has is kept, and
// a picture a tool fetched in a run, named by its attachment, is read, scaled
// as a page's picture is and kept in the file store. A still whose picture
// cannot be had is left off the card with a note, and the card still posts.
// When the post names the conversation of the run that makes it
// (runConversationId), only a picture of that conversation is kept: a still
// is of what the run's own camera tool answered with, not of any picture the
// agent ever fetched. It answers the files it wrote, to delete if the card is
// not stored.
func (self *Agent) keepFeedStills(ctx context.Context, tx db.Transaction, agentId, runConversationId string, item *models.AgentFeedItem) ([]string, error) {
	var written []string
	kept := make([]models.FeedMedia, 0, len(item.FeedMedia))
	for _, entry := range item.FeedMedia {
		if entry.FeedMediaKind != models.FeedMediaCameraStill || entry.PhotoID != "" || entry.ImageFileID != "" {
			kept = append(kept, entry)
			continue
		}
		imageFileId, image, reason, err := self.keepFeedStill(ctx, tx, agentId, runConversationId, &entry)
		if err != nil {
			return written, err
		}
		if reason != "" {
			item.PostNotes = append(item.PostNotes, fmt.Sprintf("the still from %s was left off the card: %s", entry.CameraName, reason))
			continue
		}
		written = append(written, imageFileId)
		entry.ImageFileID, entry.AttachmentID = imageFileId, ""
		entry.WidthPixels, entry.HeightPixels = image.WidthPixels, image.HeightPixels
		kept = append(kept, entry)
	}
	item.FeedMedia = kept
	return written, nil
}

// keepFeedStill is one still; a reason says why its picture cannot be had.
func (self *Agent) keepFeedStill(ctx context.Context, tx db.Transaction, agentId, runConversationId string, entry *models.FeedMedia) (string, *linkpreview.Image, string, error) {
	if entry.AttachmentID == "" {
		return "", nil, "it has no picture: a still of a new moment needs a new picture, from a camera tool's answer (attachment_id) or one of their photos (photo_id)", nil
	}
	attachment, err := tx.GetAgentAttachment(entry.AttachmentID)
	if err != nil {
		return "", nil, "", err
	}
	if attachment == nil || attachment.AgentID != agentId || !strings.HasPrefix(attachment.ContentType, "image/") {
		return "", nil, fmt.Sprintf("there is no picture with the attachment_id %s; use the one a camera tool's answer gave", entry.AttachmentID), nil
	}
	if runConversationId != "" && attachment.ConversationID != runConversationId {
		return "", nil, fmt.Sprintf("the attachment_id %s is not a picture of this conversation; use the one a camera tool's answer gave here", entry.AttachmentID), nil
	}
	content, err := self.settings.Storage.GetFile(ctx, attachment.ID)
	if err != nil {
		log.Warningf("cannot read the picture %s of a camera still: %s", attachment.ID, err)
		return "", nil, "its picture could not be read", nil
	}
	image, err := linkpreview.KeepImageScaled(ctx, content, feedPictureWidthPixels)
	if err != nil {
		return "", nil, "its picture could not be read as a picture", nil
	}
	imageFileId := fmt.Sprintf("feed-still-%d-%s", time.Now().UnixNano(), entry.MediaKey)
	if err := self.settings.Storage.PutFile(ctx, imageFileId, image.Content); err != nil {
		log.Warningf("cannot keep the picture of a camera still: %s", err)
		return "", nil, "its picture could not be kept", nil
	}
	return imageFileId, image, "", nil
}
