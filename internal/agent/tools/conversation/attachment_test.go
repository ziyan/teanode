package conversation_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A long file is read through a page at a time, every character once, from
// next_offset until there is none; a file that is not the agent's, or that has
// no text, gives nothing away.
func TestReadAttachmentPagesThroughALongFile(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &fakeRun{database: database}
	var long, picture, foreign *models.AgentAttachment
	text := strings.Repeat("0123456789", tools.AttachmentPageCharacters/10*2+7)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		other, err := tx.CreateUser(&models.User{Username: "bob", Name: "Bob Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		theirs, err := tx.CreateAgent(&models.Agent{UserID: other.ID, Enabled: true, Name: "Robin"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if long, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: run.agent.ID, Name: "long.txt", ContentType: "text/plain", Text: text}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
		if picture, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: run.agent.ID, Name: "boat.png", ContentType: "image/png"}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
		if foreign, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: theirs.ID, Name: "theirs.txt", ContentType: "text/plain", Text: "not yours"}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "conversation")
	call := func(arguments string) (map[string]any, *tools.Result, error) {
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: json.RawMessage(arguments)})
		if err != nil {
			return nil, nil, err
		}
		var answer map[string]any
		_ = json.Unmarshal([]byte(result.Content), &answer)
		return answer, result, nil
	}

	var gathered strings.Builder
	offset, pages := 0, 0
	for {
		answer, result, err := call(`{"action":"read_attachment","agent_attachment_id":"` + long.ID + `","offset":` + itoa(offset) + `}`)
		if err != nil {
			t.Fatalf("read_attachment: %s", err)
		}
		if !result.Untrusted {
			t.Fatal("what a file says is data, not instructions")
		}
		gathered.WriteString(answer["text"].(string))
		pages++
		next, hasNext := answer["next_offset"].(float64)
		if !hasNext {
			break
		}
		offset = int(next)
	}
	if gathered.String() != text || pages != 3 {
		t.Fatalf("read %d characters in %d pages, want %d in 3", gathered.Len(), pages, len(text))
	}

	answer, _, err := call(`{"action":"read_attachment","agent_attachment_id":"` + picture.ID + `"}`)
	if err != nil || answer["text"] != "" || answer["note"] == nil {
		t.Fatalf("a file with no text says so: %v %v", answer, err)
	}
	if _, _, err := call(`{"action":"read_attachment","agent_attachment_id":"` + foreign.ID + `"}`); err == nil {
		t.Fatal("somebody else's file was read")
	}
	if _, _, err := call(`{"action":"read_attachment"}`); err == nil {
		t.Fatal("a read with no file was answered")
	}
}

func itoa(number int) string {
	encoded, _ := json.Marshal(number)
	return string(encoded)
}
