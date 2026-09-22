package apigraph

import "testing"

// How many pages the index answers with, for each way of asking.
//
// Asking for more than there is to give used to land on the default, so a
// caller asking for five thousand got five hundred: fewer than asking for
// nothing, silently, which is the opposite of what asking for more means.
// It is how an audit of a graph came to be run against a quarter of it.
func TestAskingForMoreDoesNotGiveLess(test *testing.T) {
	test.Parallel()

	limitFor := func(asked int) int {
		limit := asked
		switch {
		case limit <= 0:
			limit = agentGraphIndexPages
		case limit > agentGraphIndexPagesMost:
			limit = agentGraphIndexPagesMost
		}
		return limit
	}

	for _, each := range []struct {
		asked int
		want  int
	}{
		{0, agentGraphIndexPages},
		{-1, agentGraphIndexPages},
		{1, 1},
		{500, 500},
		{2000, agentGraphIndexPagesMost},
		{5000, agentGraphIndexPagesMost},
		{1 << 30, agentGraphIndexPagesMost},
	} {
		if got := limitFor(each.asked); got != each.want {
			test.Errorf("asking for %d gave %d, wanted %d", each.asked, got, each.want)
		}
	}

	// The property behind the cases: asking for more never gives less.
	previous := 0
	for _, asked := range []int{1, 10, 100, 500, 1000, 2000, 5000, 100000} {
		got := limitFor(asked)
		if got < previous {
			test.Errorf("asking for %d gave %d, fewer than the %d before it", asked, got, previous)
		}
		previous = got
	}
	if agentGraphIndexPagesMost < agentGraphIndexPages {
		test.Errorf("the most (%d) is under the default (%d)", agentGraphIndexPagesMost, agentGraphIndexPages)
	}
}
