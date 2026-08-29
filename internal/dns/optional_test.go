package dns

import "testing"

// A domain reachable over IPv4 alone is correctly configured. Counting its
// absent AAAA as a fault paints the page red for something nothing depends on,
// which teaches the reader to stop reading the colour.
func TestAnAbsentOptionalRecordDoesNotFailTheSet(t *testing.T) {
	t.Parallel()

	set := &RecordSet{Records: []*Record{
		{Type: "A", Verified: true},
		{Type: "AAAA", Verified: false, Optional: true},
		{Type: "MX", Verified: true},
	}}
	if !set.Verified() {
		t.Error("a set whose only gap is an optional record was reported incomplete")
	}

	// A required record still fails it.
	set.Records = append(set.Records, &Record{Type: "TXT", Verified: false})
	if set.Verified() {
		t.Error("a missing required record did not fail the set")
	}
}

// An empty set is not "verified"; it means nothing has been checked.
func TestAnEmptySetIsNotVerified(t *testing.T) {
	t.Parallel()
	if (&RecordSet{}).Verified() {
		t.Error("an empty record set reported itself as verified")
	}
}

// Only optional records, all absent, still counts: there was nothing that had
// to be published.
func TestOnlyOptionalRecordsCount(t *testing.T) {
	t.Parallel()
	set := &RecordSet{Records: []*Record{{Type: "AAAA", Optional: true}}}
	if !set.Verified() {
		t.Error("a set of nothing but optional records was reported incomplete")
	}
}
