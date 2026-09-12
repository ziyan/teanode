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
// over a websocket. The extension says who it is in its first message, with
// the token it signed in with; a dashboard session with its CSRF token is
// taken too. The server carries the agent's requests across and the
// extension's answers back; the page stays on the person's screen.

// tabProtocol is the version the extension must speak. 2 says who it is
// with a token in the hello.
const tabProtocol = 2

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
	// Who this is comes with the hello, so the socket is opened first and
	// refused in words the extension can show.
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
	defer func() { _ = conn.Close() }()
	socket := &tabSocket{conn: conn}
	refuse := func(reason string) {
		refused, _ := json.Marshal(map[string]any{"type": "refused", "reason": reason})
		_ = socket.Send(refused)
	}
	var found *models.Agent
	username := ""
	attached := false
	defer func() {
		if attached {
			worker.DetachTab(found.ID, socket)
			log.Noticef("%s detached their browser tab", username)
		}
	}()
	for {
		// Whoever has not said who they are gets a few seconds, not the
		// five minutes an attached tab may stay silent for.
		if attached {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		} else {
			_ = conn.SetReadDeadline(time.Now().Add(helloWait))
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var message struct {
			Type     string          `json:"type"`
			Protocol int             `json:"protocol"`
			Token    string          `json:"token"`
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
			if attached {
				continue
			}
			if message.Protocol != tabProtocol {
				refuse(fmt.Sprintf("this server speaks protocol %d; update the extension", tabProtocol))
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
				refuse("sign in first: the hello carried no token, and no dashboard session matched")
				return
			}
			var err error
			found, err = self.agentOfPerson(username)
			if err != nil {
				refuse(err.Error())
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
		case "ping":
			// What keeps the extension's worker, and this socket, alive
			// while nothing else is said.
			pong, _ := json.Marshal(map[string]any{"type": "pong"})
			_ = socket.Send(pong)
		case "result":
			if attached {
				worker.TabAnswered(found.ID, socket, message.ID, message.OK, message.Data, message.Error)
			}
		case "bye":
			return
		}
	}
}

// helloWait is how long a socket has to say who it is.
const helloWait = 15 * time.Second

// usernameOfToken is whose a token is, verified exactly as an Authorization
// header would be: the request it came on, with that header set, is put to
// the authenticator. Empty when the token is not one this server takes, or
// is the server's own local token, which is nobody.
func (self *graph) usernameOfToken(request *http.Request, token string) string {
	if self.authenticator == nil {
		return ""
	}
	carrier := request.Clone(request.Context())
	carrier.Header = http.Header{}
	carrier.Header.Set("Authorization", "Bearer "+token)
	// Where the token is used from is recorded on it; that is read from
	// these, which the socket's request carries.
	for _, name := range []string{"X-Forwarded-For", "X-Real-IP", "User-Agent"} {
		if value := request.Header.Get(name); value != "" {
			carrier.Header.Set(name, value)
		}
	}
	username, ok := self.authenticator.Authenticate(carrier)
	if !ok || username == "" || username == localUsername {
		return ""
	}
	return username
}

// agentOfPerson is the agent a device attaches to: the person must exist,
// be allowed to use an agent, and have one that is on.
func (self *graph) agentOfPerson(username string) (*models.Agent, error) {
	if username == "" || username == localUsername {
		return nil, fmt.Errorf("sign in first")
	}
	user, err := self.database.GetUserByUsername(username)
	if err != nil || user == nil || user.Disabled() {
		return nil, fmt.Errorf("sign in first")
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
		return nil, fmt.Errorf("you have no agent to attach to")
	}
	return found, nil
}
