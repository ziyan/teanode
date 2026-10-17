package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// chatReadThroughKey is where a chat source's cursor remembers the newest
// message it has filed. The ingest pass keeps the keys it does not know, and
// db.SetChatAgentReading sets it to the newest message there is when the
// member switches the reading on, so that neither what was said before nor
// what is said while it is off is read.
const chatReadThroughKey = "chatReadThrough"

// readChat files what was said in a chat since the last pass, for the agent
// of a member who let it remember the chat. A pass takes the messages after
// the newest one filed and files each day's as a document of its own, so a
// day said over several passes is several documents rather than one that is
// read again: what is filed is not changed, and the dream consolidates the
// rest like any other chat.
//
// It reads as the member does: what was said while they were in the chat, by
// anybody, their own agent included. It stops when they leave or switch it
// off, which also switches the source off (db.switchOffChatAgent).
func (self *Agent) readChat(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, cursor map[string]any) (string, db.SourceCounts, error) {
	counts := db.SourceCounts{}
	chatID := source.Specification.ChatID
	if chatID == "" {
		return "", counts, fmt.Errorf("which chat?")
	}
	after, _ := cursor[chatReadThroughKey].(string)
	var chat *models.Chat
	var messages []*models.ChatMessage
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if chat, err = tx.GetChat(chatID); err != nil || chat == nil {
			return err
		}
		member := chat.Member(run.Owner.ID)
		if member == nil || member.IsRemoved || !member.IsAgentReading {
			chat = nil
			return nil
		}
		messages, err = tx.ListChatMessagesAfter(chatID, after, ingestEntries)
		return err
	}); err != nil {
		return "", counts, err
	}
	if chat == nil || len(messages) == 0 {
		return "", counts, nil
	}
	title := db.ChatViewOf(chat, run.Owner.ID).Title
	location := Location(run.Owner)
	for start := 0; start < len(messages); {
		// One document for a run of messages on the same day, in the zone
		// of the person who reads them.
		day := messages[start].CreatedAt.In(location).Format("2006-01-02")
		end := start
		for end < len(messages) && messages[end].CreatedAt.In(location).Format("2006-01-02") == day {
			end++
		}
		if err := self.checkSourceRead(ctx, source); err != nil {
			return "", counts, err
		}
		written, err := self.fileChatDay(ctx, run, source, chat, title, day, messages[start:end])
		if err != nil {
			return "", counts, err
		}
		counts.Documents++
		counts.Chunks += written
		start = end
	}
	cursor[chatReadThroughKey] = messages[len(messages)-1].ID
	// A full page means there may be more.
	if len(messages) == ingestEntries {
		return messages[len(messages)-1].ID, counts, nil
	}
	return "", counts, nil
}

// fileChatDay files one run of a day's messages as a document.
func (self *Agent) fileChatDay(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, chat *models.Chat, title, day string, messages []*models.ChatMessage) (int, error) {
	location := Location(run.Owner)
	var text strings.Builder
	fmt.Fprintf(&text, "A chat with other people: %s, %s.\n\n", title, day)
	speakers := []string{}
	for _, message := range messages {
		name := message.AuthorName
		if message.IsByAgent() {
			name += " (agent)"
		}
		if !slices.Contains(speakers, message.AuthorName) {
			speakers = append(speakers, message.AuthorName)
		}
		fmt.Fprintf(&text, "[%s] %s: %s", message.CreatedAt.In(location).Format("15:04"), name, strings.TrimSpace(message.Body))
		for _, attachment := range message.Attachments {
			fmt.Fprintf(&text, " (sent a file: %s)", attachment.Name)
		}
		text.WriteString("\n")
	}
	happened := messages[0].CreatedAt
	hash := sha256.Sum256([]byte(text.String()))
	entry := computer.ScanEntry{
		ExternalID: chat.ID + "/" + messages[0].ID,
		Kind:       "chat",
		Title:      fmt.Sprintf("Chat %s, %s", title, day),
		HappenedAt: &happened,
		Hash:       hex.EncodeToString(hash[:]),
		Text:       text.String(),
		Size:       int64(text.Len()),
		// Only its people can read it.
		Private:  true,
		Metadata: map[string]any{"chat": title, "people": speakers, "from": messages[0].CreatedAt.Format(time.RFC3339)},
	}
	return self.fileDocument(ctx, run, source, entry, "")
}
