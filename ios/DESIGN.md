# How the TeaNode app looks and behaves

The app should feel like it was made by Apple: the system's own controls,
materials, type and motion, with TeaNode's green as the one color of its own.
A person who knows iOS should know how every screen works before touching it.
These rules apply to every screen, and a screen is checked against them before
its pull request is opened.

## The platform

- The app targets iOS 26 and uses its design language, Liquid Glass: the
  translucent material Apple gives navigation bars, tab bars, toolbars, sheets
  and floating controls. It is built with SwiftUI and nothing else: no UIKit
  view unless SwiftUI has no equivalent, and no third-party UI library.
- Prefer the system component to anything drawn by hand. `NavigationStack`,
  `List`, `Form`, `TabView` with `Tab`, `toolbar`, `sheet`, `searchable`,
  `Menu`, `swipeActions`, `contextMenu`, `ShareLink`, `PhotosPicker`,
  `ContentUnavailableView`, `ProgressView`, `Label`. They bring Liquid Glass,
  accessibility, Dynamic Type and right-to-left layout for free, and they keep
  looking current when iOS changes.

## Liquid Glass

- Bars, toolbars and sheets get glass from the system. Do not paint a
  background behind them or set a custom blur.
- Glass is for controls that float over content: the message composer, call
  controls, a floating action. Use `.glassEffect()` on them, group neighbors in
  a `GlassEffectContainer` so they blend and morph together, and give
  interactive ones `.interactive()`.
- Buttons go by where they sit. In a Form or List an action is a plain row
  button (it takes the tint; `role: .destructive` when it deletes), a row that
  opens a screen is a `NavigationLink`, and sharing is a `ShareLink`: never a
  capsule inside a row. A screen's one primary action outside a list is a
  full-width `.borderedProminent` capsule, `.controlSize(.large)`, beside a
  `.bordered` one when there is a second. Toolbar actions are toolbar items.
  `.glass` and `.glassProminent` only on controls floating over content.
  A screen has at most one prominent button.
- Never put glass on content: a message, a card, a list row. Content sits on
  the plain background; glass sits on top of it.
- Let a tab bar get out of the way while reading
  (`.tabBarMinimizeBehavior(.onScrollDown)`), and let a scroll view's content run
  under the bars (no fixed insets that stop at the bar's edge).

## Color

- Use semantic colors only: `.primary`, `.secondary`, `.tertiary` for text;
  `Color(.systemBackground)` and `Color(.secondarySystemBackground)` for
  surfaces; `.red` for destruction. Never a fixed black, white or gray: every
  color must hold in light mode, dark mode and increased contrast.
- The accent is TeaNode green, the `AccentColor` in the asset catalog, from the
  logo (`#729D39`, lighter in dark mode so it keeps its contrast). It marks what
  can be pressed and the person's own messages. Nothing else is green, so
  green always means the same thing.
- State colors carry a symbol or words too. Red alone never says "failed".

## Type

- Text styles only: `.largeTitle`, `.title`, `.headline`, `.body`, `.callout`,
  `.subheadline`, `.footnote`, `.caption`. No fixed point sizes, so Dynamic Type
  works up to the accessibility sizes, and every screen is checked at the
  largest.
- Codes and identifiers are `.monospaced()`; numbers that change in place use
  `.contentTransition(.numericText())`.
- Markdown in an answer renders with the system's `AttributedString`. Headings
  inside an answer are no larger than `.headline`.

## Symbols and images

- SF Symbols for every icon, with `.symbolRenderingMode(.hierarchical)` by
  default. A state change animates its symbol (`.symbolEffect`), such as the
  microphone while listening.
- An icon-only button always has an accessibility label.

## Layout

- Content first, chrome last. A conversation is the messages and the composer;
  everything else is in the toolbar or a menu.
- A conversation reads like talking to a person, as in Messages: both sides
  are bubbles. The agent's sit on the left in `secondarySystemBackground`; the
  person's on the right in the accent at 20 percent, with primary text. While
  the agent works, its bubble shows typing dots. Otherwise green is kept for
  what can be pressed: the send button, links, a chosen answer. The time shows
  between two messages far apart.
- Cards that wait for the person (approve, a question, a coding session
  asking) share one surface: `secondarySystemBackground`, an 18 point corner,
  a hairline edge. A small secondary header says what the card is; answers
  are rows with a radio or a check, not buttons of their own; actions are
  standard capsule buttons of equal width, the prominent one first.
- The composer is one rounded field with its buttons inside it: the words on
  top, adding files at the bottom left, calling and sending at the bottom
  right.
- A list row says who, what and when, and only marks what asks for attention
  (high priority, a reply owed, a sender whose checks failed). Routine
  categories and passing checks are not drawn.
- The tools the agent used in a row are one quiet line ("Used calendar,
  mail"), `.caption` and `.secondary`, opening on tap.
- A turn the agent took on its own is one short centered line saying where
  it came from ("A coding session asks you"), never the instructions it was
  started with.
- Respect the safe areas and the keyboard; standard system spacing; tap
  targets at least 44 points.
- Actions are full-size: a row of their own with a symbol, as "Add a
  receipt", or a standard capsule button. Never a footnote-size link or
  several small buttons side by side in a row or a card. Destructive ones
  are red, symbol and words alike.
- Show content, do not fold it. Lines of a receipt, a quoted message, a
  summary: show them whole, in body text, one row each. A long list moves to
  a screen of its own behind a row, as iOS Settings does; it does not open in
  place with a disclosure arrow.
- A swipe right anywhere on a screen opens the side menu, so rows have no
  swipe-right actions there; what a left swipe does not hold goes in the
  long-press menu.
- In the side menu itself a swipe left closes it, so its rows have no swipe
  actions at all: everything a row offers is in its long-press menu.

## Motion and feel

- System animations (`.animation(.default)`, `.spring`), and none at all when
  Reduce Motion is on (`@Environment(\.accessibilityReduceMotion)`).
- Haptics through `.sensoryFeedback`: success when something is sent or
  approved, warning when something is refused. Never decoration.
- Pull to refresh where a list can change, swipe actions for the obvious things
  (reply, copy, delete), and long press for a context menu.

## Words

- Short, plain, in sentence case, saying what happens rather than what the code
  does: "Connect to mail.example.com as robin?", not "Authenticate session".
- Every string is localized (English, Simplified Chinese, Japanese, like the
  dashboard), through a String Catalog.
- Errors say what to do next. No error codes on screen.

## Checking a screen

Before a pull request with a new or changed screen:

1. It builds in CI, and CI's simulator screenshots of it are looked at in
   light mode, dark mode and at the largest text size.
2. VoiceOver reads it in a sensible order, and every control says what it does.
3. Nothing on it breaks the rules above. Where a rule had to bend, the pull
   request says why.
