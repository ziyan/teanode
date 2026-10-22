package apigraph

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// The dashboard's browser page: a live view of the person's own browser
// tab, and taking it over. Who it is comes from its first message: a token,
// or nothing, and then the dashboard session whose cookie came with the
// upgrade, from a page of this server only, as the GraphQL socket takes it. Then it says which tab to watch and is
// sent its pictures; it may take the tab over and send what the person
// does in it. Handing back is HandBackAgentTab, a mutation, since it also
// answers the agent's card asking for it, which is the API's to do. Only the person's own browser is ever shown:
// the agent is found from whoever signed in.

// tabViewerReadLimit bounds what a viewer may send at once: input, and
// pasted text, which the page sends in pieces well under this; never
// pictures.
const tabViewerReadLimit = 256 << 10

// tabViewerQuiet is how long a viewer may say nothing: it pings every
// twenty seconds.
const tabViewerQuiet = 90 * time.Second

// tabViewerSocket is the page's websocket as the relay writes to it. One
// goroutine writes; the relay only queues, so a viewer on a slow network
// never holds up the extension's socket, a turn, or a hand-back.
type tabViewerSocket struct {
	conn *websocket.Conn
	// writeMutex keeps the writer and a refusal from writing at once.
	writeMutex sync.Mutex

	// messages are the ones a viewer must not miss, in order; a viewer
	// too far behind to take them is closed. pendingState and pendingFrame
	// are the newest state and picture not yet written, which replace older
	// ones,
	// and ready says there is one.
	messages     chan []byte
	frameMutex   sync.Mutex
	pendingState []byte
	pendingFrame []byte
	ready        chan struct{}
	closed       chan struct{}
	closeOnce    sync.Once
}

// tabViewerBacklog is how many messages may wait for a viewer.
const tabViewerBacklog = 64

func newTabViewerSocket(conn *websocket.Conn) *tabViewerSocket {
	socket := &tabViewerSocket{conn: conn, messages: make(chan []byte, tabViewerBacklog), ready: make(chan struct{}, 1), closed: make(chan struct{})}
	go socket.write()
	return socket
}

// Send queues a message for the writer.
func (self *tabViewerSocket) Send(message []byte) error {
	select {
	case <-self.closed:
		return websocket.ErrCloseSent
	default:
	}
	select {
	case self.messages <- message:
		return nil
	default:
		self.close()
		return websocket.ErrCloseSent
	}
}

// SendState keeps how things stand for the writer, replacing a state it
// has not written yet.
func (self *tabViewerSocket) SendState(message []byte) {
	self.frameMutex.Lock()
	self.pendingState = message
	self.frameMutex.Unlock()
	self.wake()
}

// SendFrame keeps the picture for the writer, replacing one it has not
// written yet.
func (self *tabViewerSocket) SendFrame(message []byte) {
	self.frameMutex.Lock()
	self.pendingFrame = message
	self.frameMutex.Unlock()
	self.wake()
}

// wake tells the writer there is a state or a picture.
func (self *tabViewerSocket) wake() {
	select {
	case self.ready <- struct{}{}:
	default:
	}
}

// write writes what is queued: messages, then the newest state, then the
// newest picture. A write that fails closes the socket, which ends the
// reader and with it the view.
func (self *tabViewerSocket) write() {
	for {
		var batch [][]byte
		select {
		case <-self.closed:
			return
		case message := <-self.messages:
			batch = append(batch, message)
		case <-self.ready:
			self.frameMutex.Lock()
			batch = append(batch, self.pendingState, self.pendingFrame)
			self.pendingState, self.pendingFrame = nil, nil
			self.frameMutex.Unlock()
		}
		for _, message := range batch {
			if message == nil {
				continue
			}
			if err := self.writeNow(message); err != nil {
				self.close()
				return
			}
		}
	}
}

// writeNow writes one message, waiting up to ten seconds.
func (self *tabViewerSocket) writeNow(message []byte) error {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	_ = self.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return self.conn.WriteMessage(websocket.TextMessage, message)
}

// refuse says why the view is not opened, written before the socket is
// closed behind it.
func (self *tabViewerSocket) refuse(reason string) {
	refused, _ := json.Marshal(map[string]any{"type": "refused", "reason": reason})
	_ = self.writeNow(refused)
}

func (self *tabViewerSocket) close() {
	self.closeOnce.Do(func() {
		close(self.closed)
		_ = self.conn.Close()
	})
}

// tabViewerMessage is what the page sends: who it is, what it watches,
// taking over and handing back, and what the person does in the tab.
type tabViewerMessage struct {
	Type  string          `json:"type"`
	Token string          `json:"token"`
	Tab   json.RawMessage `json:"tab"`
	// For viewport: the size of the page's picture, in CSS pixels.
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	agent.TabInputMessage
}

// tabNameOf reads a tab as the page names it: text, or a number from a
// page that kept the extension's.
func tabNameOf(raw json.RawMessage) string {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return name
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

// tabViewerView is the browser page's websocket.
func (self *graph) tabViewerView(response http.ResponseWriter, request *http.Request) {
	configuration := self.config.Current()
	worker := self.agentWorker()
	if worker == nil || !agent.FeatureAllowed(configuration, "browser") || (configuration.Agent.Browser.AttachTabs != nil && !*configuration.Agent.Browser.AttachTabs) {
		http.Error(response, "attaching a tab is off on this server", http.StatusForbidden)
		return
	}
	sessionUsername := api.UsernameFromRequest(request)
	upgrader := websocket.Upgrader{CheckOrigin: func(request *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(tabViewerReadLimit)
	socket := newTabViewerSocket(conn)
	defer socket.close()
	refuse := socket.refuse
	var found *models.Agent
	username := ""
	var credential *socketCredential
	defer func() {
		if found != nil {
			worker.RemoveTabViewer(found.ID, socket)
		}
	}()
	for {
		if found != nil {
			_ = conn.SetReadDeadline(time.Now().Add(tabViewerQuiet))
		} else {
			_ = conn.SetReadDeadline(time.Now().Add(helloWait))
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var message tabViewerMessage
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}
		if found == nil {
			if message.Type != "hello" {
				refuse("say hello first")
				return
			}
			// What signed the socket in, kept to ask about again: the token,
			// or none for the cookie the upgrade carried.
			credentialToken := message.Token
			switch {
			case message.Token != "":
				username = self.usernameOfToken(request, message.Token)
				if username == "" {
					refuse("the token is not one this server takes; sign in again")
					return
				}
			case sessionUsername != "" && request.Header.Get("Origin") != "" && api.FromThisServer(request):
				// A cookie the browser attached by itself: only a page of
				// this server may open the view with it. An operator signed
				// in as somebody does not watch or work their browser.
				if api.ImpersonatorUsernameFromRequest(request) != "" {
					refuse("an operator signed in as somebody cannot watch their browser")
					return
				}
				username = sessionUsername
			default:
				refuse("sign in first")
				return
			}
			found, err = self.agentOfPerson(username)
			if err != nil {
				refuse(err.Error())
				return
			}
			credential = self.newSocketCredential(request, credentialToken, username)
			worker.AddTabViewer(found.ID, socket)
			continue
		}
		// A token revoked or a session ended since the hello, or an account
		// since disabled, ends the view at the next message after the interval.
		if !credential.StillHolds() {
			refuse("you are not signed in any more; sign in again")
			return
		}
		switch message.Type {
		case "watch":
			worker.WatchTab(found.ID, socket, tabNameOf(message.Tab))
		case "take_over":
			if worker.TakeOverTab(found.ID) {
				log.Noticef("%s took over their browser tab from the dashboard", username)
			}
		case "viewport":
			worker.SetTabViewport(found.ID, socket, message.Width, message.Height)
		case "pointer", "wheel", "key", "text":
			// The type is read into the outer message; the input carries it on.
			input := message.TabInputMessage
			input.Type = message.Type
			worker.TabInput(found.ID, socket, input)
		case "ping":
			pong, _ := json.Marshal(map[string]any{"type": "pong"})
			_ = socket.Send(pong)
		}
	}
}
