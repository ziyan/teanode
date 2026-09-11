package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The tab relay: the browser extension attaches the person's own tab here,
// over a websocket the session cookie authenticates and the CSRF token
// confirms. The server carries the agent's requests across and the
// extension's answers back; the page stays on the person's screen.

// tabProtocol is the version the extension must speak.
const tabProtocol = 1

// tabSocket is the websocket as the relay sends to it.
type tabSocket struct {
	conn  *websocket.Conn
	mutex sync.Mutex
}

func (self *tabSocket) Send(message []byte) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	_ = self.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return self.conn.WriteMessage(websocket.TextMessage, message)
}

// AgentTabQuery says whether a tab is attached.
type AgentTabQuery interface {
	// Whether the caller has a browser tab attached through the
	// extension, and which. Needs agent:use.
	ReadAgentTab(ctx context.Context) (*AgentTabView, error)
}

// AgentTabView is the attached tab, if any.
type AgentTabView struct {
	Attached bool   `json:"attached"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Allowed  bool   `json:"allowed"`
}

func (self *graph) ReadAgentTab(ctx context.Context) (*AgentTabView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	allowed := configuration.Agent.Browser.AttachTabs == nil || *configuration.Agent.Browser.AttachTabs
	view := &AgentTabView{Allowed: allowed && agent.FeatureAllowed(configuration, "browser")}
	if worker := self.agentWorker(); worker != nil {
		view.Attached, view.Title, view.URL = worker.TabAttached(found.ID)
	}
	return view, nil
}

// tabView is the extension's websocket.
func (self *graph) tabView(response http.ResponseWriter, request *http.Request) {
	configuration := self.config.Current()
	worker := self.agentWorker()
	if worker == nil || !agent.FeatureAllowed(configuration, "browser") || (configuration.Agent.Browser.AttachTabs != nil && !*configuration.Agent.Browser.AttachTabs) {
		http.Error(response, "attaching a tab is off on this server", http.StatusForbidden)
		return
	}
	username := api.UsernameFromRequest(request)
	if username == "" || username == localUsername {
		http.Error(response, "sign in to the dashboard first", http.StatusUnauthorized)
		return
	}
	user, err := self.database.GetUserByUsername(username)
	if err != nil || user == nil || user.Disabled() {
		http.Error(response, "sign in to the dashboard first", http.StatusUnauthorized)
		return
	}
	var found *models.Agent
	if err := self.database.Transaction(func(tx db.Transaction) error {
		permissions, err := tx.EffectivePermissions(user.ID)
		if err != nil {
			return err
		}
		if !permissions.Has(models.PermissionAgentUse) {
			return fmt.Errorf("no agent")
		}
		found, err = tx.GetAgentByUser(user.ID)
		return err
	}); err != nil || found == nil || !found.Active() {
		http.Error(response, "you have no agent to attach a tab to", http.StatusForbidden)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(request *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	socket := &tabSocket{conn: conn}
	csrfCookie := ""
	if cookie, _ := request.Cookie("csrftoken"); cookie != nil {
		csrfCookie = cookie.Value
	}
	attached := false
	defer func() {
		if attached {
			worker.DetachTab(found.ID, socket)
			log.Noticef("%s detached their browser tab", username)
		}
	}()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var message struct {
			Type     string          `json:"type"`
			Protocol int             `json:"protocol"`
			CSRF     string          `json:"csrf"`
			ID       int64           `json:"id"`
			Title    string          `json:"title"`
			URL      string          `json:"url"`
			OK       bool            `json:"ok"`
			Data     json.RawMessage `json:"data"`
			Error    string          `json:"error"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}
		switch message.Type {
		case "hello":
			if csrfCookie == "" || message.CSRF != csrfCookie {
				refused, _ := json.Marshal(map[string]any{"type": "refused", "reason": "the CSRF token does not match"})
				_ = socket.Send(refused)
				return
			}
			if message.Protocol != tabProtocol {
				refused, _ := json.Marshal(map[string]any{"type": "refused", "reason": fmt.Sprintf("this server speaks protocol %d", tabProtocol)})
				_ = socket.Send(refused)
				return
			}
			worker.AttachTab(found.ID, socket, message.Title, message.URL)
			attached = true
			welcome, _ := json.Marshal(map[string]any{"type": "welcome", "protocol": tabProtocol})
			_ = socket.Send(welcome)
			log.Noticef("%s attached their browser tab %q", username, message.Title)
		case "update":
			if attached {
				worker.UpdateTab(found.ID, message.Title, message.URL)
			}
		case "result":
			if attached {
				worker.TabAnswered(found.ID, message.ID, message.OK, message.Data, message.Error)
			}
		case "bye":
			return
		}
	}
}
