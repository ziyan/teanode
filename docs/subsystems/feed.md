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
`agent_conversation.is_goal_notified_when_posted`. `internal/db/database_feed.go` keeps
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

## Where unasked words go

- **Alerts.** The alert job decides as it did, within its bounds, and says each
  alert as a card of kind alert, with the alert's subject as coalesce key.
  Urgent alerts are cards of urgency now and notify the phones; the rest are
  today. The `agent_alert` row is still written, so repeats and mutes work.
- **Speaking first.** Ideas become a card made with no turn (buttons Do it and
  Not now). Memory checks and trip notes run a turn in a run conversation of
  their own that is told to post one card with the feed tool. The introduction
  and the phone tour stay in the main conversation, since they are
  conversations. `mail_organize` is a weekly look, only when the Inbox holds 30
  or more routine messages from the last week: a turn that reads and changes
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
subject), or when it expired; of two dismissed cards on one subject only the
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

**Withdrawn is gone.** The tool's `withdraw` (`WithdrawAgentFeedItem`,
`teanode agent feed withdraw`) deletes the card: a card the agent took back
because it no longer holds is not among the dismissed cards and cannot be put
back.

## Liking

A like or an unlike is written to the agent's memory as a preference on the
page `self/feed`, and "show less like this" writes the opposite and dismisses
the card. Goals read that page before choosing what to post.

## Reading and live updates

`ListAgentFeed` pages newest first with a cursor. `unreadCount` counts cards,
not entries. `agentFeedChanged` (subscription field `AgentFeedChanged`) says a
card was posted, replaced, settled, dismissed or restored, with the unread count; it
travels by `pg_notify` on the `feed_event` channel like the shopping and call
events.

## Phone notifications

Only a card of urgency now notifies, as content kind `agent_feed` with
`feedItemId`. A decision carries its choices as buttons, `feed:<choice key>`,
at most four. A coding session's card sends no second notification, since the
existing herdr question notification stands for it. A goal's "ready"
notification is sent with `isQuiet`.
