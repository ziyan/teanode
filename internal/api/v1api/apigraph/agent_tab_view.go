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
// tab, and taking it over. The page says who it is in its first message,
// as the extension does, with a token or with the CSRF token of the
// dashboard session it runs in. Then it says which tab to watch and is
// sent its pictures; it may take the tab over and send what the person
// does in it. Handing back is HandBackAgentTab, a mutation, since it also
// answers the agent's card asking for it, which is the API's to do. Only the person's own browser is ever shown:
// the agent is found from whoever signed in.

// tabViewerReadLimit bounds what a viewer may send at once: input and
// notes, never pictures.
const tabViewerReadLimit = 64 << 10

// tabViewerQuiet is how long a viewer may say nothing: it pings every
// twenty seconds.
const tabViewerQuiet = 90 * time.Second

// tabViewerSocket is the page's websocket as the relay writes to it.
type tabViewerSocket struct {
	conn  *websocket.Conn
	mutex sync.Mutex

	// frame is the newest picture not yet written, and ready says there
	// is one; closed ends the writer.
	frameMutex sync.Mutex
	frame      []byte
	ready      chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once
}

func newTabViewerSocket(conn *websocket.Conn) *tabViewerSocket {
	return &tabViewerSocket{conn: conn, ready: make(chan struct{}, 1), closed: make(chan struct{})}
}

// Send writes a message now. A write that fails closes the socket, which
// ends the reader and with it the view.
func (self *tabViewerSocket) Send(message []byte) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	_ = self.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := self.conn.WriteMessage(websocket.TextMessage, message); err != nil {
		self.close()
		return err
	}
	return nil
}

// SendFrame keeps the picture for the writer, replacing one it has not
// written yet.
func (self *tabViewerSocket) SendFrame(message []byte) {
	self.frameMutex.Lock()
	self.frame = message
	self.frameMutex.Unlock()
	select {
	case self.ready <- struct{}{}:
	default:
	}
}

// writeFrames writes pictures as fast as the viewer takes them, the newest
// each time.
func (self *tabViewerSocket) writeFrames() {
	for {
		select {
		case <-self.closed:
			return
		case <-self.ready:
		}
		self.frameMutex.Lock()
		frame := self.frame
		self.frame = nil
		self.frameMutex.Unlock()
		if frame != nil && self.Send(frame) != nil {
			return
		}
	}
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
	CSRF  string          `json:"csrf"`
	Tab   json.RawMessage `json:"tab"`
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
	csrfCookie := ""
	if cookie, _ := request.Cookie("csrftoken"); cookie != nil {
		csrfCookie = cookie.Value
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(request *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(tabViewerReadLimit)
	socket := newTabViewerSocket(conn)
	defer socket.close()
	refuse := func(reason string) {
		refused, _ := json.Marshal(map[string]any{"type": "refused", "reason": reason})
		_ = socket.Send(refused)
	}
	var found *models.Agent
	username := ""
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
			switch {
			case message.Token != "":
				username = self.usernameOfToken(request, message.Token)
				if username == "" {
					refuse("the token is not one this server takes; sign in again")
					return
				}
			case csrfCookie != "" && message.CSRF == csrfCookie:
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
			go socket.writeFrames()
			worker.AddTabViewer(found.ID, socket)
			continue
		}
		switch message.Type {
		case "watch":
			worker.WatchTab(found.ID, socket, tabNameOf(message.Tab))
		case "take_over":
			worker.TakeOverTab(found.ID)
			log.Noticef("%s took over their browser tab from the dashboard", username)
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
