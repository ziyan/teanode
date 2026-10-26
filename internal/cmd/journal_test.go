package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
)

// journalMoment is a time on 2026-10-22 on this machine, written the way the
// server writes one, so that the clock times the commands print do not
// depend on the zone the test runs in.
func journalMoment(hour, minute int) string {
	return time.Date(2026, 10, 22, hour, minute, 0, 0, time.Local).Format(time.RFC3339)
}

// journalServer answers the documents the journal commands send with a small
// world: the caller wrote on Thursday 2026-10-22 and had a story written for
// it, and records the variables of each document by operation name.
func journalServer(test *testing.T) (*httptest.Server, func(string) map[string]any, func(string) int) {
	test.Helper()
	return journalServerFor(test, "")
}

// journalServerFor is journalServer for a caller whose account reads
// measurements in the given measurement system. The shell's language is
// cleared first, so that what a test prints does not depend on the machine
// it runs on; a test that wants one sets it after.
func journalServerFor(test *testing.T, measurementSystem string) (*httptest.Server, func(string) map[string]any, func(string) int) {
	test.Helper()
	for _, variable := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		test.Setenv(variable, "")
	}
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	counts := map[string]int{}
	words := `{"original":"Rowed on the harbor before breakfast.","modifiedAt":"2026-10-22T20:15:30.123456Z","isTouchUpOn":true,"touchedUp":"I rowed on the harbor before breakfast.",
		"touchUpHow":"first person","isTouchedUpStale":false,"touchUpPending":false,"journalNotes":[{"documentId":"d-note","title":"Shopping","text":"Milk and bread."}]}`
	facts := `{"photoCount":12,"placeCount":3,"stepCount":8200,"sleepMinutes":435,"codingSessionCount":2,"mailReceivedCount":14,"transactionCount":2,"spentMinorUnits":4250,"spentCurrency":"NZD"}`
	day := `{"day":"2026-10-22","timeZone":"Pacific/Auckland","isToday":false,"weekday":"thursday","weekStart":"2026-10-19",
		"words":` + words + `,
		"mood":{"moodLevel":4,"moodWords":"a calm one","moodSource":"health"},
		"weather":{"weatherCode":61,"weatherKind":"rain","highCelsius":17.4,"lowCelsius":11.2,"precipitationMM":3.2,"placeName":"Harbor"},
		"facts":` + facts + `,
		"narrative":{"headline":"Harbor and coffee","narrative":"Ada rowed, then read [[mail:m1]] from [[contact:gone]].\n\nShe met [[contact:c1]] for lunch.",
			"references":[{"recordKind":"mail","recordId":"m1","reference":"mail:m1","title":"Lease renewal","isUnavailable":false},
			              {"recordKind":"contact","recordId":"gone","reference":"contact:gone","title":"","isUnavailable":true},
			              {"recordKind":"contact","recordId":"c1","reference":"contact:c1","title":"Sam Harbor","isUnavailable":false}],
			"writtenAt":"2026-10-23T03:00:00Z","isStale":true,"narrativePending":false,"isBeyondDepth":false,"failure":"",
			"corrections":[{"correctionText":"It was Sam at lunch.","createdAt":"2026-10-23T04:00:00Z"},{"correctionText":"Not a colleague.","createdAt":"2026-10-23T04:05:00Z"}],"sourceCount":9},
		"choices":[{"linkId":"l1","record":{"recordKind":"mail","recordId":"m1","reference":"mail:m1","title":"Lease renewal"},"linkSource":"agent","linkReason":"asked her to decide","isHidden":false},
		           {"linkId":"l2","record":{"recordKind":"mail","recordId":"m2","reference":"mail:m2","title":"Newsletter"},"linkSource":"agent","linkReason":"","isHidden":true}],
		"pins":[{"linkId":"l3","record":{"recordKind":"contact","recordId":"c1","reference":"contact:c1","title":"Sam Harbor"},"linkSource":"person","linkReason":"lunch","isHidden":false}],
		"coverPhotoId":"p1",
		"codingSessions":[{"documentId":"s1","title":"Fix the login page","directory":"~/code/site","assistant":"claude-code","startedAt":"` + journalMoment(9, 5) + `","endedAt":"` + journalMoment(10, 40) + `","linkReason":""}],
		"documents":[{"documentId":"w1","title":"Release plan","sourceName":"Work chat","happenedAt":"` + journalMoment(11, 0) + `","isChosen":true,"linkReason":"you were asked to decide"}],
		"documentTotal":38,
		"conversations":[{"conversationId":"cv1","title":"Packing list","firstAt":"` + journalMoment(7, 30) + `","lastAt":"` + journalMoment(7, 45) + `","messageCount":6}],
		"chats":[{"chatId":"ch1","chatName":"Home","firstAt":"` + journalMoment(18, 0) + `","lastAt":"` + journalMoment(18, 20) + `","messageCount":1}],
		"mail":[{"itemId":"m1","fromName":"Landlord","subject":"Lease renewal","receivedAt":"` + journalMoment(8, 15) + `","isSent":false,"isImportant":true,"isChosen":true,"linkReason":"asked her to decide"},
		        {"itemId":"m9","fromName":"Ada","subject":"Re: Lease renewal","receivedAt":"` + journalMoment(12, 5) + `","isSent":true,"isImportant":false,"isChosen":false,"linkReason":""}],
		"mailTotal":14,
		"recordings":[{"recordingId":"r1","title":"Stand-up","recordedAt":"` + journalMoment(9, 30) + `","durationMS":900000,"summary":""}],
		"photos":[{"photoId":"p1","takenAt":"` + journalMoment(8, 0) + `","placeName":"Harbor","isVideo":false},{"photoId":"p2","takenAt":"` + journalMoment(17, 45) + `","placeName":"Harbor","isVideo":false}],
		"photoTotal":12,
		"visits":[{"visitId":"v1","placeId":"pl1","placeName":"Harbor","arrivedAt":"` + journalMoment(6, 30) + `","departedAt":"` + journalMoment(8, 30) + `","latitude":-36.8,"longitude":174.8}],
		"events":[{"eventId":"e1","title":"Dentist","calendarName":"Home","startsAt":"` + journalMoment(14, 0) + `","endsAt":"` + journalMoment(15, 0) + `","isAllDay":false,"location":"Main Street","reference":"calendar_occurrence:e1@x"}],
		"reminders":[{"reminderId":"rm1","title":"Call back","dueAt":"` + journalMoment(16, 0) + `","doneAt":null}],
		"transactions":[{"transactionId":"t1","postedOn":"2026-10-22","description":"Coffee","accountName":"Everyday","amountMinorUnits":-450,"currency":"NZD"}],
		"health":{"stepCount":8200,"stepsByHour":[],"sleepMinutes":435,"sleepStages":[],"restingHeartRate":58,"workouts":[{"sampleId":"w9","activityType":"Rowing","startedAt":"` + journalMoment(6, 40) + `","minutes":45,"meters":7000}]},
		"shoppingTrips":[{"tripId":"st1","listNames":["Groceries"],"startedAt":"` + journalMoment(17, 0) + `","finishedAt":null,"boughtCount":5,"itemCount":6}],
		"notes":[],"memoryPages":[{"nodeId":"n1","path":"people/sam","title":"Sam Harbor","factCount":2}],
		"unavailableSections":["recordings"],
		"moments":[{"at":"` + journalMoment(6, 30) + `","recordKind":"place_visit","recordId":"v1","title":"Harbor","detail":"2 h","dashboardPath":"/location"},
		           {"at":"` + journalMoment(8, 15) + `","recordKind":"mail","recordId":"m1","title":"Lease renewal","detail":"","dashboardPath":"/mail"}]}`
	week := `{"periodKind":"week","periodStart":"2026-10-19","periodEnd":"2026-10-25","isCurrent":true,
		"lookingBack":{"original":"A good week.","modifiedAt":"2026-10-25T09:00:00.5Z","isTouchUpOn":false,"touchedUp":"","touchUpHow":"","isTouchedUpStale":false,"touchUpPending":false,"journalNotes":[]},
		"narrative":{"headline":"A harbor week","narrative":"Seven days by the water, with [[mail:m1]].","references":[{"recordKind":"mail","recordId":"m1","reference":"mail:m1","title":"Lease renewal"}],"writtenAt":"2026-10-25T03:00:00Z","corrections":[]},
		"choices":[],"days":[{"day":"2026-10-22","isToday":false,"mood":{"moodLevel":4,"moodWords":"","moodSource":"person"},"headline":"Harbor and coffee","wordsExcerpt":"","hasWords":true,"facts":{},"photoIds":[],"hasNarrative":true},
		           {"day":"2026-10-23","isToday":true,"mood":null,"headline":"","wordsExcerpt":"Slept in.","hasWords":true,"facts":{},"photoIds":[],"hasNarrative":false}],
		"months":[],"totals":` + facts + `,"moodCounts":[0,1,1,2,0],"dayCount":5,"chapters":[]}`
	year := `{"periodKind":"year","periodStart":"2026-01-01","periodEnd":"2026-12-31","isCurrent":true,
		"lookingBack":{"original":"","isTouchUpOn":false,"touchedUp":"","journalNotes":[]},"narrative":{"headline":"The year of the harbor","narrative":"","references":[],"corrections":[]},
		"choices":[],"days":[],"months":[{"periodStart":"2026-03-01","headline":"Settling in","dayMoods":[0,3,4],"placeCount":5,"photoCount":80}],
		"totals":{},"moodCounts":[0,0,0,0,0],"dayCount":0,"chapters":[{"fromDay":"2026-01-01","untilDay":"2026-03-31","title":"Settling in","detail":"A new city."}]}`
	answers := map[string]string{
		"ListJournalDays": `{"totalCount":20,"today":"2026-10-23","firstDay":"2026-09-01","days":[
			{"day":"2026-10-23","isToday":true,"mood":null,"weather":null,"headline":"","wordsExcerpt":"Started the morning with a run along the harbor and a long coffee afterwards, then a slow walk home through the market.","hasWords":true,"facts":{},"photoIds":[],"hasNarrative":false},
			{"day":"2026-10-22","isToday":false,"mood":{"moodLevel":4,"moodWords":"","moodSource":"person"},"weather":null,"headline":"Harbor and coffee","wordsExcerpt":"Rowed.","hasWords":true,"facts":{},"photoIds":[],"hasNarrative":true}]}`,
		"GetCurrentUser":    `{"id":"u1","username":"ada","measurementSystem":"` + measurementSystem + `"}`,
		"ReadJournalDay":    day,
		"ReadJournalPeriod": week,
		"SearchJournal": `{"totalCount":22,"hits":[{"day":"2026-10-22","periodKind":"","headline":"Harbor and coffee","snippet":"Rowed on the harbor before breakfast."},
			{"day":"2026-10-01","periodKind":"month","headline":"","snippet":"October was the harbor month."}]}`,
		"SetJournalWords":         `{"day":"2026-10-22","words":{"original":"x","isTouchUpOn":true,"touchedUp":"","touchUpHow":"first person","journalNotes":[]},"mood":null}`,
		"SetJournalMood":          `{"day":"2026-10-22","mood":{"moodLevel":4,"moodWords":"a calm one","moodSource":"person"}}`,
		"SetJournalLookingBack":   `{"periodKind":"week","periodStart":"2026-10-19","periodEnd":"2026-10-25","lookingBack":{"original":"x","journalNotes":[]}}`,
		"TouchUpJournalWords":     `{"day":"2026-10-22","periodKind":"","words":{"original":"x","isTouchUpOn":true,"touchedUp":"","journalNotes":[]}}`,
		"StopJournalTouchUp":      `{"day":"2026-10-22","periodKind":"","words":{"original":"x","isTouchUpOn":false,"touchedUp":"","journalNotes":[]}}`,
		"CorrectJournalNarrative": `{"headline":"","narrative":"","references":[],"narrativePending":true,"corrections":[{"correctionText":"a"},{"correctionText":"b"},{"correctionText":"c"}]}`,
		"RemoveJournalCorrection": `{"headline":"","narrative":"","references":[],"narrativePending":true,"corrections":[]}`,
		"WriteJournalNarrative":   `{"headline":"","narrative":"","references":[],"narrativePending":true,"corrections":[]}`,
		"DeleteJournalNarrative":  `{"headline":"","narrative":"","references":[],"corrections":[]}`,
		"PinJournalRecord":        `{"linkId":"l9","record":{"recordKind":"mail","recordId":"m1","reference":"mail:m1","title":"Lease renewal"},"linkSource":"person","linkReason":"the lease","isHidden":false}`,
		"RemoveJournalLink":       `true`,
		"ReadJournalSettings":     `{"isWeatherOn":true,"isEveningQuestionOn":true,"eveningQuestionTime":"21:00","weekStartsOn":"monday","narrativeDepthDays":92,"modelProviderName":"Example AI"}`,
		"UpdateJournalSettings":   `{"isWeatherOn":false,"isEveningQuestionOn":true,"eveningQuestionTime":"07:45","weekStartsOn":"monday","narrativeDepthDays":92,"modelProviderName":""}`,
		"EraseJournal":            `{"dayCount":40,"periodCount":9,"linkCount":120}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		data := map[string]json.RawMessage{}
		for operation, answer := range answers {
			if strings.Contains(document.Query, operation+"(") || strings.Contains(document.Query, operation+" ") || strings.Contains(document.Query, operation+"\n") {
				if operation == "ReadJournalPeriod" && document.Variables["periodKind"] == "year" {
					answer = year
				}
				data[operation] = json.RawMessage(answer)
				lock.Lock()
				calls[operation] = document.Variables
				counts[operation]++
				lock.Unlock()
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"data": data})
	}))
	test.Cleanup(server.Close)
	return server, func(operation string) map[string]any {
			lock.Lock()
			defer lock.Unlock()
			return calls[operation]
		}, func(operation string) int {
			lock.Lock()
			defer lock.Unlock()
			return counts[operation]
		}
}

func runJournalCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewJournalCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "journal"}, arguments...))
	})
	return output, err
}

func mustContain(test *testing.T, output string, wants ...string) {
	test.Helper()
	for _, want := range wants {
		if !strings.Contains(output, want) {
			test.Errorf("the output should hold %q:\n%s", want, output)
		}
	}
}

// With no subcommand the command lists, a page of 14 days at a time, the
// newest first with the mood and the headline or the start of the words.
func TestJournalListsDaysAndPages(test *testing.T) {
	server, called, _ := journalServer(test)
	for _, arguments := range [][]string{{}, {"list"}} {
		output, err := runJournalCommand(test, server, arguments...)
		if err != nil {
			test.Fatal(err)
		}
		mustContain(test, output, "2026-10-23 Fri (today)", "2026-10-22 Thu", "good", "Harbor and coffee", "Started the morning with a run along the harbor and a long coffee aft…")
		if variables := called("ListJournalDays"); variables["first"] != float64(14) || variables["offset"] != nil || variables["fromDay"] != nil {
			test.Errorf("the first page: %v", variables)
		}
	}
	if _, err := runJournalCommand(test, server, "list", "--page", "2", "--from", "2026-10-01", "--until", "yesterday"); err != nil {
		test.Fatal(err)
	}
	variables := called("ListJournalDays")
	if variables["offset"] != float64(14) || variables["fromDay"] != "2026-10-01" || variables["untilDay"] != time.Now().AddDate(0, 0, -1).Format(time.DateOnly) {
		test.Errorf("the second page of a range: %v", variables)
	}
	if _, err := runJournalCommand(test, server, "--page", "3"); err != nil || called("ListJournalDays")["offset"] != float64(28) {
		test.Errorf("flags before the subcommand: %v, %v", called("ListJournalDays"), err)
	}
	output, err := runJournalCommand(test, server, "list", "--json")
	if err != nil {
		test.Fatal(err)
	}
	var decoded struct {
		TotalCount int `json:"totalCount"`
		Days       []struct {
			Day string `json:"day"`
		} `json:"days"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil || decoded.TotalCount != 20 || len(decoded.Days) != 2 || decoded.Days[1].Day != "2026-10-22" {
		test.Errorf("the JSON: %v %s", err, output)
	}
	for _, arguments := range [][]string{{"list", "--page", "0"}, {"list", "--from", "2026-13-40"}, {"list", "--until", "soon"}} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}
}

// The weather is written in the units the account reads: as chosen, and for
// an account that chose nothing as the region of the shell's language says,
// metric when it names none.
func TestJournalDayWeatherFollowsTheMeasurementSystem(test *testing.T) {
	for _, scenario := range []struct {
		name              string
		measurementSystem string
		language          string
		wanted            string
	}{
		{"metric chosen beats a US language", "metric", "en_US.UTF-8", "rain, 11 to 17°C, "},
		{"us chosen beats a metric language", "us", "ja_JP.UTF-8", "rain, 52 to 63°F, "},
		{"automatic in a US language", "", "en_US.UTF-8", "rain, 52 to 63°F, "},
		{"automatic in a language with no region", "", "en", "rain, 11 to 17°C, "},
		{"automatic in another region", "", "en_GB.UTF-8", "rain, 11 to 17°C, "},
		{"automatic with nothing said", "", "", "rain, 11 to 17°C, "},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			server, _, _ := journalServerFor(test, scenario.measurementSystem)
			test.Setenv("LANG", scenario.language)
			output, err := runJournalCommand(test, server, "day", "2026-10-22")
			if err != nil {
				test.Fatal(err)
			}
			mustContain(test, output, scenario.wanted)
		})
	}
}

// A day prints its header, your words with the touched-up version first and
// the original after it, the story with each mention replaced by the record's
// title, the corrections numbered, what was chosen and pinned with the id to
// unlink, each source in a line or a few, and the moments in order.
func TestJournalDayPrintsEverything(test *testing.T) {
	server, called, _ := journalServer(test)
	output, err := runJournalCommand(test, server, "day", "2026-10-22")
	if err != nil {
		test.Fatal(err)
	}
	if called("ReadJournalDay")["day"] != "2026-10-22" {
		test.Errorf("the day asked for: %v", called("ReadJournalDay"))
	}
	mustContain(test, output,
		"2026-10-22 Thu\n",
		"mood", "good, a calm one (from Apple Health)",
		"weather", "rain, 11 to 17°C, 3 mm of rain at Harbor",
		"2 coding sessions", "12 photos", "8,200 steps", "7 h 15 min asleep", "42.50 NZD spent",
		"Words (touched up: first person)\nI rowed on the harbor before breakfast.\n",
		"Original\nRowed on the harbor before breakfast.\n",
		"From your notes: Shopping\nMilk and bread.",
		"Harbor and coffee\n\nAda rowed, then read Lease renewal from a record you can no longer see.\n\nShe met Sam Harbor for lunch.\n",
		"(written by TeaNode on 2026-10-2", "something changed since",
		"Your corrections\n  1. It was Sam at lunch.\n  2. Not a colleague.\n",
		"TeaNode chose\n  mail:m1: Lease renewal (asked her to decide) (link l1)\n  mail:m2: Newsletter [hidden] (link l2)\n",
		"You pinned\n  contact:c1: Sam Harbor (lunch) (link l3)\n",
		"Work\n  09:05  Fix the login page (~/code/site)\n",
		"Documents\n  11:00  Release plan (Work chat): you were asked to decide\n  and 37 more\n",
		"Conversations\n  07:30  Packing list, 6 messages\n  18:00  Home, 1 message\n",
		"Mail\n  08:15  Landlord: Lease renewal (asked her to decide)\n  12:05  sent: Re: Lease renewal\n  and 12 more\n",
		"Recordings\n  09:30  Stand-up, 15 min\n",
		"Photos\n  12 photos, from 08:00 to 17:45\n",
		"Places\n  06:30 to 08:30  Harbor\n",
		"Calendar\n  14:00 to 15:00  Dentist, Main Street\n",
		"Reminders\n  Call back, due at 16:00\n",
		"Money\n  -4.50 NZD  Coffee, Everyday\n",
		"Health\n  8,200 steps\n  slept 7 h 15 min\n  resting heart rate 58 bpm\n  06:40  Rowing, 45 min\n",
		"Shopping\n  17:00  Groceries, 5 of 6 bought\n",
		"Memory\n  Sam Harbor, 2 facts\n",
		"Through the day\n  06:30  Harbor, 2 h\n  08:15  Lease renewal\n",
		"Not shown (no permission): recordings",
	)
	// The sections come in the order of the day's page: the words before the story.
	if strings.Index(output, "Words (") > strings.Index(output, "Harbor and coffee\n\nAda") || strings.Index(output, "TeaNode chose") > strings.Index(output, "Work\n") {
		test.Errorf("the order of the parts:\n%s", output)
	}
	if strings.Contains(output, "[[") {
		test.Errorf("a mention was left in brackets:\n%s", output)
	}

	if _, err := runJournalCommand(test, server, "day"); err != nil || called("ReadJournalDay")["day"] != time.Now().Format(time.DateOnly) {
		test.Errorf("a day with none named is today: %v, %v", called("ReadJournalDay"), err)
	}
	if _, err := runJournalCommand(test, server, "day", "yesterday"); err != nil || called("ReadJournalDay")["day"] != time.Now().AddDate(0, 0, -1).Format(time.DateOnly) {
		test.Errorf("yesterday: %v, %v", called("ReadJournalDay"), err)
	}
	for _, arguments := range [][]string{{"day", "2026-02-30"}, {"day", "2026-10-22", "extra"}} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}
	jsonOutput, err := runJournalCommand(test, server, "day", "2026-10-22", "--json")
	if err != nil {
		test.Fatal(err)
	}
	var decoded struct {
		Weekday string `json:"weekday"`
		Words   struct {
			TouchedUp string `json:"touchedUp"`
		} `json:"words"`
		Moments []struct {
			RecordKind string `json:"recordKind"`
		} `json:"moments"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &decoded); err != nil || decoded.Weekday != "thursday" || decoded.Words.TouchedUp == "" || len(decoded.Moments) != 2 {
		test.Errorf("the JSON: %v %s", err, jsonOutput)
	}
}

// A week, a month and a year ask for the period that holds the day and print
// its story with the mention replaced, your looking back, the moods, the
// totals and its days; a year prints its months and chapters.
func TestJournalPeriodsPrintTheirStoryAndDays(test *testing.T) {
	server, called, _ := journalServer(test)
	output, err := runJournalCommand(test, server, "week", "2026-10-21")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("ReadJournalPeriod"); variables["periodKind"] != "week" || variables["day"] != "2026-10-21" {
		test.Errorf("the week asked for: %v", variables)
	}
	mustContain(test, output,
		"Week 2026-10-19 to 2026-10-25 (this week)", "1 low, 1 okay, 2 good", "5 with something in them", "14 mails received",
		"A harbor week\n\nSeven days by the water, with Lease renewal.", "Looking back\nA good week.",
		"2026-10-22 Thu", "Harbor and coffee", "2026-10-23 Fri (today)", "Slept in.")
	output, err = runJournalCommand(test, server, "year")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("ReadJournalPeriod"); variables["periodKind"] != "year" || variables["day"] != time.Now().Format(time.DateOnly) {
		test.Errorf("the year asked for: %v", variables)
	}
	mustContain(test, output, "Year 2026-01-01 to 2026-12-31", "The year of the harbor", "Chapters\n  2026-01-01 to 2026-03-31  Settling in: A new city.", "2026-03", "80 photos", "5 places")
	if _, err := runJournalCommand(test, server, "month", "2026-10-21", "2026-10-22"); err == nil {
		test.Error("two days were accepted")
	}
}

// A search joins its words, names a period by its kind, and pages.
func TestJournalSearch(test *testing.T) {
	server, called, _ := journalServer(test)
	output, err := runJournalCommand(test, server, "search", "harbor", "month", "--page", "2")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("SearchJournal"); variables["query"] != "harbor month" || variables["first"] != float64(20) || variables["offset"] != float64(20) {
		test.Errorf("the search: %v", variables)
	}
	mustContain(test, output, "2026-10-22", "Harbor and coffee", "Rowed on the harbor before breakfast.", "month of 2026-10-01", "October was the harbor month.")
	if _, err := runJournalCommand(test, server, "search"); err == nil {
		test.Error("an empty search was accepted")
	}
}

// Writing reads the day first and sends the modification time it read, so a
// save made elsewhere in between is refused; appending puts a blank line
// between the old words and the new; the text comes from --text, --file or
// standard input, and exactly one of them.
func TestJournalWriteReadsTheDayFirstAndSendsWhatItRead(test *testing.T) {
	server, called, count := journalServer(test)
	if _, err := runJournalCommand(test, server, "write", "2026-10-22", "--text", "A new paragraph.\n"); err != nil {
		test.Fatal(err)
	}
	if count("ReadJournalDay") != 1 {
		test.Errorf("the day was read %d times", count("ReadJournalDay"))
	}
	variables := called("SetJournalWords")
	if variables["day"] != "2026-10-22" || variables["words"] != "A new paragraph." || variables["baseModifiedAt"] != "2026-10-22T20:15:30.123456Z" {
		test.Errorf("a replacement: %v", variables)
	}

	output, err := runJournalCommand(test, server, "write", "yesterday", "--append", "--text", "And later, rain.")
	if err != nil {
		test.Fatal(err)
	}
	variables = called("SetJournalWords")
	if variables["words"] != "Rowed on the harbor before breakfast.\n\nAnd later, rain." || variables["baseModifiedAt"] != "2026-10-22T20:15:30.123456Z" {
		test.Errorf("an append: %v", variables)
	}
	mustContain(test, output, "saved the words of", "the touched-up version is being made again")

	file := filepath.Join(test.TempDir(), "words.txt")
	if err := os.WriteFile(file, []byte("From a file.\r\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	if _, err := runJournalCommand(test, server, "write", "--file", file); err != nil || called("SetJournalWords")["words"] != "From a file." || called("SetJournalWords")["day"] != time.Now().Format(time.DateOnly) {
		test.Errorf("a file: %v, %v", called("SetJournalWords"), err)
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		test.Fatal(err)
	}
	_, _ = writer.WriteString("From standard input.\n")
	_ = writer.Close()
	original := os.Stdin
	os.Stdin = reader
	_, err = runJournalCommand(test, server, "write", "2026-10-21", "-")
	os.Stdin = original
	if err != nil || called("SetJournalWords")["words"] != "From standard input." || called("SetJournalWords")["day"] != "2026-10-21" {
		test.Errorf("standard input: %v, %v", called("SetJournalWords"), err)
	}

	before := count("SetJournalWords")
	for name, arguments := range map[string][]string{
		"nothing to write":  {"write", "2026-10-22"},
		"two sources":       {"write", "--text", "a", "--file", file},
		"empty text":        {"write", "--text", "  "},
		"two days":          {"write", "2026-10-22", "2026-10-21", "--text", "a"},
		"words as a day":    {"write", "later", "--text", "a"},
		"a day that is not": {"write", "2026-10-32", "--text", "a"},
	} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
	if count("SetJournalWords") != before {
		test.Errorf("a refused write reached the server")
	}
}

// A mood is a word, none takes it away, anything else is refused before the
// server is asked.
func TestJournalMood(test *testing.T) {
	server, called, count := journalServer(test)
	output, err := runJournalCommand(test, server, "mood", "2026-10-22", "Good", "--words", " a calm one ")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("SetJournalMood"); variables["day"] != "2026-10-22" || variables["moodLevel"] != float64(4) || variables["moodWords"] != "a calm one" {
		test.Errorf("a mood: %v", variables)
	}
	mustContain(test, output, "2026-10-22: good, a calm one")
	for word, level := range map[string]float64{"awful": 1, "low": 2, "okay": 3, "great": 5} {
		if _, err := runJournalCommand(test, server, "mood", word); err != nil || called("SetJournalMood")["moodLevel"] != level || called("SetJournalMood")["day"] != time.Now().Format(time.DateOnly) {
			test.Errorf("%s: %v, %v", word, called("SetJournalMood"), err)
		}
	}
	if _, err := runJournalCommand(test, server, "mood", "yesterday", "none"); err != nil {
		test.Fatal(err)
	}
	if variables := called("SetJournalMood"); variables["moodLevel"] != nil || variables["day"] != time.Now().AddDate(0, 0, -1).Format(time.DateOnly) {
		test.Errorf("no mood: %v", variables)
	}
	before := count("SetJournalMood")
	for _, arguments := range [][]string{{"mood"}, {"mood", "fine"}, {"mood", "2026-10-22"}, {"mood", "2026-10-22", "good", "great"}} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}
	if count("SetJournalMood") != before {
		test.Errorf("a refused mood reached the server")
	}
}

// Looking back names its period first, reads the period for the time of the
// last save, and sends the text with it.
func TestJournalLookBack(test *testing.T) {
	server, called, _ := journalServer(test)
	output, err := runJournalCommand(test, server, "look-back", "week", "2026-10-21", "--text", "A good week, all told.")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("SetJournalLookingBack")
	if variables["periodKind"] != "week" || variables["day"] != "2026-10-21" || variables["lookingBack"] != "A good week, all told." || variables["baseModifiedAt"] != "2026-10-25T09:00:00.5Z" {
		test.Errorf("looking back: %v", variables)
	}
	mustContain(test, output, "2026-10-19 to 2026-10-25")
	for _, arguments := range [][]string{{"look-back"}, {"look-back", "fortnight", "--text", "a"}, {"look-back", "week"}, {"look-back", "2026-10-21", "--text", "a"}} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}
}

// Touching up sends the instruction only when one was given, names the period
// when asked, and stops with --off; the two do not go together.
func TestJournalTouchUp(test *testing.T) {
	server, called, count := journalServer(test)
	output, err := runJournalCommand(test, server, "touch-up", "2026-10-22", "--how", "  shorter ")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("TouchUpJournalWords"); variables["day"] != "2026-10-22" || variables["how"] != "shorter" || variables["periodKind"] != nil {
		test.Errorf("touching up: %v", variables)
	}
	mustContain(test, output, "touching up 2026-10-22", "your original stays")
	if _, err := runJournalCommand(test, server, "touch-up", "2026-10-22", "--period", "month"); err != nil {
		test.Fatal(err)
	}
	if variables := called("TouchUpJournalWords"); variables["how"] != nil || variables["periodKind"] != "month" {
		test.Errorf("touching up a month with the last instruction: %v", variables)
	}
	output, err = runJournalCommand(test, server, "touch-up", "--off")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("StopJournalTouchUp"); variables["day"] != time.Now().Format(time.DateOnly) {
		test.Errorf("stopping: %v", variables)
	}
	mustContain(test, output, "stopped touching up", "your original shows again")
	before := count("StopJournalTouchUp") + count("TouchUpJournalWords")
	for _, arguments := range [][]string{{"touch-up", "--off", "--how", "x"}, {"touch-up", "--period", "decade"}, {"touch-up", "2026-10-22", "extra"}} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}
	if count("StopJournalTouchUp")+count("TouchUpJournalWords") != before {
		test.Errorf("a refused touch-up reached the server")
	}
}

// A correction takes the words after an optional day; the first argument is a
// day only when words follow it. Corrections are numbered from 1 for the
// person and from 0 for the server.
func TestJournalCorrectionsAreNumberedFromOne(test *testing.T) {
	server, called, _ := journalServer(test)
	today := time.Now().Format(time.DateOnly)
	output, err := runJournalCommand(test, server, "correct", "2026-10-21", "It", "was", "Sam", "at", "lunch.")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("CorrectJournalNarrative"); variables["day"] != "2026-10-21" || variables["correctionText"] != "It was Sam at lunch." || variables["periodKind"] != nil {
		test.Errorf("a correction of a day: %v", variables)
	}
	mustContain(test, output, "noted (3 corrections kept for 2026-10-21)")
	if _, err := runJournalCommand(test, server, "correct", "Sam was there."); err != nil || called("CorrectJournalNarrative")["day"] != today || called("CorrectJournalNarrative")["correctionText"] != "Sam was there." {
		test.Errorf("a correction of today: %v, %v", called("CorrectJournalNarrative"), err)
	}
	if _, err := runJournalCommand(test, server, "correct", "--period", "week", "yesterday", "Wrong week."); err != nil || called("CorrectJournalNarrative")["periodKind"] != "week" ||
		called("CorrectJournalNarrative")["day"] != time.Now().AddDate(0, 0, -1).Format(time.DateOnly) {
		test.Errorf("a correction of a week: %v, %v", called("CorrectJournalNarrative"), err)
	}
	// A day alone is words that name nothing, and a date that is not one is refused.
	for _, arguments := range [][]string{{"correct"}, {"correct", "2026-13-01", "words"}} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}

	output, err = runJournalCommand(test, server, "uncorrect", "2026-10-22", "2")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("RemoveJournalCorrection"); variables["day"] != "2026-10-22" || variables["correctionIndex"] != float64(1) {
		test.Errorf("taking back the second: %v", variables)
	}
	mustContain(test, output, "took back correction 2 of 2026-10-22")
	if _, err := runJournalCommand(test, server, "uncorrect", "1", "--period", "month"); err != nil || called("RemoveJournalCorrection")["correctionIndex"] != float64(0) || called("RemoveJournalCorrection")["periodKind"] != "month" {
		test.Errorf("taking back the first of the month: %v, %v", called("RemoveJournalCorrection"), err)
	}
	for _, arguments := range [][]string{{"uncorrect"}, {"uncorrect", "0"}, {"uncorrect", "first"}, {"uncorrect", "-1"}} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}
}

// Rewriting and forgetting a narrative name the day or the period, pinning
// takes a day and kind:id and unlinking a link id; settings read with no
// flag, change with flags, and refuse what is not a switch or a time; erasing
// asks first, and --yes is the answer.
func TestJournalRewriteForgetPinUnlinkSettingsAndErase(test *testing.T) {
	server, called, count := journalServer(test)
	today := time.Now().Format(time.DateOnly)
	if output, err := runJournalCommand(test, server, "rewrite", "2026-10-20"); err != nil || called("WriteJournalNarrative")["day"] != "2026-10-20" {
		test.Errorf("rewrite: %v, %v", called("WriteJournalNarrative"), err)
	} else {
		mustContain(test, output, "the story of 2026-10-20 is being written")
	}
	if _, err := runJournalCommand(test, server, "rewrite", "--period", "year"); err != nil || called("WriteJournalNarrative")["periodKind"] != "year" || called("WriteJournalNarrative")["day"] != today {
		test.Errorf("rewrite a year: %v, %v", called("WriteJournalNarrative"), err)
	}
	if output, err := runJournalCommand(test, server, "forget-narrative", "2026-10-20"); err != nil || called("DeleteJournalNarrative")["day"] != "2026-10-20" {
		test.Errorf("forget: %v, %v", called("DeleteJournalNarrative"), err)
	} else {
		mustContain(test, output, "deleted the story of 2026-10-20", "corrections are kept")
	}

	output, err := runJournalCommand(test, server, "pin", "2026-10-20", "mail:m1", "--reason", "the lease")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("PinJournalRecord"); variables["day"] != "2026-10-20" || variables["recordKind"] != "mail" || variables["recordId"] != "m1" || variables["linkReason"] != "the lease" {
		test.Errorf("pin: %v", variables)
	}
	mustContain(test, output, "pinned Lease renewal to 2026-10-20 (link l9)")
	if _, err := runJournalCommand(test, server, "pin", "yesterday", "calendar_occurrence:e1@2026-10-21T09:00:00Z"); err != nil || called("PinJournalRecord")["recordId"] != "e1@2026-10-21T09:00:00Z" || called("PinJournalRecord")["linkReason"] != nil {
		test.Errorf("pin an occurrence: %v, %v", called("PinJournalRecord"), err)
	}
	for _, arguments := range [][]string{{"pin"}, {"pin", "2026-10-20"}, {"pin", "mail:m1"}, {"pin", "2026-10-20", "mail"}, {"pin", "2026-10-20", ":m1"}, {"pin", "later", "mail:m1"}} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}
	if output, err := runJournalCommand(test, server, "unlink", "l1"); err != nil || called("RemoveJournalLink")["linkId"] != "l1" {
		test.Errorf("unlink: %v, %v", called("RemoveJournalLink"), err)
	} else {
		mustContain(test, output, "removed the link l1")
	}
	if _, err := runJournalCommand(test, server, "unlink"); err == nil {
		test.Error("unlink with no link")
	}

	output, err = runJournalCommand(test, server, "settings")
	if err != nil {
		test.Fatal(err)
	}
	mustContain(test, output, "weather", "on", "on, at 21:00", "monday", "92 days", "run by Example AI")
	if count("UpdateJournalSettings") != 0 {
		test.Error("reading the settings changed them")
	}
	if _, err := runJournalCommand(test, server, "settings", "--weather", "off", "--evening-question-time", "07:45"); err != nil {
		test.Fatal(err)
	}
	if variables := called("UpdateJournalSettings"); variables["isWeatherOn"] != false || variables["eveningQuestionTime"] != "07:45" || variables["isEveningQuestionOn"] != nil {
		test.Errorf("settings: %v", variables)
	}
	for _, arguments := range [][]string{{"settings", "--weather", "maybe"}, {"settings", "--evening-question-time", "9pm"}, {"settings", "--evening-question-time", "7:45"}} {
		if _, err := runJournalCommand(test, server, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}

	// Standard input is not a terminal here, so the question cannot be asked.
	if _, err := runJournalCommand(test, server, "erase"); err == nil || count("EraseJournal") != 0 {
		test.Errorf("erasing without a yes: %v, %d calls", err, count("EraseJournal"))
	}
	output, err = runJournalCommand(test, server, "erase", "--yes")
	if err != nil || count("EraseJournal") != 1 {
		test.Fatalf("erase --yes: %v", err)
	}
	mustContain(test, output, "erased 40 days, 9 periods and 120 links")
	if _, err := runJournalCommand(test, server, "erase", "--force"); err != nil || count("EraseJournal") != 2 {
		test.Errorf("erase --force: %v", err)
	}
}

// An excerpt is cut at its width, and money is written in whole units and cents.
func TestJournalExcerptAndMoneyWords(test *testing.T) {
	if got := journalExcerpt("  A   spaced\n out text that goes on and on ", 12); got != "A spaced ou…" {
		test.Errorf("an excerpt: %q", got)
	}
	if got := journalExcerpt("short", 12); got != "short" {
		test.Errorf("a short excerpt: %q", got)
	}
	if got := journalMoneyWords(-1205, "USD"); got != "-12.05 USD" {
		test.Errorf("money: %q", got)
	}
	if got := journalMoneyWords(7, ""); got != "0.07" {
		test.Errorf("a small amount: %q", got)
	}
}
