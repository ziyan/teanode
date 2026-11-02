package share_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/share"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// fakeRun is a turn with a database, a store, an agent and a
// conversation, and a computer that answers a fetch.
type fakeRun struct {
	tools.Run
	database     db.Database
	store        storage.Storage
	agent        *models.Agent
	conversation *models.AgentConversation
	computer     tools.Computer
}

func (self *fakeRun) Database() db.Database                   { return self.database }
func (self *fakeRun) Storage() storage.Storage                { return self.store }
func (self *fakeRun) Agent() *models.Agent                    { return self.agent }
func (self *fakeRun) Conversation() *models.AgentConversation { return self.conversation }
func (self *fakeRun) Headless() bool                          { return false }
func (self *fakeRun) CanAsk() bool                            { return !self.Headless() }
func (self *fakeRun) Configuration() *config.Configuration {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	return configuration
}
func (self *fakeRun) ComputersAllowed() bool              { return true }
func (self *fakeRun) AttachedComputers() []tools.Computer { return []tools.Computer{self.computer} }
func (self *fakeRun) Offered() []*tools.Tool              { return nil }

// lookBytesForTest is the tool's own cap on a picture it may look at.
const lookBytesForTest = 10 << 20

// A one-pixel PNG.
var pixel = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 0x49, 0x48, 0x44, 0x52, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89}

type fakeComputer struct{}

func (self *fakeComputer) Ask(_ context.Context, action string, args any, _ time.Duration) (json.RawMessage, error) {
	encoded, _ := json.Marshal(args)
	if action != "filesystem" || !strings.Contains(string(encoded), `"action":"fetch"`) {
		return nil, nil
	}
	if strings.Contains(string(encoded), "missing") {
		return json.RawMessage(`{"error":"no such file"}`), nil
	}
	return json.RawMessage(`{"path":"~/Pictures/cat.png","name":"cat.png","bytes":33,"content_type":"image/png","base64":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ"}`), nil
}
func (self *fakeComputer) Name() string        { return "laptop" }
func (self *fakeComputer) System() string      { return "linux" }
func (self *fakeComputer) Home() string        { return "/home/alice" }
func (self *fakeComputer) Description() string { return "" }

func find(t *testing.T, name string) *tools.Tool {
	t.Helper()
	for _, tool := range tools.Build().All() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %q", name)
	return nil
}

// A file from the computer becomes a file of the conversation, shown to
// the model when it asks to look; a file already there is named, not
// copied; what is not there is an error the model can read.
func TestShareFileHandsOverAndLooks(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	run := &fakeRun{database: database, store: store, computer: &fakeComputer{}}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "share_file")
	call := func(arguments string) (map[string]any, *tools.Result, error) {
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: json.RawMessage(arguments)})
		if err != nil {
			return nil, nil, err
		}
		var answer map[string]any
		_ = json.Unmarshal([]byte(result.Content), &answer)
		return answer, result, nil
	}

	answer, result, err := call(`{"source":"computer","path":"~/Pictures/cat.png","look":true,"caption":"the cat"}`)
	if err != nil {
		t.Fatalf("from the computer: %s", err)
	}
	id, _ := answer["attachment_id"].(string)
	if id == "" || answer["name"] != "cat.png" || answer["content_type"] != "image/png" || answer["caption"] != "the cat" || answer["url"] != "/api/v1/agent/attachments/"+id {
		t.Fatalf("the answer names the file: %v", answer)
	}
	if len(result.Images) != 1 || result.Images[0].MediaType != "image/png" || len(result.Images[0].Data) != len(pixel) {
		t.Fatalf("the picture is shown to the model: %+v", result.Images)
	}
	if !strings.HasPrefix(result.Note, "handed over") {
		t.Fatalf("the note: %q", result.Note)
	}
	content, err := store.GetFile(ctx, id)
	if err != nil || len(content) != len(pixel) {
		t.Fatalf("the bytes are kept: %d %v", len(content), err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, _ := tx.GetAgentAttachment(id)
		if stored == nil || stored.MessageID != "shared" || stored.ConversationID != run.conversation.ID {
			t.Fatalf("the row is the conversation's, marked shared: %+v", stored)
		}
	})

	// Already there: named again, not copied; looked at on request.
	again, result, err := call(`{"source":"conversation","attachment_id":"` + id + `","look":true}`)
	if err != nil || again["attachment_id"] != id || len(result.Images) != 1 {
		t.Fatalf("a file of the conversation is named: %v %v %d", again, err, len(result.Images))
	}
	if _, _, err := call(`{"source":"conversation","attachment_id":"nothing"}`); err == nil {
		t.Fatal("an unknown attachment is an error")
	}

	// A file of another conversation of the same agent is not this
	// conversation's to hand over: the ids of files in other
	// conversations are readable, and a turn here can be started by
	// somebody else in a linked group chat.
	var elsewhere *models.AgentAttachment
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		other, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationNamed, Title: "Elsewhere", LastAt: time.Now()})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		elsewhere, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: run.agent.ID, ConversationID: other.ID, MessageID: "m1", Name: "private.pdf", ContentType: "application/pdf", Size: 4})
		if err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	if _, _, err := call(`{"source":"conversation","attachment_id":"` + elsewhere.ID + `"}`); err == nil {
		t.Fatal("another conversation's file is not this one's to hand over")
	}
	// Not a picture: handed over, not looked at.
	if err := store.PutFile(ctx, "doc1", []byte("%PDF-1.4")); err != nil {
		t.Fatalf("PutFile: %s", err)
	}
	var document *models.AgentAttachment
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		document, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: run.agent.ID, ConversationID: run.conversation.ID, Name: "notes.pdf", ContentType: "application/pdf", Size: 8})
		if err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	answer, result, err = call(`{"source":"conversation","attachment_id":"` + document.ID + `","look":true}`)
	if err != nil || len(result.Images) != 0 || !strings.Contains(answer["look"].(string), "not a picture") {
		t.Fatalf("a document is not looked at: %v %v", answer, err)
	}

	// A picture in the conversation too large to look at is handed over
	// and not shown: an empty picture sent to a provider ends the turn.
	var huge *models.AgentAttachment
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		huge, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: run.agent.ID, ConversationID: run.conversation.ID, Name: "huge.png", ContentType: "image/png", Size: lookBytesForTest + 1})
		if err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	answer, result, err = call(`{"source":"conversation","attachment_id":"` + huge.ID + `","look":true}`)
	if err != nil {
		t.Fatalf("a large picture is still handed over: %s", err)
	}
	if len(result.Images) != 0 {
		t.Fatal("and never shown as an empty picture")
	}
	if !strings.Contains(answer["look"].(string), "too large") {
		t.Fatalf("and says why: %v", answer["look"])
	}

	// What the computer has not: its words, as an error.
	if _, _, err := call(`{"source":"computer","path":"~/missing.png"}`); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("the computer's refusal is relayed: %v", err)
	}
	if _, _, err := call(`{"source":"mail"}`); err == nil {
		t.Fatal("mail without an item is an error")
	}
	if _, _, err := call(`{"source":"elsewhere"}`); err == nil {
		t.Fatal("an unknown source is an error")
	}
}

// linkingRun is a run that signs addresses: a conversation, which hands one
// out beside the path when asked, or a program over MCP, whose every file
// goes by its link.
type linkingRun struct {
	*fakeRun
	isLinkInPlaceOfPath bool
}

func (self *linkingRun) SharedLink(attachmentId string) string {
	return "https://mail.example.org/api/v1/agent/attachments/" + attachmentId + "?share=signed"
}
func (self *linkingRun) IsLinkInPlaceOfPath() bool { return self.isLinkInPlaceOfPath }
func (self *linkingRun) Surface() string           { return "drawer" }

// A person who asks for a link to open elsewhere gets a signed one beside
// the path; a file handed over without asking keeps its path alone; and a
// program over MCP gets the link in place of the path.
func TestShareFileHandsOutALinkWhenAsked(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	base := &fakeRun{database: database, store: store, computer: &fakeComputer{}}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if base.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if base.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: base.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	tool := find(t, "share_file")
	share := func(run tools.Run, arguments string) map[string]any {
		t.Helper()
		result, err := tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: json.RawMessage(arguments)})
		if err != nil {
			t.Fatalf("share_file: %s", err)
		}
		var answer map[string]any
		_ = json.Unmarshal([]byte(result.Content), &answer)
		return answer
	}

	conversation := &linkingRun{fakeRun: base}
	asked := share(conversation, `{"source":"computer","path":"~/Pictures/cat.png","link":true}`)
	id, _ := asked["attachment_id"].(string)
	if asked["url"] != "/api/v1/agent/attachments/"+id || !strings.HasSuffix(asked["share_link"].(string), id+"?share=signed") {
		t.Fatalf("asked for a link, the path stays and a signed link comes beside it: %v", asked)
	}
	if unasked := share(conversation, `{"source":"computer","path":"~/Pictures/cat.png"}`); unasked["share_link"] != nil {
		t.Fatalf("not asked, no link: %v", unasked)
	}
	program := share(&linkingRun{fakeRun: base, isLinkInPlaceOfPath: true}, `{"source":"computer","path":"~/Pictures/cat.png"}`)
	if !strings.HasSuffix(program["url"].(string), "?share=signed") || program["share_link"] != nil {
		t.Fatalf("a program over MCP gets the link in place of the path: %v", program)
	}
}

// headlessRun is a run that signs addresses and has nobody present.
type headlessRun struct {
	*linkingRun
}

func (self *headlessRun) Headless() bool { return true }
func (self *headlessRun) CanAsk() bool   { return false }

// A link that opens a copy of a file with no sign-in is made only in a
// conversation with the person: a run nobody is present for is refused
// before it reads the file, whatever the file or its source.
func TestShareFileLinkIsRefusedWhenNobodyIsPresent(t *testing.T) {
	run := &headlessRun{linkingRun: &linkingRun{fakeRun: &fakeRun{computer: &fakeComputer{}}}}
	tool := find(t, "share_file")
	_, err := tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: json.RawMessage(`{"source":"computer","path":"~/Pictures/cat.png","link":true}`)})
	if err == nil || !strings.Contains(err.Error(), "conversation with the person") {
		t.Fatalf("a link in a run nobody is present for is refused: %v", err)
	}
}

// sendingRun is a run that sends files to the main chat, and remembers what
// it sent.
type sendingRun struct {
	*fakeRun
	sent       []string
	captions   []string
	senders    []string
	isProgram  bool
	uploadLink string
}

func (self *sendingRun) SendToMainChat(_ context.Context, attachmentId, caption, sentBy string) error {
	self.sent = append(self.sent, attachmentId)
	self.captions = append(self.captions, caption)
	self.senders = append(self.senders, sentBy)
	return nil
}
func (self *sendingRun) UploadLink(attachmentId string) string {
	if self.uploadLink == "" {
		return ""
	}
	return self.uploadLink + attachmentId
}
func (self *sendingRun) IsCalledByPersonsProgram() bool { return self.isProgram }

// A file sent to the main chat is a copy of its own, sent with its caption
// and who sent it; from the main chat itself it is handed over there; and a
// program uploads its bytes to the address answered.
func TestShareFileSendsToTheMainChat(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	base := &fakeRun{database: database, store: store, computer: &fakeComputer{}}
	var main, side *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if base.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if main, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: base.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		if side, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: base.agent.ID, Kind: models.AgentConversationNamed, Title: "Invoices", LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	tool := find(t, "share_file")
	share := func(ctx context.Context, run tools.Run, arguments string) (map[string]any, error) {
		t.Helper()
		result, err := tool.Run(tools.WithRun(ctx, run), &tools.Call{ID: "c1", Arguments: json.RawMessage(arguments)})
		if err != nil {
			return nil, err
		}
		var answer map[string]any
		_ = json.Unmarshal([]byte(result.Content), &answer)
		return answer, nil
	}

	// From a side conversation: a copy in no conversation yet, sent.
	base.conversation = side
	fromSide := &sendingRun{fakeRun: base}
	answer, err := share(context.Background(), fromSide, `{"source":"computer","path":"~/Pictures/cat.png","to":"main_chat","caption":"The cat"}`)
	if err != nil {
		t.Fatalf("share_file: %s", err)
	}
	if len(fromSide.sent) != 1 || fromSide.sent[0] != answer["attachment_id"] || fromSide.captions[0] != "The cat" || fromSide.senders[0] != `the conversation "Invoices"` {
		t.Fatalf("the file was not sent to the main chat with its caption and sender: %+v %v", fromSide, answer)
	}
	var copied *models.AgentAttachment
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if copied, err = tx.GetAgentAttachment(fromSide.sent[0]); err != nil {
			t.Fatal(err)
		}
	})
	if copied.ConversationID != "" || copied.Size != 33 {
		t.Errorf("the copy for the main chat is %+v", copied)
	}

	// Left out, the file is handed over here, and nothing is sent.
	if _, err := share(context.Background(), fromSide, `{"source":"computer","path":"~/Pictures/cat.png"}`); err != nil {
		t.Fatalf("share_file: %s", err)
	}
	if len(fromSide.sent) != 1 {
		t.Errorf("a file handed over here was sent to the main chat")
	}

	// In the main chat, the main chat is here.
	base.conversation = main
	inMain := &sendingRun{fakeRun: base}
	if _, err := share(context.Background(), inMain, `{"source":"computer","path":"~/Pictures/cat.png","to":"main_chat"}`); err != nil {
		t.Fatalf("share_file: %s", err)
	}
	if len(inMain.sent) != 0 {
		t.Errorf("a file in the main chat was sent there a second time")
	}

	// A program over MCP, in no conversation, uploads the bytes itself.
	base.conversation = nil
	program := &sendingRun{fakeRun: base, isProgram: true, uploadLink: "https://mail.example.org/upload/"}
	answer, err = share(tools.WithSenderName(context.Background(), "Example Coder"), program, `{"source":"upload","name":"screen.mp4","to":"main_chat"}`)
	if err != nil {
		t.Fatalf("share_file: %s", err)
	}
	if answer["upload_url"] != "https://mail.example.org/upload/"+answer["attachment_id"].(string) || len(program.sent) != 1 || program.senders[0] != "Example Coder" {
		t.Fatalf("the upload was not answered with its address and sent: %v %+v", answer, program)
	}
	var waiting *models.AgentAttachment
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if waiting, err = tx.GetAgentAttachment(program.sent[0]); err != nil {
			t.Fatal(err)
		}
	})
	if waiting.MessageID != models.AttachmentMessageUpload || waiting.ContentType != "video/mp4" {
		t.Errorf("the file waiting for its bytes is %+v", waiting)
	}
	if _, err := share(context.Background(), program, `{"source":"upload","name":"screen.mp4"}`); err == nil {
		t.Error("an upload was taken for somewhere other than the main chat")
	}
	if _, err := share(context.Background(), &sendingRun{fakeRun: base}, `{"source":"upload","name":"screen.mp4","to":"main_chat"}`); err == nil {
		t.Error("an upload was taken with no address to send its bytes to")
	}
}
