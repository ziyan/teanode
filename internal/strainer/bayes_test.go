package strainer

import "testing"

// The classifier speaks only once it has learned enough of each kind. Two
// hundred ham and forty spam used to count as ready, and the classifier
// then said "certainly not spam" to every message that did not resemble
// those forty — which on the server that found this was every message of
// the day, two phishes included.
func TestCorpusReadyNeedsEnoughOfEachKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		spam, ham, minimum int64
		want               bool
	}{
		{0, 0, 200, false},
		{47, 192, 200, false},
		{200, 199, 200, false},
		{199, 200, 200, false},
		{200, 200, 200, true},
		{1000, 300, 200, true},
		// The default, when the setting is unset.
		{199, 5000, 0, false},
		{200, 200, 0, true},
		// A smaller minimum is honoured.
		{10, 10, 10, true},
	}
	for _, testCase := range cases {
		if got := corpusReady(testCase.spam, testCase.ham, testCase.minimum); got != testCase.want {
			t.Errorf("corpusReady(%d spam, %d ham, minimum %d) = %v, want %v",
				testCase.spam, testCase.ham, testCase.minimum, got, testCase.want)
		}
	}
}

// A verdict of spam is worth the full weight; a verdict of ham is worth a
// third of it. Certainty about ham comes cheap — it only takes words the
// classifier has not seen in spam — and the full weight in that direction
// was three free points for every message.
func TestBayesScoreIsBoundedOnTheHamSide(t *testing.T) {
	t.Parallel()

	if got := bayesScore(1.0, 3.0); got != 3.0 {
		t.Errorf("certain spam = %v, want the full weight 3", got)
	}
	if got := bayesScore(0.0, 3.0); got != -1.0 {
		t.Errorf("certain ham = %v, want a third of the weight, -1", got)
	}
	if got := bayesScore(0.5, 3.0); got != 0 {
		t.Errorf("no opinion = %v, want 0", got)
	}
	if got := bayesScore(0.75, 3.0); got != 1.5 {
		t.Errorf("half-sure of spam = %v, want 1.5", got)
	}
	// The default weight when the setting is unset.
	if got := bayesScore(1.0, 0); got != 3.0 {
		t.Errorf("certain spam at the default weight = %v, want 3", got)
	}
}
