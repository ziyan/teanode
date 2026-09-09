package mx

import "testing"

// Who ends up in the address book.
//
// A contact is for somebody you might write to, and it is also what the
// "sender is known" rule reads. An address that cannot receive an answer is
// neither, and putting one in makes that rule true for precisely the mail it
// exists to tell apart from a stranger's.
func TestNoReplyAddress(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"no-reply@example.com":                    true,
		"noreply@example.com":                     true,
		"no_reply@example.com":                    true,
		"no.reply@example.com":                    true,
		"NoReply@example.com":                     true,
		"donotreply@example.com":                  true,
		"do-not-reply@example.com":                true,
		"noreply-alerts@example.com":              true,
		"noreply+tag@example.com":                 true,
		"no-reply@mail.notifications.example.com": true,

		// People, and addresses that read answers.
		"ann@example.com":        false,
		"reply@example.com":      false,
		"replies@example.com":    false,
		"support@example.com":    false,
		"norbert@example.com":    false,
		"nora.reyes@example.com": false,
		// A domain beginning the same way is a domain, not a promise about
		// the mailbox.
		"ann@noreply.example.com": false,
		"":                        false,
		"not-an-address":          false,
	}

	for address, want := range tests {
		t.Run(address, func(t *testing.T) {
			t.Parallel()
			if got := noReplyAddress(address); got != want {
				t.Errorf("noReplyAddress(%q) = %v, want %v", address, got, want)
			}
		})
	}
}
