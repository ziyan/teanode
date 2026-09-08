package dns

import "testing"

// What is stopping a logo from being shown, which is the reason the row
// exists.
//
// A BIMI record is ignored by every receiver while the domain's DMARC policy
// is none — and none is the policy this page recommends starting with, so
// every domain here begins unable to use one. Publishing the record and
// waiting to see what happens is how somebody spends a week finding that out.
func TestTheDMARCPolicyIsReadOutOfWhatIsPublished(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		records []string
		want    string
	}{
		{
			name:    "a policy of none, which is where every domain starts",
			records: []string{"v=DMARC1; p=none; rua=mailto:dmarc@example.com"},
			want:    "none",
		},
		{
			name:    "quarantine",
			records: []string{"v=DMARC1; p=quarantine; pct=100"},
			want:    "quarantine",
		},
		{
			name:    "reject, written without spaces",
			records: []string{"v=DMARC1;p=reject;rua=mailto:dmarc@example.com"},
			want:    "reject",
		},
		{
			// A name's TXT records are a mixed bag, and only one of them is
			// this: an SPF record with a "p" in it must not be read as one.
			name:    "somebody else's record at the same name",
			records: []string{"v=spf1 include:example.com ~all", "v=DMARC1; p=reject"},
			want:    "reject",
		},
		{
			name:    "a record with no policy at all",
			records: []string{"v=DMARC1; rua=mailto:dmarc@example.com"},
			want:    "",
		},
		{
			name:    "nothing published",
			records: nil,
			want:    "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := dmarcPolicy(test.records); got != test.want {
				t.Errorf("dmarcPolicy() = %q, want %q", got, test.want)
			}
		})
	}
}

// How that policy is said in the sentence under the row, including the case
// where there is no record to have a policy.
func TestAPolicyIsNamedForTheSentence(t *testing.T) {
	t.Parallel()

	if got := describePolicy(""); got != "not published" {
		t.Errorf("describePolicy(%q) = %q", "", got)
	}
	if got := describePolicy("none"); got != "none" {
		t.Errorf("describePolicy(%q) = %q", "none", got)
	}
}
