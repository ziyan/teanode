# Dismissed cards: a list of them, drawn like the feed, put back by a swipe

## Purpose / Big Picture

Dismissing a card (#787) offers Undo for a few seconds, and after that the card
is gone from every surface: nobody can find a card they dismissed yesterday,
and an answered decision sits in the feed for 30 days. After this change each
surface has a Dismissed view that lists the person's dismissed cards, drawn
exactly as in the feed (nothing dimmed or disabled: heart, discuss, choices,
info sheet and unread dot all work), and a swipe puts a card back. An answered
card leaves the feed by itself a day after the answer, into the dismissed
cards. A card the agent withdraws is removed, so it is not among them.

## Interfaces (fixed; every surface builds on these)

GraphQL, `ListAgentFeed` gains `isDismissedOnly: Boolean`:

- true lists the caller's dismissed cards that have not expired, the most
  recently dismissed first, with a cursor on `(dismissed_at, id)`.
- It does not fold stacks: every dismissed card is an entry of its own, with
  `stackCardCount` 1 and `stackUnreadCount` 0 or 1. Clients group entries by
  `feedStackKey` themselves and draw a group as a deck.
- `includeDismissed` is unchanged; `isDismissedOnly` wins when both are given.
- `unreadCount` is still the feed's unread count, not the dismissed list's.

A card gains `dismissedAt: String` (RFC 3339, null when not dismissed), so a
client can say when it was dismissed. `isDismissed` stays.

Acting on a dismissed card that has not expired works:

- like and unlike, discuss, mark read, show less, and choosing a choice.
- A choice leaves the card dismissed; a dismiss choice on a dismissed card only
  records the answer.
- Show less on a dismissed card files the preference and nothing else.

`RestoreAgentFeedItems(feedItemIds)` changes:

- It restores a card a choice settled.
- A card settled by a dismiss choice ("Not now") has its answer cleared, so it
  can be answered again.
- A card answered with an action keeps its answer, since the action was
  carried out.
- A card is left out (not restored, not an error) when a card that is not
  dismissed holds its coalesce key, or when it has expired.
- A restored card stays read.

The hourly sweep dismisses every card that was answered with a choice more than
a day ago and is not dismissed, with a `dismissed` feed event each, so open
feeds drop it. (`models.FeedAnsweredLifetime`, 24 hours.)

The `feed` tool:

- `withdraw` deletes the card it posted (one `dismissed` event, so open feeds
  drop it). It is not among the dismissed cards and cannot be restored.
- `list` takes `dismissed_only: true` for the dismissed cards.
- A goal's own turn (a headless run whose conversation is a goal) cannot
  `restore`: what the person dismissed stays dismissed until they bring it
  back.

CLI: `teanode agent feed list --dismissed` and `teanode agent feed withdraw ID`.

## Clients

Every surface:

- The Dismissed view is reached from the feed's menu: the iPhone Feed toolbar
  menu ("Dismissed Cards", beside "Dismiss Read"), the Android top bar menu,
  and a "Dismissed" button in the dashboard feed header (route
  `/feed/dismissed`, so the URL holds it, with a way back to the feed).
- It lists `ListAgentFeed(isDismissedOnly: true)` pages, cards drawn by the same
  card view as the feed with nothing dimmed or hidden.
- Consecutive entries with one non-empty `feedStackKey` are grouped into one
  deck, the newest on top, opened like a feed stack but from the entries
  already read (the server's stack read lists only cards that are not
  dismissed).
- Restore puts a card back:
  - a swipe on touch screens: iOS `.swipeActions` or the existing swipe
    gesture, Android the existing `SwipeToDismissBox`, the web a horizontal
    pointer swipe on touch;
  - a small restore icon button in the card footer on the web (with a
    tooltip);
  - a Restore button in the info sheet, in place of Show less and Dismiss.
- A restored card leaves the list at once and the feed reads again, and its id
  stays hidden from a page read already on the way. The toast says "Card
  restored" (no undo).
- Empty: "Nothing dismissed", with one line saying dismissed cards are kept
  until they expire.
- In the feed itself, when a dismissal, show less or restore leaves the list
  with no cards, show the loading mark and read the list again; say empty
  only when that read is empty. Cards this page took off stay hidden from that
  read.
- A dismissed card's choices are shown and can be pressed (Android hid them
  with `!item.isDismissed`).
- Strings in English, Simplified Chinese and Japanese on every surface.
- Demo servers (iOS `FeedDemo.swift`, Android `FeedDemo.kt`) answer
  `isDismissedOnly` with a few dismissed cards, two of them in one stack, so
  the screenshots show the view; add a screenshot of it (iOS `shoot` lines in
  `.github/workflows/ios.yml`, Android a Roborazzi test).

## Validation

- Server tests:
  - the dismissed-only list: order, cursor, no folding, expired left out,
    another person's cards left out;
  - acting on a dismissed card: like, choose (it stays dismissed), show less
    (preference only);
  - restore: a dismiss-chosen card cleared, an action-chosen card keeps its
    answer, a key held by an open card left out;
  - the sweep of answered cards;
  - withdraw deletes;
  - a goal turn's restore refused.
- CLI parity test.
- Dashboard vitest and tsc.
- iPhone kit tests and a build on the Mac.
- Android kit and app tests on station.
