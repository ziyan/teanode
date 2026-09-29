package agent

import (
	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

// Operations is the server as the person: every tool that touches the
// server goes through it, so a tool can do exactly what the person can do
// and nothing else. It is the API — the same operations the dashboard and
// the command line call, with the same permissions, validation and audit —
// handed to the agent by the package that owns them, executed as the
// person with the audit trail saying the agent did it.
type Operations = tools.Operations

// narrowOperations holds operations to what the limit allows as well: the
// operations' own narrowing where they have one, which refuses every call
// beyond it, and otherwise the narrower set said by Permissions, which is
// what the tools offered are chosen by. A nil limit, from work kept before
// the limit was, leaves them as they are.
func narrowOperations(operations Operations, limit *models.EffectivePermissions) Operations {
	if limit == nil {
		return operations
	}
	if narrowable, ok := operations.(tools.NarrowableOperations); ok {
		return narrowable.NarrowedTo(limit)
	}
	return &narrowedOperations{Operations: operations, permissions: operations.Permissions().Within(limit)}
}

// narrowedOperations are operations that say a narrower set of
// permissions than their own.
type narrowedOperations struct {
	Operations
	permissions *models.EffectivePermissions
}

func (self *narrowedOperations) Permissions() *models.EffectivePermissions {
	return self.permissions
}
