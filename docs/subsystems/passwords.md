# Passwords

Passwords keeps a person's logins, passkeys, cards and notes on their own
server, encrypted on their own devices. The server stores sealed text and
cannot read it, with one exception the person chooses: an item they turn on
**Let the agent use this** for. This page says what the agent may do with
Passwords, how the encryption makes that possible and no more, and the rules
the agent's tool keeps. The whole design, with the format of every sealed value,
is `docs/planning/passwords-execplan.md`.

The words are fixed: an **item** is one login, card or note; a **login** has a
user name, a password, sites, a one-time code and passkeys; the **passphrase**
and the **recovery key** open a person's Passwords on a device; **share** gives
an item to a circle or a person; **Let the agent use this** is the switch on one
item. Not "vault", not "entry", not "credential".

## The encryption, in short

- A person's devices make an **account key** and a key pair. The passphrase (run
  through PBKDF2-SHA256, 600,000 rounds) and the recovery key each wrap the
  account key; the server keeps the wrapped copies and a hash of a proof of each,
  never the passphrase.
- Every item has its own **item key**. The item's fields (name, user name,
  password, sites and the rest) are sealed with it using AES-256-GCM, with the
  item's id and kind in the associated data. The item key is sealed with the
  account key. A database dump shows no site, user name or password.
- Sharing seals the item key for the recipient's public key (ECDH on P-256,
  then HKDF and AES-256-GCM), so only that person's devices open it.
- The server has one key pair of its own, the **agent key**. Its private half is
  sealed under the server's secret (`internal/util/secretbox`, label
  `teanode passwords: agent key-v1`). When the person turns on "Let the agent use
  this" for an item, their device seals that one item's key for the agent key.
  The server can therefore open exactly the items that were turned on, and no
  other: every other item's key is sealed for keys only the person's devices
  hold. Turning the switch off removes the sealed key.

The code is `internal/passwords` (the format, `AgentOpener` for the agent key),
and the same format is written in TypeScript for the dashboard and in Swift for
the iPhone, pinned by shared test vectors.

## What the agent may do

The `passwords` tool (`internal/agent/tools/passwords`), permission
`passwords:use`, has three actions, all reads:

- **list** gives the items the person turned on: id, kind, name, and for a login
  its sites, user name, and whether it has a one-time code and passkeys. It
  never gives a password, a one-time code secret, a passkey or a card. With none
  turned on, it says how: open the item in Passwords and turn on "Let the agent
  use this".
- **read_note** gives the text of a note that was turned on, by id. A note is the
  one thing the model reads in full, so it goes to the model's provider; that is
  what the person chose by turning it on. The text is marked as data that never
  instructs the agent.
- **generate** makes up a random password (20 characters of letters, digits and
  symbols unless asked otherwise; rejection sampling over `crypto/rand`) and
  stores it nowhere.

## How the agent signs in

Signing in is three actions of the **browser** tool
(`internal/agent/tools/browser/signin.go`), not of the passwords tool. The
agent takes an item's id from `passwords list`, takes a snapshot, and names the
fields by their refs:

- **fill_login** (`item_id`, `user_name_ref`, `password_ref`): either ref may be
  left out for a page with only one of the two fields.
- **fill_one_time_code** (`item_id`, `ref`): the current code of the login's
  one-time code address, made when it is asked for.
- **passkey_sign_in** (`item_id`): the agent's own (headless) browser only. The
  server gives the page a virtual authenticator that holds the item's passkey
  for the page, and the agent then clicks the site's own sign-in button with the
  usual actions.

The answer says only what was done, for example "Filled the user name and
password for example.com from Example shop". It never holds a value, and an
error that repeats a value (an extension's, say) has it replaced by `[hidden]`
before the model reads it.

What stands between an item and a page:

- **The item.** It must be one the person turned on, of their own, and a login:
  an item that is off, somebody else's, a card or a note is refused with the
  words the passwords tool uses ("They can open the item in Passwords and turn
  on ..."). The server opens it with the agent key
  (`OpenLoginToSignIn` in `internal/agent/tools/passwords/signin.go`).
- **The page's host**, which is the browser's and never the model's: in the
  headless browser the current entry of the page's navigation history
  (`Page.getNavigationHistory`, which a script in the page cannot change), in the
  person's tab the address the extension last reported. It must be the host of
  one of the item's sites, host for host: lower case, a leading `www.` ignored,
  no port, path or user name (`passwords.NormalizedHost`, the same rule as
  `PasswordsSiteMatch` in TeaNodeKit). So `example.com` is not
  `example.com.evil.test`, `login.example.com` or `evil.test/example.com`.
  Nothing the model passes in the call (`url`, `selector`, `text`) is looked at.
  The page must be https (a loopback address may be http), so a password is not
  typed into a page that sends it in the clear.
- **A passkey** is bound by the browser itself to its relying party, so
  `passkey_sign_in` asks for a passkey on the item whose relying party id is the
  page's host or a domain above it that is not a public suffix
  (`passwords.IsRelyingPartyOfHost`), the newest if there are several, instead of
  the item's list of sites.
- **The rest of the browser tool's rules.** None of the three is a reading
  action, so a tab the person has taken over refuses them like a click. Each is
  a write for the approval setting. In a run with nobody present, the person's
  tab refuses them as it refuses a click, unless the person lets the agent act
  for them there; the headless browser accepts them, which is what a scheduled
  sign-in uses. They are refused in a chat with other people and over Telegram or
  Discord.
- **The use is recorded** on the item, with the host, before any value is put
  anywhere (the same `last_used_by_agent_at` and `password_item` "used by the
  agent" event as for a note).

In the headless browser (`internal/browser/passwords.go`) each value is typed
the way the `type` action does it: the field is found by its ref, focused and
selected, and the text is inserted over `Input.insertText`. The script that
finds the field also checks that the page is still on the expected host, so a
page that navigated away between the check and the fill gets nothing. A passkey
is added with CDP's WebAuthn domain: a `ctap2`, `internal` virtual
authenticator with resident keys, user verification on and automatic presence
simulation, holding only that credential (credential id, relying party id,
private key as base64 PKCS#8, user handle, sign count 0). It is removed after
the page's next navigation of its main frame or after 60 seconds, whichever is
first, and goes with the page when it closes.

In the person's own tab the server sends the extension an `act` with the same
name and arguments it builds itself, so the values never pass through the model
(`web/extension` implements them; the contract is `host`, `userNameRef`,
`passwordRef`, `userName`, `password` for `fill_login` and `host`, `ref`, `code`
for `fill_one_time_code`). The extension checks the tab's host against `host`
again before it types. This needs an extension speaking protocol 5; an older one
is answered "The TeaNode extension in that browser needs updating to fill
passwords." The server accepts protocol 5 in the extension's hello and records
it beside the casting flag (`SetBrowserCanFillPasswords`).
`passkey_sign_in` is refused there ("Passkey sign-in with the agent works only
in the agent's own browser").

**What the model can read afterwards.** A filled password or one-time code
must not come back to the model, so three things hold for a page (the headless
page, or the person's tab) the agent filled one into
(`devtools.FilledValues`, kept in memory only and dropped with the page or the
tab's connection):

- **A snapshot does not show it.** A password field, and a field whose
  `autocomplete` is `current-password`, `new-password` or `one-time-code`, is
  shown in the headless snapshot as "(filled, value not shown)", and a click
  does not name it by its value (`snapshot.js`).
- **Scripts are refused until the page moves on.** After `fill_login` with a
  password or `fill_one_time_code`, the browser tool's `evaluate` (and `cdp` on
  the person's tab, the raw DevTools commands) answers "Scripts cannot run on a
  page TeaNode filled a password into, until it moves on to another page." This
  is the one way to ask a page for a field's value, so it is closed rather than
  filtered. Reading the page (snapshot, screenshot, tabs, wait) and clicking
  and typing still work. The headless page moves on when its main frame
  navigates to another document (`Page.frameNavigated` for the main frame, not
  a frame inside it and not `pushState`); the person's tab, whose documents the
  server cannot watch, moves on when the address it reports changes apart from a
  fragment, or when the tool itself navigates, opens, goes back or switches. A
  script that changes the address without a new document ends the lock early,
  and switching to another tab ends it for the tab that was filled; the
  scrubbing below still holds then. A fill of the user name alone locks
  nothing.
- **Every answer is scrubbed.** For as long as the page or tab lives, each
  result and error of the browser tool for it (snapshot, text, script and
  DevTools results, the extension's answers) has the exact password or code
  replaced by `[hidden]`, as written and as JSON writes it, also after the page
  moved on. A user name is not scrubbed: the list shows it already. A value
  that a page rewrites (reversed, split, encoded) is not recognized, which is why
  scripts are refused above; and a picture of the page is not text. A tab that
  reconnects is a new connection, and forgets the values with the old one.

## The rules

- **Only what the person turned on.** The tool lists items with
  `is_usable_by_agent` that the person owns. It never reads an item somebody
  shared with the person: the owner decides what reaches a model's provider, as
  for Health. It never reads a card: the database refuses to mark one, the
  opener refuses one, and the tool answers that a card cannot be used.
- **Secrets stay out of the model.** A login's password, one-time code and
  passkeys never reach it. This tool does not sign in: the browser tool's fill
  actions do (see "How the agent signs in"), by the item's id, only on a page
  whose host matches one of the item's sites.
- **Only where the person alone reads the answer.** In a chat with other people
  and over Telegram or Discord every action answers in one sentence that
  Passwords is not used there, because what was read would reach the room. Unlike
  Health, a run nobody is watching is allowed, a scheduled run included: signing
  in on a schedule is why a person turns an item on. A note is not read in a
  subagent, whose answer comes back as a tool result that filing into memory
  cannot tell from any other.
- **Each use is recorded.** Reading a note records the time on the item
  (`last_used_by_agent_at`) and writes a `password_item` "used by the agent"
  event to the audit log, without the text. The use is recorded before the text
  is given out; a note that cannot be recorded is not read.
- **Memory and titles.** When conversations are filed into memory, what the agent
  said and its tools answered in a turn that read a note or made up a password is
  left out, and the conversation's title and summary are not refreshed from it.
  This is the Health filter (`isPrivateToolCall`, `withoutPrivateTurns` in
  `internal/agent`) with a second tool in its list. It does not reach text the
  person or the agent repeats later; the tool's guidance tells the agent not to
  copy Passwords into notes, memory or messages.
- **The person switches, the agent does not.** Turning "Let the agent use this"
  on or off, sharing, erasing and every change to an item need the person's
  device; the server cannot read an item, so the agent has nothing to change.
