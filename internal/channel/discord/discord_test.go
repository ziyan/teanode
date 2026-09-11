package discord

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

	"github.com/gorilla/websocket"
)

// fakeDiscord is enough of Discord for the client: a gateway that says
// hello, takes an identify or a resume, sends a message, and answers
// heartbeats; and the REST calls, recorded.
type fakeDiscord struct {
	mutex      sync.Mutex
	calls      []string
	identified int
	resumed    int
	heartbeats int
	closeFirst bool
}

func (self *fakeDiscord) gateway(t *testing.T) *httptest.Server {
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 200}})
		for {
			var received map[string]any
			if err := conn.ReadJSON(&received); err != nil {
				return
			}
			op, _ := received["op"].(float64)
			switch int(op) {
			case 2:
				data := received["d"].(map[string]any)
				if data["token"] != "TOKEN" {
					_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4004, "bad token"), time.Now().Add(time.Second))
					return
				}
				self.mutex.Lock()
				self.identified++
				closeFirst := self.closeFirst
				self.closeFirst = false
				self.mutex.Unlock()
				_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]any{"session_id": "session1", "resume_gateway_url": "ws://" + request.Host}})
				if closeFirst {
					// Dropped once: the client comes back with a resume.
					_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1001, "going away"), time.Now().Add(time.Second))
					return
				}
				_ = conn.WriteJSON(map[string]any{"op": 0, "t": "MESSAGE_CREATE", "s": 2, "d": map[string]any{
					"id": "m1", "channel_id": "c1", "content": "hello <@7>", "author": map[string]any{"id": "42", "username": "alice"},
					"mentions":    []map[string]any{{"id": "7", "username": "bertie"}},
					"attachments": []map[string]any{{"id": "a1", "filename": "notes.txt", "content_type": "text/plain", "url": "http://" + request.Host + "/files/notes.txt"}},
				}})
			case 6:
				self.mutex.Lock()
				self.resumed++
				self.mutex.Unlock()
				_ = conn.WriteJSON(map[string]any{"op": 0, "t": "RESUMED", "s": 3, "d": map[string]any{}})
				_ = conn.WriteJSON(map[string]any{"op": 0, "t": "MESSAGE_CREATE", "s": 4, "d": map[string]any{
					"id": "m2", "channel_id": "c1", "content": "after the drop", "author": map[string]any{"id": "42", "username": "alice"},
				}})
			case 1:
				self.mutex.Lock()
				self.heartbeats++
				self.mutex.Unlock()
				_ = conn.WriteJSON(map[string]any{"op": 11})
			}
		}
	}))
}

func (self *fakeDiscord) rest(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		self.mutex.Lock()
		self.calls = append(self.calls, request.Method+" "+request.URL.Path+" "+string(body))
		self.mutex.Unlock()
		if request.Header.Get("Authorization") != "Bot TOKEN" {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"message":"401: Unauthorized"}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/users/@me":
			_, _ = writer.Write([]byte(`{"id":"7","username":"bertie","bot":true}`))
		case strings.HasSuffix(request.URL.Path, "/messages") && request.Method == http.MethodPost:
			_, _ = writer.Write([]byte(`{"id":"m99","channel_id":"c1"}`))
		case strings.HasPrefix(request.URL.Path, "/files/"):
			_, _ = writer.Write([]byte("the notes"))
		default:
			_, _ = writer.Write([]byte(`{}`))
		}
	}))
}

func (self *fakeDiscord) count(field string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	switch field {
	case "identified":
		return self.identified
	case "resumed":
		return self.resumed
	case "heartbeats":
		return self.heartbeats
	}
	return len(self.calls)
}

func TestGatewayIdentifiesHeartbeatsAndResumes(t *testing.T) {
	fake := &fakeDiscord{closeFirst: true}
	gateway := fake.gateway(t)
	defer gateway.Close()
	rest := fake.rest(t)
	defer rest.Close()
	client := New("TOKEN", rest.Client(), rest.URL, "ws"+strings.TrimPrefix(gateway.URL, "http"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mutex sync.Mutex
	var heard []string
	done := make(chan error, 1)
	go func() {
		done <- client.Listen(ctx, func(message *Message) {
			mutex.Lock()
			heard = append(heard, message.Content)
			mutex.Unlock()
		})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mutex.Lock()
		count := len(heard)
		mutex.Unlock()
		if count >= 1 && fake.count("heartbeats") >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(heard) == 0 || heard[0] != "after the drop" {
		t.Fatalf("after the drop the session is resumed and the message arrives: %v", heard)
	}
	if fake.count("identified") != 1 || fake.count("resumed") != 1 {
		t.Fatalf("one identify, one resume: %d %d", fake.count("identified"), fake.count("resumed"))
	}
	if fake.count("heartbeats") == 0 {
		t.Fatal("the client heartbeats")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Listen ends with the context")
	}
}

func TestRestAndTheBot(t *testing.T) {
	fake := &fakeDiscord{}
	gateway := fake.gateway(t)
	defer gateway.Close()
	rest := fake.rest(t)
	defer rest.Close()
	ctx := context.Background()
	bot, err := OpenAt(ctx, "TOKEN", rest.URL, "ws"+strings.TrimPrefix(gateway.URL, "http"))
	if err != nil || bot.Name() != "@bertie" {
		t.Fatalf("open %v %v", bot, err)
	}
	if _, err := OpenAt(ctx, "WRONG", rest.URL, gateway.URL); err == nil {
		t.Fatal("a wrong token is refused")
	}
	client := New("TOKEN", rest.Client(), rest.URL, "")
	id, err := client.Send(ctx, "c1", "hi", "m1")
	if err != nil || id != "m99" {
		t.Fatalf("send %q %v", id, err)
	}
	if err := client.Edit(ctx, "c1", "m99", "hi there"); err != nil {
		t.Fatalf("edit %v", err)
	}
	if err := client.Typing(ctx, "c1"); err != nil {
		t.Fatalf("typing %v", err)
	}
	if err := client.SendFile(ctx, "c1", "chart.png", "image/png", []byte{1}, "the chart"); err != nil {
		t.Fatalf("file %v", err)
	}
	fake.mutex.Lock()
	joined := strings.Join(fake.calls, "\n")
	fake.mutex.Unlock()
	for _, want := range []string{"POST /channels/c1/messages", `"message_reference"`, "PATCH /channels/c1/messages/m99", "POST /channels/c1/typing", "payload_json"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the calls lack %q:\n%s", want, joined)
		}
	}

}

func TestIncomingReadsAMention(t *testing.T) {
	bot := &Bot{me: &User{ID: "7", Username: "bertie"}, client: New("TOKEN", nil, "", "")}
	var message Message
	_ = json.Unmarshal([]byte(`{"id":"m1","channel_id":"c1","guild_id":"g1","content":"<@7> what needs me?","author":{"id":"42","username":"alice","global_name":"Alice"},"mentions":[{"id":"7","username":"bertie"}],"attachments":[{"id":"a1","filename":"notes.txt","content_type":"text/plain","url":"http://x/notes.txt"}]}`), &message)
	incoming := bot.incoming(&message)
	if !incoming.Group || !incoming.ToBot || incoming.Text != "what needs me?" || incoming.SenderName != "Alice" || len(incoming.Files) != 1 || incoming.Files[0].Name != "notes.txt" {
		t.Fatalf("incoming %+v", incoming)
	}
	direct := Message{ID: "m2", ChannelID: "d1", Content: "hi", Author: &User{ID: "42", Username: "alice"}}
	if got := bot.incoming(&direct); got.Group || got.ChatName != "alice" {
		t.Fatalf("a direct message: %+v", got)
	}
}
