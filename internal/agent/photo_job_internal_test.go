package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// photoProvider is a model that answers the chat with the next of its
// answers, and embeds any text that mentions a lighthouse in one direction
// and any other text in another.
type photoProvider struct {
	mutex      sync.Mutex
	answers    []string
	chatBodies []string
	embedded   []string
}

func (self *photoProvider) serve(test *testing.T) *httptest.Server {
	test.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream bool     `json:"stream"`
			Input  []string `json:"input"`
		}
		raw := new(bytes.Buffer)
		_, _ = raw.ReadFrom(request.Body)
		_ = json.Unmarshal(raw.Bytes(), &body)
		if strings.HasSuffix(request.URL.Path, "/embeddings") {
			self.mutex.Lock()
			self.embedded = append(self.embedded, body.Input...)
			self.mutex.Unlock()
			data := make([]map[string]any, 0, len(body.Input))
			for index, input := range body.Input {
				vector := []float32{0, 1, 0}
				if strings.Contains(strings.ToLower(input), "lighthouse") {
					vector = []float32{1, 0, 0}
				}
				data = append(data, map[string]any{"index": index, "embedding": vector})
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"data": data, "usage": map[string]int{"prompt_tokens": 7, "total_tokens": 7}})
			return
		}
		self.mutex.Lock()
		answer := self.answers[min(len(self.chatBodies), len(self.answers)-1)]
		if body.Stream {
			self.chatBodies = append(self.chatBodies, raw.String())
		}
		self.mutex.Unlock()
		content, _ := json.Marshal(answer)
		if !body.Stream {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer, `{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{}}`, content)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{}}\n\ndata: [DONE]\n\n", content)
	}))
	test.Cleanup(server.Close)
	return server
}

func (self *photoProvider) chatCount() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.chatBodies)
}

// photoJobFixture is a person with a photo whose large preview is stored, an
// agent, and a model answering to order.
type photoJobFixture struct {
	database      db.Database
	configuration *config.Configuration
	worker        *Agent
	store         storage.Storage
	provider      *photoProvider
	owner         *models.User
	agent         *models.Agent
	photo         *models.Photo
}

// newPhotoJobFixture makes the fixture. With no model answers there is no
// provider at all; withEmbedding configures an embedding model beside the chat
// one.
func newPhotoJobFixture(test *testing.T, withEmbedding bool, answers ...string) *photoJobFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(bool)
	store, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = store.Close() })
	settings := &Settings{Database: database, Storage: store, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour}
	fixture := &photoJobFixture{database: database, configuration: configuration, store: store, provider: &photoProvider{answers: answers}}
	if len(answers) > 0 {
		server := fixture.provider.serve(test)
		configuration.Agent.Providers = []config.AgentProvider{{Name: "p", Kind: "openai", BaseURL: server.URL, APIKey: "k"}}
		configuration.Agent.Models.Default = "p:thinker"
		if withEmbedding {
			configuration.Agent.Models.Embedding = "p:embedder"
		}
		if settings.Registry, err = llm.Open(&configuration.Agent); err != nil {
			test.Fatal(err)
		}
	}
	fixture.worker = New(settings)
	fixture.worker.SetOperationsFactory(func(context.Context, *models.User) (Operations, error) { return &digestSplitOperations{}, nil })
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if fixture.owner, err = tx.CreateUser(&models.User{Username: "robin", Name: "Robin Example", Timezone: "UTC"}); err != nil {
			test.Fatal(err)
		}
		if fixture.agent, err = tx.CreateAgent(&models.Agent{UserID: fixture.owner.ID, Enabled: true}); err != nil {
			test.Fatal(err)
		}
	})
	fixture.photo = fixture.addPhoto(test, "lighthouse-one", "Harborview, Exampleland", true)
	return fixture
}

// addPhoto stores a photo of the fixture's person, with a large preview when
// asked.
func (self *photoJobFixture) addPhoto(test *testing.T, clientPhotoId, placeName string, hasPreview bool) *models.Photo {
	test.Helper()
	var photo *models.Photo
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if photo, _, err = tx.CreatePhoto(&models.Photo{UserID: self.owner.ID, ClientPhotoID: clientPhotoId, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine}); err != nil {
			test.Fatal(err)
		}
		if err := tx.UpdatePhotoMetadata(photo.ID, &db.PhotoMetadata{PlaceName: placeName}); err != nil {
			test.Fatal(err)
		}
		if !hasPreview {
			return
		}
		if _, err := tx.UpsertPhotoPreview(self.owner.ID, &models.PhotoPreview{PhotoID: photo.ID, PhotoPreviewSize: models.PhotoPreviewLarge, ContentType: "image/jpeg", PreviewSizeBytes: 1}); err != nil {
			test.Fatal(err)
		}
	})
	if hasPreview {
		picture := image.NewRGBA(image.Rect(0, 0, 64, 48))
		for x := 0; x < 64; x++ {
			for y := 0; y < 48; y++ {
				picture.Set(x, y, color.RGBA{R: 200, G: uint8(x * 3), B: uint8(y * 4), A: 255})
			}
		}
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, picture, nil); err != nil {
			test.Fatal(err)
		}
		if err := self.store.PutFile(test.Context(), models.PhotoPreviewFileID(photo.ID, models.PhotoPreviewLarge), encoded.Bytes()); err != nil {
			test.Fatal(err)
		}
	}
	return photo
}

func (self *photoJobFixture) run(photo *models.Photo) *Run {
	return &Run{
		Agent: self.agent, Owner: self.owner, Now: time.Now(), settings: self.worker.settings,
		Job: &models.AgentJob{ID: "job-photo", AgentID: self.agent.ID, Kind: models.AgentJobDescribePhoto, SubjectID: photo.ID},
	}
}

// request marks the photo as one a description is wanted for, as asking does.
func (self *photoJobFixture) request(test *testing.T, photo *models.Photo) {
	test.Helper()
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		if err := tx.RequestPhotoDescription(self.owner.ID, photo.ID); err != nil {
			test.Fatal(err)
		}
	})
}

func (self *photoJobFixture) read(test *testing.T, photo *models.Photo) *models.Photo {
	test.Helper()
	var found *models.Photo
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if found, err = tx.GetPhoto(self.owner.ID, photo.ID); err != nil {
			test.Fatal(err)
		}
	})
	return found
}

// search is the photos the person's search finds, with the meaning of the
// words when the deployment can embed them.
func (self *photoJobFixture) search(test *testing.T, searchText string) []string {
	test.Helper()
	var photoIds []string
	filter := &db.PhotoFilter{PageSize: 20, SearchText: searchText}
	filter.SearchMeaning = self.worker.EmbedPhotoSearch(test.Context(), self.agent, searchText)
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		photos, err := tx.ListPhotos(self.owner.ID, filter)
		if err != nil {
			test.Fatal(err)
		}
		for _, found := range photos {
			photoIds = append(photoIds, found.ID)
		}
	})
	return photoIds
}

func (self *photoJobFixture) runTitles(test *testing.T) []string {
	test.Helper()
	var titles []string
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		conversations, err := tx.ListAgentConversations(self.agent.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil {
			test.Fatal(err)
		}
		for _, conversation := range conversations {
			if conversation.JobKind == string(models.AgentJobDescribePhoto) {
				titles = append(titles, conversation.Title)
			}
		}
	})
	return titles
}

const lighthouseAnswer = `{"photoDescription": "A red lighthouse on a rocky point under a clear sky. Two people stand near its base.", "visibleText": "HARBOR ENTRANCE"}`

// A photo that is asked for is looked at by the model, which is shown its
// large preview and told whose picture it is; the description and the words
// read in it are kept with the model that wrote them, the request is cleared,
// and the photo is found by a word of the description and, with an embedding
// model, by a question that means the same.
func TestThePhotoJobDescribesAPhotoAndItIsFoundByWhatIsInIt(test *testing.T) {
	fixture := newPhotoJobFixture(test, true, lighthouseAnswer)
	other := fixture.addPhoto(test, "plain-one", "Hillcrest, Exampleland", true)
	fixture.request(test, fixture.photo)

	if found := fixture.search(test, "lighthouse"); len(found) != 0 {
		test.Fatalf("a photo nobody described was found by what is in it: %v", found)
	}
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("runDescribePhoto: %s", err)
	}
	described := fixture.read(test, fixture.photo)
	if described.DescribedAt == nil || !strings.HasPrefix(described.PhotoDescription, "A red lighthouse on a rocky point") || described.DescriptionVisibleText != "HARBOR ENTRANCE" ||
		described.DescriptionModel != "p:thinker" || described.IsDescriptionRequested {
		test.Fatalf("the description is kept with its model and the request is cleared: %+v", described)
	}
	if fixture.provider.chatCount() != 1 {
		test.Fatalf("one model call: %d", fixture.provider.chatCount())
	}
	body := fixture.provider.chatBodies[0]
	if !strings.Contains(body, "Robin Example") || !strings.Contains(body, "Harborview, Exampleland") || !strings.Contains(body, "image_url") || !strings.Contains(body, "data:image/jpeg;base64,") {
		test.Fatalf("the model is shown the picture and told whose it is and where the camera put it: %.600s", body)
	}
	// By a word of the description, by a word of what was read in the picture,
	// and by a place that was known all along.
	for _, searchText := range []string{"lighthouse", "entrance", "harborview"} {
		if found := fixture.search(test, searchText); len(found) != 1 || found[0] != fixture.photo.ID {
			test.Fatalf("%q finds %v, want the described photo first", searchText, found)
		}
	}
	// The text that was embedded is the description, the words read and the
	// place, and a search that only means the same finds the photo: nothing in
	// "beacon on the rocks" is a word of the description, and the fake
	// embedder puts every text it does not know in another direction, so this
	// one is found because the words "lighthouse" and "beacon" are both asked.
	isDescriptionEmbedded := false
	for _, text := range fixture.provider.embedded {
		isDescriptionEmbedded = isDescriptionEmbedded || (strings.Contains(text, "A red lighthouse") && strings.Contains(text, "HARBOR ENTRANCE") && strings.Contains(text, "Harborview"))
	}
	if !isDescriptionEmbedded {
		test.Fatalf("what was embedded: %v", fixture.provider.embedded)
	}
	if found := fixture.search(test, "beacon lighthouse"); len(found) != 1 || found[0] != fixture.photo.ID {
		test.Fatalf("found %v", found)
	}
	if found := fixture.search(test, "bicycle"); len(found) != 0 {
		test.Fatalf("a photo was found by a query that is neither its words nor its meaning: %v (the other photo is %s)", found, other.ID)
	}
	if titles := fixture.runTitles(test); len(titles) != 1 || !strings.HasPrefix(titles[0], "Described a photo: A red lighthouse") {
		test.Fatalf("the run says what it did: %q", titles)
	}
}

// An answer that cannot be read leaves the photo undescribed and the request
// cleared, since asking again would ask the same.
func TestThePhotoJobLeavesAPhotoUndescribedWhenTheAnswerCannotBeRead(test *testing.T) {
	for _, answer := range []string{"I cannot tell what this is.", `{"photoDescription": "   ", "visibleText": ""}`, `{"visibleText": "STOP"}`} {
		fixture := newPhotoJobFixture(test, true, answer)
		fixture.request(test, fixture.photo)
		if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
			test.Fatalf("%q: runDescribePhoto: %s", answer, err)
		}
		photo := fixture.read(test, fixture.photo)
		if photo.DescribedAt != nil || photo.PhotoDescription != "" || photo.IsDescriptionRequested || photo.DescriptionRequestedAt != nil {
			test.Fatalf("%q: the photo is undescribed and nothing is pending: %+v", answer, photo)
		}
		if titles := fixture.runTitles(test); len(titles) != 1 || !strings.Contains(titles[0], "could not tell what the answer said") {
			test.Fatalf("%q: the run says why: %q", answer, titles)
		}
		if found := fixture.search(test, "stop"); len(found) != 0 {
			test.Fatalf("%q: %v", answer, found)
		}
	}
}

// The words read in a picture may come back as a list, which some models
// answer with, and an over-long answer is cut.
func TestThePhotoJobAcceptsAListOfWordsAndCutsWhatIsTooLong(test *testing.T) {
	long := strings.Repeat("word ", db.MaximumPhotoDescriptionCharacters)
	fixture := newPhotoJobFixture(test, false, fmt.Sprintf(`{"photoDescription": %q, "visibleText": ["OPEN", "ALL DAY"]}`, long))
	fixture.request(test, fixture.photo)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("runDescribePhoto: %s", err)
	}
	described := fixture.read(test, fixture.photo)
	if described.DescriptionVisibleText != "OPEN\nALL DAY" || len([]rune(described.PhotoDescription)) != db.MaximumPhotoDescriptionCharacters {
		test.Fatalf("%q (%d characters)", described.DescriptionVisibleText, len([]rune(described.PhotoDescription)))
	}
}

// With no embedding model the photo is still described and still found by its
// words.
func TestThePhotoJobDescribesWithoutAnEmbeddingModel(test *testing.T) {
	fixture := newPhotoJobFixture(test, false, lighthouseAnswer)
	fixture.request(test, fixture.photo)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("runDescribePhoto: %s", err)
	}
	if found := fixture.search(test, "lighthouse"); len(found) != 1 || found[0] != fixture.photo.ID {
		test.Fatalf("found %v", found)
	}
	if len(fixture.provider.embedded) != 0 {
		test.Fatalf("something was embedded: %v", fixture.provider.embedded)
	}
}

// A person with no model for pictures is told so on the run and the request
// is cleared, without a model being called.
func TestThePhotoJobRefusesQuietlyWithoutAModel(test *testing.T) {
	fixture := newPhotoJobFixture(test, true)
	fixture.request(test, fixture.photo)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("a missing model is not a failure to retry: %s", err)
	}
	photo := fixture.read(test, fixture.photo)
	if photo.DescribedAt != nil || photo.IsDescriptionRequested {
		test.Fatalf("%+v", photo)
	}
	if titles := fixture.runTitles(test); len(titles) != 1 || !strings.HasPrefix(titles[0], "Did not describe a photo: Describing photos needs") {
		test.Fatalf("%q", titles)
	}
}

// A photo already described, and not asked for again, is not described a
// second time by a job that was queued twice; one asked for again is.
func TestThePhotoJobDescribesAgainOnlyWhenAskedTo(test *testing.T) {
	fixture := newPhotoJobFixture(test, true, lighthouseAnswer, `{"photoDescription": "A stone bridge over a river.", "visibleText": ""}`)
	fixture.request(test, fixture.photo)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatal(err)
	}
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatal(err)
	}
	if fixture.provider.chatCount() != 1 {
		test.Fatalf("a described photo was described again unasked: %d calls", fixture.provider.chatCount())
	}
	fixture.request(test, fixture.photo)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatal(err)
	}
	if fixture.provider.chatCount() != 2 {
		test.Fatalf("a new description was not written: %d calls", fixture.provider.chatCount())
	}
	if found := fixture.search(test, "lighthouse"); len(found) != 0 {
		test.Fatalf("the old description still finds the photo: %v", found)
	}
	if found := fixture.search(test, "bridge"); len(found) != 1 {
		test.Fatalf("the new one does not: %v", found)
	}
}

// A photo whose previews are not made yet is tried again later, one whose
// previews failed is refused, and one that is gone ends the job.
func TestThePhotoJobWaitsForPreviewsAndLetsGoOfWhatIsGone(test *testing.T) {
	fixture := newPhotoJobFixture(test, true, lighthouseAnswer)
	waiting := fixture.addPhoto(test, "waiting-one", "", false)
	fixture.request(test, waiting)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(waiting)); err == nil || !strings.Contains(err.Error(), "not made yet") {
		test.Fatalf("a photo waiting for its previews is tried again: %v", err)
	}
	if fixture.provider.chatCount() != 0 {
		test.Fatal("a model was called without a picture")
	}
	dbtest.MarkPhotoProcessingFailed(test, fixture.database, waiting.ID, "The picture could not be read.", true, 1)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(waiting)); err != nil {
		test.Fatalf("a photo with no previews for good is refused, not retried: %s", err)
	}
	if fixture.read(test, waiting).IsDescriptionRequested {
		test.Fatal("the request stays pending for a photo that cannot be described")
	}
	gone := &models.Photo{ID: "no-such-photo"}
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(gone)); err != nil {
		test.Fatalf("a photo that is gone ends the job: %s", err)
	}
}

// Asking marks the photo and queues one job however often it is asked, and a
// photo of somebody else's is not found.
func TestQueuingADescriptionMarksThePhotoAndQueuesOneJob(test *testing.T) {
	fixture := newPhotoJobFixture(test, true, lighthouseAnswer)
	var first, second *models.AgentJob
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		var err error
		if first, err = fixture.worker.QueuePhotoDescription(tx, fixture.agent.ID, fixture.photo.ID); err != nil {
			test.Fatal(err)
		}
		if second, err = fixture.worker.QueuePhotoDescription(tx, fixture.agent.ID, fixture.photo.ID); err != nil {
			test.Fatal(err)
		}
	})
	if first.ID != second.ID || first.Kind != models.AgentJobDescribePhoto || first.SubjectID != fixture.photo.ID {
		test.Fatalf("%+v %+v", first, second)
	}
	if !fixture.read(test, fixture.photo).IsDescriptionRequested {
		test.Fatal("the photo is not marked as being described")
	}
	var stranger string
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "stranger", Name: "Stranger Example"})
		if err != nil {
			test.Fatal(err)
		}
		stranger = user.ID
		photo, _, err := tx.CreatePhoto(&models.Photo{UserID: stranger, ClientPhotoID: "theirs", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := fixture.worker.QueuePhotoDescription(tx, fixture.agent.ID, photo.ID); err == nil {
			test.Fatal("a photo of somebody else's was queued")
		}
	})
}

func TestThePhotoDescriptionIsUnavailableWithoutTheAssistantOrAModel(test *testing.T) {
	fixture := newPhotoJobFixture(test, true, lighthouseAnswer)
	if why := fixture.worker.PhotoDescriptionUnavailableMessage(fixture.configuration); why != "" {
		test.Fatalf("available but %q", why)
	}
	fixture.configuration.Agent.Enabled = false
	if why := fixture.worker.PhotoDescriptionUnavailableMessage(fixture.configuration); why != "Describing photos needs the assistant to be on." {
		test.Fatalf("%q", why)
	}
	fixture = newPhotoJobFixture(test, true)
	if why := fixture.worker.PhotoDescriptionUnavailableMessage(fixture.configuration); !strings.Contains(why, "Describing photos needs a model") {
		test.Fatalf("%q", why)
	}
}
