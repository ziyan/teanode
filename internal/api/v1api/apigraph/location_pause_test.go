package apigraph

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const pauseFieldsForTest = `id startedAt endedAt`

// pastPause makes a pause that began and ended in the past, as one the person
// made earlier and a phone that was offline then does not know of.
func (self *locationWorld) pastPause(t *testing.T, user *models.User, startedAgo, endedAgo time.Duration) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		endedAt := time.Now().Add(-endedAgo)
		if _, _, err := tx.PauseLocation(user.ID, &endedAt, time.Now().Add(-startedAgo)); err != nil {
			t.Fatalf("PauseLocation: %s", err)
		}
	})
}

func (self *locationWorld) reportPaused(t *testing.T, user *models.User, points, visits string) map[string]any {
	t.Helper()
	reportAnswerData := self.call(t, user, fmt.Sprintf(`mutation { ReportLocation(points: [%s], visits: [%s]) {
		newPointCount newVisitCount updatedVisitCount pausedPointCount pausedVisitCount pause { `+pauseFieldsForTest+` } } }`, points, visits))
	return reportAnswerData["ReportLocation"].(map[string]any)
}

func (self *locationWorld) visitsOf(t *testing.T, user *models.User) []map[string]any {
	t.Helper()
	page := self.call(t, user, `query { ListLocationVisits { placeVisits { id arrivedAt departedAt place { placeName } } } }`)["ListLocationVisits"].(map[string]any)
	visits := []map[string]any{}
	for _, each := range page["placeVisits"].([]any) {
		visits = append(visits, each.(map[string]any))
	}
	return visits
}

func pauseTimeOf(t *testing.T, timeValue any) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, fmt.Sprint(timeValue))
	if err != nil {
		t.Fatalf("%v is not a time: %s", timeValue, err)
	}
	return parsed
}

// While a person is paused, anybody they share with sees the pause and no
// position and no visit in progress, at either level, in GetLocation and in
// ListSharedLocations; the person still sees their own; and after they resume
// the position is shown again.
func TestAViewerSeesThePauseAndNoPositionWhilePaused(t *testing.T) {
	for _, level := range []string{"current", "history"} {
		t.Run(level, func(t *testing.T) { viewerSeesThePause(t, level) })
	}
}

// viewerSeesThePause is one level of TestAViewerSeesThePauseAndNoPositionWhilePaused.
func viewerSeesThePause(t *testing.T, level string) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40, -75, ago(5*time.Minute)), visitInput("home", 40, -75, ago(20*time.Minute), ""))
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: %q) { id } }`, world.ben.ID, level))
	const viewed = `{ userId latestPoint { latitudeDegrees } openVisit { id } pause { ` + pauseFieldsForTest + ` } }`
	location := func(query, key string) map[string]any {
		t.Helper()
		viewedData := world.call(t, world.ben, query)[key]
		if list, isList := viewedData.([]any); isList {
			if len(list) != 1 {
				t.Fatalf("%s: %v", key, list)
			}
			return list[0].(map[string]any)
		}
		return viewedData.(map[string]any)
	}
	read := func() map[string]map[string]any {
		return map[string]map[string]any{
			"GetLocation":         location(fmt.Sprintf(`query { GetLocation(userId: %q) %s }`, world.ada.ID, viewed), "GetLocation"),
			"ListSharedLocations": location(`query { ListSharedLocations `+viewed+` }`, "ListSharedLocations"),
		}
	}

	for name, seen := range read() {
		if seen["latestPoint"] == nil || seen["openVisit"] == nil || seen["pause"] != nil {
			t.Fatalf("%s at %s before the pause shows where they are: %v", name, level, seen)
		}
	}

	endedAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	paused := world.call(t, world.ada, fmt.Sprintf(`mutation { PauseLocation(endedAt: %q) { `+pauseFieldsForTest+` } }`, endedAt))["PauseLocation"].(map[string]any)
	if paused["id"] == nil || !pauseTimeOf(t, paused["endedAt"]).Equal(pauseTimeOf(t, endedAt)) {
		t.Fatalf("the pause: %v", paused)
	}
	for name, seen := range read() {
		if seen["latestPoint"] != nil || seen["openVisit"] != nil || seen["pause"] == nil || !pauseTimeOf(t, seen["pause"].(map[string]any)["endedAt"]).Equal(pauseTimeOf(t, endedAt)) {
			t.Errorf("%s at %s while paused shows the pause and no position: %v", name, level, seen)
		}
	}
	own := world.call(t, world.ada, `query { GetLocation { latestPoint { latitudeDegrees } pause { `+pauseFieldsForTest+` } } }`)["GetLocation"].(map[string]any)
	if own["latestPoint"] == nil || own["pause"] == nil {
		t.Errorf("they still see their own position, and the pause: %v", own)
	}

	if resumed := world.call(t, world.ada, `mutation { ResumeLocation }`)["ResumeLocation"]; resumed != true {
		t.Fatalf("resuming a pause: %v", resumed)
	}
	for name, seen := range read() {
		if seen["latestPoint"] == nil || seen["pause"] != nil {
			t.Errorf("%s at %s after resuming shows where they are again: %v", name, level, seen)
		}
	}
	if resumed := world.call(t, world.ada, `mutation { ResumeLocation }`)["ResumeLocation"]; resumed != false {
		t.Errorf("resuming when not paused: %v", resumed)
	}
}

// A pause that ended by itself shows nothing: the viewer sees the position as
// it was before.
func TestAPauseThatEndedIsNotShown(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40, -75, ago(3*time.Hour)), "")
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	world.pastPause(t, world.ada, 2*time.Hour, time.Hour)
	seen := world.call(t, world.ben, fmt.Sprintf(`query { GetLocation(userId: %q) { latestPoint { latitudeDegrees } pause { id } } }`, world.ada.ID))["GetLocation"].(map[string]any)
	if seen["latestPoint"] == nil || seen["pause"] != nil {
		t.Errorf("the pause is over: %v", seen)
	}
}

// Pausing ends the visit in progress at the start of the pause, so that a
// viewer is never left seeing the person at a place for the length of it, and
// pausing again changes only the end of the pause.
func TestPausingEndsTheVisitInProgressAndPausingAgainChangesTheEnd(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40, -75, ago(5*time.Minute)), visitInput("home", 40, -75, ago(20*time.Minute), ""))
	if visits := world.visitsOf(t, world.ada); len(visits) != 1 || visits[0]["departedAt"] != nil {
		t.Fatalf("a visit in progress: %v", visits)
	}

	first := world.call(t, world.ada, `mutation { PauseLocation { `+pauseFieldsForTest+` } }`)["PauseLocation"].(map[string]any)
	if first["endedAt"] != nil {
		t.Errorf("no end given, no end: %v", first)
	}
	visits := world.visitsOf(t, world.ada)
	if len(visits) != 1 || visits[0]["departedAt"] == nil {
		t.Fatalf("the visit ended: %v", visits)
	}
	if difference := pauseTimeOf(t, visits[0]["departedAt"]).Sub(pauseTimeOf(t, first["startedAt"])); difference > time.Millisecond || difference < -time.Millisecond {
		t.Errorf("the visit ended at the start of the pause: %v and %v", visits[0]["departedAt"], first["startedAt"])
	}
	if open := world.call(t, world.ada, `query { GetLocation { openVisit { id } } }`)["GetLocation"].(map[string]any); open["openVisit"] != nil {
		t.Errorf("nobody is at a place while paused: %v", open)
	}

	endedAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	second := world.call(t, world.ada, fmt.Sprintf(`mutation { PauseLocation(endedAt: %q) { `+pauseFieldsForTest+` } }`, endedAt))["PauseLocation"].(map[string]any)
	if second["id"] != first["id"] || !pauseTimeOf(t, second["endedAt"]).Equal(pauseTimeOf(t, endedAt)) || !pauseTimeOf(t, second["startedAt"]).Equal(pauseTimeOf(t, first["startedAt"])) {
		t.Errorf("pausing while paused changes the end of the pause in effect: %v then %v", first, second)
	}
	pauses := world.call(t, world.ada, fmt.Sprintf(`query { ListLocationPauses(from: %q, until: %q) { `+pauseFieldsForTest+` } }`, ago(time.Hour), time.Now().Add(time.Hour).UTC().Format(time.RFC3339)))["ListLocationPauses"].([]any)
	if len(pauses) != 1 {
		t.Errorf("one pause: %v", pauses)
	}
}

// The end of a pause is after now and at most thirty days ahead; the agent
// pauses as the person does.
func TestThePauseBoundsAndTheAgentPausing(t *testing.T) {
	world := newLocationWorld(t)
	for name, endedAt := range map[string]time.Time{
		"in the past":      time.Now().Add(-time.Minute),
		"now":              time.Now(),
		"past thirty days": time.Now().Add(31 * 24 * time.Hour),
	} {
		refusalMessage := world.refused(t, world.ada, fmt.Sprintf(`mutation { PauseLocation(endedAt: %q) { id } }`, endedAt.UTC().Format(time.RFC3339)))
		if !strings.Contains(refusalMessage, "pause") {
			t.Errorf("%s: the refusal says why: %s", name, refusalMessage)
		}
	}
	if pause := world.call(t, world.ada, `query { GetLocation { pause { id } } }`)["GetLocation"].(map[string]any); pause["pause"] != nil {
		t.Errorf("a refusal pauses nothing: %v", pause)
	}
	world.call(t, world.ada, fmt.Sprintf(`mutation { PauseLocation(endedAt: %q) { id } }`, time.Now().Add(29*24*time.Hour).UTC().Format(time.RFC3339)))

	answerData, failures := world.askWithHeaders(t, world.ben, models.CredentialSession, map[string]string{api.AgentCommandHeader: "1"}, `mutation { PauseLocation { id } }`)
	if len(failures) > 0 || answerData["PauseLocation"] == nil {
		t.Errorf("the agent pauses as the person does: %v %v", answerData, failures)
	}
	// Resuming records more, so a command the agent ran is refused it; the
	// person's own command resumes.
	_, failures = world.askWithHeaders(t, world.ben, models.CredentialSession, map[string]string{api.AgentCommandHeader: "1"}, `mutation { ResumeLocation }`)
	if !strings.Contains(strings.Join(failures, "; "), "the person's own") {
		t.Errorf("the agent's command does not resume: %v", failures)
	}
	answerData, failures = world.askWithHeaders(t, world.ben, models.CredentialToken, nil, `mutation { ResumeLocation }`)
	if len(failures) > 0 || answerData["ResumeLocation"] != true {
		t.Errorf("the person's command resumes: %v %v", answerData, failures)
	}
}

// A report of positions and visits measured while the person had paused stores
// none of them and counts them, whatever the phone knew: the part outside the
// pause is stored.
func TestAReportInsideAPauseStoresNothingAndCountsIt(t *testing.T) {
	world := newLocationWorld(t)
	// Paused from 3 hours ago to 2 hours ago.
	world.pastPause(t, world.ada, 3*time.Hour, 2*time.Hour)

	points := strings.Join([]string{
		pointInput(40, -75, ago(4*time.Hour)),
		pointInput(40.1, -75, ago(150*time.Minute)),
		pointInput(40.2, -75, ago(140*time.Minute)),
		pointInput(40.3, -75, ago(90*time.Minute)),
	}, ", ")
	visits := strings.Join([]string{
		visitInput("inside", 40.1, -75, ago(145*time.Minute), ago(135*time.Minute)),
		visitInput("outside", 40.3, -75, ago(90*time.Minute), ago(80*time.Minute)),
	}, ", ")
	report := world.reportPaused(t, world.ada, points, visits)
	if report["newPointCount"] != float64(2) || report["pausedPointCount"] != float64(2) || report["newVisitCount"] != float64(1) || report["pausedVisitCount"] != float64(1) || report["pause"] != nil {
		t.Errorf("two positions and a visit were inside it: %v", report)
	}
	if visits := world.visitsOf(t, world.ada); len(visits) != 1 {
		t.Errorf("only the visit outside the pause: %v", visits)
	}
	stored := world.call(t, world.ada, fmt.Sprintf(`query { ListLocationPoints(from: %q, until: %q) { latitudeDegrees } }`, ago(5*time.Hour), ago(0)))["ListLocationPoints"].([]any)
	if len(stored) != 2 || stored[0].(map[string]any)["latitudeDegrees"] != float64(40) || stored[1].(map[string]any)["latitudeDegrees"] != float64(40.3) {
		t.Errorf("the positions outside it: %v", stored)
	}

	// The pause includes its first instant and not its last.
	edges := world.reportPaused(t, world.ada, strings.Join([]string{pointInput(41, -75, ago(3*time.Hour)), pointInput(41.1, -75, ago(2*time.Hour))}, ", "), "")
	if edges["pausedPointCount"] != float64(1) || edges["newPointCount"] != float64(1) {
		t.Errorf("the start is inside and the end is not: %v", edges)
	}

	// Sent again, nothing changes, and nothing is stored either way.
	again := world.reportPaused(t, world.ada, points, visits)
	if again["newPointCount"] != float64(0) || again["newVisitCount"] != float64(0) || again["pausedPointCount"] != float64(2) || again["pausedVisitCount"] != float64(1) {
		t.Errorf("the same batch again: %v", again)
	}
}

// A report while the pause is in effect says so, so that a phone learns of a
// pause set somewhere else, and drops what it measured since.
func TestAReportAnswersThePauseInEffect(t *testing.T) {
	world := newLocationWorld(t)
	world.call(t, world.ada, `mutation { PauseLocation { id } }`)
	soon := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	report := world.reportPaused(t, world.ada, pointInput(40, -75, soon), visitInput("now", 40, -75, soon, ""))
	if report["pause"] == nil || report["pausedPointCount"] != float64(1) || report["pausedVisitCount"] != float64(1) || report["newPointCount"] != float64(0) {
		t.Errorf("the report while paused: %v", report)
	}
	if visits := world.visitsOf(t, world.ada); len(visits) != 0 {
		t.Errorf("no visit while paused: %v", visits)
	}
	// Nor a place made for it.
	places := world.call(t, world.ada, `query { ListPlaces { places { id } } }`)["ListPlaces"].(map[string]any)["places"].([]any)
	if len(places) != 0 {
		t.Errorf("a visit measured during a pause makes no place: %v", places)
	}
	world.call(t, world.ada, `mutation { ResumeLocation }`)
	if report = world.reportPaused(t, world.ada, pointInput(40, -75, soon), ""); report["pause"] != nil || report["newPointCount"] != float64(1) {
		t.Errorf("after resuming it is stored: %v", report)
	}
}

// A visit that arrived before a pause is kept up to its start and ends there,
// whether its departure is inside the pause, after it, or never reported; one
// that arrived inside the pause keeps only its departure after it.
func TestAVisitAcrossAPauseEndsWhereThePauseBegan(t *testing.T) {
	cases := map[string]struct {
		visit          func(key string) string
		wantArrived    bool
		wantDepartedAt time.Duration // how long ago; zero means the visit is open
	}{
		"departure inside the pause":    {func(key string) string { return visitInput(key, 40, -75, ago(5*time.Hour), ago(150*time.Minute)) }, true, 3 * time.Hour},
		"departure after the pause":     {func(key string) string { return visitInput(key, 40, -75, ago(5*time.Hour), ago(30*time.Minute)) }, true, 3 * time.Hour},
		"no departure reported":         {func(key string) string { return visitInput(key, 40, -75, ago(5*time.Hour), "") }, true, 3 * time.Hour},
		"arrived inside, left after it": {func(key string) string { return visitInput(key, 40, -75, ago(150*time.Minute), ago(30*time.Minute)) }, false, 30 * time.Minute},
		"before the pause entirely":     {func(key string) string { return visitInput(key, 40, -75, ago(5*time.Hour), ago(4*time.Hour)) }, true, 4 * time.Hour},
		"after the pause entirely":      {func(key string) string { return visitInput(key, 40, -75, ago(90*time.Minute), ago(80*time.Minute)) }, true, 80 * time.Minute},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			world := newLocationWorld(t)
			world.pastPause(t, world.ada, 3*time.Hour, 2*time.Hour)
			report := world.reportPaused(t, world.ada, "", test.visit("across"))
			if report["newVisitCount"] != float64(1) || report["pausedVisitCount"] != float64(0) {
				t.Fatalf("the visit is stored: %v", report)
			}
			visits := world.visitsOf(t, world.ada)
			if len(visits) != 1 {
				t.Fatalf("one visit: %v", visits)
			}
			if (visits[0]["arrivedAt"] != nil) != test.wantArrived {
				t.Errorf("arrival kept is %v, want %v: %v", visits[0]["arrivedAt"] != nil, test.wantArrived, visits[0])
			}
			if visits[0]["departedAt"] == nil {
				t.Fatalf("the visit ended: %v", visits[0])
			}
			if difference := time.Since(pauseTimeOf(t, visits[0]["departedAt"])) - test.wantDepartedAt; difference > 5*time.Second || difference < -5*time.Second {
				t.Errorf("the visit ended %v ago, want %v: %v", time.Since(pauseTimeOf(t, visits[0]["departedAt"])), test.wantDepartedAt, visits[0])
			}
		})
	}
}

// A departure that comes later for a visit stored before the pause, from inside
// it or from after it, ends the visit where the pause began.
func TestALaterDepartureOfAVisitBeforeAPauseEndsItAtThePause(t *testing.T) {
	for name, departure := range map[string]time.Duration{"inside": 150 * time.Minute, "after": 30 * time.Minute} {
		t.Run(name, func(t *testing.T) {
			world := newLocationWorld(t)
			world.report(t, world.ada, "", visitInput("open", 41, -75, ago(4*time.Hour+30*time.Minute), ""))
			world.pastPause(t, world.ada, 3*time.Hour, 2*time.Hour)
			departed := fmt.Sprintf(`{clientVisitKey: "open", latitudeDegrees: 41, longitudeDegrees: -75, accuracyMeters: 0, departedAt: %q, visitSource: "visit"}`, ago(departure))
			if report := world.reportPaused(t, world.ada, "", departed); report["pausedVisitCount"] != float64(0) || report["updatedVisitCount"] != float64(1) {
				t.Fatalf("the departure completes the visit: %v", report)
			}
			visits := world.visitsOf(t, world.ada)
			if len(visits) != 1 || visits[0]["arrivedAt"] == nil || visits[0]["departedAt"] == nil {
				t.Fatalf("one visit, with both times: %v", visits)
			}
			if ended := time.Since(pauseTimeOf(t, visits[0]["departedAt"])); ended < 3*time.Hour-5*time.Second || ended > 3*time.Hour+5*time.Second {
				t.Errorf("the visit ended %v ago, not at the start of the pause: %v", ended, visits[0])
			}
		})
	}
}

// A visit that arrived before two pauses and left inside the second ends where
// the first began, since it is not known where the person was from then on.
func TestAVisitAcrossTwoPausesThatLeftInTheSecondEndsAtTheFirst(t *testing.T) {
	at := func(hour, minute int) time.Time { return time.Date(2026, 3, 4, hour, minute, 0, 0, time.UTC) }
	pauseBetween := func(startedAt, endedAt time.Time) *models.LocationPause {
		return &models.LocationPause{StartedAt: startedAt, EndedAt: &endedAt}
	}
	pauses := locationPauseSet{pauseBetween(at(12, 0), at(12, 30)), pauseBetween(at(11, 0), at(11, 30))}
	arrivedAt, departedAt := at(10, 0), at(12, 10)

	clipped := pauses.clipVisit(&arrivedAt, &departedAt, nil)
	if clipped.IsDropped || !clipped.IsDepartureCut || clipped.DepartedAt == nil || !clipped.DepartedAt.Equal(at(11, 0)) {
		t.Errorf("the visit ends at the start of the first pause: %+v", clipped)
	}

	// The same for a departure reported alone for a visit stored earlier.
	known := &models.PlaceVisit{ArrivedAt: &arrivedAt}
	if clipped = pauses.clipVisit(nil, &departedAt, known); clipped.IsDropped || clipped.DepartedAt == nil || !clipped.DepartedAt.Equal(at(11, 0)) {
		t.Errorf("a later departure ends at the start of the first pause: %+v", clipped)
	}

	// With the arrival between the pauses, the second is the first to begin after it.
	arrivedAt = at(11, 45)
	if clipped = pauses.clipVisit(&arrivedAt, &departedAt, nil); clipped.DepartedAt == nil || !clipped.DepartedAt.Equal(at(12, 0)) {
		t.Errorf("the visit ends at the start of the pause after its arrival: %+v", clipped)
	}
}

// A departure cut to the start of a pause is not told to anybody, and a visit
// measured entirely inside one makes no notice.
func TestADepartureCutAtAPauseIsNotToldAndAVisitInsideItIsNotStored(t *testing.T) {
	world := newLocationWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		startedAt := time.Now().Add(-3 * time.Hour)
		endedAt := time.Now().Add(-2 * time.Hour)
		pauses := locationPauseSet{{ID: "pause", UserID: world.ada.ID, StartedAt: startedAt, EndedAt: &endedAt}}
		reported := func(key string, arrivedAt, departedAt time.Duration) *storedVisit {
			arrived, departed := time.Now().Add(-arrivedAt), time.Now().Add(-departedAt)
			stored, err := world.graph.putReportedVisit(tx, world.ada.ID, ReportedVisit{
				ClientVisitKey: key, LatitudeDegrees: 40 + float64(len(key)), LongitudeDegrees: -75, AccuracyMeters: 10,
				VisitSource: string(models.VisitSourceVisit), ArrivedAt: &arrived, DepartedAt: &departed,
			}, pauses)
			if err != nil {
				t.Fatalf("putReportedVisit: %s", err)
			}
			return stored
		}
		if stored := reported("cut", 5*time.Hour, 150*time.Minute); stored.Notice != nil || stored.IsPaused || !stored.IsNew {
			t.Errorf("a departure cut at the start of the pause is told to nobody: %+v", stored)
		}
		if stored := reported("outside", 90*time.Minute, 80*time.Minute); stored.Notice == nil || stored.IsPaused {
			t.Errorf("a visit outside the pause is told as ever: %+v", stored)
		}
		if stored := reported("inside", 170*time.Minute, 130*time.Minute); !stored.IsPaused || stored.Notice != nil || stored.MadePlace != nil {
			t.Errorf("a visit inside the pause is not stored, told or given a place: %+v", stored)
		}
	})
}

// A check-in is a visit measured now, which a pause does not store: it is
// refused while paused, in words that say when the pause ends.
func TestACheckInIsRefusedWhileThePersonIsPaused(t *testing.T) {
	world := newLocationWorld(t)
	world.call(t, world.ada, `mutation { SavePlace(placeName: "Corner Cafe", latitudeDegrees: 40, longitudeDegrees: -75) { id } }`)
	places := world.call(t, world.ada, `query { ListPlaces { places { id } } }`)["ListPlaces"].(map[string]any)["places"].([]any)
	placeId := places[0].(map[string]any)["id"].(string)
	// The refusal gives the end in the zone the person last used, with its name.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.TouchUserLocation(world.ada.ID, "Asia/Tokyo", "", false, time.Now()); err != nil {
			t.Fatalf("TouchUserLocation: %s", err)
		}
	})
	endedAt := time.Now().Add(time.Hour).Truncate(time.Second)
	world.call(t, world.ada, fmt.Sprintf(`mutation { PauseLocation(endedAt: %q) { id } }`, endedAt.UTC().Format(time.RFC3339)))
	wanted := "paused until " + endedAt.In(time.FixedZone("JST", 9*60*60)).Format("2006-01-02 15:04 MST")
	if refusalMessage := world.refused(t, world.ada, fmt.Sprintf(`mutation { CheckIn(placeId: %q) { id } }`, placeId)); !strings.Contains(refusalMessage, wanted) {
		t.Errorf("the refusal says when the pause ends, in the person's zone, %q: %s", wanted, refusalMessage)
	}
	world.call(t, world.ada, `mutation { ResumeLocation }`)
	world.call(t, world.ada, fmt.Sprintf(`mutation { CheckIn(placeId: %q) { id } }`, placeId))
}

// A report waits for the person's lock behind whatever is changing their
// location, and a pause that began in the meantime is in effect when it runs:
// a visit in progress that the report keeps open ends where that pause began.
// The pause is started 300 ms after the report is sent, while it waits; a
// report that had not yet been sent by then would see the pause too, so the
// test cannot fail for a slow start, only pass.
func TestAReportThatWaitedForTheLockSeesThePauseThatBeganMeanwhile(t *testing.T) {
	world := newLocationWorld(t)
	type reportOutcome struct {
		answerData     map[string]any
		answerFailures []string
	}
	outcome := make(chan reportOutcome, 1)
	var pauseStartedAt time.Time
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.LockLocation(world.ada.ID); err != nil {
			t.Fatalf("LockLocation: %s", err)
		}
		go func() {
			answerData, answerFailures := world.ask(t, world.ada, fmt.Sprintf(`mutation { ReportLocation(points: [], visits: [%s]) {
				newVisitCount pausedVisitCount pause { `+pauseFieldsForTest+` } } }`, visitInput("waiting", 40, -75, ago(10*time.Minute), "")))
			outcome <- reportOutcome{answerData, answerFailures}
		}()
		time.Sleep(300 * time.Millisecond)
		pauseStartedAt = time.Now()
		if _, _, err := tx.PauseLocation(world.ada.ID, nil, pauseStartedAt); err != nil {
			t.Fatalf("PauseLocation: %s", err)
		}
		// The lock is let go when this transaction commits, on return.
	})
	var answered reportOutcome
	select {
	case answered = <-outcome:
	case <-time.After(30 * time.Second):
		t.Fatal("the report did not finish")
	}
	report, _ := answered.answerData["ReportLocation"].(map[string]any)
	if len(answered.answerFailures) > 0 || report["pause"] == nil {
		t.Fatalf("the report did not see the pause: %v %v", answered.answerData, answered.answerFailures)
	}
	visits := world.visitsOf(t, world.ada)
	if len(visits) != 1 || visits[0]["departedAt"] == nil {
		t.Fatalf("the visit was left open across the pause: %v", visits)
	}
	if difference := pauseTimeOf(t, visits[0]["departedAt"]).Sub(pauseStartedAt); difference > time.Second || difference < -time.Second {
		t.Errorf("the visit ended %v from where the pause began: %v", difference, visits[0])
	}
}

// The pauses of a range are the person's own, and another person's at level
// history only, by the reach rules of the visits.
func TestPausesAreListedByTheReachRulesOfVisits(t *testing.T) {
	world := newLocationWorld(t)
	world.pastPause(t, world.ada, 3*time.Hour, 2*time.Hour)
	world.pastPause(t, world.ada, 10*time.Hour, 9*time.Hour)
	listing := func(userId string) string {
		return fmt.Sprintf(`query { ListLocationPauses(userId: %q, from: %q, until: %q) { `+pauseFieldsForTest+` } }`, userId, ago(24*time.Hour), ago(0))
	}

	own := world.call(t, world.ada, fmt.Sprintf(`query { ListLocationPauses(from: %q, until: %q) { `+pauseFieldsForTest+` } }`, ago(24*time.Hour), ago(0)))["ListLocationPauses"].([]any)
	if len(own) != 2 || pauseTimeOf(t, own[0].(map[string]any)["startedAt"]).Before(pauseTimeOf(t, own[1].(map[string]any)["startedAt"])) {
		t.Errorf("their own, newest first: %v", own)
	}
	narrow := world.call(t, world.ada, fmt.Sprintf(`query { ListLocationPauses(from: %q, until: %q) { id } }`, ago(4*time.Hour), ago(0)))["ListLocationPauses"].([]any)
	if len(narrow) != 1 {
		t.Errorf("only the pause that overlaps the range: %v", narrow)
	}

	world.refused(t, world.ben, listing(world.ada.ID))
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	world.refused(t, world.ben, listing(world.ada.ID))
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "history") { id } }`, world.ben.ID))
	if seen := world.call(t, world.ben, listing(world.ada.ID))["ListLocationPauses"].([]any); len(seen) != 2 {
		t.Errorf("at history they are shown: %v", seen)
	}
	world.refused(t, world.cora, listing(world.ada.ID))
	world.refused(t, world.ada, fmt.Sprintf(`query { ListLocationPauses(from: %q, until: %q) { id } }`, ago(0), ago(time.Hour)))
}

// Erasing history erases past pauses and leaves the one in effect, so it does
// not resume recording.
func TestErasingHistoryLeavesThePauseInEffect(t *testing.T) {
	world := newLocationWorld(t)
	world.pastPause(t, world.ada, 3*time.Hour, 2*time.Hour)
	world.call(t, world.ada, `mutation { PauseLocation { id } }`)
	world.call(t, world.ada, `mutation { EraseLocationHistory { pointCount } }`)
	pauses := world.call(t, world.ada, fmt.Sprintf(`query { ListLocationPauses(from: %q, until: %q) { endedAt } }`, ago(24*time.Hour), time.Now().Add(time.Hour).UTC().Format(time.RFC3339)))["ListLocationPauses"].([]any)
	if len(pauses) != 1 || pauses[0].(map[string]any)["endedAt"] != nil {
		t.Errorf("the past pause is erased and the one in effect stays: %v", pauses)
	}
}

// Pausing and resuming are news to the maps that follow the person, so that
// they read again and show the pause, and a report inside a pause is not news.
func TestPausingAndResumingAreNewsToTheMapsFollowingThePerson(t *testing.T) {
	world := newLocationWorld(t)
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
	hub := &world.graph.locationEvents
	benEvents, releaseBen, err := hub.subscribe(world.database, world.ben.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseBen()
	received(benEvents, "nobody", 300*time.Millisecond)

	world.call(t, world.ada, `mutation { PauseLocation { id } }`)
	if !received(benEvents, world.ada.ID, 5*time.Second) {
		t.Error("pausing is news")
	}
	world.report(t, world.ada, pointInput(40, -75, time.Now().Add(time.Minute).UTC().Format(time.RFC3339)), "")
	if received(benEvents, world.ada.ID, 300*time.Millisecond) {
		t.Error("a report inside the pause stores nothing and is no news")
	}
	world.call(t, world.ada, `mutation { ResumeLocation }`)
	if !received(benEvents, world.ada.ID, 5*time.Second) {
		t.Error("resuming is news")
	}
	world.call(t, world.ada, `mutation { ResumeLocation }`)
	if received(benEvents, world.ada.ID, 300*time.Millisecond) {
		t.Error("resuming when not paused is no news")
	}
}

// A position the browser gave the agent is not kept while the person has
// paused, like a phone's, and is kept after.
func TestAPositionTheBrowserGaveIsNotKeptWhilePaused(t *testing.T) {
	world := newLocationWorld(t)
	principal := &api.Principal{User: world.ada, Permissions: &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionLocationUse}}}
	answer := &agent.LocationAnswer{LatitudeDegrees: 40, LongitudeDegrees: -75, AccuracyMeters: 30}
	storedCount := func() int {
		return len(world.call(t, world.ada, fmt.Sprintf(`query { ListLocationPoints(from: %q, until: %q) { latitudeDegrees } }`, ago(time.Hour), time.Now().Add(time.Hour).UTC().Format(time.RFC3339)))["ListLocationPoints"].([]any))
	}

	world.call(t, world.ada, `mutation { PauseLocation { id } }`)
	answer.MeasuredAt = time.Now()
	world.graph.keepBrowserLocation(context.Background(), principal, answer)
	if storedCount() != 0 {
		t.Error("a position measured while paused is not kept, whoever sends it")
	}
	world.call(t, world.ada, `mutation { ResumeLocation }`)
	answer.MeasuredAt = time.Now()
	world.graph.keepBrowserLocation(context.Background(), principal, answer)
	if storedCount() != 1 {
		t.Error("after resuming it is kept")
	}
}
