package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/models"
)

// The computer relay: `teanode computer` attaches the person's own computer
// here, over a websocket, saying who it is with the token the command line
// signed in with. The server carries the agent's shell and filesystem
// requests across and the program's answers back.

// computerProtocol is the version the program must speak.
const computerProtocol = 1

// AgentComputerQuery says which computers are attached.
type AgentComputerQuery interface {
	// The computers the caller has attached through `teanode computer`,
	// and whether the operator allows any. Needs agent:use.
	ReadAgentComputers(ctx context.Context) (*AgentComputersView, error)
}

// AgentComputersView is what is attached.
type AgentComputersView struct {
	Allowed   bool                `json:"allowed"`
	Computers []AgentComputerView `json:"computers"`
}

// AgentComputerView is one attached computer.
type AgentComputerView struct {
	Name   string    `json:"name"`
	System string    `json:"system,omitempty"`
	Since  time.Time `json:"since"`
}

func (self *graph) ReadAgentComputers(ctx context.Context) (*AgentComputersView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	view := &AgentComputersView{Allowed: agent.FeatureAllowed(configuration, "computer"), Computers: []AgentComputerView{}}
	if worker := self.agentWorker(); worker != nil {
		for _, computer := range worker.ComputersAttached(found.ID) {
			view.Computers = append(view.Computers, AgentComputerView{Name: computer.Name, System: computer.System, Since: computer.Since})
		}
	}
	return view, nil
}

// computerView is the program's websocket.
func (self *graph) computerView(response http.ResponseWriter, request *http.Request) {
	configuration := self.config.Current()
	worker := self.agentWorker()
	if worker == nil || !agent.FeatureAllowed(configuration, "computer") {
		http.Error(response, "attaching a computer is off on this server", http.StatusForbidden)
		return
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
	username, name := "", ""
	attached := false
	defer func() {
		if attached {
			worker.DetachComputer(found.ID, socket)
			log.Noticef("%s detached their computer %q", username, name)
		}
	}()
	for {
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
			Name     string          `json:"name"`
			System   string          `json:"system"`
			Home     string          `json:"home"`
			ID       int64           `json:"id"`
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
			if message.Protocol != computerProtocol {
				refuse(fmt.Sprintf("this server speaks protocol %d; update teanode", computerProtocol))
				return
			}
			username = self.usernameOfToken(request, message.Token)
			if username == "" {
				refuse("the token is not one this server takes; sign in again with teanode auth login")
				return
			}
			found, err = self.agentOfPerson(username)
			if err != nil {
				refuse(err.Error())
				return
			}
			name = message.Name
			worker.AttachComputer(found.ID, socket, message.Name, message.System, message.Home)
			attached = true
			welcome, _ := json.Marshal(map[string]any{"type": "welcome", "protocol": computerProtocol, "username": username})
			_ = socket.Send(welcome)
			log.Noticef("%s attached their computer %q (%s)", username, message.Name, message.System)
		case "ping":
			pong, _ := json.Marshal(map[string]any{"type": "pong"})
			_ = socket.Send(pong)
		case "result":
			if attached {
				worker.ComputerAnswered(found.ID, socket, message.ID, message.OK, message.Data, message.Error)
			}
		case "bye":
			return
		}
	}
}
