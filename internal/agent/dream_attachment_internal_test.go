package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// The night's two questions about a picture: whether it is worth opening,
// and what is in it.
//
// Both are worth a test through the phase rather than at the database,
// because between them sit a prompt, a model, a parse, a fetch out of the
// store and a write, and every one of those is somewhere the phase can
// quietly do the wrong thing -- open what it should not have, mark read
// what it never read, or send a video to a model that cannot look at one.

// attachmentOffer is one file as the deciding call was shown it: the
// identifier the prompt listed it under and the name beside it.
type attachmentOffer struct {
	documentID string
	name       string
}

// attachmentCalls is everything the model standing in for the provider was
// asked, so that a test can say what was put to it and what never was.
type attachmentCalls struct {
	// offered is every file that appeared in a deciding call, and
	// pictures the prompt of every call that really carried an image.
	offered  []attachmentOffer
	pictures []string
}

// offeredNames is the files a decision was bought about.
func (self attachmentCalls) offeredNames() []string {
	names := make([]string, 0, len(self.offered))
	for _, offer := range self.offered {
		names = append(names, offer.name)
	}
	return names
}

// attachmentItem matches one file in the list the deciding prompt shows:
// its identifier in brackets and the name that follows.
var attachmentItem = regexp.MustCompile(`(?m)^\[([^\]]+)\] (\S+)`)

// attachmentProvider is a model that answers the night's two questions.
//
// It chooses whichever of the files put to it are named in open, with the
// given reason, and describes any picture it is sent with described.
// failPictures makes it refuse to look at a picture, which is what a
// provider that is down or out of quota does.
func attachmentProvider(open map[string]string, described string, failPictures bool) (*httptest.Server, func() attachmentCalls) {
	var mutex sync.Mutex
	var calls attachmentCalls
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream   bool             `json:"stream"`
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)

		// A turn's content is a string when it is only words and a list
		// of parts when it carries a picture, so both shapes are read and
		// the picture is noticed by its part rather than by its size.
		said, picture := "", false
		for _, message := range body.Messages {
			switch content := message["content"].(type) {
			case string:
				said += content
			case []any:
				for _, part := range content {
					fields, ok := part.(map[string]any)
					if !ok {
						continue
					}
					if fields["type"] == "image_url" {
						picture = true
						continue
					}
					if text, ok := fields["text"].(string); ok {
						said += text
					}
				}
			}
		}

		answer := ""
		switch {
		case picture:
			mutex.Lock()
			calls.pictures = append(calls.pictures, said)
			mutex.Unlock()
			if failPictures {
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte(`{"error":{"message":"the model is not answering tonight"}}`))
				return
			}
			answer = described
		case strings.Contains(said, "<files>"):
			var wanted []map[string]string
			for _, found := range attachmentItem.FindAllStringSubmatch(said, -1) {
				mutex.Lock()
				calls.offered = append(calls.offered, attachmentOffer{documentID: found[1], name: found[2]})
				mutex.Unlock()
				if reason, ok := open[found[2]]; ok {
					wanted = append(wanted, map[string]string{"id": found[1], "reason": reason})
				}
			}
			if wanted == nil {
				wanted = []map[string]string{}
			}
			encoded, _ := json.Marshal(map[string]any{"open": wanted})
			answer = string(encoded)
		default:
			answer = "{}"
		}

		encoded, _ := json.Marshal(answer)
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
				encoded)
			return
		}
		_, _ = fmt.Fprintf(writer,
			`{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			encoded)
	}))
	return server, func() attachmentCalls {
		mutex.Lock()
		defer mutex.Unlock()
		return attachmentCalls{
			offered:  append([]attachmentOffer(nil), calls.offered...),
			pictures: append([]string(nil), calls.pictures...),
		}
	}
}

// picturesOf is how many of the calls that carried an image were about
// the named file. A describing prompt names the file it is about, so this
// is what says which picture really went to a model.
func (self attachmentCalls) picturesOf(name string) int {
	count := 0
	for _, prompt := range self.pictures {
		if strings.Contains(prompt, name) {
			count++
		}
	}
	return count
}

// watchedStore is the object store with a note kept of every fetch, so a
// test can prove that the bytes of a file the night declined were never
// carried out of it.
type watchedStore struct {
	storage.Storage
	mutex   sync.Mutex
	fetched []string
}

func (self *watchedStore) GetFile(ctx context.Context, id string) ([]byte, error) {
	self.mutex.Lock()
	self.fetched = append(self.fetched, id)
	self.mutex.Unlock()
	return self.Storage.GetFile(ctx, id)
}

func (self *watchedStore) fetches() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string(nil), self.fetched...)
}

// attachmentWorld is an agent whose one model is the given provider, the
// person who owns it, a run to work in, and the store the bytes are in.
func attachmentWorld(t *testing.T, database db.Database, providerURL string) (*Agent, *Run, *watchedStore) {
	t.Helper()
	worker, run := digestSplitWorld(t, database, providerURL)
	// The run and the worker share one Settings, so putting the watched
	// store on it puts it everywhere the phase reaches for one.
	watched := &watchedStore{Storage: worker.settings.Storage}
	worker.settings.Storage = watched
	return worker, run, watched
}

// attachmentFile is one file a record came with, filed the way the ingest
// files it: a document with no text, its bytes in the store under a key,
// and the record it arrived with on its metadata.
type attachmentFile struct {
	name        string
	contentType string
	bytes       int64
	said        string
	content     []byte
}

// attachmentKeyOf is where a file's bytes are kept, which for a real one
// is the hash of them: the store refuses anything with a dot in it, so a
// test naming its objects after the files would not write one at all.
func attachmentKeyOf(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// fileAttachments writes the given files down as a source's documents and
// puts their bytes in the store, and answers the documents by name.
func fileAttachments(t *testing.T, database db.Database, run *Run, store storage.Storage,
	files []attachmentFile) map[string]*models.AgentDocument {
	t.Helper()
	documents := map[string]*models.AgentDocument{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.Agent.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "~/records", Format: models.FormatRecords,
			},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		for _, file := range files {
			key := attachmentKeyOf(file.name)
			content := file.content
			if content == nil {
				content = []byte("the bytes of " + file.name)
			}
			if err := store.PutFile(context.Background(), key, content); err != nil {
				t.Fatalf("PutFile %q: %s", file.name, err)
			}
			size := file.bytes
			if size == 0 {
				size = 40960
			}
			happened := time.Date(2020, 10, 16, 20, 26, 21, 0, time.UTC)
			document, err := tx.PutAgentDocument(&models.AgentDocument{
				AgentID: run.Agent.ID, SourceID: source.ID, ExternalID: "posts.jsonl#" + key,
				Kind: models.DocumentAttachment, Title: file.name, Hash: key, StorageKey: key,
				Bytes: size, HappenedAt: &happened,
				Metadata: map[string]any{
					"contentType": file.contentType,
					"path":        "~/records/" + file.name,
					"author":      "alice",
					"channel":     "#support",
					"thread":      "the container will not start",
					"id":          "post-" + file.name,
					"said":        file.said,
				},
			})
			if err != nil {
				t.Fatalf("PutAgentDocument %q: %s", file.name, err)
			}
			documents[file.name] = document
		}
	})
	return documents
}

// passagesOf is the text a document ended the night with.
func passagesOf(t *testing.T, database db.Database, document *models.AgentDocument) string {
	t.Helper()
	var text string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		chunks, err := tx.ListAgentChunks(document.AgentID, document.ID)
		if err != nil {
			t.Fatalf("ListAgentChunks: %s", err)
		}
		lines := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			lines = append(lines, chunk.Text)
		}
		text = strings.Join(lines, "\n")
	})
	return text
}

// declinedReason is what the row says the night decided, read back the
// way anything else reads it back.
func declinedReason(t *testing.T, database db.Database, document *models.AgentDocument) string {
	t.Helper()
	var reason string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := tx.GetAgentDocument(document.AgentID, document.ID)
		if err != nil || found == nil {
			t.Fatalf("GetAgentDocument: %v %s", found, err)
		}
		reason = found.Declined()
	})
	return reason
}

// digested says whether the night marked this as read.
func digested(t *testing.T, database db.Database, document *models.AgentDocument) bool {
	t.Helper()
	read := false
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := tx.GetAgentDocument(document.AgentID, document.ID)
		if err != nil || found == nil {
			t.Fatalf("GetAgentDocument: %v %s", found, err)
		}
		_, read = found.Metadata["digested"]
	})
	return read
}

// The night opens what it chose and records why it left the rest.
//
// This is the whole shape of the phase in one test: the ones it passed
// over carry a reason and cost nothing beyond the one call that decided
// about them -- their bytes are never carried out of the store -- and the
// one it chose ends the night with the model's words as its text, which
// is what makes it searchable and quotable like any other document.
func TestTheNightOpensWhatItChoseAndSaysWhyItLeftTheRest(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	const described = `A terminal window from a container host. The meaningful status reads ` +
		`"Container is empty" with timestamp "2020/10/16 20:26:21 JST" and scene identifier ` +
		`"test201016_0004.northwind.dae".`
	provider, calls := attachmentProvider(
		map[string]string{"shot.png": "the failing container in the #support thread"},
		described, false)
	defer provider.Close()

	worker, run, store := attachmentWorld(t, database, provider.URL)
	documents := fileAttachments(t, database, run, store, []attachmentFile{
		{name: "shot.png", contentType: "image/png", bytes: 412000,
			said: "look at this, it dies the moment it starts"},
		{name: "avatar.png", contentType: "image/png", bytes: 3100, said: "new profile picture"},
		{name: "logo.png", contentType: "image/png", bytes: 2400, said: "the new logo"},
	})

	worker.dreamAttachments(context.Background(), run, &dreamBudget{})

	// All three were decided about, in one call.
	offered := calls().offeredNames()
	if len(offered) != 3 {
		t.Fatalf("every file waiting is put to the decision once: %v", offered)
	}

	// The one it chose: opened, and its text is what the model said.
	made := calls()
	if len(made.pictures) != 1 || made.picturesOf("shot.png") != 1 {
		t.Fatalf("one picture was sent, and it is the one it chose: %d sent", len(made.pictures))
	}
	text := passagesOf(t, database, documents["shot.png"])
	if !strings.Contains(text, "Container is empty") || !strings.Contains(text, "test201016_0004.northwind.dae") {
		t.Fatalf("the picture's passages are what the model read out of it: %q", text)
	}
	if reason := declinedReason(t, database, documents["shot.png"]); reason != "" {
		t.Fatalf("and it is not marked as declined: %q", reason)
	}
	// Not read, either: it has passages now and the reading comes for it
	// like any other document. Marking it read here would mean nothing
	// ever learned anything from what it says.
	if digested(t, database, documents["shot.png"]) {
		t.Fatal("a picture that was opened is not thereby read")
	}

	// The two it did not: a reason on each, no passages, and their bytes
	// never left the store.
	for _, name := range []string{"avatar.png", "logo.png"} {
		if reason := declinedReason(t, database, documents[name]); reason == "" {
			t.Fatalf("%s says why it was left alone", name)
		}
		if text := passagesOf(t, database, documents[name]); text != "" {
			t.Fatalf("%s was not opened, so it has no passages: %q", name, text)
		}
		if digested(t, database, documents[name]) {
			t.Fatalf("%s was not read either", name)
		}
	}
	for _, fetched := range store.fetches() {
		if fetched != attachmentKeyOf("shot.png") {
			t.Fatalf("the bytes of a file the night declined were fetched: %q", fetched)
		}
	}

	// And a second night pays for none of it again.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		waiting, err := tx.ListAgentAttachmentsToDecide(run.Agent.ID, 10)
		if err != nil {
			t.Fatalf("ListAgentAttachmentsToDecide: %s", err)
		}
		if len(waiting) != 0 {
			t.Fatalf("nothing is left to decide about: %d", len(waiting))
		}
	})
}

// What no model here can look at is never put to one.
//
// A video, an archive or a spreadsheet is not a picture, and buying a
// judgement about whether to open something that cannot be opened is the
// one call that can never pay for itself. It is settled from the content
// type, before anything is fetched, and the row says so the same way a
// decline does.
func TestAFileThatIsNotAPictureIsSkippedWithAReasonAndNeverSent(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, calls := attachmentProvider(
		map[string]string{"clip.mp4": "surely worth a look", "papers.zip": "and this"},
		"a video of something", false)
	defer provider.Close()

	worker, run, store := attachmentWorld(t, database, provider.URL)
	documents := fileAttachments(t, database, run, store, []attachmentFile{
		{name: "clip.mp4", contentType: "video/mp4", bytes: 4 << 20, said: "here is the recording"},
		{name: "papers.zip", contentType: "application/zip", bytes: 900000, said: "the drawings"},
		{name: "mystery", contentType: "", bytes: 51200, said: "no idea what this is"},
	})

	worker.dreamAttachments(context.Background(), run, &dreamBudget{})

	made := calls()
	if len(made.offered) != 0 {
		t.Fatalf("nothing that cannot be opened is put to a model: %v", made.offeredNames())
	}
	if len(made.pictures) != 0 {
		t.Fatalf("and nothing is sent as a picture: %d were", len(made.pictures))
	}
	if fetched := store.fetches(); len(fetched) != 0 {
		t.Fatalf("and no bytes are carried out of the store: %v", fetched)
	}
	for name, want := range map[string]string{
		"clip.mp4":   "video/mp4",
		"papers.zip": "application/zip",
		"mystery":    "nothing said what kind of file it is",
	} {
		reason := declinedReason(t, database, documents[name])
		if !strings.Contains(reason, want) {
			t.Fatalf("%s says why in words a person can read, not %q", name, reason)
		}
		if digested(t, database, documents[name]) {
			t.Fatalf("%s is not read: nothing read it", name)
		}
	}
}

// A picture too large to be worth sending is passed over, not sent.
//
// A four-thousand-pixel screenshot costs several times what a
// thousand-pixel one does and says the same thing, and until the records
// script can shrink one before it is uploaded the answer is to leave it
// with its reason on the row.
func TestAPictureOverTheBoundIsSkippedWithItsReason(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, calls := attachmentProvider(
		map[string]string{"huge.png": "worth a look"}, "an enormous screenshot", false)
	defer provider.Close()

	worker, run, store := attachmentWorld(t, database, provider.URL)
	documents := fileAttachments(t, database, run, store, []attachmentFile{
		{name: "huge.png", contentType: "image/png", bytes: pictureLargest + 1,
			said: "the whole dashboard, at last"},
	})

	worker.dreamAttachments(context.Background(), run, &dreamBudget{})

	made := calls()
	if len(made.offered) != 0 || len(made.pictures) != 0 {
		t.Fatalf("nothing was put to a model: %v offered, %d sent as a picture",
			made.offeredNames(), len(made.pictures))
	}
	if fetched := store.fetches(); len(fetched) != 0 {
		t.Fatalf("and nothing was carried out of the store: %v", fetched)
	}
	reason := declinedReason(t, database, documents["huge.png"])
	if !strings.Contains(reason, "8.0 MB") {
		t.Fatalf("the row says how large it may be, not %q", reason)
	}
}

// A model that will not look leaves the picture exactly as it was.
//
// Not declined -- the night wanted to open it -- and above all not read,
// because nothing ever comes back for a document that says it was read.
// Tomorrow asks again.
func TestAModelThatFailsLeavesThePictureUntouched(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, calls := attachmentProvider(
		map[string]string{"shot.png": "the failing container"}, "", true)
	defer provider.Close()

	worker, run, store := attachmentWorld(t, database, provider.URL)
	documents := fileAttachments(t, database, run, store, []attachmentFile{
		{name: "shot.png", contentType: "image/png", bytes: 412000, said: "look at this"},
	})

	worker.dreamAttachments(context.Background(), run, &dreamBudget{})

	if calls().picturesOf("shot.png") == 0 {
		t.Fatal("the picture was put to the model, which is what this test is about")
	}
	if text := passagesOf(t, database, documents["shot.png"]); text != "" {
		t.Fatalf("a refused call leaves no passages behind: %q", text)
	}
	if reason := declinedReason(t, database, documents["shot.png"]); reason != "" {
		t.Fatalf("and does not count as a decision against it: %q", reason)
	}
	if digested(t, database, documents["shot.png"]) {
		t.Fatal("and above all does not mark it read")
	}
	// Still waiting, so the next night tries again.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		waiting, err := tx.ListAgentAttachmentsToDecide(run.Agent.ID, 10)
		if err != nil {
			t.Fatalf("ListAgentAttachmentsToDecide: %s", err)
		}
		if len(waiting) != 1 {
			t.Fatalf("the file waits for a night that answers: %d", len(waiting))
		}
	})
}

// A night whose share of the day is gone asks nothing and breaks nothing.
func TestANightWithNothingLeftToSpendOpensNothing(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, calls := attachmentProvider(
		map[string]string{"shot.png": "the failing container"}, "a terminal", false)
	defer provider.Close()

	worker, run, store := attachmentWorld(t, database, provider.URL)
	documents := fileAttachments(t, database, run, store, []attachmentFile{
		{name: "shot.png", contentType: "image/png", bytes: 412000, said: "look at this"},
		{name: "clip.mp4", contentType: "video/mp4", bytes: 4 << 20, said: "the recording"},
	})

	worker.dreamAttachments(context.Background(), run, &dreamBudget{exhausted: true})

	made := calls()
	if len(made.offered) != 0 || len(made.pictures) != 0 {
		t.Fatalf("a night with nothing to spend asks nothing: %v %v", made.offeredNames(), made.pictures)
	}
	if fetched := store.fetches(); len(fetched) != 0 {
		t.Fatalf("and fetches nothing: %v", fetched)
	}
	// Not even the free decision about what cannot be opened: a night
	// that spent its share is over, and tomorrow's does the lot.
	for _, name := range []string{"shot.png", "clip.mp4"} {
		if reason := declinedReason(t, database, documents[name]); reason != "" {
			t.Fatalf("%s is left alone entirely, not declined: %q", name, reason)
		}
		if digested(t, database, documents[name]) {
			t.Fatalf("%s is not marked read", name)
		}
	}
}

// A file left over from a full list says the cap bound, not that it lost.
//
// The night shows a batch and takes at most a quarter of it. When the
// model names its whole allowance, everything else in the batch is left
// without ever having been weighed against what it named, and the row
// has to say so: the old sentence claimed a judgement of the name, the
// size, the kind and the words, and on a full list no such judgement
// happened.
func TestAFileLeftOverFromAFullListSaysItWasPassedOver(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, _ := attachmentProvider(
		map[string]string{"shot.png": "the failing container in the #support thread"},
		"a terminal window", false)
	defer provider.Close()

	worker, run, store := attachmentWorld(t, database, provider.URL)
	documents := fileAttachments(t, database, run, store, []attachmentFile{
		{name: "shot.png", contentType: "image/png", bytes: 412000,
			said: "look at this, it dies the moment it starts"},
		{name: "board.png", contentType: "image/png", bytes: 380000, said: "the whiteboard from monday"},
		{name: "trace.png", contentType: "image/png", bytes: 290000, said: "and the stack trace"},
		{name: "avatar.png", contentType: "image/png", bytes: 3100, said: "new profile picture"},
	})

	// Four files, so the night could take one, and the model took one.
	if attachmentsMost(4) != 1 {
		t.Fatalf("this test is about a list that comes back full: %d", attachmentsMost(4))
	}

	worker.dreamAttachments(context.Background(), run, &dreamBudget{})

	for _, name := range []string{"board.png", "trace.png", "avatar.png"} {
		reason := declinedReason(t, database, documents[name])
		if reason == declinedByDefault {
			t.Fatalf("%s was never judged, so its row must not say it was: %q", name, reason)
		}
		if reason != declinedWhenFull(4) {
			t.Fatalf("%s says how many the night could take and that it was passed over: %q", name, reason)
		}
	}
	if reason := declinedReason(t, database, documents["shot.png"]); reason != "" {
		t.Fatalf("the one it chose is not declined at all: %q", reason)
	}
}

// A file left over from a list with room in it did lose on its merits.
//
// Nothing stopped the model naming this one, and it named nothing, so
// the sentence about the name, the size, the kind and the words is the
// true one and is what the row keeps.
func TestAFileLeftWhileThereWasRoomSaysItWasJudged(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, calls := attachmentProvider(map[string]string{}, "", false)
	defer provider.Close()

	worker, run, store := attachmentWorld(t, database, provider.URL)
	documents := fileAttachments(t, database, run, store, []attachmentFile{
		{name: "avatar.png", contentType: "image/png", bytes: 3100, said: "new profile picture"},
		{name: "logo.png", contentType: "image/png", bytes: 2400, said: "the new logo"},
		{name: "cat.png", contentType: "image/png", bytes: 90000, said: "look at him go"},
	})

	worker.dreamAttachments(context.Background(), run, &dreamBudget{})

	// The model was allowed one and asked for none, so the cap bound
	// nothing here.
	if len(calls().offered) != 3 {
		t.Fatalf("all three were put to the one decision: %v", calls().offeredNames())
	}
	for _, name := range []string{"avatar.png", "logo.png", "cat.png"} {
		if reason := declinedReason(t, database, documents[name]); reason != declinedByDefault {
			t.Fatalf("%s was judged and not chosen, and its row says that: %q", name, reason)
		}
	}
}
