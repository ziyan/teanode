package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A text file is read to the model, capped; a picture is not text; what
// is neither is named, never read.
func TestExtractAttachmentTextReadsTextOnly(t *testing.T) {
	if got := ExtractAttachmentText("notes.txt", "text/plain", []byte("hello")); got != "hello" {
		t.Fatalf("a text file: %q", got)
	}
	if got := ExtractAttachmentText("data.csv", "", []byte("a,b\n1,2")); got != "a,b\n1,2" {
		t.Fatalf("a csv by name: %q", got)
	}
	if got := ExtractAttachmentText("photo.jpg", "image/jpeg", []byte{0xff, 0xd8, 0xff}); got != "" {
		t.Fatalf("a picture became text: %q", got)
	}
	if got := ExtractAttachmentText("clip.mp4", "video/mp4", []byte("not a video")); got != "" {
		t.Fatalf("a video became text: %q", got)
	}
	long := strings.Repeat("x", attachmentTextCharacters+100)
	if got := ExtractAttachmentText("long.txt", "text/plain", []byte(long)); len(got) > attachmentTextCharacters+100 || !strings.HasSuffix(got, "the file goes on]") {
		t.Fatalf("a long file was not cut: %d characters", len(got))
	}
}

// The turn the model is given: references first, the words, a text file
// read out, a picture as an image part, and a video named with a line
// saying it cannot be opened. Earlier turns name every file.
func TestUserTurnCarriesWhatTheModelCanRead(t *testing.T) {
	store := &fakeFiles{files: map[string][]byte{"pic": {0x89, 'P', 'N', 'G'}}}
	attachments := []*models.AgentAttachment{
		{ID: "pic", Name: "boat.png", ContentType: "image/png", Size: 4},
		{ID: "txt", Name: "notes.txt", ContentType: "text/plain", Size: 5, Text: "hello"},
		{ID: "vid", Name: "clip.mp4", ContentType: "video/mp4", Size: 1 << 20},
	}
	references := []models.AgentReference{{ItemID: "item1", ThreadID: "thread1", Subject: "Thursday?", From: "maria@example.net"}}
	message := userTurn(context.Background(), store, "What do you make of these?", attachments, references)
	for _, want := range []string{"<references>", "item_id item1", `subject "Thursday?"`, "What do you make of these?", `<attachment name="notes.txt"`, "hello", "clip.mp4 (video/mp4, 1.0 MB)", "cannot open these kinds of file", "[picture attached: boat.png"} {
		if !strings.Contains(message.Content, want) {
			t.Fatalf("the turn lacks %q:\n%s", want, message.Content)
		}
	}
	if len(message.Parts) != 2 || message.Parts[0].Type != "text" || message.Parts[1].Type != "image" || message.Parts[1].MediaType != "image/png" || len(message.Parts[1].Data) != 4 {
		t.Fatalf("parts: %+v", message.Parts)
	}
	if message.Parts[0].Text != message.Content {
		t.Fatal("the text part should be the whole content")
	}

	stored := &models.AgentMessage{Content: "What do you make of these?", References: references}
	for _, attachment := range attachments {
		stored.Attachments = append(stored.Attachments, *attachment)
	}
	history := historyTurn(stored)
	for _, want := range []string{"<references>", "<attachments>", "boat.png (image/png, 4 bytes)", "notes.txt", "clip.mp4"} {
		if !strings.Contains(history, want) {
			t.Fatalf("the history turn lacks %q:\n%s", want, history)
		}
	}
	if strings.Contains(history, "hello") {
		t.Fatal("an earlier turn should name a text file, not carry it again")
	}
}

// A turn without files or references is the words alone.
func TestUserTurnPlain(t *testing.T) {
	message := userTurn(context.Background(), nil, "hi", nil, nil)
	if message.Content != "hi" || len(message.Parts) != 0 {
		t.Fatalf("message %+v", message)
	}
}

// fakeFiles is a file store in memory.
type fakeFiles struct {
	files map[string][]byte
}

func (self *fakeFiles) Put(context.Context, string, []string, []byte) error { return nil }
func (self *fakeFiles) Get(context.Context, string) ([]string, []byte, error) {
	return nil, nil, nil
}
func (self *fakeFiles) Delete(context.Context, string) error { return nil }
func (self *fakeFiles) PutFile(_ context.Context, id string, content []byte) error {
	self.files[id] = content
	return nil
}
func (self *fakeFiles) GetFile(_ context.Context, id string) ([]byte, error) {
	content, ok := self.files[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return content, nil
}
func (self *fakeFiles) DeleteFile(_ context.Context, id string) error {
	delete(self.files, id)
	return nil
}
func (self *fakeFiles) Close() error { return nil }
