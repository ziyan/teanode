package apigraph

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A program's upload goes to the address it was given, once, and the file is
// then posted to the person's main chat; an address with a bad signature, or
// one used already, takes nothing.
func TestAFileUploadedForTheMainChatIsPostedOnce(test *testing.T) {
	world := newPhotoWorld(test)
	world.withAssistant()
	userId := world.person("upload-sender", models.PermissionAgentUse)
	person := world.giveAgent(userId, true)
	var waiting *models.AgentAttachment
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		var err error
		if waiting, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: person.ID, MessageID: models.AttachmentMessageUpload, Name: "notes.html", ContentType: "text/html"}); err != nil {
			test.Fatal(err)
		}
	})
	worker := world.graph.settings.Agent.(*agent.Agent)
	if err := worker.SendFileToMainChat(test.Context(), person.ID, waiting.ID, "Notes from the build", "Example Coder"); err != nil {
		test.Fatalf("SendFileToMainChat: %s", err)
	}
	link := worker.UploadLinkFrom(world.server.URL, waiting.ID)
	put := func(address string, content []byte) int {
		test.Helper()
		request, err := http.NewRequest(http.MethodPut, address, bytes.NewReader(content))
		if err != nil {
			test.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			test.Fatal(err)
		}
		_ = response.Body.Close()
		return response.StatusCode
	}

	if status := put(strings.Replace(link, "upload=", "upload=1", 1), []byte("<p>forged</p>")); status != http.StatusNotFound {
		test.Errorf("a forged address answered %d", status)
	}
	if status := put(link, nil); status != http.StatusBadRequest {
		test.Errorf("an empty upload answered %d", status)
	}
	if status := put(link, []byte("<p>the notes</p>")); status != http.StatusOK {
		test.Fatalf("the upload answered %d", status)
	}
	if status := put(link, []byte("<p>again</p>")); status != http.StatusConflict {
		test.Errorf("a second upload answered %d", status)
	}
	content, err := world.files.GetFile(test.Context(), waiting.ID)
	if err != nil || string(content) != "<p>the notes</p>" {
		test.Fatalf("the bytes kept are %q, %v", content, err)
	}
	var messages []*models.AgentMessage
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		main, err := tx.MainAgentConversation(person.ID, "drawer")
		if err != nil {
			test.Fatal(err)
		}
		if messages, err = tx.ListAgentMessages(main.ID, nil); err != nil {
			test.Fatal(err)
		}
	})
	if len(messages) != 4 || messages[3].Content != "Notes from the build" {
		test.Fatalf("the file was not posted to the main chat once: %+v", messages)
	}
}

// A page or a PDF sent to the main chat opens in place: the page in the
// sandbox an artifact is shown in. One handed over in a conversation still
// downloads.
func TestAPageSentToTheMainChatOpensInItsSandbox(test *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/attachments/attachment-3", nil)
	recorder := httptest.NewRecorder()
	page := &models.AgentAttachment{ID: "attachment-3", Name: "notes.html", ContentType: "text/html; charset=utf-8", SentBy: "Example Coder"}
	writeAgentAttachment(recorder, request, page, []byte("<p>notes</p>"))
	if !strings.HasPrefix(recorder.Header().Get("Content-Disposition"), "inline") || !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") {
		test.Errorf("a sent page: %v", recorder.Header())
	}

	recorder = httptest.NewRecorder()
	document := &models.AgentAttachment{ID: "attachment-4", Name: "report.pdf", ContentType: "application/pdf", SentBy: "Example Coder"}
	writeAgentAttachment(recorder, request, document, []byte("%PDF-1.4"))
	if recorder.Header().Get("Content-Type") != "application/pdf" || !strings.HasPrefix(recorder.Header().Get("Content-Disposition"), "inline") ||
		!strings.Contains(recorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		test.Errorf("a sent PDF: %v", recorder.Header())
	}

	recorder = httptest.NewRecorder()
	handed := &models.AgentAttachment{ID: "attachment-5", Name: "report.pdf", ContentType: "application/pdf", MessageID: models.AttachmentMessageShared}
	writeAgentAttachment(recorder, request, handed, []byte("%PDF-1.4"))
	if recorder.Header().Get("Content-Type") != "application/octet-stream" {
		test.Errorf("a PDF handed over in a conversation: %v", recorder.Header())
	}
}
