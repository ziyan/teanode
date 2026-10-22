# Dashboard design

The rules the dashboard under `web/src` follows, and why. They exist so that a
page nobody has seen before still behaves the way the last one did: the same
panel, the same place for the button that adds a thing, the same dialog for
naming it. A person who has learned one settings tab has learned all of them.

Everything here is a convention, not a framework. Nothing enforces it, so
reach for the shared component before writing markup — when a page hand-rolls
what a component already does, the two drift apart within a release, and then
one of them gets the fix.

## Where the pieces are

| What | Where |
| --- | --- |
| Panel with a heading, a hint and one action, then rows | `SettingsSection`, `SettingsRow`, `SettingsEmpty` in `components/settingsList.tsx` |
| A secret shown once | `SecretDialog` in `components/settingsList.tsx` |
| Make or change one thing | `FormDialog` in `components/dialog.tsx` |
| Ask before something irreversible | `ConfirmDialog` in `components/dialog.tsx` |
| A long list that is sorted, filtered or paged | `DataTable` in `components/dataTable.tsx` |
| The pages of a list that is not a table | `Pager` and `usePageInAddress` in `components/pager.tsx` |
| A field naming one of the agent's pages | `PagePicker` in `components/pagePicker.tsx` |
| The foot of a settings form | `SaveRow` in `components/common.tsx` |
| A row of a key/value table | `Field` in `components/common.tsx` |
| Waiting, and failing | `Loading`, `ErrorMessage` in `components/common.tsx` |
| A word about state | `Tag` in `components/common.tsx` |
| The days of the week as toggle buttons | `WeekdayButtons` in `components/weekdayButtons.tsx` |
| Row action icons | `components/icons.tsx` |
| Tabs within a page | `Tabs` in `components/tabs.tsx` |

## Where a page lives

The first segment of a path says whose the page is, and the rail follows it:

- `/settings/...` is the signed-in person's own settings: their preferences,
  their agent, their mailbox, their passwords, passkeys, tokens and sessions.
  The account's rail.
- `/manage/...` is the operator's: every message, the queue, reports,
  domains, who may do what, the server, everybody's agents. Anything that
  needs an operator permission, or configures the server or other people,
  goes here. The management rail.
- A feature page is neither, and keeps a path of its own: the mailbox and
  what hangs off it (`/mailbox/...`, its calendar and contacts), the agent's
  chats (`/chat`; `/agent` forwards there), Knowledge (`/knowledge`), Finance (`/finance`).
  Their rows are in the mailbox's rail. Circles (`/circles`, the people you
  share the server with) is one too, but has no row: it is reached from the
  account menu, where a badge counts the invitations waiting for an answer.
  Sharing (`/sharing`, everything shared by you and with you, in two tabs that
  are paths) is reached the same way, from the account menu and from Circles.

A page that moves does not leave a redirect behind: every link to it in the
code moves with it, and the old path is not found.

Where you are is in the address. A tab, the row whose details are open, a
list's filters and its page are each in the path or the query string, so
the browser's back and forward buttons, a reload and a copied link all land
in the same place.

A new page starts from the page most like it. A list with a reading pane
looks like the mailbox; a page of settings looks like the other settings
pages. A layout no other page has needs a reason the pull request gives.

## Panels

A page is a stack of panels. A panel is `.card`: a surface, a border, one
padding. Its first line is an `<h3>`, and the sentence under the heading is a
`<p className="muted">` that says what the panel is for.

Panels are the width of the page. A form inside one is not: a text field
stretched across a wide window is hard to aim at and hard to read back. Cap the
fields, not the panel, with `<div className="form-narrow">` **inside** the
card. A narrow card beside a wide one reads as two different pages, which is
the bug this rule exists to prevent.

`SettingsSection` draws the heading, the hint and the one action that adds to
the panel, in the arrangement that folds correctly on a phone — the action
drops under the heading rather than squeezing beside it. Pass `card` when the
section should be a panel; leave it off when the page is a single list, or
when each item in the list is a panel of its own.

## Lists

Three shapes, chosen by what the list holds:

- **`SettingsRow`** for things a person owns and manages: app passwords,
  tokens, passkeys, sessions, roles. Each row is a name, a line of detail, a
  badge, and what can be done to it. Rows wrap on a phone; columns cannot.
- **A plain `<table>`** when the rows line up and the alignment is the point —
  a folder tree, a list of addresses, the settings to type into a mail
  program. A tree is the clearest case: indentation is a column that
  `DataTable` has no way to draw.
- **`DataTable`** when the list is long enough to want sorting, filtering,
  paging or selection. Do not grow a plain table into one by hand, and do not
  put an empty-state paragraph beside one — it has `emptyMessage`.

`SettingsEmpty` says a list is empty. Having none of something is ordinary, so
it is said in muted prose inside a dashed block, not raised as an error.

A list longer than a page pages; it does not load more. A button that adds
the next rows under the ones shown grows the page without end, loses the
place on a reload, and gives Back nothing to go back to. A table the server
pages is `DataTable` with `remote={{ total, onRange }}`; any other list puts
`Pager` in its foot (`list-foot`) with `usePageInAddress`, so both say
"51 to 100 of 1,234", or "of at least" where the server stops counting,
with the same previous and next buttons. The page is in the address, a
changed filter or search starts again at the first page, and two lists on
one screen name their pages apart (`pageParameter`). A list whose server
gives no total says what it does know rather than inventing one. A dialog
has no address, so a list inside one keeps its page itself.

## Row actions

A row's actions are icon buttons, however many there are: `className="icon-action"`,
`icon-action danger` for the one that destroys or ends something, inside a
`<div className="row-actions">` when there is more than one. The common pair
is a pencil that opens the dialog to change the thing and a trash can that
removes it (or ends it, for something that is ended rather than deleted, such
as a budget), after a `ConfirmDialog`. A cross (`CloseIcon`) takes something
off what is being put together, such as an attachment from a message being
written, where nothing stored is lost. Icons in a row line up from one row to
the next where words of different lengths did not, and four of them fit
beside a name on a phone where four words did not. `icon-button` is the
larger toolbar icon and is not a row action.

A text-link button (`className="link"`) is not used for an action. A link is
for going somewhere: a router link, an anchor, a page named in a list, or a
button that opens a page or a conversation in place. The few other buttons
styled as links are words inside a sentence (the notice that offers to load
remote images) or a toggle that shows or hides text beside it (the quoted text
of a reply). A short action on its own, "Open Circles", "Set up", "Clear all",
is a real button even when it only navigates: a `<Link className="button">`
(or `button-quiet` beside a field label), placed in the card's action row or
at the end of the row it belongs to rather than on a line of its own in the
middle of text. A page-level action beside Save is a real button
(`className="danger"` when it removes the thing).

`make lint-ci` holds this line: a button with the link class fails
`check-link-buttons` unless the line of its tag, the line above it or the
first line inside it says why, as
`<button /* link-button: words inside a sentence */ ...>`. The class counts
wherever it sits in a `className` (a template or a ternary too), and so does
any class that `style.css` draws the same way: accent-colored text with no
border and no background. An anchor
with an `href` is not checked, since it goes somewhere; an anchor without one,
and a router `Link` carrying the class, always fail.

The exception is a row that is itself a button, such as an idea on the
agent's Ideas tab: its one other action is an icon in the row's corner.

A page that is one thing, a circle, puts the icons that change that thing
(rename, delete) in its first panel's head, in
`<div className="row-actions head-icons">`. On a phone `head-icons` keeps them
on the heading's line, where a row of words would drop below it. What ends
the person's part in the thing without being what the page is for, leaving a
circle, is a `className="danger"` button in `page-actions page-actions-end`
at the foot of the page, after every panel, with a confirmation that says what
will happen.

Every icon button needs its name shown on hover and an `aria-label` that names
the thing it acts on, because the icon alone says nothing to a screen reader
and nothing to a person who has not met it before. The name on hover is a
`Tooltip` around the button, or a `title` where the button already sits inside
one (a chip with its own tooltip):

    <Tooltip label={t('common.delete')}>
      <button
        type="button"
        className="icon-action danger"
        aria-label={`${folder.name}: ${t('common.delete')}`}
        onClick={() => setDeleting(folder)}
      >
        <TrashIcon size={16} />
      </button>
    </Tooltip>

An affordance that only appears on hover must sit behind `@media (hover:
hover)`. On a phone the first tap reveals it and the second one acts, which
turns every open into a double tap.

## The agent drawer's head

The marks in the agent drawer's head (the conversations, the goal, what is
attached, the day's usage, what runs in the background) each open a dropdown
under the head, the way the list of conversations does: `HeadMenu` in
`components/agentDrawer.tsx`, with a backdrop that closes it and Escape from
anywhere. Not a dialog: the drawer is where the person is looking, and a dialog
over the page takes them out of it. Not a panel over the whole box: the head is
what the box is dragged by and its edges what it is resized by. So the goal is
set there with a small form of its own rather than a `FormDialog`, the one
exception to the rule below.

A chat with people is one more conversation in this drawer, and its menu is
another of the head's dropdowns (`ChatMenu`): who is in it, its name if it is a
group, its sound, and leaving it. Starting one is in the picker's "New
conversation" menu, beside its search box, with the new side chat ("Message
someone", "New group"), so the picker lists only conversations; the iPhone's
side menu has the same menu at its top. With the agent off the drawer opens
on the person's chats, and the offer to turn the agent on takes the main
conversation's place. There is no page of chats and no row for them in the
rail; the number on the drawer's button is the unread messages in the chats
that are not muted, asked again every 15 seconds and at once when the window
regains focus. A small picture under a message is a member who has read through
it: the newest message by somebody else that they have read, never one of their
own, so in a direct chat it is under the signed-in person's own newest message
the other has read. The usage ring in the head is the agent's budget, so it is
left out in a chat and with the agent off, and with the agent off nothing that
only the agent has is started or told about: no feed of the main conversation,
and no toast for a refusal that only says the agent is off. What a chat needs
is kept: the feed of a chat comes from the server's chat hub whether or not it
runs a model, so the drawer follows it like any other and never reads a chat
again on a timer, and the tab reports whether it is shown, so a message is not
sent to the person's phone while they are reading it.

An agent is asked in a chat with an `@`. Typing one at the start of a word offers
the agents that can answer (`MentionList`, above the box): the person's own,
first, and the agent of each member whose owner let the others ask it; an agent
that is off is not offered. Choosing one puts its name in the words and sends
its id as `addressedAgentIds`, and only a name still in the words is sent, so an
agent's name typed by hand, or taken out again, addresses nothing. Replying to
an agent's message addresses that agent. The answer is a message of the chat
under the agent's name; while it is written every member sees "is thinking"
under the messages (a person is "is typing"), then its words as they come as a
message that the stored one replaces where it stood. The turn's own `message`
and `asked` events draw nothing in a chat. For the owner the calls the answer
took are the rows before it, as in any conversation; nobody else is sent them.
The two switches for the person's own agent, which the others see the result of,
are in the members dialog: one makes it remember the chat, one lets the others
ask it, each with a line saying what it means.

## Making, changing, destroying

Making a thing and changing it ask the same questions, so they are one
`FormDialog`, opened empty or filled in. A dialog rather than a form in the
page: a form that appears under a list moves the list while it is being read,
and a row that turns into a form makes the page jump.

Destroying something goes through `ConfirmDialog`, and the body says what will
be lost — how many messages are in the folder, which program stops receiving
mail. Never `window.confirm`: it is not styled, not translated, and not
dismissable the way every other dialog here is.

A secret shown once goes in `SecretDialog`, never in the page. It carries a
copy button and does not close on Escape or on the backdrop, because every
other dialog can be dismissed by accident without cost and this one cannot be
reopened.

A settings form ends in `SaveRow`: the failure, the button, and the word that
says it worked. Its `note` is the sentence the reader needs — whether the
setting took effect now or waits for a restart — and `canSave` is false while
there is nothing to save.

## Waiting and failing

`Loading` while the first answer is outstanding, and only then — a list that
already has rows should keep them while it refreshes. `ErrorMessage` for a
failure, placed where the thing that failed is; it draws nothing when there is
no error, so it does not need a guard around it. A dialog shows its own
failure through its `error` prop, so it stays open with what was typed still
in it.

## Words

Every string goes through `t()` and lives in all three catalogs, `en`, `zh`
and `ja`. `make lint-ci` fails when they disagree. Write the English first and
make it a sentence: "No mail program has been set up yet", not "No devices".
Buttons say what will happen — `Create`, `Revoke`, `Save`.

Say less. A hint that explains what the page plainly shows, a count beside
a heading that already has one, a status pill that repeats the row: each
takes room and is read once. When a word on screen needs a paragraph to
explain it, the paragraph belongs in the dialog that the action opens, not
on the page.

Spelling is US English throughout: "authorizes", "canceled", "behavior".

## Style

Colors, spacing and type come from the tokens in `style.css`. An inline
`style={{ ... }}` is a rule that no other page can share and that no media
query can override, so it is nearly always wrong — a repeated one is a class
waiting to be written. The margins around a panel's heading, its last
paragraph and the fields in a `.row` are already rules; so is right-aligning a
lone action, with `page-actions-end`.

A checkbox and its label on one line is `label.checkbox`.
A checkbox or radio whose words are the page's own, not a hint under a field,
is `label.checkbox.is-plain`: the text size and color, with its explanation
muted under it.

Toasts are at the foot of the window, at the left of the page (past the rail),
and on a phone above the agent's button. They were at the top right, where they
covered the buttons a page keeps at the right of its header and took the click
meant for the next one. A page's floating things go at the right, above the
agent's button (the upload tray, the recording bar), so the two never meet.

Nothing moves when it is hovered, chosen or opened. A row that is selected,
a menu item that is the current page and a button that is pressed keep
their size, so the rows around them stay put; a border or an icon that
appears on selection has its room taken from the start. A control's whole
shape answers a click, not only its text: if the hover color or the ripple
covers it, so does the click.

The breakpoints are 900px, 760px and 600px, and a phone is the narrow end.
Check a change at 390px before sending it: a row of buttons, a wide table and
a dialog all behave differently there, and all three are easy to get wrong.

Where you are is one answer in two places, both from `useTrail()` in
`components/breadcrumb.tsx`: on a wide screen the crumbs above the page say
how you got here and `PageHeading` says what it is, and on a phone the bar at
the top carries the trail on its own, ending in the page's own name. A page
does not declare where it sits — the route does — and the one thing a page
supplies is `useBreadcrumbDetail()`, the name of the thing it is showing. Two
rules fall out of the phone bar: the last crumb is never a link, because it is
the page you are on, and a step that is only a step on a phone — a message
that takes the screen its folder's list had — is added only when the window
is narrow.

The dashboard is not one file. What every session needs -- the shell, the
rail, the mailbox -- is in the first download; the libraries are beside it in
a file of their own, so a release does not make anybody fetch React again; and
a page somebody may never open is fetched when they open it. A new page joins
that by being declared with `lazyPage()` in `app.tsx` rather than imported at
the top, with a `webpackChunkName` naming the part of the dashboard it belongs
to -- pages opened in the same breath share a file, so that going from a
domain to one of its templates is not two waits. Keep it out of anything the
shell imports, or it comes back into the first download by the back door.
English is in that download too; the other catalogues are fetched before the
first paint, which is why `index.tsx` waits for `loadCatalog` before it draws.

`web/.prettierrc.json` is the formatter, and `npx prettier --write` on a file
you are already changing is welcome. Running it over files you are not is not:
most of the tree predates the config and the diff would bury the change.
