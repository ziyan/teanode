package smtpc

import "testing"

// TestCollapseResponse covers what a real refusal from Gmail looked like in
// the delivery detail: the enhanced status code repeated in the middle of
// every sentence, because textproto strips the "550-" from a continuation
// line but not the "5.7.1" that RFC 2034 puts after it.
func TestCollapseResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "the one that was reported",
			input: "5.7.1 [2406:da14:433:2d00::1000 19] Gmail has detected that this\n" +
				"5.7.1 message is likely suspicious due to the very low reputation of the\n" +
				"5.7.1 sending domain. To best protect our users from spam, the message has\n" +
				"5.7.1 been blocked. For more information, go to\n" +
				"5.7.1 https://support.google.com/mail/answer/188131 41be03b00d2f7 - gsmtp",
			want: "5.7.1 [2406:da14:433:2d00::1000 19] Gmail has detected that this " +
				"message is likely suspicious due to the very low reputation of the " +
				"sending domain. To best protect our users from spam, the message has " +
				"been blocked. For more information, go to " +
				"https://support.google.com/mail/answer/188131 41be03b00d2f7 - gsmtp",
		},
		{
			name:  "a single line is untouched",
			input: "5.7.1 Message rejected",
			want:  "5.7.1 Message rejected",
		},
		{
			name:  "no enhanced code, so nothing to strip",
			input: "Requested action not taken\nmailbox unavailable",
			want:  "Requested action not taken mailbox unavailable",
		},
		{
			// The code is only removed where it repeats the first line's. A
			// continuation beginning with some other number is prose, and
			// cutting it would change what the server said.
			name:  "a different code is left alone",
			input: "5.7.1 Refused\n4.2.2 quota exceeded",
			want:  "5.7.1 Refused 4.2.2 quota exceeded",
		},
		{
			name:  "a version number is not a status code",
			input: "5.7.1 Refused\nby filter 1.2.3 on host",
			want:  "5.7.1 Refused by filter 1.2.3 on host",
		},
		{
			name:  "blank continuation lines are dropped",
			input: "5.5.0 Error\n5.5.0 \n5.5.0 see the log",
			want:  "5.5.0 Error see the log",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CollapseResponse(test.input); got != test.want {
				t.Errorf("got:\n  %s\nwant:\n  %s", got, test.want)
			}
		})
	}
}
