# The agent's feed

What the agent says without being asked, it says as cards in a feed instead of
as turns in the person's main conversation. A card is a headline, a few lines
of Markdown, sometimes a short table of facts, and sometimes buttons. This says
how a card is posted, replaced, stacked, answered and read, and where each
unasked thing the agent does now goes. The plan with its decisions is
`docs/planning/agent-feed-execplan.md`.

## The pieces

`internal/models/agent_feed.go` is the card (`AgentFeedItem`) and its enums:
kind (update, decision, alert, report, idea, news), source (goal, alert,
speak_first, coding_session, agent), urgency (now, today, whenever),
choice role (action, dismiss, discuss, open) and layout (text, list, weather,
agenda). Migration 0276 makes the table `agent_feed_item` and the column
`agent.feed_destination`; migration 0281 adds
`agent_conversation.is_goal_notified_when_posted`; migration 0284 adds the resolution
columns and `chart_blocks`. `internal/db/database_feed.go` keeps
the rows. `internal/agent/agent_feed_post.go` posts, `agent_feed_choose.go`
answers, likes and settles, `feed_layout.go` checks a layout's content,
`reference_feed.go` tells a turn what a card says. `internal/api/v1api/apigraph/agent_feed.go`
is what the dashboard, the phone apps, the command line (`teanode agent feed`)
and the agent's `feed` tool all call.

## Posting

`Agent.PostFeedItem` is the one way in, for the tool, the API and the agent's
own jobs. It trims and bounds the card (at most eight fact rows, four buttons,
twelve source links), and refuses what cannot be shown with a message that says
what is wrong.

- **Replacing.** A post whose `coalesce_key` matches an open card of the same
  agent replaces that card's content in place: it keeps its id and its likes,
  moves to the top, becomes unread, and forgets any choice made on it. The
  unique index on (agent, coalesce key) where the card is not dismissed makes
  this an upsert. A dismissed card frees its key.
- **No news twice.** A `source_links` address already in an open card of the
  last 30 days refuses the post, except from the card being replaced.
- **A decision's question.** A post with an action choice, made from a turn,
  raises a durable question (`models.AgentInteraction`) in that turn's
  conversation with the buttons' labels as its options, and keeps its id on the
  card. Nothing waits on it. A card that replaces another stops the old
  question.
- **Expiry.** A card leaves the list 30 days after it is posted or settled,
  and an unanswered decision never expires.

## Stacks, brief order and layouts

Cards of one source pile up, so the list shows one entry per stack: the top
card, with `stackItemCount` and `stackUnreadCount`. The stack key defaults to
`goal:<id>` for a goal and `speak_first:<reason>` for the agent speaking
first; a turn of a goal on set times stacks under `goal:<id>:<job id>`, so each
morning's brief is a stack of its own. A card with a button that acts, dismisses or opens,
and an alert never stack, because each needs the person on its own. A coding
session's questions are the exception: a busy day of sessions asks many, so
they stack by the day on the person's clock, `coding:<date>`, titled
"Coding sessions, <day>" in the person's language, and each card still answers
its own session once the stack is open. Inside a stack the cards with a `stack_position` come first,
the lowest first, and the rest newest first; the top of a stack is its lowest
position, else its newest card. `ListAgentFeedStack` pages one stack with an
offset, since its order is not by time alone. `feed_stack_title` is the first
title any card of the stack gave.

A card has a layout and, for the layouts but text, structured content that
every app draws natively: `list` (at most 10 rows, `rowUrl` http or https),
`weather` (at most 12 hourly entries) and `agenda`. The content is checked
strictly as it is posted, and a malformed one is refused with the field named.
The body stays plain Markdown for every layout.

## Pictures and videos

A card carries `feed_media`: at most six pictures and videos of its own and a
thumbnail for each row of a list card (`listRowIndex`). An entry is one of the
person's photos or videos by `photoId`, checked with the photo's access rules
as the card is posted and served from the photo preview routes, or the
picture of a web page by `pageUrl`. A post that gives none shows the picture of
a text card's first source link and of a list card's linked rows.

A page's picture is fetched after the post, in the background, by the same
fetcher as link previews: the page is read, its picture fetched, scaled to at
most 1200 pixels on its long side, kept in the file store and served from
`/api/v1/feed/items/{feedItemId}/media/{mediaKey}` to the card's person only.
Until then the entry is not shown; once it is kept (or found missing), the card
is announced as replaced and open feeds draw it. A page is fetched only when
its address appears word for word in a tool's answer in the conversation that
posted the card, or the person posted it: an address the model composed could
carry what it read in its query, so it is never asked for. A card replaced or
changed keeps the pictures of the pages it still shows and deletes the rest.

The apps draw one picture full width at the top of the card, two to four as a
grid and more as a row that scrolls sideways; a video plays in place. A stack
is drawn as a deck that fans out in place when tapped and gathers back.

## Resolved cards

A card whose problem cleared is marked resolved instead of being followed by a
card that says all is clear. `ResolveAgentFeedItem` (`teanode agent feed
resolve`, the tool's `resolve`) sets `resolved_at`, a one-line `resolution_note`
of at most 160 characters (cut, and said so in `postNotes`, when longer) and who
did it: `resolved_by_kind` is `goal` (with the goal's conversation as
`resolved_by_id`), `person` (their user id) or `agent`. `UnresolveAgentFeedItem`
takes it all off. Each change is announced as a `resolved` or `unresolved`
event, and neither moves the card, makes it unread or notifies the phones.

- **The first resolution stands.** Resolving a card already resolved is refused
  ("already resolved"), so a later note never replaces the first.
- **A goal's own turn** resolves only a card it posted (its `feed_source_id` is
  that goal), and never one the person resolved: it cannot replace that, nor take
  it off. The tool names the goal's conversation as `runConversationId` when the
  turn is a goal's own (the same test as the restore refusal), and the operation
  holds it to those rules. A person tapping, or the command line, is `person`;
  the agent's tool outside a goal's own turn is `agent`.
- **A shell command of the agent** (the command line run by the agent, which
  sends the `X-Teanode-Agent-Command` header with the person's token) cannot
  resolve, unresolve, restore or withdraw: the operations answer permission
  denied, saying these are for the person and for the feed tool inside a turn.
  This holds whatever `runConversationId` the command sends, so the goal limits
  above cannot be skipped by leaving it out. The in-process feed tool carries no
  request and is not affected.
- **Posting the same subject again** (a coalesce key replace) clears the
  resolution, since it is a new state of the subject. An `update` that changes
  the text keeps it.

The rules live in `agent.ResolveFeedItem` and `agent.UnresolveFeedItem`, which
take the caller's transaction, so the API and the tests share them.

## Charts

A card carries at most two charts in `chart_blocks` (`models.FeedChartBlock`),
drawn natively by every app.

- `chartKind` is `series` or `bars`; `chartTitle`; `unit` ("USD", "steps");
  `provenance`, required, one line (120 characters) saying where and when the
  numbers were read, such as "Read from the bank at 09:40 EST".
- A **series** has `xLabels` (2 to 60, in order), 1 to 3 lines
  `{seriesName, seriesRole, values}` and up to 3 `markers {markerAt,
  markerLabel}`. `values` has one number per label, null for none; `seriesRole`
  is `measured` (the default, drawn in the accent), `reference` (a solid ink line
  to compare with) or `projection` (dashed). A marker's `markerAt` is one of the
  `xLabels`.
- **Bars** are 1 to 8 `{barLabel, barValue}`, never negative. The server sorts
  them largest first.
- A value of absolute size over 1e15 is refused with its field named, and so is
  an `xLabels` entry that repeats an earlier one (a marker or a reader could not
  tell the two places on the axis apart).

A chart that cannot be drawn is refused with the field named, such as
`chartBlocks[0].series[1].values has 3 entries; give one number for each of the 4
xLabels`. The server does not detect an unfinished bucket (today's partial
total) drawn as `measured`: the tool's description tells the model to end the
series at the last finished bucket or draw it as a projection.

Three or more fact rows that are each a time or date and a number are refused
too, with a pointer to `chart_blocks`: they are a series, and a table of them
cannot show the shape. The check reads the row's label as a time or date (an ISO
date, a clock time, a month or weekday name) and its value as a number with an
optional sign, currency symbol or unit.

## Camera stills

A media entry of kind `camera_still` is a picture from one of the person's
cameras, with `cameraName`, `capturedAt` (RFC 3339 on the camera's own clock with
its offset, never converted to UTC), `stillReason` (why this camera, at most 120
characters), and optional `playbackUrl` and `liveUrl` (https or a `teanode://`
address). A tap on the still opens the playback address and Watch live the live
one.

**How the picture reaches the run.** A skill's step with `result: image` (the
camera skills' snapshot) is fetched by the server, shown to the model, and kept
as a file of the conversation, an `AgentAttachment` whose id the tool's answer
gives as `files[].attachment_id` (`keepSkillFiles` in `tools_skill.go`). The
`feed` tool's media entry `{camera_still: {attachment_id, ...}}` names that id.
As the card is stored, `keepFeedStills` reads the attachment (it must be the
agent's own and an image, and when the post names the conversation of the run
that makes it, of that conversation), scales it to 1200 pixels on its long side with the
same code as a page's picture, keeps it in the file store as `ImageFileID`, and
the card serves it from `/api/v1/feed/items/{feedItemId}/media/{mediaKey}`. The
person's own photo of that moment (`photo_id`) is accepted instead, and is served
from the photo routes.

- A still whose picture cannot be had (no such attachment, not a picture, cannot
  be decoded) is left off, with a line in `postNotes`, and the card still posts.
- An update or a replacing post keeps a still only when it names the same camera
  and moment: a new moment needs a new picture, and a still of another moment
  with none is left off with a note.

## Rules of a card

The `feed` tool's description and the goal prompt's feed lines
(`goalFeedLines`, `goalOnTimesFeedLines`, which share `feedCardRules`) say what
a good card is: what happened, why it matters and one or two next steps; numbers
only from this turn's tool results, each chart with its provenance; one card per
subject, with `list` first so nothing is said twice; never rewrite a card into a
different subject; plain words with no ids or tool names; a title that says what
the body says; silence as the default. When a problem clears, resolve its card
rather than posting an all clear; post a new card only for a new problem or one
that got materially worse.

The server holds a few of them itself, in `NormalizeFeedItem`:

- **Dashes.** Em and en dashes in a card's title, body, fact rows, choice labels,
  chart text, layout text, captions and stack title are rewritten: a spaced dash
  becomes a comma, an unspaced one a hyphen, a spaced dash at a line's start is a
  list bullet and one at the edge of the text goes.
- **Cut, not refused.** A title, label or caption a little too long is cut with
  an ellipsis, and the answer's `postNotes` lists the fields cut ("title was cut
  to 200 characters"). A change does not list again what an earlier post cut.
- **An update keeps what it leaves out.** `PostAgentFeedItem` with a `feedItemId`
  writes only the fields it was given: title, kind, body, choices, media, charts
  and layout stay as they were.

## Where unasked words go

- **Alerts.** The alert job decides as it did, within its bounds, and says each
  alert as a card of kind alert, with the alert's subject as coalesce key.
  Urgent alerts are cards of urgency now and notify the phones; the rest are
  today. The `agent_alert` row is still written, so repeats and mutes work.
- **Speaking first.** Ideas become a card made with no turn (buttons Do it and
  Not now). Memory checks and trip notes run a turn in a run conversation of
  their own that is told to post one card with the feed tool. The introduction
  and the phone tour stay in the main conversation, since they are
  conversations. `mail_organize` is a look, only when the Inbox holds 30
  or more routine messages from the last week that no earlier look has seen: a turn that reads and changes
  nothing and posts at most one decision card (Approve, Discuss, Not now).
- **Goals** post with the tool during their turns, told how in their check-in.
  With `is_goal_notified_when_posted` the first card of a turn of a goal on set
  times sends one quiet notification, "<goal> is ready".
- **Coding sessions.** The asking event posts a decision card as well as the
  phone notification. Its source id is `computer/pane`, its coalesce key ends
  in the question's fingerprint, and each option is a choice `option:N`. A
  question that takes several options or words has one `open` choice. When the
  session reports the question answered the card settles as "Answered on the
  computer".

The per-agent setting `feedDestination` (`feed` or `main`) sends alerts,
speaking first and what goals post back to the main conversation.

## Choosing

`ChooseAgentFeedItem` settles a card once: a second answer is refused with
"already answered". By the choice's role:

- **dismiss** settles the card and answers its question with no turn and no
  message.
- **action** says the choice in the main conversation as the person's own
  message, "<label>: <card title>", with the card attached as a reference
  (`AgentReference.FeedItemID`), which starts an ordinary turn there. The card
  stores that conversation, shows `isOutcomePending` while the turn runs, and
  then the first line of its answer as `outcomeText`. A card shows as pending
  for at most 30 minutes, so a restart cannot leave one waiting forever.
- **a coding session's option** answers the session directly with no turn and
  no message, and only then settles the card.
- **discuss** and **open** change nothing on the server: the screen opens the
  conversation (`DiscussAgentFeedItem` answers the main conversation's id and
  marks the card read) or the session.

## Dismissing and restoring

`DismissAgentFeedItems` closes cards: exactly one of `feedItemIds` (at most
500, each the caller's own, or none is closed), `feedStackKey` (every open card
of that stack) or `isEveryReadCard` (every read card that is not a decision
still waiting for an answer). It answers the ids this call closed, leaves out
cards already closed, stops the durable question behind a decision, and sends
one `dismissed` event per card. `RestoreAgentFeedItems` (at most 500 ids)
brings back dismissed cards, still read, and answers the ids restored, with
one `restored` event each. A card answered "not now" (a dismiss choice) has
its answer cleared, so it can be answered again; one answered with an action
keeps its answer, since the action was carried out. A card is left out when a
card that is not dismissed holds its coalesce key (the newer word on that
subject, including one a post opened while the restore was running: the update
is made one card at a time under a savepoint, and a unique violation on the key
leaves just that card out), or when it expired; of two dismissed cards on one subject only the
one dismissed last comes back. A question a dismissal stopped stays stopped,
though the card's buttons still work because they post to the main
conversation. Every surface offers Undo after a dismissal, which is a restore
of the ids the dismissal answered. The agent's `feed` tool has `dismiss` and
`restore` actions for when the person asks, and a goal's own turn cannot
restore; `teanode agent feed dismiss` and `restore` do the same. The dashboard
has a dismiss button on each card, Dismiss all on a stack and Dismiss read
above the list.

**The dismissed cards.** `ListAgentFeed(isDismissedOnly: true)` lists the
dismissed cards that have not expired, the most recently dismissed first, with
a cursor on when they were dismissed. Stacks are not folded: each card is an
entry, and the screens group neighboring entries of one stack into a deck
themselves. Every surface has a Dismissed view (the dashboard's
`/feed/dismissed`, the Feed menu on both phones, `teanode agent feed list
--dismissed`, the tool's `list` with `dismissed_only`), which draws the cards
exactly as the feed does. A dismissed card can still be liked, discussed,
marked read and answered; it stays dismissed, and show less on it only files
the preference. A swipe, the footer's restore button (dashboard) or Restore in
the info sheet puts a card back.

**Answered cards leave by themselves.** The server's hourly sweep dismisses a
card a day after it was answered with a choice
(`models.FeedAnsweredLifetime`), with a `dismissed` event, so it is found
among the dismissed cards rather than crowding what still needs the person.
The sweep takes only a card not changed since the cutoff (`updated_at` as well
as `chosen_at`), and a restore stamps `updated_at`, so a restored card stays a
day. It runs last in the hourly cleanup, and its error is logged without
stopping the other cleanups; it reads the unread count once for each person,
and still sends one event for each card.

**Withdrawn is gone.** The tool's `withdraw` (`WithdrawAgentFeedItem`,
`teanode agent feed withdraw`) deletes the card: a card the agent took back
because it no longer holds is not among the dismissed cards and cannot be put
back. A card the person answered or resolved is theirs and is refused ("dismiss
it instead"); a goal's own turn (the tool sends `runConversationId`, and the
operation checks it again) may withdraw only a card whose feed source is that
goal; and the files of the card's pictures are deleted with it, once the
deletion commits. A shell command of the agent is refused, as for resolving.

## Liking

A like or an unlike is written to the agent's memory as a preference on the
page `self/feed`, and "show less like this" writes the opposite and dismisses
the card. Goals read that page before choosing what to post. A sentence already
on the page is not filed twice.

## Reading and live updates

`ListAgentFeed` pages newest first with a cursor. `unreadCount` counts cards,
not entries. `agentFeedChanged` (subscription field `AgentFeedChanged`) says a
card was posted, replaced, settled, dismissed, restored, resolved or unresolved, with the unread count; it
travels by `pg_notify` on the `feed_event` channel like the shopping and call
events.

## Phone notifications

Only a card of urgency now notifies, as content kind `agent_feed` with
`feedItemId`. A decision carries its choices as buttons, `feed:<choice key>`,
at most four. A coding session's card sends no second notification, since the
existing herdr question notification stands for it. A goal's "ready"
notification is sent with `isQuiet`.
