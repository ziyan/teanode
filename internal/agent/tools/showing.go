package tools

// Showing is a run that can move the dashboard to a page of itself, when
// the person reads the run there: the drawer, on a desk or a phone. A run
// anywhere else -- a chat app, a terminal, a program over MCP, a run with
// nobody present -- either does not implement it or says it cannot show a
// page, and a tool that would move the dashboard is not offered there
// (Tool.DashboardOnly).
type Showing interface {
	// CanShowPages says the person reads this run in the dashboard, so a
	// page it shows reaches them. A tool that can say where to go instead
	// says so when this is false.
	CanShowPages() bool

	// ShowPage has the dashboard open a path of its own. The caller has
	// checked the path; the dashboard checks it again.
	ShowPage(path string)
}
