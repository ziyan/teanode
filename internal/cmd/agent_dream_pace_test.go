package cmd

import "testing"

// How many nights the backlog is, at the pace of the night being described.
//
// The line says "at this pace" and divided by a constant two thousand. A
// night that read four thousand two hundred therefore reported twice the
// nights it needed -- to somebody watching a hundred and fifty thousand
// documents go by and deciding whether to wait up.
func TestTheNightsLeftAreCountedAtTheNightsOwnPace(t *testing.T) {
	for _, each := range []struct {
		what     string
		backlog  int
		digested int
		finished bool
		want     int
		known    bool
	}{
		{"a night that read four thousand", 150000, 4000, true, 38, true},
		{"the last night, which needs one more", 4000, 4000, true, 1, true},
		{"a backlog that does not divide", 4001, 4000, true, 2, true},
		{"a night still going, which has no pace yet", 150000, 40, false, 0, false},
		{"a night that read nothing", 150000, 0, true, 0, false},
		{"nothing waiting", 0, 4000, true, 0, false},
	} {
		got, known := dreamsLeft(each.backlog, each.digested, each.finished)
		if got != each.want || known != each.known {
			t.Errorf("%s: %d nights (known %t), not %d (known %t)", each.what, got, known, each.want, each.known)
		}
	}
}
