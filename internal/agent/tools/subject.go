package tools

import "strings"

// Subjects as replies and conversations carry them.

// ThreadSubject is a conversation's subject without the Re: and Fwd: its
// answers added.
func ThreadSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	for {
		lower := strings.ToLower(subject)
		trimmed := subject
		for _, prefix := range []string{"re:", "fwd:", "fw:", "aw:", "wg:", "sv:", "vs:", "tr:"} {
			if strings.HasPrefix(lower, prefix) {
				trimmed = strings.TrimSpace(subject[len(prefix):])
				break
			}
		}
		if trimmed == subject {
			return subject
		}
		subject = trimmed
	}
}

// ReplySubject is the answer's subject: the original's with one Re:.
func ReplySubject(subject string) string {
	return "Re: " + ThreadSubject(subject)
}
