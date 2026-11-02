package agent

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// mainChatFixture is a worker with a person whose agent is on, and a store
// for files.
type mainChatFixture struct {
	database db.Database
	store    storage.Storage
	worker   *Agent
	owner    *models.User
	agent    *models.Agent
}

func newMainChatFixture(test *testing.T) *mainChatFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Server.Secret = "a secret long enough to seal things with"
	store, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = store.Close() })
	fixture := &mainChatFixture{database: database, store: store}
	fixture.worker = New(&Settings{Database: database, Storage: store, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if fixture.owner, err = tx.CreateUser(&models.User{Username: "robin", Name: "Robin Example", Timezone: "UTC"}); err != nil {
			test.Fatal(err)
		}
		if fixture.agent, err = tx.CreateAgent(&models.Agent{UserID: fixture.owner.ID, Enabled: true}); err != nil {
			test.Fatal(err)
		}
	})
	return fixture
}

// file keeps a file of the agent's with the bytes given.
func (self *mainChatFixture) file(test *testing.T, name, contentType string, content []byte) *models.AgentAttachment {
	test.Helper()
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: self.agent.ID, MessageID: models.AttachmentMessageShared, Name: name, ContentType: contentType, Size: int64(len(content))}); err != nil {
			test.Fatal(err)
		}
	})
	if err := self.store.PutFile(test.Context(), attachment.ID, content); err != nil {
		test.Fatal(err)
	}
	return attachment
}

// main is the person's main chat.
func (self *mainChatFixture) main(test *testing.T) *models.AgentConversation {
	test.Helper()
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if conversation, err = scheduleConversation(tx, self.agent.ID, ""); err != nil {
			test.Fatal(err)
		}
	})
	return conversation
}

func (self *mainChatFixture) messages(test *testing.T, conversationId string) []*models.AgentMessage {
	test.Helper()
	var messages []*models.AgentMessage
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if messages, err = tx.ListAgentMessages(conversationId, nil); err != nil {
			test.Fatal(err)
		}
	})
	return messages
}

func (self *mainChatFixture) attachment(test *testing.T, attachmentId string) *models.AgentAttachment {
	test.Helper()
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.GetAgentAttachment(attachmentId); err != nil {
			test.Fatal(err)
		}
	})
	return attachment
}

// A file sent to the main chat is posted there as a file the agent handed
// over: a marked line saying who sent it, the call, the file as its answer,
// and the caption, which is what the phone and a chat app are told.
func TestAFileSentToTheMainChatIsPostedAsAHandedOverFile(t *testing.T) {
	t.Parallel()
	fixture := newMainChatFixture(t)
	chart := fixture.file(t, "chart.png", "image/png", []byte("a picture"))

	if err := fixture.worker.SendFileToMainChat(t.Context(), fixture.agent.ID, chart.ID, "The week's spending", "Example Coder"); err != nil {
		t.Fatalf("SendFileToMainChat: %s", err)
	}
	main := fixture.main(t)
	messages := fixture.messages(t, main.ID)
	if len(messages) != 4 {
		t.Fatalf("the main chat has %d messages, not the four of a handed over file: %+v", len(messages), messages)
	}
	if !strings.HasPrefix(messages[0].Content, models.FileSentMarker) || !strings.Contains(messages[0].Content, "Example Coder") || !models.IsOwnTurn(messages[0].Content) {
		t.Errorf("the line does not say who sent it as the agent's own: %q", messages[0].Content)
	}
	if len(messages[1].ToolCalls) != 1 || messages[1].ToolCalls[0].Name != "share_file" {
		t.Errorf("the agent's call is not share_file's: %+v", messages[1].ToolCalls)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(messages[2].Content), &answer); err != nil {
		t.Fatalf("the file's answer is not JSON: %q", messages[2].Content)
	}
	if messages[2].ToolCallID != messages[1].ToolCalls[0].ID || answer["attachment_id"] != chart.ID || answer["url"] != "/api/v1/agent/attachments/"+chart.ID {
		t.Errorf("the file is not the call's answer in the shape the dashboard draws: %+v %v", messages[2], answer)
	}
	if messages[3].Content != "The week's spending" {
		t.Errorf("the agent says %q, not the caption", messages[3].Content)
	}
	posted := fixture.attachment(t, chart.ID)
	if posted.ConversationID != main.ID || posted.MainChatQueuedAt != nil || posted.SentBy != "Example Coder" {
		t.Errorf("the file is not the main chat's, posted: %+v", posted)
	}

	// Sent again, it is not posted twice by a sweep that finds it waiting.
	fixture.worker.postWaitingFiles(t.Context())
	if got := len(fixture.messages(t, main.ID)); got != 4 {
		t.Errorf("the sweep posted it again: %d messages", got)
	}
}

// A file sent while a turn runs in the main chat waits for the turn to end,
// and the sweep posts it then.
func TestAFileSentDuringATurnIsPostedAfterIt(t *testing.T) {
	t.Parallel()
	fixture := newMainChatFixture(t)
	main := fixture.main(t)
	report := fixture.file(t, "report.pdf", "application/pdf", []byte("%PDF-1.4"))

	fixture.worker.runsMutex.Lock()
	if fixture.worker.latest == nil {
		fixture.worker.latest = map[string]*AskRun{}
	}
	fixture.worker.latest[main.ID] = &AskRun{}
	fixture.worker.runsMutex.Unlock()
	if err := fixture.worker.SendFileToMainChat(t.Context(), fixture.agent.ID, report.ID, "", "a reminder"); err != nil {
		t.Fatalf("SendFileToMainChat: %s", err)
	}
	if got := len(fixture.messages(t, main.ID)); got != 0 {
		t.Fatalf("the file was written into a turn in progress: %d messages", got)
	}
	if waiting := fixture.attachment(t, report.ID); waiting.MainChatQueuedAt == nil {
		t.Fatal("the file is not waiting for the turn to end")
	}

	fixture.worker.runsMutex.Lock()
	delete(fixture.worker.latest, main.ID)
	fixture.worker.runsMutex.Unlock()
	fixture.worker.postWaitingFiles(t.Context())
	messages := fixture.messages(t, main.ID)
	if len(messages) != 4 || messages[3].Content != "report.pdf, from a reminder." {
		t.Fatalf("the sweep did not post the file once the turn ended: %+v", messages)
	}
}

// A file waiting for its upload is not posted until its bytes arrive, and the
// address that takes them takes no other file's, nor is it an address that
// opens a file.
func TestAnUploadAddressIsForItsOwnFileOnly(t *testing.T) {
	t.Parallel()
	fixture := newMainChatFixture(t)
	var waiting *models.AgentAttachment
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if waiting, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agent.ID, MessageID: models.AttachmentMessageUpload, Name: "screen.mp4", ContentType: "video/mp4"}); err != nil {
			t.Fatal(err)
		}
	})
	if err := fixture.worker.SendFileToMainChat(t.Context(), fixture.agent.ID, waiting.ID, "", "Example Coder"); err != nil {
		t.Fatalf("SendFileToMainChat: %s", err)
	}
	fixture.worker.postWaitingFiles(t.Context())
	if got := len(fixture.messages(t, fixture.main(t).ID)); got != 0 {
		t.Fatalf("a file with no bytes yet was posted: %d messages", got)
	}

	link := fixture.worker.UploadLinkFrom("https://teanode.example", waiting.ID)
	address, err := url.Parse(link)
	if err != nil || address.Path != "/api/v1/agent/attachments/"+waiting.ID+"/upload" {
		t.Fatalf("the upload address is %q", link)
	}
	upload := address.Query().Get("upload")
	if !fixture.worker.UploadAllowed(waiting.ID, upload) {
		t.Error("the address does not take its own file")
	}
	if fixture.worker.UploadAllowed("another-file", upload) {
		t.Error("the address takes another file")
	}
	if fixture.worker.SharedAttachment(waiting.ID, upload) {
		t.Error("the upload address opens the file")
	}
	if fixture.worker.UploadAllowed(waiting.ID, fixture.worker.ShareAttachment(waiting.ID, time.Now().Add(time.Hour))) {
		t.Error("an address that opens the file takes an upload")
	}
	if fixture.worker.UploadLinkFrom("", waiting.ID) != "" {
		t.Error("an upload address was made with no origin")
	}
}
