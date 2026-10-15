package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// herdrFilesTestStorage holds files by id, and nothing else.
type herdrFilesTestStorage struct {
	storage.Storage
	contents map[string][]byte
}

func (self *herdrFilesTestStorage) GetFile(_ context.Context, id string) ([]byte, error) {
	content, ok := self.contents[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return content, nil
}

func TestAHerdrSendCarriesOnlyFilesItsCallerMayHand(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	files := &herdrFilesTestStorage{contents: map[string][]byte{}}
	var person, stranger *models.Agent
	var here, unsent, elsewhere, foreign, large *models.AgentAttachment
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		owner, err := transaction.CreateUser(&models.User{Username: "fixture-owner"})
		if err != nil {
			test.Fatal(err)
		}
		other, err := transaction.CreateUser(&models.User{Username: "fixture-other"})
		if err != nil {
			test.Fatal(err)
		}
		if person, err = transaction.CreateAgent(&models.Agent{UserID: owner.ID}); err != nil {
			test.Fatal(err)
		}
		if stranger, err = transaction.CreateAgent(&models.Agent{UserID: other.ID}); err != nil {
			test.Fatal(err)
		}
		conversation, err := transaction.CreateAgentConversation(&models.AgentConversation{AgentID: person.ID, Kind: models.AgentConversationNamed})
		if err != nil {
			test.Fatal(err)
		}
		otherConversation, err := transaction.CreateAgentConversation(&models.AgentConversation{AgentID: person.ID, Kind: models.AgentConversationNamed})
		if err != nil {
			test.Fatal(err)
		}
		create := func(attachment *models.AgentAttachment, content string) *models.AgentAttachment {
			created, err := transaction.CreateAgentAttachment(attachment)
			if err != nil {
				test.Fatal(err)
			}
			files.contents[created.ID] = []byte(content)
			return created
		}
		here = create(&models.AgentAttachment{AgentID: person.ID, ConversationID: conversation.ID, Name: "screen.png", Size: 7}, "picture")
		unsent = create(&models.AgentAttachment{AgentID: person.ID, Name: "notes.txt", Size: 5}, "notes")
		elsewhere = create(&models.AgentAttachment{AgentID: person.ID, ConversationID: otherConversation.ID, Name: "other.png", Size: 5}, "other")
		foreign = create(&models.AgentAttachment{AgentID: stranger.ID, Name: "theirs.png", Size: 6}, "theirs")
		large = create(&models.AgentAttachment{AgentID: person.ID, ConversationID: conversation.ID, Name: "large.bin", Size: HerdrFileBytes}, "large")
	})
	ctx := test.Context()

	sent, err := HerdrFilesOf(ctx, database, files, person.ID, here.ConversationID, []string{here.ID, unsent.ID})
	if err != nil {
		test.Fatal(err)
	}
	if len(sent) != 2 || sent[0].FileName != "screen.png" || string(sent[0].FileContent) != "picture" || sent[1].FileName != "notes.txt" {
		test.Errorf("%+v", sent)
	}
	// Another conversation's file is not this one's to send.
	if _, err := HerdrFilesOf(ctx, database, files, person.ID, here.ConversationID, []string{elsewhere.ID}); err == nil {
		test.Error("another conversation's file was sent")
	}
	// The person's own call names any of their agent's files, and no one
	// else's.
	if _, err := HerdrFilesOf(ctx, database, files, person.ID, "", []string{elsewhere.ID}); err != nil {
		test.Errorf("the person's own file: %v", err)
	}
	if _, err := HerdrFilesOf(ctx, database, files, person.ID, "", []string{foreign.ID}); err == nil {
		test.Error("another agent's file was sent")
	}
	if _, err := HerdrFilesOf(ctx, database, files, person.ID, "", []string{here.ID, large.ID}); err == nil || !strings.Contains(err.Error(), "more than") {
		test.Errorf("too large together: %v", err)
	}
	tooMany := make([]string, HerdrFilesMost+1)
	for index := range tooMany {
		tooMany[index] = here.ID
	}
	if _, err := HerdrFilesOf(ctx, database, files, person.ID, "", tooMany); err == nil {
		test.Error("too many files were sent")
	}
}
