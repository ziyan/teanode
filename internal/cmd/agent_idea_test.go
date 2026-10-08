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

// A page of ideas says which ideas of how many it holds and the flags that
// read the next, with the limit given, so that --limit 5 --offset 5 reads
// on five at a time rather than everything after.
func TestTheIdeaPageNoteRepeatsTheLimit(t *testing.T) {
	for _, each := range []struct {
		shownCount, offset, limit, totalCount, nextOffset int
		want                                              string
	}{
		{5, 5, 5, 23, 10, "note: 6 to 10 of 23; add --offset 10 --limit 5 for the next page"},
		{3, 20, 5, 23, 0, "note: 21 to 23 of 23"},
		{0, 40, 5, 23, 0, "note: --offset 40 is past the end, there are 23 in all"},
		{23, 0, 0, 23, 0, ""},
	} {
		if got := ideaPageNote(each.shownCount, each.offset, each.limit, each.totalCount, each.nextOffset); got != each.want {
			t.Errorf("%+v: %q, want %q", each, got, each.want)
		}
	}
}
