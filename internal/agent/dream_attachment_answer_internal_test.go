package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// answeringWith is a model that replies to the question about which files
// are worth opening with exactly these words, and says nothing useful to
// anything else.
func answeringWith(reply string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream   bool             `json:"stream"`
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		said := ""
		for _, message := range body.Messages {
			if content, ok := message["content"].(string); ok {
				said += content
			}
		}
		answer := "{}"
		if strings.Contains(said, "<files>") {
			answer = reply
		}
		encoded, _ := json.Marshal(answer)
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
				encoded)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer,
			`{"id":"c1","model":"m","choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			encoded)
	}))
}

// A model that said nothing about a batch has not declined it.
//
// An object with no list in it decodes into an empty list without
// complaint, so it used to be taken for a decision -- and a decision to
// open none of them is a decision to decline all of them, and a declined
// file is passed over by every night after. One error object could
// therefore take a batch out of the reading queue for good.
func TestAnErrorObjectDoesNotDeclineTheBatch(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	provider := answeringWith(`{"error":"the model is not answering tonight"}`)
	defer provider.Close()

	worker, run, store := attachmentWorld(test, database, provider.URL)
	documents := fileAttachments(test, database, run, store, []attachmentFile{
		{name: "shot.png", contentType: "image/png", bytes: 412000, said: "look at this"},
		{name: "board.png", contentType: "image/png", bytes: 380000, said: "the whiteboard"},
	})

	worker.dreamAttachments(context.Background(), run, &dreamBudget{})

	for name, document := range documents {
		if reason := declinedReason(test, database, document); reason != "" {
			test.Fatalf("%s was declined on the strength of an error object: %q", name, reason)
		}
	}
}

// And a model that weighed the batch and wanted none of it has.
//
// The pair is the point: these two answers are opposite and they decode
// the same, so a test for one of them alone would pass against the bug.
func TestAnEmptyListDoesDeclineTheBatch(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	provider := answeringWith(`{"open":[]}`)
	defer provider.Close()

	worker, run, store := attachmentWorld(test, database, provider.URL)
	documents := fileAttachments(test, database, run, store, []attachmentFile{
		{name: "shot.png", contentType: "image/png", bytes: 412000, said: "look at this"},
		{name: "board.png", contentType: "image/png", bytes: 380000, said: "the whiteboard"},
	})

	worker.dreamAttachments(context.Background(), run, &dreamBudget{})

	for name, document := range documents {
		if reason := declinedReason(test, database, document); reason == "" {
			test.Fatalf("%s was weighed and not wanted, so its row says so", name)
		}
	}
}

// The night opens no more than it was allowed to ask for.
//
// The bound was in the prompt and in the sentence written against the
// files left over, and nowhere in the code, so a model naming the whole
// batch had the whole batch opened at a vision call each.
func TestTheNightTakesNoMoreThanItsShareOfABatch(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	// Every file in the batch, asked for by name.
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream   bool             `json:"stream"`
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		said := ""
		for _, message := range body.Messages {
			if content, ok := message["content"].(string); ok {
				said += content
			}
		}
		answer := "{}"
		if strings.Contains(said, "<files>") {
			var wanted []map[string]string
			for _, found := range attachmentItem.FindAllStringSubmatch(said, -1) {
				wanted = append(wanted, map[string]string{"id": found[1], "reason": "all of them"})
			}
			encoded, _ := json.Marshal(map[string]any{"open": wanted})
			answer = string(encoded)
		}
		encoded, _ := json.Marshal(answer)
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
				encoded)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer,
			`{"id":"c1","model":"m","choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			encoded)
	}))
	defer provider.Close()

	worker, run, store := attachmentWorld(test, database, provider.URL)
	documents := fileAttachments(test, database, run, store, []attachmentFile{
		{name: "one.png", contentType: "image/png", bytes: 412000, said: "one"},
		{name: "two.png", contentType: "image/png", bytes: 380000, said: "two"},
		{name: "three.png", contentType: "image/png", bytes: 290000, said: "three"},
		{name: "four.png", contentType: "image/png", bytes: 310000, said: "four"},
	})

	worker.dreamAttachments(context.Background(), run, &dreamBudget{})

	// Four files, so the night could take one of them.
	most := attachmentsMost(len(documents))
	kept := 0
	for _, document := range documents {
		if declinedReason(test, database, document) == "" {
			kept++
		}
	}
	if kept > most {
		test.Fatalf("the night kept %d of %d files when it was allowed %d", kept, len(documents), most)
	}
}
