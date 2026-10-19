package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Health never goes into memory: what the agent said in a turn that read
// the person's Health, and what its tools answered, are not read by the run
// that files a conversation, while the person's own words and the turns
// around it are.
func TestAFilingRunSkipsATurnThatReadHealth(t *testing.T) {
	message := func(id string, role llm.Role, content string, toolNames ...string) *models.AgentMessage {
		found := &models.AgentMessage{ID: id, Role: string(role), Content: content}
		for _, name := range toolNames {
			found.ToolCalls = append(found.ToolCalls, models.AgentToolCall{ID: id + name, Name: name})
		}
		return found
	}
	messages := []*models.AgentMessage{
		message("u1", llm.RoleUser, "I moved to a new flat"),
		message("a1", llm.RoleAssistant, "Noted, congratulations"),
		message("u2", llm.RoleUser, "How did I sleep this week?"),
		message("a2", llm.RoleAssistant, "Let me look", "health"),
		message("t2", llm.RoleTool, `{"summary":"6 h 40 min"}`),
		message("a3", llm.RoleAssistant, "You slept 6 h 40 min on average"),
		message("u3", llm.RoleUser, "Thanks, and what is the weather"),
		message("a4", llm.RoleAssistant, "Sunny", "weather"),
		message("t4", llm.RoleTool, `{"sky":"sunny"}`),
		message("a5", llm.RoleAssistant, "It is sunny"),
		message("u4", llm.RoleUser, "Steps?"),
		message("a6", llm.RoleAssistant, "", "health_summary"),
		message("t6", llm.RoleTool, `{"steps":8412}`),
		message("a7", llm.RoleAssistant, "8,412 steps a day"),
	}

	var got []string
	for _, each := range worthReading(messages) {
		got = append(got, each.ID)
	}
	want := []string{"u1", "a1", "u2", "u3", "a4", "a5", "u4"}
	if len(got) != len(want) {
		t.Fatalf("worthReading kept %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("worthReading kept %v, want %v", got, want)
		}
	}
}
