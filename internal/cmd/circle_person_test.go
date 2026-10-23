package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"
)

// personCardServer answers ListCirclePeople and ReadPersonCard with a card
// that shows each kind of part: location shown, health not shared, photos
// needing a contact, mail with more, recordings failed, memory shown.
func personCardServer(test *testing.T) (*httptest.Server, func() map[string]any) {
	test.Helper()
	return personCardServerLocatedBy(test, `{"userId":"user-sam","shareLevel":"current",
				"latestPoint":{"latitudeDegrees":40.0005,"longitudeDegrees":-75.0003,"accuracyMeters":10,"measuredAt":"2030-03-01T09:30:00Z","locationSource":"phone"},"openVisit":null,"pause":null}`)
}

// personCardServerLocatedBy is personCardServer with the location of the card
// given as the JSON of a location.
func personCardServerLocatedBy(test *testing.T, locationJSON string) (*httptest.Server, func() map[string]any) {
	test.Helper()
	var lock sync.Mutex
	var variables map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		if strings.Contains(document.Query, "ListCirclePeople") {
			_, _ = response.Write([]byte(`{"data":{"ListCirclePeople":[{"userId":"user-sam","username":"sam","name":"Sam Example"}]}}`))
			return
		}
		lock.Lock()
		variables = document.Variables
		lock.Unlock()
		_, _ = response.Write([]byte(`{"data":{"ReadPersonCard":{
			"person":{"userId":"user-sam","username":"sam","name":"Sam Example","relationshipWord":"brother"},
			"contact":{"id":"contact-1","name":"Sam Example","organization":"","emails":["sam@example.org"],"phones":[],"addresses":[],"note":""},
			"sharedCircles":[{"circleId":"circle-1","circleName":"Home","circleKind":"family"}],
			"canMessage":true,
			"location":{"sectionState":"shown","hasMoreTodayVisits":false,"location":` + locationJSON + `,"todayVisits":[]},
			"health":{"sectionState":"notShared","hasMore":false,"healthTypes":[]},
			"finance":{"sectionState":"shown","hasMore":true,"financeAccounts":[
				{"financeAccountId":"account-1","ownerUserId":"user-sam","ownerUsername":"sam","ownerName":"Sam Example","financeShareLevel":"balance","accountName":"Store Card","accountMask":"5566","accountKind":"credit","currencyCode":"USD","currentBalance":"-100.0000","creditUsage":{"owedAmount":"100.0000","creditLimitAmount":"400.0000","creditLimitSource":"provider","usageShare":0.25}}]},
			"photos":{"photosState":"needsContact","hasMorePhotos":false,"albumsState":"unavailable","hasMoreAlbums":false,"photos":[],"albums":[]},
			"mail":{"sectionState":"shown","hasMore":true,"mails":[
				{"mailboxItemId":"item-1","mailboxId":"mailbox-1","folderPath":"Inbox","subject":"Harbor schedule","fromAddress":"sam@example.org","fromName":"Sam","isFromPerson":true,"isUnread":true,"receivedAt":"2030-03-01T09:30:00Z","snippet":""}]},
			"recordings":{"sectionState":"failed","hasMore":false,"recordings":[]},
			"memory":{"sectionState":"shown","hasMore":false,"pages":[{"nodePath":"people/sam","nodeName":"Sam","nodeKind":"person","summary":"Plays the cello","updatedAt":"2030-03-01T09:30:00Z"}]}}}}`))
	}))
	test.Cleanup(server.Close)
	return server, func() map[string]any {
		lock.Lock()
		defer lock.Unlock()
		return variables
	}
}

func runCirclePersonAgainst(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewCircleCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "circle", "person"}, arguments...))
	})
	return output, err
}

// A card is asked for by username, which is turned into the account id from
// the people the account shares a circle with, and printed part by part: a part
// that is shown with its items, one that was not shared or needs a contact with
// the reason, one the account may not use left out, and a part that failed said
// so.
func TestCirclePersonPrintsEachPartOfTheCard(test *testing.T) {
	server, recorded := personCardServer(test)
	output, err := runCirclePersonAgainst(test, server, "SAM", "--limit", "5")
	if err != nil {
		test.Fatal(err)
	}
	variables := recorded()
	if variables["userId"] != "user-sam" || variables["itemLimit"] != float64(5) || len(variables) != 2 {
		test.Errorf("the server was asked with %v", variables)
	}
	for _, wanted := range []string{
		"Sam Example (sam), your brother", "Home (family)", "can message", "sam@example.org",
		"location\n", "40.00050, -75.00030", "current",
		"health: not shared with you",
		"finance\n", "Store Card ••5566", "-100.00 USD", "25%", "and more accounts",
		"photos of them: no contact of yours is linked to them",
		"mail with them\n", "Harbor schedule", "from them", "(unread)", "and more mail",
		"recordings they speak in: could not be read just now",
		"memory\n", "people/sam", "Plays the cello",
	} {
		if !strings.Contains(output, wanted) {
			test.Errorf("the output has no %q:\n%s", wanted, output)
		}
	}
	if strings.Contains(output, "albums they shared") {
		test.Errorf("a part the account may not use was printed:\n%s", output)
	}
}

// A person who paused recording has no position on the card: it says the
// location is paused, until when if that is known.
func TestCirclePersonSaysThePersonPausedRecording(test *testing.T) {
	for _, each := range []struct {
		name, pause, wanted string
	}{
		{"open", `{"id":"pause-1","startedAt":"2030-03-01T09:00:00Z","endedAt":null}`, "Location paused until you resume"},
		{"timed", `{"id":"pause-1","startedAt":"2030-03-01T09:00:00Z","endedAt":"2030-03-01T10:00:00Z"}`, "Location paused until 2030-03-01"},
	} {
		test.Run(each.name, func(test *testing.T) {
			server, _ := personCardServerLocatedBy(test, `{"userId":"user-sam","shareLevel":"current","latestPoint":null,"openVisit":null,"pause":`+each.pause+`}`)
			output, err := runCirclePersonAgainst(test, server, "sam")
			if err != nil {
				test.Fatal(err)
			}
			if !strings.Contains(output, each.wanted) || strings.Contains(output, "position") {
				test.Errorf("the card does not say they paused:\n%s", output)
			}
		})
	}
}

// A contact is named by its address book and itself, and the account is not
// looked up.
func TestCirclePersonByContact(test *testing.T) {
	server, recorded := personCardServer(test)
	if _, err := runCirclePersonAgainst(test, server, "--contact", "book-1/contact-1"); err != nil {
		test.Fatal(err)
	}
	variables := recorded()
	if variables["addressBookId"] != "book-1" || variables["contactId"] != "contact-1" || variables["userId"] != nil {
		test.Errorf("the server was asked with %v", variables)
	}
}

// With --json it prints the card as the server answered it and nothing else.
func TestCirclePersonWithJSONPrintsTheCard(test *testing.T) {
	server, _ := personCardServer(test)
	output, err := runCirclePersonAgainst(test, server, "user-sam", "--json")
	if err != nil {
		test.Fatal(err)
	}
	var card struct {
		CanMessage bool `json:"canMessage"`
		Person     struct {
			Username string `json:"username"`
		} `json:"person"`
		Health struct {
			SectionState string `json:"sectionState"`
		} `json:"health"`
	}
	if err := json.Unmarshal([]byte(output), &card); err != nil || card.Person.Username != "sam" || !card.CanMessage || card.Health.SectionState != "notShared" {
		test.Errorf("it printed %q: %v", output, err)
	}
}

// Nobody and everybody at once, a contact that is not two ids, and a
// username nobody has are refused before a card is asked for.
func TestCirclePersonRefusesWhatItCannotAskFor(test *testing.T) {
	server, recorded := personCardServer(test)
	for name, arguments := range map[string][]string{
		"no one":            {},
		"two at once":       {"sam", "--contact", "book-1/contact-1"},
		"half a contact":    {"--contact", "contact-1"},
		"a stranger's name": {"stranger"},
	} {
		if _, err := runCirclePersonAgainst(test, server, arguments...); err == nil {
			test.Errorf("%s was not refused", name)
		}
	}
	if recorded() != nil {
		test.Error("a card was asked for")
	}
}
