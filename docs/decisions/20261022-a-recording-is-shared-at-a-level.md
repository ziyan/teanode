# A recording is shared at a level, and the audio never is

- Status: proposed
- Date: 2026-10-22
- Deciders: the-owner

## Context

A recording is a meeting, and the people who were in it, a household or a
team, often want what came out of it: what was decided, who said what. Until
now nobody but the account that made it could read one, so the way to share a
meeting was to copy the summary into a message by hand.

A recording is also the most personal thing the server keeps. It holds other
people's voices, and they did not agree to be shared with anybody by agreeing
to be recorded. Sharing has to be a choice the owner makes knowing that.

## Decision

A person shares one of their recordings with a circle or with one person in a
circle they share, at one **recording share level**:

- `summary`: the title, the date, the length, the generated summary, and the
  names of the speakers the owner confirmed.
- `transcript`: the summary, and the transcript as the owner reads it: the
  latest completed version of each original with the owner's corrections to
  the words and to who spoke.

Never at either level: the audio or video, the voice samples, the contacts a
speaker's name is tied to. Those are the parts that identify or reach a
person beyond what they said, and the owner's choice to show a meeting's
words is not a choice to hand over anyone's voice.

Only the owner shares, and every surface says before it does that the other
people who speak have not agreed. The agent's `share` is a granting call, so
it asks the person first, with that sentence on the card.

What the agent reads of another person's recording is read where it lives and
never copied, as a shared page is. It reads one only in a conversation the
person started, never in a chat of several people and never in a dream or a
background run, and nothing it read is filed in memory as the person's own.

## Consequences

One table, `recording_share`, shaped like `calendar_share`. A share reaches a
person only while the two still share a circle, and a share to a circle only
while the owner is still in it; both are decided by joining `circle_member` in
the query, so nothing waits for a cleanup to take effect, and a recording with
no owner or one that was deleted reaches nobody.

Redaction is one function, `models.RedactRecording`, which names the fields
that leave, so a field added to a recording later is private until somebody
decides otherwise.

A listing of the shared recordings leaves out the speaker names and the words,
because finding the names reads the whole timeline of each recording.
