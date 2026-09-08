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
| The foot of a settings form | `SaveRow` in `components/common.tsx` |
| A row of a key/value table | `Field` in `components/common.tsx` |
| Waiting, and failing | `Loading`, `ErrorMessage` in `components/common.tsx` |
| A word about state | `Tag` in `components/common.tsx` |
| Row action icons | `components/icons.tsx` |
| Tabs within a page | `Tabs` in `components/tabs.tsx` |

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

## Row actions

One action on a row is a text button: `className="link"`, or `link danger`
when it destroys something. The word is clearer than any icon, and there is
room for it.

Two or more actions are icon buttons — `className="icon-action"` inside a
`<div className="row-actions">`, `icon-action danger` for the destructive one.
Three words in a row crowd out what the row is about. `icon-button` is the
larger toolbar icon and is not a row action.

Every icon button needs a `title` and an `aria-label` that names the thing it
acts on, because the icon alone says nothing to a screen reader and nothing to
a person who has not met it before:

    <button
      type="button"
      className="icon-action danger"
      title={t('common.delete')}
      aria-label={`${folder.name}: ${t('common.delete')}`}
    >
      <TrashIcon size={16} />
    </button>

An affordance that only appears on hover must sit behind `@media (hover:
hover)`. On a phone the first tap reveals it and the second one acts, which
turns every open into a double tap.

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

Spelling is US English throughout: "authorizes", "canceled", "behavior".

## Style

Colors, spacing and type come from the tokens in `style.css`. An inline
`style={{ ... }}` is a rule that no other page can share and that no media
query can override, so it is nearly always wrong — a repeated one is a class
waiting to be written. The margins around a panel's heading, its last
paragraph and the fields in a `.row` are already rules; so is right-aligning a
lone action, with `page-actions-end`.

A checkbox and its label on one line is `label.checkbox`.

The breakpoints are 900px, 760px and 600px, and a phone is the narrow end.
Check a change at 390px before sending it: a row of buttons, a wide table and
a dialog all behave differently there, and all three are easy to get wrong.

`web/.prettierrc.json` is the formatter, and `npx prettier --write` on a file
you are already changing is welcome. Running it over files you are not is not:
most of the tree predates the config and the diff would bury the change.
