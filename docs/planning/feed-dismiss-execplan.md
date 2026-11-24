# Dismissing feed cards, one, a stack or every read one, with undo

## Purpose / Big Picture

A person can dismiss a feed card only from inside its information sheet (iPhone, dashboard); Android has no plain dismiss at all. There is no way to clear a stack or the cards already read, and no undo. After this change a card is dismissed with one gesture on every surface, a stack's cards together, or every read card at once, and each dismissal offers Undo.

## Interfaces (fixed; every surface builds on these)

GraphQL, replacing `DismissAgentFeedItem(feedItemId)`:

    DismissAgentFeedItems(feedItemIds: [String!], feedStackKey: String, isEveryReadCard: Boolean): [String!]!

Exactly one of the three is given. `feedItemIds`: those cards (at most 500), each the caller's own. `feedStackKey`: every card of that stack that is not dismissed. `isEveryReadCard: true`: every card of the caller's feed that is read and not dismissed and has no open choice (a decision still waiting for an answer is not dismissed this way). Returns the ids actually dismissed (already dismissed ones are left out). A card with a durable question has it stopped, as the single dismiss did. One `dismissed` feed event per card, so open feeds update.

    RestoreAgentFeedItems(feedItemIds: [String!]!): [String!]!

Brings back cards the person dismissed (at most 500): clears `dismissed_at` where it is set and no choice settled the card (`chosen_choice_key` is null). Returns the ids restored. A feed event of a new kind `restored` per card. The interaction a dismissal stopped stays stopped; the card's buttons still work, as they always post to the main conversation.

Agent tool `feed`: actions `dismiss` (feed_item_ids, or stack_key, or every_read true) and `restore` (feed_item_ids), only when the person asks ("clear my feed", "bring that back"); risk write.

CLI: `teanode agent feed dismiss [ID...] [--stack KEY] [--read]` and `teanode agent feed restore ID...`, printing how many.

## Clients

- iPhone: a card's context menu (long press) has Dismiss; a stack's top card also "Dismiss All" (the stack's count). The Feed's toolbar menu has "Dismiss Read". Every dismissal shows the app's toast with Undo, which restores those ids.
- Android: a card swipes away (SwipeToDismissBox, either direction) and its long-press menu has Dismiss and, on a stack, Dismiss all; the top bar menu has Dismiss read; a snackbar with Undo.
- Dashboard: each card has a dismiss button (an x icon button in the card header, with a tooltip), a stack's has Dismiss all, the feed header has Dismiss read; a toast with Undo.
- The single-card dismiss in the information sheet stays, calling the new operation with one id.
- Strings in English, Simplified Chinese and Japanese.

## Validation

Server tests: ids, stack, every read (a waiting decision kept), restore (a choice-settled card not restored), ownership (another person's id refused), events. CLI parity test. Dashboard vitest. iPhone kit tests and a build; Android kit and app tests and a build.
