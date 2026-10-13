package recording

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// importTestRun is a turn with a database, a store and a conversation, whose
// account API is recorded.
type importTestRun struct {
	recordingTestRun
	database     db.Database
	store        storage.Storage
	agent        *models.Agent
	conversation *models.AgentConversation
}

func (self *importTestRun) Database() db.Database                   { return self.database }
func (self *importTestRun) Storage() storage.Storage                { return self.store }
func (self *importTestRun) Agent() *models.Agent                    { return self.agent }
func (self *importTestRun) Conversation() *models.AgentConversation { return self.conversation }

// An import reads its file with share_file's permissions and hands the
// conversation attachment to the recording import, with no title.
func TestImportBringsTheFileIntoTheConversationThenImportsIt(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	store, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	ownerId := dbtest.CreateUser(test, database, "recording-import-owner")
	operations := &recordingTestOperations{responseJSON: `{"ImportRecordingAttachment":{"recording":{"id":"fixture-recording"}}}`}
	run := &importTestRun{recordingTestRun: recordingTestRun{operations: operations}, database: database, store: store}
	var attachment, elsewhere *models.AgentAttachment
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if run.agent, err = transaction.CreateAgent(&models.Agent{UserID: ownerId, Enabled: true, Name: "Fixture assistant"}); err != nil {
			test.Fatal(err)
		}
		if run.conversation, err = transaction.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			test.Fatal(err)
		}
		other, err := transaction.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			test.Fatal(err)
		}
		if attachment, err = transaction.CreateAgentAttachment(&models.AgentAttachment{AgentID: run.agent.ID, ConversationID: run.conversation.ID, Name: "standup.m4a", ContentType: "audio/mp4", Size: 4}); err != nil {
			test.Fatal(err)
		}
		if elsewhere, err = transaction.CreateAgentAttachment(&models.AgentAttachment{AgentID: run.agent.ID, ConversationID: other.ID, Name: "other.m4a", ContentType: "audio/mp4", Size: 4}); err != nil {
			test.Fatal(err)
		}
	})
	ctx := tools.WithRun(context.Background(), run)
	call := func(arguments map[string]any) (*tools.Result, error) {
		encoded, _ := json.Marshal(arguments)
		return runRecording(ctx, &tools.Call{ID: "fixture-call", Arguments: encoded})
	}
	if _, err := call(map[string]any{"operation": "import", "source": "conversation", "attachment_id": attachment.ID}); err != nil {
		test.Fatal(err)
	}
	if operations.variables["agentAttachmentId"] != attachment.ID || operations.variables["recordingTitle"] != "" {
		test.Fatal("the attachment was not imported as an untitled recording", operations.variables)
	}
	operations.variables = nil
	if _, err := call(map[string]any{"operation": "import", "source": "conversation", "attachment_id": elsewhere.ID}); err == nil || operations.variables != nil {
		test.Fatal("a file from another conversation was imported", err)
	}
}
