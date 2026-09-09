# Subscriptions that arrive stripped, and subscriptions you want quiet

Three things the subscriptions page cannot do yet, found by using it: it
misses newsletters whose unsubscribe header was removed in transit, it cannot
be told to stop putting a list in the Inbox, and it stops at 200 rows without
saying so.

## Purpose / Big Picture

The page promises "every mailing list you receive, and the way out of each".
Two of those words are doing more work than the code supports.

**Every.** A message is a subscription here when it carries `List-Unsubscribe`
or `List-Id`. Measured against a real mailbox: 51 of 80 messages qualify, 29
do not, and 11 of the 29 came through Apple's private relay, which rewrites
the sender and removes `List-Unsubscribe` while leaving
`List-Unsubscribe-Post` behind. Six of those eleven still carry that orphan.
It is not a guess: a sender only ever emits `List-Unsubscribe-Post` alongside
`List-Unsubscribe`, because on its own it says nothing — so its presence with
no address means the address was taken out on the way. That is proof enough,
and it recovers the mail somebody most wants grouped: the newsletters they
signed up for with a relay address precisely because they expected to want out
later.

The rest of the 29 are not lists. Nextdoor publishes no list headers at all;
neither does an Amazon shipping notice, a GitLab sign-in alert or a Fidelity
alert. Guessing from a `no-reply@` sender would sweep all four in, and a page
listing things you cannot leave is worse than one that admits it missed some.
So this widens by proof and not by inference.

**The way out.** Unsubscribing tells the sender a person reads this address,
and some lists have no way out at all. A reader who does not want to send that
signal, or who cannot, has only one recourse today: a rule, written by hand,
matching a header they have to go and find. Muting is that recourse made
direct — the list keeps arriving and stops being in the way.

And a list of 200 with no way to see the 201st is a bug on any page; this one
prints the true total next to the truncated list, which makes it a lie.

## Progress

- [x] Milestone 1 — `ParseList` takes a third way in, `list_stripped` on the
      message, migration 0028 re-opening `list_checked` where no key was
      found. Three cases in `list_test.go`: the orphan alone, the orphan with
      a `List-Id`, and `Precedence: bulk` still being nothing.
- [x] Milestone 2 — mute. `muted_at` on the subscription row, the folder
      chosen in `deliverToMailbox`, and `MuteMailboxSubscription`, which also
      clears what the Inbox already holds. `TestAMutedListSkipsTheInbox` walks
      the real path against a real database: Inbox before, Archive and read
      after, Junk still winning for a suspicious message, and unmuting putting
      it back.
- [x] Milestone 3 — pagination, and a foot that says how many of how many.
- [x] Asked for while this was being built: a list sender no longer becomes a
      contact, and neither does an address that says it takes no replies. Both
      polluted completion, and both made "sender is known" true for exactly
      the mail that rule exists to tell apart from a stranger's.
- [x] Also asked for: one icon for leaving a list wherever it is offered, and
      a message that belongs to a list now opens that list rather than asking
      about leaving in a second place.

## Context and Orientation

- `internal/util/mailparse/list.go` — `ParseList(headers, from)`. Returns the
  zero value unless `List-Unsubscribe` or `List-Id` is present, which is the
  test to widen. `ListInfo.OneClick` already requires an https address, so a
  stripped message cannot accidentally claim one-click.
- `internal/db/database_mail.go` — `applyListInfo` on the way in,
  `SetMailList` for the backfill, `ListMailNeedingList` picking up whatever
  has `list_checked = false`. Re-opening that flag is how already-stored mail
  gets re-examined under the new rule.
- `internal/db/database_subscription.go` — `mailbox_subscription`, keyed by
  mailbox and list key. It already outlives the mail, which is what a mute
  has to do.
- `internal/mx/exchange_mailbox.go` — `deliverToMailbox` chooses Inbox, or
  Junk when the message is suspicious, then runs the mailbox's rules. The mail
  row is created before this, so `mail.ListKey` is populated by the time the
  choice is made.
- `web/src/pages/mailbox.tsx` — the paging this page should have copied:
  `PAGE_SIZE`, `load(offset)` merging by id, and a foot saying "x of y" with a
  link to load more.

## Plan of Work

### Milestone 1 — a stripped unsubscribe is still a subscription

`ParseList` accepts a third way in: `List-Unsubscribe-Post` present while
`List-Unsubscribe` is absent. The message is keyed like any other — by
`List-Id` when it has one, by the sending address otherwise — with no
addresses to leave by, and a new `Stripped` flag saying why.

`list_stripped` on `mail`, set by both writers. Migration 0028 adds it and
sets `list_checked = false` where `list_key = ''`, so the backfill walks the
mail that was examined under the old rule. Mail that already has a key is left
alone: re-examining it would cost the same work to reach the same answer.

Acceptance: the six messages in the measured mailbox become subscriptions;
`mailparse` tests cover the orphan header, the orphan with a `List-Id`, and
`Precedence: bulk` alone still being nothing.

### Milestone 2 — mute

`muted_at` on `mailbox_subscription`. `MuteMailboxSubscription(mailboxId, key,
muted)` sets or clears it, and on muting also files whatever the Inbox already
holds from that list into Archive, marked read — muting a list while its mail
sits in front of you should clear it, not only the next one.

`deliverToMailbox` consults it where it already chooses a folder. Junk still
wins: what the filter called spam does not become a tidy archive. The rules
run afterwards as they do now, so a rule the reader wrote themselves still has
the last word.

Acceptance: a muted list's next message lands in Archive, read, and never in
the Inbox; unmuting restores the ordinary path; a suspicious message from a
muted list still goes to Junk.

### Milestone 3 — pagination, and saying what is shown

The page's own `first: 200` becomes the house pattern from `mailbox.tsx`: a
page size, an offset, rows merged by key, and a foot that says how many of how
many are shown with a link for the rest.

Acceptance: a mailbox with more subscriptions than one page shows the first
page, says so, and loads the rest on request.

## Validation

`make test`, `make lint-ci`, and the layout audit at 1400 and 390 over the
subscriptions page, including the new button and the foot.

## Risks and what to do about them

- **The orphan header is weaker proof than it looks.** If some sender emits
  `List-Unsubscribe-Post` with no `List-Unsubscribe` on ordinary mail, that
  mail becomes a subscription with no way out. Bounded: it is keyed by sender
  like any headerless list, and the row says plainly that no address was
  offered. Watched by counting what the backfill recovers.
- **A mute is invisible if somebody forgets it.** Mail arriving in Archive
  with nobody remembering why is the failure mode. The row says it is muted,
  and unmuting is the same button.
