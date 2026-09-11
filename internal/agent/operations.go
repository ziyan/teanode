package agent

import "github.com/ziyan/teanode/internal/agent/tools"

// Operations is the server as the person: every tool that touches the
// server goes through it, so a tool can do exactly what the person can do
// and nothing else. It is the API — the same operations the dashboard and
// the command line call, with the same permissions, validation and audit —
// handed to the agent by the package that owns them, executed as the
// person with the audit trail saying the agent did it.
type Operations = tools.Operations
