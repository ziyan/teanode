package apigraph

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// searchWorld is a photo world with a person who may use everything a search
// across everything reaches, and something of theirs in each of several areas
// that all hold the same word.
type searchWorld struct {
	*photoWorld
}

func (self *searchWorld) search(username, words string, perAreaLimit int, areas ...string) (*client.SearchResults, string) {
	self.test.Helper()
	variables := map[string]any{"words": words}
	if perAreaLimit > 0 {
		variables["perAreaLimit"] = perAreaLimit
	}
	if len(areas) > 0 {
		variables["areas"] = areas
	}
	body, _ := json.Marshal(map[string]any{"query": client.DocumentSearchEverything, "variables": variables})
	response := self.do(http.MethodPost, api.PathGraphQL, func(request *http.Request) {
		if username != "" {
			asSession(username)(request)
		}
		request.Header.Set("Content-Type", "application/json")
	}, body)
	text := readBody(self.test, response)
	var decoded struct {
		Data struct {
			SearchEverything *client.SearchResults `json:"SearchEverything"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		self.test.Fatalf("%s: %s", err, text)
	}
	return decoded.Data.SearchEverything, text
}

func searchAreaNames(results *client.SearchResults) []string {
	names := []string{}
	for _, area := range results.Areas {
		names = append(names, area.SearchArea)
	}
	return names
}

func (self *searchWorld) areaOf(results *client.SearchResults, name string) *client.SearchArea {
	for _, area := range results.Areas {
		if area.SearchArea == name {
			return area
		}
	}
	return nil
}

// fill gives a person one thing in each area the word reaches: a conversation
// with their agent, a contact, three photos, a memory page, a recording and a
// place.
func (self *searchWorld) fill(userId, word string) (photoIds []string) {
	self.test.Helper()
	agent := self.giveAgent(userId, true)
	for index := range 3 {
		photo := self.storePhoto(userId, fmt.Sprintf("%s-%d", word, index), fmt.Sprintf("%s-%d.jpg", word, index), []byte(fmt.Sprintf("%s original %d", word, index)), nil)
		photoIds = append(photoIds, photo.ID)
	}
	dbtest.RunTransactionOn(self.test, self.database, func(tx db.Transaction) {
		check := func(err error) {
			self.test.Helper()
			if err != nil {
				self.test.Fatal(err)
			}
		}
		// A photo is found by its search document, which is made when it is
		// described (or its place or camera is found), not when it is stored.
		for _, photoId := range photoIds {
			check(tx.SetPhotoDescription(userId, photoId, &db.PhotoDescription{PhotoDescription: "A " + word + " in a tank.", ModelName: "p:thinker"}))
		}
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, Title: "The " + word + " plan", LastAt: time.Now()})
		check(err)
		_, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: "nothing else"})
		check(err)

		book, err := tx.CreateAddressBook(&models.AddressBook{UserID: userId, Name: "Contacts"})
		check(err)
		_, err = tx.PutContact(&models.Contact{
			AddressBookID: book.ID, UID: "uid-" + word, ETag: "etag", Name: word + " Supply",
			Card: "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:" + word + " Supply\r\nEND:VCARD\r\n",
		})
		check(err)

		_, err = tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "projects/" + word, Kind: models.NodeProject, Name: word, Summary: "A page about the " + word + "."})
		check(err)

		_, err = tx.CreateRecording(&models.Recording{UserID: userId, ClientRecordingID: "recording-" + word, RecordingTitle: "The " + word + " talk", RecordingOrigin: models.RecordingOriginCapture})
		check(err)

		_, err = tx.CreatePlace(userId, &models.Place{PlaceName: word + " Pier", IsNamedByPerson: true, LatitudeDegrees: 10, LongitudeDegrees: 20})
		check(err)
	})
	return photoIds
}

func newSearchWorld(test *testing.T) *searchWorld {
	test.Helper()
	world := &searchWorld{photoWorld: newPhotoWorld(test)}
	world.withSchema()
	return world
}

var searchEverything = []models.Permission{
	models.PermissionPhotosUse, models.PermissionContactsUse, models.PermissionAgentUse,
	models.PermissionLocationUse, models.PermissionMailRead,
}

// A person who may use everything gets a group for each area that has the
// word, in the fixed order, each result carrying what opens it.
func TestSearchFindsTheWordInEveryAreaInTheFixedOrder(test *testing.T) {
	world := newSearchWorld(test)
	userId := world.person("search-everything", searchEverything...)
	photoIds := world.fill(userId, "lanternfish")

	results, text := world.search("search-everything", " lanternfish ", 0)
	if results == nil {
		test.Fatalf("no results: %s", text)
	}
	if want := []string{"chats", "contacts", "photos", "memory", "recordings", "places"}; !slices.Equal(searchAreaNames(results), want) {
		test.Fatalf("areas = %v, want %v\n%s", searchAreaNames(results), want, text)
	}
	if results.Words != "lanternfish" || len(results.FailedAreas) != 0 {
		test.Errorf("words %q, failed areas %v", results.Words, results.FailedAreas)
	}
	titles := map[string]string{
		"chats": "The lanternfish plan", "contacts": "lanternfish Supply", "memory": "lanternfish",
		"recordings": "The lanternfish talk", "places": "lanternfish Pier",
	}
	for area, title := range titles {
		found := world.areaOf(results, area)
		if found == nil || len(found.Results) != 1 || found.HasMore {
			test.Errorf("%s: %+v", area, found)
			continue
		}
		if found.Results[0].TitleText != title || found.Results[0].TargetID == "" || found.Results[0].SearchArea != area {
			test.Errorf("%s: %+v, want the title %q", area, found.Results[0], title)
		}
	}
	if memory := world.areaOf(results, "memory"); memory != nil && memory.Results[0].TargetID != "projects/lanternfish" {
		test.Errorf("a memory result opens by %q, want its path", memory.Results[0].TargetID)
	}
	photos := world.areaOf(results, "photos")
	if photos == nil || len(photos.Results) != 3 || photos.HasMore {
		test.Fatalf("photos: %+v", photos)
	}
	for _, result := range photos.Results {
		if !slices.Contains(photoIds, result.TargetID) {
			test.Errorf("a photo result %q is not one of the person's photos", result.TargetID)
		}
	}
}

// The person's main conversation has no title of its own, so a match in it is
// titled as the lists of conversations name it, and says it is the main one;
// a named conversation keeps its own title.
func TestSearchTitlesTheMainConversationAsMainChat(test *testing.T) {
	world := newSearchWorld(test)
	userId := world.person("search-main-chat", searchEverything...)
	agent := world.giveAgent(userId, true)
	var mainId, namedId string
	dbtest.RunTransactionOn(world.test, world.database, func(tx db.Transaction) {
		main, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			test.Fatal(err)
		}
		mainId = main.ID
		_, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: main.ID, Role: "user", Content: "Please remind me about the lanternfish."})
		if err != nil {
			test.Fatal(err)
		}
		named, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, Title: "Tank cleaning", LastAt: time.Now().Add(-time.Hour)})
		if err != nil {
			test.Fatal(err)
		}
		namedId = named.ID
		_, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: named.ID, Role: "user", Content: "How often should the lanternfish tank be cleaned?"})
		if err != nil {
			test.Fatal(err)
		}
	})

	results, text := world.search("search-main-chat", "lanternfish", 0, "chats")
	if results == nil {
		test.Fatal(text)
	}
	chats := world.areaOf(results, "chats")
	if chats == nil || len(chats.Results) != 2 {
		test.Fatalf("chats: %s", text)
	}
	for _, result := range chats.Results {
		switch result.TargetID {
		case mainId:
			if result.TitleText != "Main chat" || !result.IsMain {
				test.Errorf("the main conversation: title %q, isMain %v, want the title Main chat and isMain", result.TitleText, result.IsMain)
			}
		case namedId:
			if result.TitleText != "Tank cleaning" || result.IsMain {
				test.Errorf("a named conversation: title %q, isMain %v", result.TitleText, result.IsMain)
			}
		default:
			test.Errorf("an unexpected result %+v", result)
		}
	}
}

// Five a group by default is the server's, one more than is shown is what
// says there is more, and the areas asked for are the only ones searched.
func TestSearchKeepsFewResultsPerAreaAndSaysThereIsMore(test *testing.T) {
	world := newSearchWorld(test)
	userId := world.person("search-limits", searchEverything...)
	world.fill(userId, "lanternfish")

	results, text := world.search("search-limits", "lanternfish", 2, "photos", "contacts")
	if results == nil {
		test.Fatal(text)
	}
	if want := []string{"contacts", "photos"}; !slices.Equal(searchAreaNames(results), want) {
		test.Fatalf("areas = %v, want %v", searchAreaNames(results), want)
	}
	if photos := world.areaOf(results, "photos"); len(photos.Results) != 2 || !photos.HasMore {
		test.Errorf("photos with a limit of 2 and 3 photos: %d results, hasMore %v", len(photos.Results), photos.HasMore)
	}
	if contacts := world.areaOf(results, "contacts"); len(contacts.Results) != 1 || contacts.HasMore {
		test.Errorf("contacts: %+v", contacts)
	}
}

// An area the person may not use is left out and is not reported failed, and
// nothing of anybody else's is found.
func TestSearchLeavesOutWhatThePersonMayNotUseAndNeverReadsAnotherPersons(test *testing.T) {
	world := newSearchWorld(test)
	ownerId := world.person("search-owner", searchEverything...)
	world.fill(ownerId, "lanternfish")

	// Photos only: no agent permission, so none of the areas that need one.
	photosOnly := world.person("search-photos-only", models.PermissionPhotosUse)
	own := world.storePhoto(photosOnly, "own", "lanternfish-own.jpg", []byte("own original"), nil)
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if err := tx.SetPhotoDescription(photosOnly, own.ID, &db.PhotoDescription{PhotoDescription: "A lanternfish on its own.", ModelName: "p:thinker"}); err != nil {
			test.Fatal(err)
		}
	})
	results, text := world.search("search-photos-only", "lanternfish", 0)
	if results == nil {
		test.Fatal(text)
	}
	if want := []string{"photos"}; !slices.Equal(searchAreaNames(results), want) {
		test.Fatalf("areas = %v, want %v\n%s", searchAreaNames(results), want, text)
	}
	if photos := world.areaOf(results, "photos"); len(photos.Results) != 1 || photos.Results[0].TargetID != own.ID {
		test.Errorf("a person found photos that are not theirs: %+v", photos)
	}
	if len(results.FailedAreas) != 0 {
		test.Errorf("areas left out were reported failed: %v", results.FailedAreas)
	}

	// A person with an agent permission and a switched-off agent has no chats,
	// memory, documents or finance, and the areas that do not need the agent
	// still answer.
	offId := world.person("search-agent-off", models.PermissionAgentUse, models.PermissionContactsUse)
	world.giveAgent(offId, false)
	results, text = world.search("search-agent-off", "lanternfish", 0)
	if results == nil || len(results.Areas) != 0 || len(results.FailedAreas) != 0 {
		test.Errorf("a person with nothing of their own found %v (failed %v): %s", searchAreaNames(results), results.FailedAreas, text)
	}

	// No permission at all.
	world.person("search-nobody")
	results, text = world.search("search-nobody", "lanternfish", 0)
	if results == nil || len(results.Areas) != 0 || len(results.FailedAreas) != 0 {
		test.Errorf("a person who may use nothing found %v: %s", searchAreaNames(results), text)
	}
}

// A service the operator has turned off is left out for everybody.
func TestSearchLeavesOutAServiceThatIsOff(test *testing.T) {
	world := newSearchWorld(test)
	userId := world.person("search-service-off", searchEverything...)
	world.fill(userId, "lanternfish")

	configuration := world.config.Current()
	configuration.Services.Contacts.IsEnabled = new(false)
	configuration.Services.Photos.IsEnabled = new(false)

	results, text := world.search("search-service-off", "lanternfish", 0)
	if results == nil {
		test.Fatal(text)
	}
	if want := []string{"chats", "memory", "recordings", "places"}; !slices.Equal(searchAreaNames(results), want) {
		test.Errorf("areas = %v, want %v\n%s", searchAreaNames(results), want, text)
	}
	if len(results.FailedAreas) != 0 {
		test.Errorf("areas that are off were reported failed: %v", results.FailedAreas)
	}
}

// The words mean themselves, an area that is not one is refused by name, and
// a caller who is not signed in is refused.
func TestSearchRefusesWhatItCannotRun(test *testing.T) {
	world := newSearchWorld(test)
	userId := world.person("search-refusals", searchEverything...)
	world.fill(userId, "lanternfish")

	if results, _ := world.search("search-refusals", "%", 0); results == nil || len(results.Areas) != 0 {
		test.Errorf("a lone percent sign matched %v", searchAreaNames(results))
	}
	if results, _ := world.search("search-refusals", "   ", 0); results == nil || len(results.Areas) != 0 {
		test.Errorf("blank words matched %v", searchAreaNames(results))
	}
	if results, text := world.search("search-refusals", "lanternfish", 0, "emails"); results != nil || !strings.Contains(text, `\"emails\" is not an area to search`) {
		test.Errorf("an unknown area was searched: %s", text)
	}
	if results, text := world.search("", "lanternfish", 0); results != nil {
		test.Errorf("an anonymous caller searched: %s", text)
	}
	if results, text := world.search("search-refusals", strings.Repeat("lanternfish ", 20), 0); results != nil || !strings.Contains(text, "longer than 200 bytes") {
		test.Errorf("words longer than any area takes were searched: %s", text)
	}
}

// The agent's tools run the same document as the person, held to the agent's
// limits: it searches only the address books given to it, and narrowed to
// photos, the areas that need anything else are left out though the person
// may use them.
func TestTheAgentSearchesOnlyWhatItsLimitsAllow(test *testing.T) {
	world := newSearchWorld(test)
	userId := world.person("search-by-agent", searchEverything...)
	world.fill(userId, "lanternfish")
	var owner *models.User
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.GetUser(userId); err != nil {
			test.Fatal(err)
		}
	})
	operations := &agentOperations{graph: world.graph, user: owner, permissions: &models.EffectivePermissions{Everywhere: searchEverything}}

	// The book fill made is not given to the agent; a second one is.
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		book, err := tx.CreateAddressBook(&models.AddressBook{UserID: userId, Name: "Given"})
		if err != nil {
			test.Fatal(err)
		}
		book.AgentGranted = true
		if _, err := tx.UpdateAddressBook(book); err != nil {
			test.Fatal(err)
		}
		if _, err := tx.PutContact(&models.Contact{
			AddressBookID: book.ID, UID: "uid-given", ETag: "etag", Name: "lanternfish Given",
			Card: "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:lanternfish Given\r\nEND:VCARD\r\n",
		}); err != nil {
			test.Fatal(err)
		}
	})

	ask := func(operations *agentOperations) *client.SearchResults {
		var found struct {
			SearchEverything *client.SearchResults `json:"SearchEverything"`
		}
		if err := operations.Execute(test.Context(), client.DocumentSearchEverything, map[string]any{"words": "lanternfish"}, &found); err != nil {
			test.Fatal(err)
		}
		return found.SearchEverything
	}
	results := ask(operations)
	if want, got := []string{"chats", "contacts", "photos", "memory", "recordings", "places"}, searchAreaNames(results); !slices.Equal(got, want) {
		test.Errorf("the agent found %v, want %v", got, want)
	}
	for _, area := range results.Areas {
		if area.SearchArea != "contacts" {
			continue
		}
		var names []string
		for _, result := range area.Results {
			names = append(names, result.TitleText)
		}
		if !slices.Equal(names, []string{"lanternfish Given"}) {
			test.Errorf("the agent found the contacts %v, want only the one in the book given to it", names)
		}
	}
	narrowed := operations.NarrowedTo(&models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionPhotosUse}}).(*agentOperations)
	if want, got := []string{"photos"}, searchAreaNames(ask(narrowed)); !slices.Equal(got, want) {
		test.Errorf("a turn limited to photos found %v, want %v", got, want)
	}
}
