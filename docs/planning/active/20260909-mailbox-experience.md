# The mailbox, used rather than built

Seven things, found by using the mailbox rather than by reading it. Most are
one idea: what happened should be said once, quietly, where the reader is
looking — and what they just did should be undoable.

## Purpose / Big Picture

The mailbox tells you nothing when it works and stops you when it does not.
Archiving a conversation empties the pane and says nothing; a failure puts a
red block above the list and leaves it there until something else replaces it;
and 104 places render an `<ErrorMessage>` of their own, each deciding for
itself where the words go and how long they stay.

Every mail program worth the name answers this the same way, and has for
fifteen years: a line at the foot of the window saying what happened, with the
way back beside it. "Conversation archived. Undo." It is one sentence and it
is the difference between a program you can use quickly and one you have to
use carefully — because the cost of a wrong click stops being "find where that
went" and becomes "press undo".

The rest follows from the same idea. Archiving should leave you reading the
next message rather than staring at an empty panel, because the reason you
archived it was to get to the next one. A draft belongs inside the
conversation it answers, not on a page of its own that has lost the thread.
Attachments belong where the eye lands, not under a screen of quoted text.
And a shortcut nobody is told about is a shortcut for whoever wrote it, so
each control says its key: "Archive (E)".

## Plan of Work

### Milestone 1 — a place for the program to speak

`ToastProvider` and `useToast`, mounted once around the app. Two kinds:
something done, and something failed. Both go away on their own — done after
a few seconds, failed after longer, since a failure is read rather than
noticed. A toast may carry one action, which is how undo arrives.

Stacked, newest nearest the reader, capped so a burst cannot fill the window.
`role="status"` and a live region, because a message that only exists visually
is a message somebody using a screen reader does not get.

Acceptance: a toast appears, says what happened, goes away by itself, and can
be dismissed early.

### Milestone 2 — what you did, and the way back

Archiving, moving, junking and deleting say what they did and offer undo.

Undo is not a general mechanism and should not pretend to be: it is knowing
where each message was before the action, and putting it back. That is one
map from item to folder, captured before the mutation. Junk also unlearns what
it taught, since teaching without moving is what makes the next one arrive in
the Inbox.

### Milestone 3 — errors move into it

The inline `<ErrorMessage>` calls that report the *outcome of an action* become
toasts. The ones that describe the *state of a page* — a query that failed, a
form field that is wrong — stay where they are: a toast that vanishes is no
place for something the reader has to act on.

### Milestone 4 — archiving lands on the next message

Archiving, junking or deleting the conversation being read opens the next one
in the list rather than emptying the pane. The last one in the list falls back
to the one before it, and an empty list to the folder.

### Milestone 5 — a draft is part of its conversation

A draft row opens the composer inside the conversation rather than navigating
to a page of its own, which is where the reply already opens.

### Milestone 6 — attachments above the message

A compact line of them above the body: what is attached is why the message was
sent, and it was under a screen of quoted text.

### Milestone 7 — the keys, and saying what they are

More of them, and every control that has one says so: "Archive (E)". A
shortcut nobody can discover belongs to whoever wrote it.

## Validation

`make test`, `make lint-ci`, and the mailbox exercised in Chrome at 1400 and
390 — archive from the list and from the reader, undo each, fail something on
purpose, and walk the keys.
