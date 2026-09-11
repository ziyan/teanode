package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// Files a person hands their agent with a turn. A picture is shown to the
// model as an image, once, in the turn it came with; a text file is read
// to it, capped; everything else — a video, a recording, an archive, a
// document nobody here can parse — is named, with its type and size and a
// line saying the agent cannot open it. Earlier turns name every file
// rather than carry it again, so a conversation with pictures in it does
// not cost the pictures every round.

// The bounds.
const (
	// attachmentTextCharacters is the most of a text file the model reads.
	attachmentTextCharacters = 60000

	// attachmentImageBytes is the largest picture handed to a model as an
	// image; the providers refuse larger ones.
	attachmentImageBytes = 10 << 20

	// attachmentImagesPerTurn bounds the pictures in one turn.
	attachmentImagesPerTurn = 8

	// orphanAttachmentAge is how long an uploaded file waits for the turn
	// it was meant for before the sweep takes it.
	orphanAttachmentAge = 24 * time.Hour
)

// imageTypes are the pictures every provider here can look at.
var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// IsImageAttachment says whether a file is a picture a model can look at.
func IsImageAttachment(contentType string) bool {
	return imageTypes[strings.ToLower(strings.TrimSpace(contentType))]
}

// isTextAttachment says whether a file is read as text.
func isTextAttachment(name, contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if index := strings.Index(contentType, ";"); index >= 0 {
		contentType = strings.TrimSpace(contentType[:index])
	}
	switch {
	case strings.HasPrefix(contentType, "text/"):
		return true
	case contentType == "application/json", contentType == "application/xml", contentType == "application/x-yaml",
		contentType == "application/yaml", contentType == "application/javascript", contentType == "application/x-sh":
		return true
	}
	lower := strings.ToLower(name)
	for _, suffix := range []string{".txt", ".md", ".markdown", ".csv", ".tsv", ".json", ".yaml", ".yml", ".xml", ".log", ".ini", ".toml", ".sh", ".py", ".go", ".js", ".ts", ".html", ".htm", ".eml"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// ExtractAttachmentText is what a model reads of a file: its text when it
// is a text file, valid UTF-8, capped; empty otherwise. Called once, when
// the file is uploaded, and kept on the row.
func ExtractAttachmentText(name, contentType string, content []byte) string {
	if !isTextAttachment(name, contentType) || !utf8.Valid(content) {
		return ""
	}
	text := strings.ToValidUTF8(string(content), "")
	if len(text) > attachmentTextCharacters {
		text = text[:attachmentTextCharacters] + "\n[cut here: the file goes on]"
	}
	return text
}

// describeAttachment is a file in a line: name, type and size.
func describeAttachment(attachment *models.AgentAttachment) string {
	kind := attachment.ContentType
	if kind == "" {
		kind = "unknown type"
	}
	return fmt.Sprintf("%s (%s, %s)", attachment.Name, kind, formatBytes(attachment.Size))
}

func formatBytes(size int64) string {
	switch {
	case size >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(size)/float64(1<<30))
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(size)/float64(1<<10))
	default:
		return fmt.Sprintf("%d bytes", size)
	}
}

// attachmentLines is how earlier turns name their files, and how a turn
// names the ones the model cannot open.
func attachmentLines(attachments []models.AgentAttachment) string {
	if len(attachments) == 0 {
		return ""
	}
	lines := make([]string, 0, len(attachments))
	for index := range attachments {
		lines = append(lines, "- "+describeAttachment(&attachments[index]))
	}
	return "<attachments>\n" + strings.Join(lines, "\n") + "\n</attachments>"
}

// referenceLines is what the person pointed at, for the model.
func referenceLines(references []models.AgentReference) string {
	if len(references) == 0 {
		return ""
	}
	lines := make([]string, 0, len(references))
	for _, reference := range references {
		parts := []string{}
		if reference.ItemID != "" {
			parts = append(parts, "item_id "+reference.ItemID)
		}
		if reference.ThreadID != "" {
			parts = append(parts, "thread_id "+reference.ThreadID)
		}
		if reference.Subject != "" {
			parts = append(parts, fmt.Sprintf("subject %q", reference.Subject))
		}
		if reference.From != "" {
			parts = append(parts, "from "+reference.From)
		}
		if len(parts) > 0 {
			lines = append(lines, "- "+strings.Join(parts, ", "))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "<references>\nThe person points at these; \"this\" means them. Read one with mail_read before answering about it.\n" + strings.Join(lines, "\n") + "\n</references>"
}

// userTurn is the person's message as the model is given it in the turn it
// was said: the references, the text, the text files read out, the files
// it cannot open named, and the pictures as image parts.
func userTurn(ctx context.Context, store storage.Storage, text string, attachments []*models.AgentAttachment, references []models.AgentReference) llm.ChatMessage {
	var blocks []string
	if lines := referenceLines(references); lines != "" {
		blocks = append(blocks, lines)
	}
	blocks = append(blocks, text)
	var images []llm.ContentPart
	var named []models.AgentAttachment
	for _, attachment := range attachments {
		switch {
		case IsImageAttachment(attachment.ContentType) && attachment.Size <= attachmentImageBytes && len(images) < attachmentImagesPerTurn && store != nil:
			content, err := store.GetFile(ctx, attachment.ID)
			if err != nil {
				named = append(named, *attachment)
				continue
			}
			images = append(images, llm.ContentPart{Type: "image", MediaType: attachment.ContentType, Data: content})
			blocks = append(blocks, fmt.Sprintf("[picture attached: %s]", describeAttachment(attachment)))
		case attachment.Text != "":
			blocks = append(blocks, fmt.Sprintf("<attachment name=%q type=%q>\n%s\n</attachment>", attachment.Name, attachment.ContentType, attachment.Text))
		default:
			named = append(named, *attachment)
		}
	}
	if len(named) > 0 {
		blocks = append(blocks, attachmentLines(named)+"\nYou cannot open these kinds of file; if what is in one matters, ask the person.")
	}
	message := llm.ChatMessage{Role: llm.RoleUser, Content: strings.Join(blocks, "\n\n")}
	if len(images) > 0 {
		message.Parts = append([]llm.ContentPart{{Type: "text", Text: message.Content}}, images...)
	}
	return message
}

// historyTurn is the same message as earlier turns carry it: references and
// text, and every file by name.
func historyTurn(message *models.AgentMessage) string {
	var blocks []string
	if lines := referenceLines(message.References); lines != "" {
		blocks = append(blocks, lines)
	}
	blocks = append(blocks, message.Content)
	if lines := attachmentLines(message.Attachments); lines != "" {
		blocks = append(blocks, lines)
	}
	return strings.Join(blocks, "\n\n")
}

// deleteAttachments removes rows and bytes.
func deleteAttachments(ctx context.Context, tx db.Transaction, store storage.Storage, attachments []*models.AgentAttachment) error {
	for _, attachment := range attachments {
		if err := tx.DeleteAgentAttachment(attachment.ID); err != nil {
			return err
		}
		if store != nil {
			if err := store.DeleteFile(ctx, attachment.ID); err != nil {
				log.Warningf("cannot remove the bytes of attachment %q: %s", attachment.ID, err)
			}
		}
	}
	return nil
}

// ForgetAttachments removes every file a person's agent holds, for the
// person forgetting their agent.
func ForgetAttachments(ctx context.Context, tx db.Transaction, store storage.Storage, agentId string) error {
	attachments, err := tx.ListAgentAttachments(agentId, "")
	if err != nil {
		return err
	}
	return deleteAttachments(ctx, tx, store, attachments)
}

// scavengeAttachments removes what was uploaded and never sent.
func (self *Agent) scavengeAttachments(ctx context.Context, tx db.Transaction, now time.Time) error {
	orphans, err := tx.ListOrphanAgentAttachments(now.Add(-orphanAttachmentAge))
	if err != nil {
		return err
	}
	return deleteAttachments(ctx, tx, self.settings.Storage, orphans)
}
