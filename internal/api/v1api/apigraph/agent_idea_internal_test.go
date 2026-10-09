package apigraph

import (
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A page of ideas holds limit of them from offset, says how many there are
// in all and where the next page starts, and the last page says none; the
// identifiers asked for narrow the listing before it is paged.
func TestAPageOfIdeasSaysWhereTheNextStarts(t *testing.T) {
	ideas := make([]*models.AgentIdea, 0, 7)
	for number := 1; number <= 7; number++ {
		ideas = append(ideas, &models.AgentIdea{ID: fmt.Sprintf("idea-%d", number)})
	}

	first := pageOfIdeas(ideas, nil, 3, 0)
	if len(first.Ideas) != 3 || first.Ideas[0].ID != "idea-1" || first.TotalCount != 7 || first.NextOffset != 3 {
		t.Fatalf("first page: %d ideas from %s, %d in all, next %d", len(first.Ideas), first.Ideas[0].ID, first.TotalCount, first.NextOffset)
	}
	last := pageOfIdeas(ideas, nil, 3, 6)
	if len(last.Ideas) != 1 || last.Ideas[0].ID != "idea-7" || last.NextOffset != 0 {
		t.Fatalf("the last page holds the last idea and no next offset: %+v", last)
	}
	if past := pageOfIdeas(ideas, nil, 3, 20); len(past.Ideas) != 0 || past.TotalCount != 7 {
		t.Fatalf("an offset past the end holds nothing and still counts them: %+v", past)
	}
	if every := pageOfIdeas(ideas, nil, 0, 0); len(every.Ideas) != 7 || every.NextOffset != 0 {
		t.Fatalf("no limit is every idea: %+v", every)
	}
	if one := pageOfIdeas(ideas, []string{"idea-5"}, 0, 0); len(one.Ideas) != 1 || one.Ideas[0].ID != "idea-5" || one.TotalCount != 1 {
		t.Fatalf("an identifier narrows the listing to that idea: %+v", one)
	}
}
