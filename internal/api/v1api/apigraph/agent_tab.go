package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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

// tabProtocol is the newest version the extension may speak. 2 says who it
// is with a token in the hello and always comes with a tab attached; 3
// connects whenever the extension is signed in and says with hasTab whether
// there is a tab to act in, so the agent can open one in the person's
// browser when none is attached; 4 also streams a tab's picture and takes
// the person's input, for the dashboard's browser page, and lists its tabs
// with each update. An extension still speaking 2 or 3 is welcomed in its
// own, and only cannot be watched.
const tabProtocol = 4

// tabProtocolOldest is the oldest version still taken.
const tabProtocolOldest = 2

// tabSocket is the websocket as the relay sends to it.
type tabSocket struct {
	conn  *websocket.Conn
	mutex sync.Mutex
}

// Send writes a message. A write that fails closes the connection: a
// websocket that failed a write fails every one after it, and one whose
// reads still worked stayed attached, so every request to it waited for a
// reconnect that the closing is what brings.
func (self *tabSocket) Send(message []byte) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	_ = self.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := self.conn.WriteMessage(websocket.TextMessage, message); err != nil {
		_ = self.conn.Close()
		return err
	}
	return nil
}

// AgentTabQuery says whether a tab is attached.
type AgentTabQuery interface {
	// Whether the caller has a browser tab attached through the
	// extension, and which. Needs agent:use.
	ReadAgentTab(ctx context.Context) (*AgentTabView, error)
}

// AgentTabMutation hands a tab the person took over back to the agent.
type AgentTabMutation interface {
	// Give the agent back the browser tab the person took over from the
	// dashboard, with anything it should know, and answer the agent's
	// cards asking for it. Needs agent:use.
	HandBackAgentTab(ctx context.Context, arguments HandBackAgentTabArguments) (bool, error)
}

// HandBackAgentTabArguments carry the person's word to the agent.
type HandBackAgentTabArguments struct {
	Note string `json:"note" graphapi:"nullable"`
}

// AgentTabView is the attached tab, if any.
type AgentTabView struct {
	// Connected says the person's browser is connected through the
	// extension, whether or not a tab is attached.
	Connected bool   `json:"connected"`
	Attached  bool   `json:"attached"`
	Title     string `json:"title,omitempty"`
	URL       string `json:"url,omitempty"`
	Allowed   bool   `json:"allowed"`
	// Tabs are the tabs the agent may act in, as the extension last
	// listed them: the person's own and the ones it opened.
	Tabs []agent.TabListing `json:"tabs"`
	// IsTakenOver says the person holds the tab: the agent may read it,
	// not act in it, until it is handed back.
	IsTakenOver bool `json:"isTakenOver"`
}

func (self *graph) ReadAgentTab(ctx context.Context) (*AgentTabView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	allowed := configuration.Agent.Browser.AttachTabs == nil || *configuration.Agent.Browser.AttachTabs
	view := &AgentTabView{Allowed: allowed && agent.FeatureAllowed(configuration, "browser")}
	view.Tabs = []agent.TabListing{}
	if worker := self.agentWorker(); worker != nil {
		view.Attached, view.Title, view.URL = worker.TabAttached(found.ID)
		view.Connected = worker.BrowserConnected(found.ID)
		if view.Connected {
			view.Tabs = worker.TabList(found.ID)
		}
		view.IsTakenOver = worker.IsTabTakenOver(found.ID)
	}
	return view, nil
}

func (self *graph) HandBackAgentTab(ctx context.Context, arguments HandBackAgentTabArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, agent.ErrUnavailable
	}
	wasHeld := worker.HandBackTab(found.ID)
	// Every open card asking for the tab is answered: the agent asked
	// for it once per wait, and the person handed it back once.
	waiting, _, err := self.transaction(ctx).ListAgentWaitingInteractions(found.ID, 50)
	if err != nil {
		return false, err
	}
	answer := models.InteractionHandedBack
	if note := strings.TrimSpace(arguments.Note); note != "" {
		answer += "\n" + note
	}
	isAnswered := false
	for _, interaction := range waiting {
		if interaction.InteractionKind != models.InteractionTakeOver || interaction.ResolvedAt != nil {
			continue
		}
		if ok, err := self.answerInteraction(ctx, found, worker, interaction.CallID, answer, agent.RunCommand{RunID: interaction.RunID, Action: agent.CommandAnswer, CallID: interaction.CallID, Answer: answer}); err == nil && ok {
			isAnswered = true
		}
	}
	return wasHeld || isAnswered, nil
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
	// A size limit as well as the deadline below: the library's default is
	// no limit at all, and this socket is reached before the sender has
	// said who they are.
	conn.SetReadLimit(maximumRequestSize)
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
			log.Noticef("%s's browser disconnected", username)
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
			HasTab   *bool           `json:"hasTab"`
			OK       bool            `json:"ok"`
			Data     json.RawMessage `json:"data"`
			Error    string          `json:"error"`
			// What protocol 4 adds: the tabs with an update, and a
			// picture of a streamed tab with the page's size.
			Tabs     *[]extensionTabListing `json:"tabs"`
			Tab      int64                  `json:"tab"`
			Metadata struct {
				DeviceWidth  float64 `json:"deviceWidth"`
				DeviceHeight float64 `json:"deviceHeight"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}
		switch message.Type {
		case "hello":
			if attached {
				continue
			}
			if message.Protocol < tabProtocolOldest || message.Protocol > tabProtocol {
				refuse(fmt.Sprintf("this server speaks protocol %d to %d; update the extension", tabProtocolOldest, tabProtocol))
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
			// Protocol 2 connects only with a tab attached.
			hasTab := message.HasTab == nil || *message.HasTab
			worker.ConnectBrowser(found.ID, socket, message.Title, message.URL, hasTab)
			worker.SetBrowserCanCast(found.ID, socket, message.Protocol >= 4)
			if message.Tabs != nil {
				worker.UpdateTabList(found.ID, tabListingsOf(*message.Tabs))
			}
			attached = true
			welcome, _ := json.Marshal(map[string]any{"type": "welcome", "protocol": message.Protocol})
			_ = socket.Send(welcome)
			if hasTab {
				log.Noticef("%s attached their browser tab %q", username, message.Title)
			} else {
				log.Noticef("%s connected their browser, with no tab attached", username)
			}
		case "update":
			if attached {
				worker.UpdateTab(found.ID, message.Title, message.URL)
				if message.HasTab != nil {
					worker.SetTabHeld(found.ID, *message.HasTab)
				}
				if message.Tabs != nil {
					worker.UpdateTabList(found.ID, tabListingsOf(*message.Tabs))
				}
			}
		case "frame":
			// The picture comes as base64 text in data, which the results
			// use for any JSON.
			var picture string
			if attached && json.Unmarshal(message.Data, &picture) == nil && picture != "" {
				worker.TabFrame(found.ID, socket, strconv.FormatInt(message.Tab, 10), picture, message.Metadata.DeviceWidth, message.Metadata.DeviceHeight)
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

// extensionTabListing is one tab as the extension lists it, numbered.
type extensionTabListing struct {
	Tab     int64  `json:"tab"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Own     bool   `json:"own"`
	Current bool   `json:"current"`
}

// tabListingsOf names the extension's tabs as the viewers do.
func tabListingsOf(listed []extensionTabListing) []agent.TabListing {
	tabs := make([]agent.TabListing, 0, len(listed))
	for _, tab := range listed {
		tabs = append(tabs, agent.TabListing{Tab: strconv.FormatInt(tab.Tab, 10), Title: tab.Title, URL: tab.URL, IsOwn: tab.Own, IsCurrent: tab.Current})
	}
	return tabs
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
