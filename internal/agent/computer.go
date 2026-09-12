package agent

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// The computer relay: the person's own computer, attached through
// `teanode computer` over the server's websocket, answering the shell and
// filesystem tools. Like the tab (tab.go), it is a device (device.go): the
// program on the computer does the running and the reading, applies its
// own refusals, and the server only carries requests across. Ask only.

// attachedComputer is one of a person's attached computers.
type attachedComputer struct {
	*deviceLink
	name   string
	system string
	home   string
}

// AttachComputer records a person's computer by its name; a second attach
// under the same name replaces the first, and a different name is another
// computer beside it.
func (self *Agent) AttachComputer(agentId string, connection DeviceConnection, name, system, home string) {
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
	self.computers[agentId][name] = &attachedComputer{deviceLink: newDeviceLink("the computer "+name, connection), name: name, system: system, home: home}
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
	Name   string
	System string
	Since  time.Time
}

// ComputersAttached are the computers a person has attached, for the API.
func (self *Agent) ComputersAttached(agentId string) []AttachedComputer {
	var listed []AttachedComputer
	for _, computer := range self.computersFor(agentId) {
		listed = append(listed, AttachedComputer{Name: computer.name, System: computer.system, Since: computer.attachedAt})
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
