package tools

// Showing is a run whose person is reading it in the dashboard, which can
// be moved to a page of itself: the drawer, on a desk or a phone. A run
// anywhere else -- a chat app, a terminal, a program over MCP, a run with
// nobody present -- does not implement it, and a tool that would move the
// dashboard is not offered there (Tool.DashboardOnly).
type Showing interface {
	// ShowPage has the dashboard open a path of its own. The caller has
	// checked the path; the dashboard checks it again.
	ShowPage(path string)
}
