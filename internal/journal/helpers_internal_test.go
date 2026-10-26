package journal

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

func TestHealthMoodLevelLaysValenceOnFiveSteps(t *testing.T) {
	for _, step := range []struct {
		valences []float64
		want     int
	}{
		{nil, 0}, {[]float64{-1}, 1}, {[]float64{-0.5}, 2}, {[]float64{0}, 3}, {[]float64{0.5}, 4}, {[]float64{1}, 5},
		{[]float64{0.4, 0.6}, 4}, {[]float64{-1, 1}, 3}, {[]float64{-3}, 1}, {[]float64{7}, 5}, {[]float64{0.24}, 3}, {[]float64{0.26}, 4},
	} {
		if got := HealthMoodLevel(step.valences); got != step.want {
			t.Errorf("HealthMoodLevel(%v) = %d, want %d", step.valences, got, step.want)
		}
	}
}

func TestPostClockSpanReadsTheClockOfTheFirstAndLastPost(t *testing.T) {
	chunks := func(texts ...string) []*models.AgentChunk {
		var found []*models.AgentChunk
		for _, text := range texts {
			found = append(found, &models.AgentChunk{Text: text})
		}
		return found
	}
	for name, step := range map[string]struct {
		chunks  []*models.AgentChunk
		minutes int
		isFound bool
	}{
		"one post":      {chunks("09:00 ada: hello"), 0, true},
		"three posts":   {chunks("09:00 ada: hello\n09:20 Claude Code: hi\ncontinued\n09:45 ada: bye"), 45, true},
		"two chunks":    {chunks("09:00 ada: hello", "10:30 ada: later"), 90, true},
		"over midnight": {chunks("23:50 ada: late\n00:10 ada: later"), 20, true},
		"no posts":      {chunks("just words"), 0, false},
		"a bad clock":   {chunks("25:00 ada: x"), 0, false},
	} {
		minutes, isFound := postClockSpan(step.chunks)
		if minutes != step.minutes || isFound != step.isFound {
			t.Errorf("%s: %d %t, want %d %t", name, minutes, isFound, step.minutes, step.isFound)
		}
	}
}

func TestSpreadPhotosTakesEvenlyAndPassesOverScreenshots(t *testing.T) {
	var photos []*models.Photo
	for index := 0; index < 30; index++ {
		photos = append(photos, &models.Photo{ID: fmt.Sprintf("photo-%02d", index), IsScreenshot: index%3 == 0})
	}
	spread := spreadPhotos(photos, 12)
	if len(spread) != 12 {
		t.Fatalf("twelve photos: %v", spread)
	}
	for _, identifier := range spread {
		var index int
		if _, err := fmt.Sscanf(identifier, "photo-%d", &index); err != nil {
			t.Fatalf("%s is not a photo the test made: %s", identifier, err)
		}
		if index%3 == 0 {
			t.Errorf("a screenshot was offered: %s", identifier)
		}
	}
	if spread[0] != "photo-01" || spread[11] <= spread[5] {
		t.Errorf("spread over the day, in order: %v", spread)
	}
	// Few photos are all offered, the screenshots after the photos taken.
	if few := spreadPhotos(photos[:2], 12); fmt.Sprint(few) != "[photo-01 photo-00]" {
		t.Errorf("few photos: %v", few)
	}
	if none := spreadPhotos(nil, 12); none != nil {
		t.Errorf("no photos: %v", none)
	}
}

func TestMinorUnitsReadsADecimalInTheSmallestUnit(t *testing.T) {
	for _, step := range []struct {
		amount, currency string
		want             int64
	}{
		{"-42.17", "USD", -4217}, {"10", "USD", 1000}, {"0.005", "USD", 1}, {"-0.005", "USD", -1}, {"1234.5", "JPY", 1235},
		{"1.234", "KWD", 1234}, {"nonsense", "USD", 0}, {"", "USD", 0}, {" 3.50 ", "EUR", 350},
	} {
		if got := minorUnits(step.amount, step.currency); got != step.want {
			t.Errorf("minorUnits(%q, %s) = %d, want %d", step.amount, step.currency, got, step.want)
		}
	}
	for _, step := range []struct {
		minor    int64
		currency string
		want     string
	}{
		{-4217, "USD", "-42.17 USD"}, {5, "USD", "0.05 USD"}, {1235, "JPY", "1235 JPY"}, {1234, "KWD", "1.234 KWD"},
	} {
		if got := amountWords(step.minor, step.currency); got != step.want {
			t.Errorf("amountWords(%d, %s) = %q, want %q", step.minor, step.currency, got, step.want)
		}
	}
}

func TestMinutesOfUnionCountsOverlapOnce(t *testing.T) {
	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 22, hour, minute, 0, 0, time.UTC) }
	spans := [][2]time.Time{{at(9, 0), at(10, 0)}, {at(9, 30), at(10, 30)}, {at(12, 0), at(12, 15)}, {at(13, 0), at(13, 0)}}
	if got := minutesOfUnion(spans); got != 105 {
		t.Errorf("union = %d, want 105", got)
	}
	if got := minutesOfUnion(nil); got != 0 {
		t.Errorf("none = %d", got)
	}
}

func TestMainPlaceIsTheLongestStayInsideTheDay(t *testing.T) {
	zone := time.UTC
	at := func(day, hour int) *time.Time { value := time.Date(2026, 10, day, hour, 0, 0, 0, zone); return &value }
	from, until := *at(22, 0), *at(23, 0)
	visits := []Visit{
		{PlaceID: "home", PlaceName: "Home", ArrivedAt: at(21, 20), DepartedAt: at(22, 6), Latitude: 40.65, Longitude: -73.95},
		{PlaceID: "cafe", PlaceName: "Cafe", ArrivedAt: at(22, 9), DepartedAt: at(22, 12), Latitude: 40.7128, Longitude: -74.006},
		{PlaceID: "office", PlaceName: "Office", ArrivedAt: at(22, 12), DepartedAt: at(22, 19), Latitude: 40.7484, Longitude: -73.9857},
		{PlaceID: "unknown", ArrivedAt: nil, DepartedAt: at(22, 20)},
	}
	placeId, name, latitude, longitude := mainPlace(visits, from, until, *at(22, 23))
	if placeId != "office" || name != "Office" || latitude != 40.7 || longitude != -74.0 {
		t.Errorf("main place: %s %s %v %v", placeId, name, latitude, longitude)
	}
	// Home, from the evening before until the next evening, still going at
	// ten at night, counts for the part inside the day.
	home := []Visit{{PlaceID: "home", PlaceName: "Home", ArrivedAt: at(21, 20), Latitude: 1, Longitude: 2}, visits[1]}
	if placeId, _, _, _ := mainPlace(home, from, until, *at(22, 22)); placeId != "home" {
		t.Errorf("a visit still going counts up to now: %s", placeId)
	}
	if placeId, _, _, _ := mainPlace(nil, from, until, until); placeId != "" {
		t.Errorf("no visits, no place: %s", placeId)
	}
}

func TestSleepOfPrefersTheUnionOfTheNightAndOrdersTheStages(t *testing.T) {
	minutes, stages := sleepOf(&models.HealthPeriodSummary{CategorySeconds: map[string]float64{
		"asleep": 25200, "asleepREM": 5400, "asleepDeep": 5400, "asleepCore": 14400, "awake": 1200, "inBed": 28800,
	}})
	if minutes != 420 || fmt.Sprint(stages) != "[{deep 90} {core 240} {rem 90} {awake 20} {inBed 480}]" {
		t.Errorf("a night: %d %v", minutes, stages)
	}
	minutes, _ = sleepOf(&models.HealthPeriodSummary{CategorySeconds: map[string]float64{"asleepCore": 3600, "asleepREM": 1800, "awake": 600}})
	if minutes != 90 {
		t.Errorf("without the union the asleep stages are added: %d", minutes)
	}
	duration := 7200.0
	if minutes, stages = sleepOf(&models.HealthPeriodSummary{DurationSeconds: &duration}); minutes != 120 || len(stages) != 0 {
		t.Errorf("only a duration: %d %v", minutes, stages)
	}
}

func TestStepsByHourAsksTheClockAndNotTheOffset(t *testing.T) {
	zone := time.FixedZone("test", -4*3600)
	value := func(number float64) *float64 { return &number }
	samples := []*models.HealthSample{
		{StartedAt: time.Date(2026, 10, 22, 12, 30, 0, 0, time.UTC), QuantityValue: value(100)},
		{StartedAt: time.Date(2026, 10, 22, 12, 45, 0, 0, time.UTC), QuantityValue: value(50)},
		{StartedAt: time.Date(2026, 10, 23, 3, 59, 0, 0, time.UTC), QuantityValue: value(7)},
		{StartedAt: time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)},
	}
	hours := stepsByHour(samples, zone)
	if hours[8] != 150 || hours[23] != 7 || hours[10] != 0 {
		t.Errorf("hours: %v", hours)
	}
}

func TestWhoIsNamedInADocument(t *testing.T) {
	names := []string{"ada", "harbor"}
	document := func(metadata map[string]any) *models.AgentDocument { return &models.AgentDocument{Metadata: metadata} }
	if !isNamedIn(document(map[string]any{"author": "Ada Harbor"}), names) || !isNamedIn(document(map[string]any{"editors": []any{"Zed", "ada@example.test"}}), names) {
		t.Error("the person wrote or edited it")
	}
	if isNamedIn(document(map[string]any{"author": "Adam Quay"}), names) || isNamedIn(document(nil), names) || isNamedIn(document(map[string]any{"title": "ada"}), names) {
		t.Error("a name that merely starts alike, no metadata and an unrelated key are not the person")
	}
	chat := func(posts int, participants ...any) *models.AgentDocument {
		return document(map[string]any{"posts": posts, "participants": participants})
	}
	if !isInChatUnit(chat(2, "Ada Harbor", "ben"), names) || isInChatUnit(chat(1, "ada"), names) || isInChatUnit(chat(5, "zed"), names) {
		t.Error("an exchange the person took part in")
	}
	if codingAssistantOf(document(map[string]any{"assistant": "Claude Code"})) != "" {
		t.Error("only a chat is a coding session")
	}
	coding := &models.AgentDocument{Kind: models.DocumentChat, Metadata: map[string]any{"assistant": "Claude Code"}}
	if codingAssistantOf(coding) != "claude-code" {
		t.Error("Claude Code")
	}
	coding.Metadata["assistant"] = "Codex"
	if codingAssistantOf(coding) != "codex" {
		t.Error("Codex")
	}
	coding.Metadata["assistant"] = "Somebody else"
	if codingAssistantOf(coding) != "" {
		t.Error("another assistant")
	}
}

func TestPhotosFoldIntoMomentsAQuarterHourApart(t *testing.T) {
	at := func(minute int) time.Time {
		return time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC).Add(time.Duration(minute) * time.Minute)
	}
	moments := photoMoments([]Photo{
		{PhotoID: "a", TakenAt: at(0), PlaceName: "Quay"}, {PhotoID: "b", TakenAt: at(10)}, {PhotoID: "c", TakenAt: at(24)},
		{PhotoID: "d", TakenAt: at(60), IsVideo: true}, {PhotoID: "e", TakenAt: at(120)},
	})
	if len(moments) != 3 || moments[0].Title != "3 photos" || moments[0].RecordID != "a" || moments[0].Detail != "Quay" || moments[1].Title != "1 video" || moments[2].Title != "1 photo" {
		t.Errorf("moments: %+v", moments)
	}
}

func TestInputDigestMovesWhenARecordArrives(t *testing.T) {
	day := &Day{Day: "2026-10-22", PhotoTotal: 1, Photos: []Photo{{PhotoID: "one", TakenAt: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}}}
	base := InputDigest(day)
	day.Photos = append(day.Photos, Photo{PhotoID: "two", TakenAt: time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)})
	day.PhotoTotal = 2
	if InputDigest(day) == base {
		t.Error("a photo that arrives late changes what the day is written from")
	}
	other := &Day{Day: "2026-10-22", PhotoTotal: 1, Photos: []Photo{{PhotoID: "one", TakenAt: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}}}
	if InputDigest(other) != base {
		t.Error("the same records are the same digest")
	}
}

func TestAddDaysTotalsAndMoods(t *testing.T) {
	mood := func(level int) *int { return &level }
	days := []*models.JournalDay{
		{Day: "2026-10-20", MoodLevel: mood(4), Facts: models.JournalFacts{PhotoCount: 2, SpentMinorUnits: 500, SpentCurrency: "USD", VisitedPlaceIDs: []string{"a", "b"}, MainPlaceID: "a", MainPlaceName: "A"}},
		{Day: "2026-10-21", MoodLevel: mood(4), Facts: models.JournalFacts{PhotoCount: 3, SpentMinorUnits: 700, SpentCurrency: "USD", VisitedPlaceIDs: []string{"b", "c"}, MainPlaceID: "b", MainPlaceName: "B"}},
		{Day: "2026-10-22", MoodLevel: mood(1), Facts: models.JournalFacts{SpentMinorUnits: 90000, SpentCurrency: "JPY", VisitedPlaceIDs: []string{"b"}, MainPlaceID: "b", MainPlaceName: "B"}},
		{Day: "2026-10-23"},
	}
	totals, moods := addDays(days)
	if totals.PhotoCount != 5 || totals.PlaceCount != 3 || totals.SpentMinorUnits != 1200 || totals.SpentCurrency != "USD" || totals.MainPlaceID != "b" || !totals.HasActivity {
		t.Errorf("totals: %+v", totals)
	}
	if moods != [5]int{1, 0, 0, 2, 0} {
		t.Errorf("moods: %v", moods)
	}
}
