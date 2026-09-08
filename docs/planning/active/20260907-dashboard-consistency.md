# Dashboard consistency

Started 2026-09-07. The dashboard had grown two conventions for every list,
three for a key/value table and four for a row action, and a page picked one
by whichever page it had been copied from. `docs/coding/frontend-design.md` is
the settled answer; this note records what has been converted and what has
not.

## Done

- `SettingsSection` takes a `card` option, so a panel and a list are the same
  component. Mailbox settings, domain aliases, domain credentials, sessions
  and the server page use it.
- Creating and changing go through `FormDialog` on the mailbox's folders and
  app passwords, on a domain's aliases and credentials.
- One-time secrets go through `SecretDialog` — the app password and the domain
  credential both showed theirs in the page before, one in a banner and one in
  a card.
- Deleting an alias or a credential asks first. Neither did.
- Signing out everywhere asks in a `ConfirmDialog` rather than `window.confirm`,
  and so do restarting and upgrading the server.
- `SaveRow` and `Field` are shared; they were written out ten and two times.
- `ErrorMessage` renders nothing when there is no error, which retired
  twenty-two copies of the same guard.
- Row actions are `icon-action` inside `row-actions` wherever there is more
  than one, and the toolbar-sized `icon-button` is out of rows.
- The margins that were thirty inline `style` attributes are four CSS rules.
- 183 lines of `style.css` that had been pasted into the middle of a phone
  media query, breaking the rule they landed in, are gone.

Later in the same pass, on the mailbox pages:

- The rail draws a rule under what is pinned and another above Contacts, names
  the mailbox only when there is more than one, and has no pin on its rows —
  pinning is on the Folders tab with renaming and removing.
- Pinning lifts a folder out of the tree instead of copying it, so the rail
  lists it once.
- The signature and the automatic reply are written in the compose page's
  editor, with its rich text and plain text switch.
- The Rules tab is a list of what each rule does, with a dialog to add or
  change one.

## Not done

Each of these is a page that works and reads acceptably, listed so that the
next person does not have to find them again:

- `settings/integrations.tsx` edits SSO providers as a stack of nested cards
  with an add button under them. It wants a `FormDialog` per provider, the way
  a role is edited.
- `access/audit.tsx` builds a filter and a "show more" by hand out of
  `SettingsRow`. It is a log, so it wants `DataTable`.
- `domainDns.tsx` and `domainAliases.tsx` list in plain tables that would be
  better as `DataTable` once either grows a filter.
- The mailbox rules editor is the last inline editor. Each rule is a form of
  eight fields; a dialog holding all of it would be worse, so it stays until
  somebody has a better idea.
- Page-level "nothing here" states are still bare paragraphs in nine pages.
  `SettingsEmpty` is section-scoped; a page-scoped sibling does not exist yet.
- `settings/server.tsx` and `mailbox.tsx` describe things with `dl.properties`
  and `dl.mailbox-pane-meta` where the rest of the app uses `table.detail` and
  `Field`.
- Most of `web/src` is not formatted to `web/.prettierrc.json`. Formatting it
  in one commit would be a diff nobody can review; formatting each file as it
  is touched gets there eventually.
