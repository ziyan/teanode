package agent

import "github.com/ziyan/teanode/internal/agent/tools"

var _ tools.Showing = (*AskRun)(nil)

// CanShowPages says this turn's surface is the dashboard's own drawer
// (tools.Showing), by the same entry that decides whether a DashboardOnly
// tool is offered.
func (self *AskRun) CanShowPages() bool {
	return surfaceOf(self.settings.Surface).canShowPages
}

// ShowPage has the drawer that is following this turn move the dashboard to
// a page of it (tools.Showing).
func (self *AskRun) ShowPage(path string) {
	self.emit(Event{Kind: EventNavigate, Text: path})
}
