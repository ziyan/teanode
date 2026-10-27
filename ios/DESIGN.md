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
- A confirmation (`.confirmationDialog`) is attached to the control that asks
  for it: the row button, the swipe action or the toolbar button. On iOS 26 it
  opens as a popover pointing at that view, so one attached to the Form or the
  screen points at the wrong place. When the trigger is an item in a `Menu`,
  attach it to the `Menu`, since the item goes away with the menu.
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
- A card that only shows something (a file, a picture, a page the agent made)
  has no buttons. A tap on its picture, or on the card when it has nothing
  else to tap, does the main thing (open it); the rest (Save, Share, Copy)
  are in its long-press menu, the main action first, and are also its
  accessibility actions so VoiceOver does not need a long press. Buttons stay
  on a card only when answering is its point: an approval, a question.
- A link in a message is shown short (the host and the path, with the tracking
  taken out), never as the long address, and opens without the tracking. In
  the person's own bubble, on the accent's tint, a link is in the words' own
  color and underlined, since an accent link there is too faint to read. Under
  the bubble of the message that holds one, its first link gets a card on the
  cards' surface: the page's picture, its site, its title in semibold and two
  lines about it. A tap anywhere on it opens the link, Copy link is in its
  long-press menu, and nothing is drawn while it loads or when the page has
  nothing to show.
- A web link opens in the app's own browser, Safari's view coming up from the
  bottom over whatever is on screen, so the person does not leave the app. The
  system keeps what it does better: mailto:, tel:, sms:, teanode://, the app's
  own universal links, Maps, and a configuration profile. Every link goes
  through the environment's `openURL`, set once at the root; nothing calls
  `UIApplication.shared.open` for a web page itself.
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
  are red, symbol and words alike: `role: .destructive` reddens only the
  words, so the button carries `.tint(.red)` for its symbol.
- A symbol is the color of its words. Actions are green, or red, and grey
  while they cannot be pressed; a row that opens a page or shows something
  has its symbol in the text color, as the More list's parts do. A list row
  greens its symbol whatever its words are, so in a form or list a button's
  label takes `.actionColor()` and any other label with a symbol
  `.foregroundStyle(.primary)` (`make check-button-colors` checks both).
- Show content, do not fold it. Lines of a receipt, a quoted message, a
  summary: show them whole, in body text, one row each. A long list moves to
  a screen of its own behind a row, as iOS Settings does; it does not open in
  place with a disclosure arrow.
- Screens are pushed on the tab's own stack, so the swipe back from the left
  edge works everywhere. A part opened from More is shown over More's list
  rather than pushed, and `MoreSwipeBack` gives its first screen the same
  swipe from the left edge. A row's swipe actions are for the obvious things;
  whatever they do not hold goes in the long-press menu.

## Navigation

- The app is a `TabView` (`.tabViewStyle(.sidebarAdaptable)`) of icons only:
  three parts the person chooses (Chats, Mail and Photos at first; More's
  Edit button changes them, kept on the device), More and Search
  (`Tab(role: .search)`), five in all: a sixth makes iOS add a More of its
  own over ours. On the iPad the sidebar is arranged with the system's own
  customization (`TabViewCustomization`). A tab's name is its accessibility label, so no
  title shows under an icon. The bar shrinks to a pill while a list scrolls
  (`.tabBarMinimizeBehavior(.onScrollDown)`).
- A tab with something waiting has a badge: unread chats on Chats, coding
  sessions that ask on More (where Coding is).
- Each part keeps its own navigation inside it, never in a side menu.
  Chats opens on the list of conversations, and a conversation is pushed on it.
  Mail opens on its Mailboxes list, which pushes a folder. Finance, Location and
  Photos choose their list in the menu under the screen's title
  (`toolbarTitleMenu`), the way Files names its place.
- More is a grouped list: Every day (Calendar, Contacts, Location, Health, Passwords, Shopping), Work (Coding,
  Knowledge, Recordings), Plans (Goals, Ideas), Money (Finance), then Settings. A part
  the server does not offer is left out. A part opened from it shows a button at
  the top left that goes back to the list (`MoreBackButton`).
- A screen with a bar of its own at the bottom, such as a conversation with its
  composer, a mail with its actions, or a list in Select mode, hides the tab
  bar (`.toolbarVisibility(.hidden, for: .tabBar)`) while it is on screen.
- Search finds the person's words across chats, mail, contacts, photos, memory,
  documents, recordings, places and finance at once, a few results from each,
  grouped by part. A row opens the item in its part; Show all opens the part
  with the words in its own search.

### Sheets and covers belong to the screen

- A `.sheet`, `.fullScreenCover` or `.popover` is attached to the `List`, the
  `Form` or the screen's root, never to a `Section`, a row, a `ForEach`'s
  content, a lazy stack's content, or what a `Menu`, `contextMenu` or
  `swipeActions` builds. A list builds its sections and rows again when its
  data changes, a swipe closes or a row scrolls away, and a sheet attached to
  one opens and closes at once, or closes while the person is in it.
- The row only says which item it is about: it sets an item on its screen
  (`@State var renaming: Account?`), and the screen presents with
  `sheet(item:)` or `confirmationDialog(presenting:)`.
- A card shown in a lazy list of its own, such as a conversation's messages,
  owns no sheet either: it hands the tap to the screen
  (`MessagePresentations` in a conversation).
- `make check-presentations` (part of `make lint-ci`) fails on the cases it
  can see within one file. A presentation with a reason to sit where it is
  says so on the line before: `// check-presentations: allow, <why>`.

## The iPad

The same app runs on the iPad, and a screen is designed once for both. What
differs is decided by the window's horizontal size class (`.regular` or
`.compact`), so Slide Over and a narrow Split View get the iPhone's layout.
Only the tabs also ask whether the device is an iPad, as below.

- At regular width on the iPad the tabs are the system's sidebar
  (`.sidebarAdaptable`), with the names shown. The larger iPhones are of
  regular width when turned sideways too, but their tab bar still holds five,
  so the tabs are the iPhone's there. On the iPad, the parts that are under
  More on the phone are tabs in the sidebar's groups (`TabSection`): Every
  day, Work, Plans and Money. There is
  no More tab there, so no screen shows `MoreBackButton`.
- A screen is laid out for whatever width it is given, from about 400 to 1000
  points. Lists, forms and grids adapt by themselves. Text that is read in
  lines, such as a conversation or a mail, is kept to 720 points and
  centered with `readableWidth()`; a screen with a wide canvas, such as a map
  or the photos, fills the width.
- Anything that the system shows as a popover on the iPad (a share sheet, a
  confirmation) needs an anchor: attach a confirmation to its control, and give
  a share sheet presented from code a source view and rectangle.
- The iPad turns every way: what decides is the width of the window, not the
  way the iPad is held. CI photographs it upright only, since an iPad app that
  turns every way follows how the simulator is held and cannot turn itself.

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
