package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTelegram is enough of the Bot API for the client: what was called,
// and canned answers.
type fakeTelegram struct {
	mutex   sync.Mutex
	calls   []string
	bodies  []string
	tooMany int
	badMark bool
}

func (self *fakeTelegram) serve(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		method := request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:]
		self.mutex.Lock()
		self.calls = append(self.calls, method)
		self.bodies = append(self.bodies, string(body))
		tooMany := self.tooMany
		if tooMany > 0 {
			self.tooMany--
		}
		self.mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(request.URL.Path, "/file/") {
			_, _ = writer.Write([]byte("the file's bytes"))
			return
		}
		if !strings.HasPrefix(request.URL.Path, "/botTOKEN/") {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
			return
		}
		if tooMany > 0 {
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`))
			return
		}
		switch method {
		case "getMe":
			_, _ = writer.Write([]byte(`{"ok":true,"result":{"id":7,"is_bot":true,"first_name":"Bertie","username":"bertie_bot"}}`))
		case "getUpdates":
			_, _ = writer.Write([]byte(`{"ok":true,"result":[{"update_id":10,"message":{"message_id":1,"from":{"id":42,"first_name":"Alice"},"chat":{"id":42,"type":"private"},"text":"hello"}}]}`))
		case "sendMessage":
			if self.badMark && strings.Contains(string(body), `"parse_mode"`) {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`))
				return
			}
			_, _ = writer.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":42,"type":"private"}}}`))
		case "editMessageText":
			_, _ = writer.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`))
		case "getFile":
			_, _ = writer.Write([]byte(`{"ok":true,"result":{"file_id":"f1","file_path":"documents/notes.txt"}}`))
		default:
			_, _ = writer.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
}

func (self *fakeTelegram) called() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string(nil), self.calls...)
}

func TestClientSpeaksTheBotAPI(t *testing.T) {
	fake := &fakeTelegram{badMark: true}
	server := fake.serve(t)
	defer server.Close()
	client := New("TOKEN", server.Client(), server.URL)
	ctx := context.Background()

	identity, err := client.Me(ctx)
	if err != nil || identity.Username != "bertie_bot" || identity.Name() != "@bertie_bot" {
		t.Fatalf("identity %+v %v", identity, err)
	}
	updates, err := client.Poll(ctx, 0, time.Second)
	if err != nil || len(updates) != 1 || updates[0].Message.Said() != "hello" || updates[0].Message.Group() {
		t.Fatalf("poll %+v %v", updates, err)
	}
	// Markdown refused: sent again as plain text.
	id, err := client.Send(ctx, 42, "*hi*", 1, true)
	if err != nil || id != 99 {
		t.Fatalf("send %d %v", id, err)
	}
	calls := fake.called()
	if calls[len(calls)-2] != "sendMessage" || calls[len(calls)-1] != "sendMessage" {
		t.Fatalf("the plain send should follow the refused markdown one: %v", calls)
	}
	if strings.Contains(fake.bodies[len(fake.bodies)-1], "parse_mode") {
		t.Fatal("the second send is plain")
	}
	// An edit to the same text is not an error.
	if err := client.Edit(ctx, 42, 99, "same", false); err != nil {
		t.Fatalf("edit %v", err)
	}
	if err := client.Typing(ctx, 42); err != nil {
		t.Fatalf("typing %v", err)
	}
	content, err := client.Download(ctx, "f1")
	if err != nil || string(content) != "the file's bytes" {
		t.Fatalf("download %q %v", content, err)
	}
	if err := client.SendFile(ctx, 42, "chart.png", "image/png", []byte{1, 2, 3}, "the chart"); err != nil {
		t.Fatalf("send file %v", err)
	}
	calls = fake.called()
	if calls[len(calls)-1] != "sendPhoto" {
		t.Fatalf("a picture goes as a photo: %v", calls)
	}
	if err := client.SendFile(ctx, 42, "notes.txt", "text/plain", []byte("x"), ""); err != nil || fake.called()[len(fake.called())-1] != "sendDocument" {
		t.Fatalf("a file goes as a document: %v", err)
	}
	if err := client.SendFile(ctx, 42, "clip.mp4", "video/mp4", []byte{1}, ""); err != nil || fake.called()[len(fake.called())-1] != "sendVideo" {
		t.Fatalf("a video goes as a video: %v", err)
	}
	if err := client.SendFile(ctx, 42, "big.png", "image/png", make([]byte, photoBytes+1), ""); err != nil || fake.called()[len(fake.called())-1] != "sendDocument" {
		t.Fatalf("a picture too large for a photo goes as a document: %v", err)
	}
	// A 429 is waited out once.
	fake.mutex.Lock()
	fake.tooMany = 1
	fake.mutex.Unlock()
	started := time.Now()
	if _, err := client.Send(ctx, 42, "again", 0, false); err != nil {
		t.Fatalf("after a 429: %v", err)
	}
	if time.Since(started) < time.Second {
		t.Fatal("the retry waits for as long as Telegram asked")
	}
	// A wrong token is refused with the code.
	wrong := New("OTHER", server.Client(), server.URL)
	var refused *Error
	if _, err := wrong.Me(ctx); err == nil || !asError(err, &refused) || refused.Code != 401 {
		t.Fatalf("a wrong token: %v", err)
	}
}

func asError(err error, target **Error) bool {
	refused, ok := err.(*Error)
	if ok {
		*target = refused
	}
	return ok
}

func TestMessageAttachmentsAndNames(t *testing.T) {
	var message Message
	_ = json.Unmarshal([]byte(`{"message_id":3,"from":{"id":1,"first_name":"Ada","last_name":"Lovelace"},"chat":{"id":-100,"type":"supergroup","title":"The club"},"caption":"look","photo":[{"file_id":"small","file_size":10},{"file_id":"big","file_size":100}],"document":{"file_id":"d","file_name":"notes.pdf","mime_type":"application/pdf"}}`), &message)
	files := message.Attachments()
	if len(files) != 2 || files[0].FileID != "big" || files[0].FileName != "photo.jpg" || files[1].FileName != "notes.pdf" {
		t.Fatalf("attachments %+v", files)
	}
	if message.Said() != "look" || !message.Group() || message.ChatName() != "The club" || message.From.Name() != "Ada Lovelace" {
		t.Fatalf("words %q group %v chat %q from %q", message.Said(), message.Group(), message.ChatName(), message.From.Name())
	}
}
