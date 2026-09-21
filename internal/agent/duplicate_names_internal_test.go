package agent

import "testing"

// Whether one page's name is written inside another's.
//
// The pairs here are real, from the maintainer's graph, and they are the
// evidence the floors were set from. The ones that must match were each
// filed as a second page about a subject that already had one; the ones
// that must not are siblings that a nearness check alone put in the same
// band.
func TestWhenOneNameIsInsideAnother(test *testing.T) {
	test.Parallel()

	same := [][2]string{
		// A facet of a subject, written as though it were a subject.
		{"Ledger infrastructure", "Acme ledger infrastructure"},
		{"macOS client", "macOS client and gateway"},
		{"Handset final checks", "Handset final checks and lifecycle"},
		{"Almanac and recent projects", "Almanac and recent personal projects"},
		// The same words in another order, which is the same subject
		// written twice by two nights that did not know of each other.
		{"Relay diagnostics and configuration", "Relay configuration and diagnostics"},
		// Punctuation is a space, so this is four words inside five.
		{"Alpha/Beta interfaces", "Alpha Beta interfaces and production cycles"},
		// Written the other way up and with a word added, which is the
		// same subject, and the rule has to agree with that reading.
		{"Timing and layout", "Layout and page timing"},
	}
	for _, pair := range same {
		if !oneNameIsInsideTheOther(pair[0], pair[1]) {
			test.Errorf("%q is inside %q", pair[0], pair[1])
		}
		if !oneNameIsInsideTheOther(pair[1], pair[0]) {
			test.Errorf("and the same asked the other way about")
		}
	}

	apart := [][2]string{
		// Each holds a word the other does not, which is two subjects.
		{"Android client", "iOS client"},
		{"July 2025", "June 2025"},
		// One word is too little to say two subjects are one: in a folder
		// mirroring a checkout, a common word sits inside many
		// repositories that have nothing to do with one another.
		{"frontend", "Frontend Toolkit"},
		{"frontend", "Acme image frontend support"},
		{"Acme", "Acme Riverside"},
		{"css", "css-simulation"},
		// And nothing at all is not everything.
		{"", "Anything"},
	}
	for _, pair := range apart {
		if oneNameIsInsideTheOther(pair[0], pair[1]) {
			test.Errorf("%q and %q are two subjects", pair[0], pair[1])
		}
	}
}

// The two floors keep their order and their gap.
//
// The whole design rests on there being room between the nearest pair the
// two signals agree on wrongly and the furthest they agree on rightly. On
// the graph this was measured against those were 0.680 and 0.754, so a
// line at 0.70 sits in the gap -- and a later change that closes it should
// fail here rather than quietly start merging separate repositories.
func TestTheFloorsLeaveRoomBetweenThem(test *testing.T) {
	test.Parallel()

	if sameNameFloor >= samePageFloor {
		test.Fatalf("the name floor is the lower of the two: %v and %v", sameNameFloor, samePageFloor)
	}
	// Measured on a real graph: the nearest pair that is two repositories
	// rather than one scored 0.680, and the furthest pair that is one
	// subject written two ways scored 0.754. The floor goes between them.
	const nearestWrong, furthestRight = 0.680, 0.754
	if sameNameFloor <= nearestWrong {
		test.Fatalf("%v would take two repositories for one page", sameNameFloor)
	}
	if sameNameFloor > furthestRight {
		test.Fatalf("%v would go on filing a second page for one subject", sameNameFloor)
	}
}
