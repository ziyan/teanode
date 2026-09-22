package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/models"
)

// The computer relay: `teanode computer` attaches the person's own computer
// here, over a websocket, saying who it is with the token the command line
// signed in with. The server carries the agent's shell and filesystem
// requests across and the program's answers back.

// computerAnswerSize bounds one message from an attached computer.
//
// Larger than a GraphQL request body because this socket carries a page
// of a scan: the program bounds a page at a few megabytes of text, and
// this is that with room for the JSON around it -- and room for a
// program that bounds less well than this one, since a page over the
// limit closes the socket and fails every scan open on that computer,
// and an older program repeated that on the same file every time. A
// limit at all because the library's default is none, and this socket
// is reached before the sender has said who they are.
const computerAnswerSize = 512 << 20

// computerProtocol is the version the program must speak. It is
// internal/computer's Protocol, and the two move together: 2 added
// `scan`, which is what lets a source be read with nobody watching.
const computerProtocol = computer.Protocol

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
	Name   string `json:"name"`
	System string `json:"system,omitempty"`
	// Description is the person's sentence about what the computer is for.
	Description string    `json:"description,omitempty"`
	Since       time.Time `json:"since"`
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
			view.Computers = append(view.Computers, AgentComputerView{Name: computer.Name, System: computer.System, Description: computer.Description, Since: computer.Since})
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
	conn.SetReadLimit(computerAnswerSize)
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
			// Why matters: a computer that leaves every hundred seconds
			// is a program that crashed, a socket a proxy cut, or a
			// message too big, and the three are told apart only here.
			if attached {
				log.Noticef("the computer %q of %s stopped answering: %s", name, username, err)
			}
			return
		}
		var message struct {
			Type        string          `json:"type"`
			Protocol    int             `json:"protocol"`
			Token       string          `json:"token"`
			Name        string          `json:"name"`
			System      string          `json:"system"`
			Home        string          `json:"home"`
			Description string          `json:"description"`
			ID          int64           `json:"id"`
			OK          bool            `json:"ok"`
			Data        json.RawMessage `json:"data"`
			Error       string          `json:"error"`
			Session     string          `json:"session"`
			Event       string          `json:"event"`
			Stream      string          `json:"stream"`
			Code        int             `json:"code"`
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
			worker.AttachComputer(found.ID, socket, agent.ComputerIdentity{
				Name: message.Name, System: message.System, Home: message.Home,
				Description: strings.TrimSpace(message.Description), Terminal: message.Session,
			})
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
		case "session":
			// A session speaks without being asked, so this carries no
			// request number and answers nobody: it is filed under the
			// session's own name for whoever is driving it.
			if attached {
				worker.ComputerSessionSaid(found.ID, socket, message.Session, message.Event,
					message.Stream, agent.DecodedSessionData(message.Data), message.Code)
			}
		case "bye":
			return
		}
	}
}
