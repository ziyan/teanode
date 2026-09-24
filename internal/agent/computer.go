package agent

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
)

// The computer relay: the person's own computer, attached through
// `teanode computer` over the server's websocket, answering the shell and
// filesystem tools. Like the tab (tab.go), it is a device (device.go): the
// program on the computer does the running and the reading, applies its
// own refusals, and the server only carries requests across. Ask only.

// attachedComputer is one of a person's attached computers.
type attachedComputer struct {
	*deviceLink
	name        string
	system      string
	home        string
	description string

	// terminal is the session of the terminal the person is sitting in,
	// when they attached one; empty otherwise.
	terminal string

	// features are what the program said it offers beyond its protocol.
	features []string
}

// HasBackground says the program keeps commands running in the background.
func (self *attachedComputer) HasBackground() bool {
	return slices.Contains(self.features, computer.FeatureBackground)
}

// AttachedTerminal is the session id of the terminal the person attached
// with `teanode terminal`, or empty. The person is in it too: what the
// agent types there, they see typed.
func (self *attachedComputer) AttachedTerminal() string {
	return self.terminal
}

// ComputerIdentity is what a computer says about itself when it attaches.
type ComputerIdentity struct {
	Name   string
	System string
	Home   string

	// Description is the person's sentence about what the computer is for,
	// which the agent reads to choose between several; empty when they gave
	// none.
	Description string

	// Terminal is the session of the terminal the person attached it from,
	// when they did.
	Terminal string

	// Features are what the program offers beyond its protocol, such as
	// background commands.
	Features []string
}

// AttachComputer records a person's computer by its name; a second attach
// under the same name replaces the first, and a different name is another
// computer beside it.
func (self *Agent) AttachComputer(agentId string, connection DeviceConnection, identity ComputerIdentity) {
	name, system, home, terminal := identity.Name, identity.System, identity.Home, identity.Terminal
	self.computersMutex.Lock()
	defer self.computersMutex.Unlock()
	if self.computers == nil {
		self.computers = map[string]map[string]*attachedComputer{}
	}
	if self.computers[agentId] == nil {
		self.computers[agentId] = map[string]*attachedComputer{}
	}
	if previous := self.computers[agentId][name]; previous != nil {
		previous.drop()
	}
	computer := &attachedComputer{deviceLink: newDeviceLink("the computer "+name, connection), name: name, system: system, home: home, description: identity.Description, terminal: terminal, features: identity.Features}
	if terminal != "" {
		// The device opened it before saying hello, so it is registered
		// here without being asked for: reading and typing then work the
		// way they do for a session this side started.
		computer.adoptSession(terminal, "pty")
	}
	self.computers[agentId][name] = computer
}

// DetachComputer forgets a person's computer, if this connection is the one
// attached under its name.
func (self *Agent) DetachComputer(agentId string, connection DeviceConnection) {
	self.computersMutex.Lock()
	defer self.computersMutex.Unlock()
	for name, computer := range self.computers[agentId] {
		if connection == nil || computer.connection == connection {
			computer.drop()
			delete(self.computers[agentId], name)
		}
	}
}

// ComputerAnswered is a computer answering a request.
func (self *Agent) ComputerAnswered(agentId string, connection DeviceConnection, id int64, ok bool, data json.RawMessage, failure string) {
	self.computersMutex.Lock()
	defer self.computersMutex.Unlock()
	for _, computer := range self.computers[agentId] {
		if computer.connection == connection {
			computer.answered(id, ok, data, failure)
			return
		}
	}
}

// computersFor are a person's attached computers, by name.
func (self *Agent) computersFor(agentId string) []*attachedComputer {
	self.computersMutex.Lock()
	defer self.computersMutex.Unlock()
	computers := make([]*attachedComputer, 0, len(self.computers[agentId]))
	for _, computer := range self.computers[agentId] {
		computers = append(computers, computer)
	}
	sort.Slice(computers, func(left, right int) bool { return computers[left].name < computers[right].name })
	return computers
}

// AttachedComputer is one attached computer of a person, for the API.
type AttachedComputer struct {
	Name        string
	System      string
	Description string
	Since       time.Time
}

// ComputersAttached are the computers a person has attached, for the API.
func (self *Agent) ComputersAttached(agentId string) []AttachedComputer {
	var listed []AttachedComputer
	for _, computer := range self.computersFor(agentId) {
		listed = append(listed, AttachedComputer{Name: computer.name, System: computer.system, Description: computer.description, Since: computer.attachedAt})
	}
	return listed
}

// Ask is AskFor, as the tools see it.
func (self *attachedComputer) Ask(ctx context.Context, action string, args any, wait time.Duration) (json.RawMessage, error) {
	return self.AskFor(ctx, action, args, wait)
}

// Name, System and Home are what the computer said it is and where a
// path of the person's is from, for the tools and the overlay.
func (self *attachedComputer) Name() string   { return self.name }
func (self *attachedComputer) System() string { return self.system }
func (self *attachedComputer) Home() string   { return self.home }

// Description is the person's sentence about what the computer is for.
func (self *attachedComputer) Description() string { return self.description }

// reachOf is the computer a person's reach names for one of their skills or
// connected servers: what its requests go through. Empty means through this
// server, and so does a reach that cannot be read, with a line in the log: the
// service then fails on its own if only a computer could reach it, which says
// more than failing here.
func (self *Agent) reachOf(ctx context.Context, agentId, kind, name string) string {
	var computerName string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		reaches, err := tx.ListAgentReaches(agentId)
		if err != nil {
			return err
		}
		for _, reach := range reaches {
			if reach.Kind == kind && strings.EqualFold(reach.Name, name) {
				computerName = reach.ComputerName
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot read the reach of %s %q: %s", kind, name, err)
	}
	return computerName
}
