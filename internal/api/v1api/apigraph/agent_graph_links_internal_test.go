package apigraph

import (
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A page read with no link offset or limit has every link as before; read
// a part at a time, the parts come in one order, carry on from each other
// and say where the next starts, and the last says none.
func TestAPageReadsItsLinksAPartAtATime(t *testing.T) {
	edges := func() []*models.AgentEdge {
		var edges []*models.AgentEdge
		// Read in an order the sort does not keep: incoming before
		// outgoing, and paths backwards.
		for number := 5; number >= 1; number-- {
			edges = append(edges, &models.AgentEdge{FromPath: fmt.Sprintf("things/boat-%d", number), ToPath: "places/harbor", Relation: models.EdgeLocatedIn})
		}
		return append(edges, &models.AgentEdge{FromPath: "places/harbor", ToPath: "places/coast", Relation: models.EdgePartOf})
	}

	every, linkCount, next := pageOfLinks("places/harbor", edges(), 0, 0)
	if len(every) != 6 || linkCount != 6 || next != 0 || every[0].FromPath != "things/boat-5" {
		t.Fatalf("with neither given, every link as read: %d of %d, next %d", len(every), linkCount, next)
	}

	first, linkCount, next := pageOfLinks("places/harbor", edges(), 0, 4)
	if len(first) != 4 || linkCount != 6 || next != 4 {
		t.Fatalf("a first part of four: %d of %d, next %d", len(first), linkCount, next)
	}
	if first[0].ToPath != "places/coast" || first[1].FromPath != "things/boat-1" || first[3].FromPath != "things/boat-3" {
		t.Fatalf("the page's own links first, then those pointing at it by path: %+v", first)
	}
	rest, _, next := pageOfLinks("places/harbor", edges(), 4, 4)
	if len(rest) != 2 || rest[0].FromPath != "things/boat-4" || rest[1].FromPath != "things/boat-5" || next != 0 {
		t.Fatalf("the next part carries on to the last: %+v, next %d", rest, next)
	}
	if past, linkCount, _ := pageOfLinks("places/harbor", edges(), 10, 4); len(past) != 0 || linkCount != 6 {
		t.Fatalf("an offset past the end holds nothing and still counts them: %+v", past)
	}
}
