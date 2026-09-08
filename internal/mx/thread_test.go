package mx

import (
	"reflect"
	"testing"
)

// TestThreadCandidatesReadsBothHeaders covers what decides which conversation
// an arriving or outgoing message belongs to.
//
// The two headers are read as fields rather than as one value because a long
// References is folded across lines by the sending program, and because
// In-Reply-To may carry more than one id. A reader that took the whole value
// as a single id would find nothing and start a new conversation for every
// reply, which is the failure this pins.
func TestThreadCandidatesReadsBothHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers []string
		want    []string
	}{
		{
			name:    "a message that answers nothing",
			headers: []string{"Subject: hello"},
			want:    nil,
		},
		{
			name:    "in-reply-to alone",
			headers: []string{"In-Reply-To: <a@example.com>"},
			want:    []string{"<a@example.com>"},
		},
		{
			name: "references carries the whole chain",
			headers: []string{
				"In-Reply-To: <c@example.com>",
				"References: <a@example.com> <b@example.com> <c@example.com>",
			},
			want: []string{"<c@example.com>", "<a@example.com>", "<b@example.com>", "<c@example.com>"},
		},
		{
			name: "a folded references is still a list of ids",
			headers: []string{
				"References: <a@example.com>\r\n\t<b@example.com>\r\n <c@example.com>",
			},
			want: []string{"<a@example.com>", "<b@example.com>", "<c@example.com>"},
		},
		{
			name:    "an empty header adds nothing",
			headers: []string{"In-Reply-To: ", "References:   "},
			want:    nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := threadCandidates(test.headers)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("threadCandidates(%q) = %q, want %q", test.headers, got, test.want)
			}
		})
	}
}
