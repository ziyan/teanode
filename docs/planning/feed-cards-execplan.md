# Feed cards: rules, resolved status, charts and camera stills

## Purpose / Big Picture

A feed card today has a body, up to eight fact rows, a layout (text, list,
weather or agenda) and pictures. This change does four things:

- It writes down the rules a good card follows, and fixes small things the
  person sees.
- A card whose problem cleared is marked resolved, instead of being followed
  by an "all clear" card.
- A card can carry a chart: a series over time with a reference line and
  markers, or bars, with a line saying where and when the numbers were read.
- A card can carry a still from one of the person's cameras, with why that
  camera was chosen.

Built on the dismissed cards change (#792), as one pull request for every
surface.

## Interfaces (fixed; every surface builds on these)

### Migration 0284 (one file pair)

Columns on `agent_feed_item`, all with defaults, so the previous binary reads
and writes the table as before:

- `resolved_at timestamptz NULL`
- `resolution_note text NOT NULL DEFAULT ''`, one line of at most 160
  characters
- `resolved_by_kind text NOT NULL DEFAULT ''`, one of `goal`, `person` or
  `agent`
- `resolved_by_id text NOT NULL DEFAULT ''`, the goal's conversation id, or the
  person's user id
- `chart_blocks jsonb NOT NULL DEFAULT '[]'`

### Resolved

- **Model:** `models.AgentFeedItem` gets `ResolvedAt`, `ResolutionNote`,
  `ResolvedByKind` and `ResolvedByID`. `IsResolved()` is `ResolvedAt != nil`.
- **GraphQL:**
  - `ResolveAgentFeedItem(feedItemId: String!, resolutionNote: String): AgentFeedItem!`
  - `UnresolveAgentFeedItem(feedItemId: String!): AgentFeedItem!`
  - A card gains `isResolved`, `resolvedAt`, `resolutionNote` and
    `resolvedByKind`.
  - Each change announces a feed event of a new kind, `resolved` or
    `unresolved`.
- **Rules:**
  - A second resolve is refused ("already resolved"), so the first resolution
    and its note stand.
  - A goal's own turn may resolve only a card it posted, and never one the
    person resolved.
  - Resolving does not move the card, make it unread, or push.
  - Posting the same subject again (a coalesce key replace) clears the
    resolution: it is a new state of the subject.
  - An update that changes the text keeps the resolution.
- **Tool:** the `feed` tool gets `resolve` (feed_item_id and resolution_note)
  and `unresolve`. The description says to resolve a card when its problem
  clears, rather than posting a "cleared" card. Post a new card only for a new
  problem, or one that got materially worse.
- **CLI:** `teanode agent feed resolve ID [--note TEXT]` and `unresolve ID`.
- **Clients:**
  - A resolved card shows a success-colored chip, "Resolved 06:41", with the
    time in the person's zone, beside the time in the header.
  - The note shows as one muted line under the body.
  - The card is drawn muted, with no shadow.
  - The info sheet has Resolve (asking for an optional note) or Unresolve.

### Charts

- **Model:** `models.FeedChartBlock` in `chart_blocks`, at most 2 per card.
  - `chartKind` is `series` or `bars`.
  - `chartTitle`, plus `unit`, such as "USD" or "steps".
  - `provenance` is required: one line of at most 120 characters saying where
    and when the numbers were read, such as "Read from the bank at 09:40
    EST".
  - For `series`:
    - `xLabels`: 2 to 60 labels in order.
    - `series`: 1 to 3 entries, each `{seriesName, seriesRole, values}`.
      `seriesRole` is `measured` (the default), `reference` (a line to compare
      with, such as last month or the average) or `projection` (what is
      expected ahead). `values` has one number per label, or null for none.
    - `markers`: up to 3 entries, each `{markerAt, markerLabel}`, where
      `markerAt` is one of the `xLabels`.
  - For `bars`: `bars`, 1 to 8 entries, each `{barLabel, barValue}`, never
    negative, largest first.
- **Refused, with the field named:**
  - lengths out of bounds;
  - a marker not on the axis;
  - a negative bar;
  - a missing provenance;
  - an unfinished bucket (such as the current day) drawn as `measured`, when
    the model says it is unfinished. Do not try to detect this; just say it
    in the tool description.
  - Also refused: three or more fact rows that are each a time or date plus a
    number. They belong in a series, and the refusal points there.
- **GraphQL:** a card's `chartBlocks` (the same fields, as objects).
  `PostAgentFeedItem` takes `chartBlocks: [FeedChartBlockInput!]`.
- **Tool:** `chart_blocks` on post and update.
- **Clients:** draw them with the platform's chart library:
  - web: inline SVG if there is no chart library in web/package.json;
  - iOS: Swift Charts;
  - Android: Compose Canvas, or Vico if already a dependency.
  - A reference series is a solid ink line, a projection a dashed one, and
    measured uses the accent.
  - Markers are labeled vertical rules.
  - Bars are horizontal, largest first, the value with its unit at the end.
  - The provenance is a small muted line under the chart.
  - A client that does not know a field draws the series as measured.

### Camera stills

- **Model:** `models.FeedMedia` gains a kind `camera_still`, with:
  - `cameraName`;
  - `capturedAt` (RFC 3339, on the camera's own clock with its offset, never
    converted to UTC);
  - `stillReason`: why this camera, such as "the camera at the front door,
    where the parcel was left", at most 120 characters;
  - `playbackUrl` and `liveUrl` (https or the app's own scheme, optional).
  - The picture is kept as `ImageFileID`, as a page's picture is.
- **Getting the picture:** find how a camera skill's snapshot (the UniFi Protect
  and Home Assistant skills, `internal/skills` or the connected tools) reaches
  the run: an image tool result, an attachment, or a file. Make the `feed`
  tool's media entry `{camera_still: {...}}` take that image by the
  reference the run has for it, and store it like a page's picture.
  - A still whose image cannot be read is dropped with a note in the tool's
    answer, and the card still posts.
  - An update keeps a still only if it is unchanged: a new moment needs a new
    still.
  - If no camera image can reach a run today, implement the fields and the
    drawing, accept the media with a `photoId` (the person's photo of that
    moment) instead, and say so in the report.
- **Clients:**
  - Draw the still like a picture.
  - Under it, the caption, then a muted line, "<camera name> · 14:02:11",
    with the camera's time.
  - Then the reason.
  - Tapping opens `playbackUrl`, and a "Watch live" button opens `liveUrl`
    when it is set.

### Card rules and tidy-ups

- **Rules, in the `feed` tool description and the goal prompt's feed lines
  (`internal/agent/goal.go` `goalFeedLines`):**
  - say what happened, why it matters and one or two next steps;
  - numbers only from this turn's tool results, each chart with its
    provenance;
  - one card per subject;
  - `list` first so nothing is said twice;
  - never rewrite a card into a different subject;
  - plain words, with no ids or tool names;
  - the title says what the body says;
  - silence is the default.
- **Server:**
  - Em and en dashes in a card's title, body, fact rows, choice labels, chart
    text and layout text are rewritten: a spaced dash becomes a comma, an
    unspaced one a hyphen.
  - A title, label or caption a little too long is cut with an ellipsis, and
    the tool's answer lists what was cut, instead of refusing.
  - An `update` keeps every field it leaves out (title, kind, body, choices,
    media, charts). Add a test.
- **Clients:**
  - Every time a card shows names the zone when it is not the device's own.
  - Choices share the width equally when they fit on one line (the web uses
    `flex: 1 1 0`).
  - An answered card folds to its title and one muted line with the choice
    and outcome, with "Show the card" to open it again.

## Validation

- Server:
  - resolve and unresolve rules, including the goal-own-card, person-wins,
    first-kept and repost-clears cases;
  - chart decode and refusals;
  - fact rows refused as a series;
  - dash rewrite;
  - cut-not-refused;
  - update keeps fields;
  - camera still stored and dropped when unreadable;
  - CLI parity.
- Clients:
  - web vitest of resolved chip and note, series and bars, camera still,
    folded answer, equal choices;
  - iOS kit decoding tests, demo cards and shoot lines for a resolved card, a
    series chart, bars and a camera still;
  - Android kit tests and Roborazzi shots of the same.
