package agent

import "github.com/ziyan/teanode/internal/agent/tools"

var _ tools.Showing = (*AskRun)(nil)

// ShowPage has the drawer that is following this turn move the dashboard to
// a page of it (tools.Showing).
func (self *AskRun) ShowPage(path string) {
	self.emit(Event{Kind: EventNavigate, Text: path})
}
