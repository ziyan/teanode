package cmd

import (
	"testing"

	"github.com/ziyan/teanode/internal/client"
)

// The list says why an idea expired, in a few words, and nothing more for
// one expired before reasons were kept or in another status.
func TestTheIdeaListSaysWhyAnIdeaExpired(t *testing.T) {
	for _, each := range []struct {
		idea client.AgentIdea
		want string
	}{
		{client.AgentIdea{IdeaStatus: "expired", ExpiredReason: "past_date"}, "expired: date passed"},
		{client.AgentIdea{IdeaStatus: "expired", ExpiredReason: "missing_tool"}, "expired: tool missing"},
		{client.AgentIdea{IdeaStatus: "expired", ExpiredReason: "already_used"}, "expired: already used"},
		{client.AgentIdea{IdeaStatus: "expired"}, "expired"},
		{client.AgentIdea{IdeaStatus: "open"}, "open"},
	} {
		if got := ideaStatusText(&each.idea); got != each.want {
			t.Errorf("%+v: %q, want %q", each.idea, got, each.want)
		}
	}
}
