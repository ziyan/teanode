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
  stores it nowhere. The password appears in the conversation and so goes to the
  model's provider; for a login the person will keep, the tool says to let them
  make one with the generator in Passwords and save it themselves, which no model
  sees. It is refused in a subagent, whose answer comes back to its parent as a
  tool result that is kept like any other.

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
- **The page's origin**, which is the browser's and never the model's: in the
  headless browser the current entry of the page's navigation history
  (`Page.getNavigationHistory`, which a script in the page cannot change), in the
  person's tab the address the extension last reported. It must be the origin of
  one of the item's sites: the host the same, lower case, a leading `www.`
  ignored (`passwords.NormalizedHost`), and the port the same
  (`passwords.IsOriginOfSites`). A site that names a port (`example.com:8443`)
  matches only a page on that port; a site that names none matches only a page on
  the default port of its scheme (443 unless it was saved as `http://`). So
  `example.com` is not `example.com.evil.test`, `login.example.com`,
  `evil.test/example.com` or `example.com:8443`. Nothing the model passes in the
  call (`url`, `selector`, `text`) is looked at. The page must be https (a
  loopback address may be http, and then only a site saved as `http://` matches
  it), so a password is not typed into a page that sends it in the clear. The
  page script checks the host and the port again at the moment the value goes in.
- **A passkey** is bound by the browser itself to its relying party, so
  `passkey_sign_in` asks for a passkey on the item whose relying party id is the
  page's host or a domain above it that is not a public suffix
  (`passwords.IsRelyingPartyOfHost`), the newest if there are several, instead of
  the item's list of sites.
- **Who started the run.** The three are offered only to a person who holds
  `passwords:use` (`ActionRequires` on the browser tool), and the permission is
  asked again when the item is opened (`ListAgentUsablePasswordItems` returns
  nothing for an owner who lost it or whose account is disabled). They work only
  in a run the person started (`tools.IsStartedByPerson`): a turn they typed or
  spoke, or a schedule they wrote themselves, which is what a scheduled sign-in
  uses. A run that content started is refused: an inbound mail (research), a chat
  app, a webhook, a goal, a dream, a command left running, a restart resumed, a
  schedule the agent wrote for itself, or any surface not known to be the
  person's. A subagent is its parent's. The passwords tool's `list` and
  `read_note` keep the same rule.
- **The rest of the browser tool's rules.** None of the three is a reading
  action, so a tab the person has taken over refuses them like a click. Each is
  a write for the approval setting. In a run with nobody present, the person's
  tab refuses them as it refuses a click, unless the person lets the agent act
  for them there; the headless browser accepts them in a schedule the person
  wrote. They are refused in a chat with other people and over Telegram or
  Discord.
- **The use is recorded** on the item, with the host, before any value is put
  anywhere (the same `last_used_by_agent_at` and `password_item` "used by the
  agent" event as for a note).

In the headless browser (`internal/browser/passwords.go`) a fill is one script
in the page (`fillScript`): it checks that the page is still on the expected host
and port, finds the field by its ref, checks that the field is the kind asked
for, visible and enabled, and puts the value in with the browser's own setter of
the input's value followed by `input` and `change` events, as the extension does.
There is no gap between the check and the value going in, and no separate
`Input.insertText`. An error out of the page is not passed on, since it could
quote the script. The kinds are the same in both browsers and are refused for
anything else (a textarea, a select, an editable region, a checkbox, a button):

- a **password** goes into an `input` of type `password`, or one whose
  `autocomplete` says `current-password` or `new-password`;
- a **user name** goes into an `input` of type `text`, `email` or `tel`, or one
  whose `autocomplete` says `username` or `email` (never one of type
  `password`);
- a **one-time code** goes into an `input` of type `text`, `number` or `tel`, or
  one whose `autocomplete` says `one-time-code`.

A password or a code that goes in is also put in a `WeakSet` of the page's filled
nodes (`window.__teanodeFilled`), which the snapshot reads. A passkey
is added with CDP's WebAuthn domain: a `ctap2`, `internal` virtual
authenticator with resident keys, user verification on and automatic presence
simulation, holding only that credential (credential id, relying party id,
private key as base64 PKCS#8, user handle, sign count 0). It is removed after
the page's next navigation of its main frame or after 60 seconds, whichever is
first, and goes with the page when it closes.

In the person's own tab the server sends the extension an `act` with the same
name and arguments it builds itself, so the values never pass through the model
(`web/extension` implements them; the contract is `host`, `port`, `expectedTab`,
`userNameRef`, `passwordRef`, `userName`, `password` for `fill_login` and `host`,
`port`, `expectedTab`, `ref`, `code` for `fill_one_time_code`). The extension
checks the tab's host and port against `host` and `port` again before it types
(a call that names no port means the default port), and refuses when the tab the
actions go to is not `expectedTab`. It answers `{isFilled: true, documentId, tab,
commitCount}`: the document it filled, the tab, and how many documents that tab
has committed so far, which the server's lock is kept by (see below). This needs an extension speaking protocol 5; an older one
is answered "The TeaNode extension in that browser needs updating to fill
passwords." The server accepts protocol 5 in the extension's hello and records
it beside the casting flag (`SetBrowserCanFillPasswords`).
`passkey_sign_in` is refused there ("Passkey sign-in with the agent works only
in the agent's own browser").

**What the model can read afterwards.** A filled password or one-time code
must not come back to the model, by any path. Four things hold for a page (the
headless page, or a tab of the person's browser) the agent filled one into
(`devtools.FilledValues`, kept in memory only):

- **A snapshot does not show it.** A password field, a field whose
  `autocomplete` is `current-password`, `new-password` or `one-time-code`, and
  any input the agent filled a password or a code into, whatever its type has
  become (a show-password button turns a password field into a text one), is
  shown as "(filled, value not shown)". The person's tab also treats a field
  whose name, id, label or autocomplete says `pass` or names a card as a secret.
  A value longer than 80 characters in any field is not shown at all ("long
  value, not shown"), rather than cut, since its first characters would be a
  part of it. A click does not name a field by its value (`snapshot.js`,
  `inPage.js`).
- **The network log is off.** Before the first value goes in, the headless page
  turns `Network` capture off, and the extension sends `Network.disable` to the
  tab if the agent had a debugger on it and forgets every event it kept. A
  form's request is where a typed password travels, so the extension never keeps
  the body of a request (`postData`, `hasPostData`) in any event, refuses
  `Network.getRequestPostData`, and keeps nothing at all the page does (requests,
  console, anything) while the tab shows a document the agent filled.
- **The page is locked.** From the fill until the tab commits a new document, the
  browser tool refuses on that tab: `evaluate`, `screenshot`, `storage`,
  `cdp_events`, and every DevTools command except real input
  (`Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`, `Input.dispatchTouchEvent`,
  `Input.insertText`, `Input.imeSetComposition`; not the drag methods, which hand
  back what is dragged) and moving the page (`Page.navigate`, `Page.reload`, `Page.getNavigationHistory`,
  `Page.getFrameTree`, `Page.getLayoutMetrics`, `Page.enable`, `Page.disable`):
  `Network.`, `Fetch.`, `Runtime.`, `DOM.`, `DOMSnapshot.`, `Page.captureSnapshot`,
  `Accessibility.`, `CSS.` and whatever the protocol adds are refused, since a
  list of what is allowed cannot lose to the protocol growing. Reading the page
  (snapshot, tabs, wait), clicking, typing and navigating still work. A fill of
  the user name alone locks nothing.
- **Every answer is scrubbed.** For as long as the server runs (the values are
  the agent's, not one connection's, so a browser that drops and reconnects finds
  the same lock), each result and error of the browser tool for the page or the
  tab, its overlay and its list of tabs (their addresses and titles), has the
  exact password or code replaced by `[hidden]`. The text is scrubbed whole,
  before a snapshot is cut into parts, so a value on the edge of a part is in
  neither half. Besides the value as written, these forms are matched: as JSON
  writes it (with and without escaping `&`, `<`, `>`, and as a JSON string in a
  JSON string), with HTML's escapes (`&amp;`, `&quot;`, `&#39;`, `&#x27;`,
  `&apos;`), percent-encoded as a form or an address writes it (a space as `+`
  and as `%20`, either case of the hex digits, and twice over), in base64
  (standard and URL-safe, padded or not, wherever in a longer string such as
  `Basic` credentials it starts), and in hex. A one-time code is forgotten a
  minute after its period ends: it opens nothing by then. A user name is not
  scrubbed: the list shows it already. A value that a page rewrites some other
  way (reversed, split) is not recognized, which is why the commands that could
  read a field are refused, and a picture of the page is not text, which is why
  a screenshot is refused while the page is locked.

The lock belongs to the tab and to its document, not to the tool:

- The headless browser has one page. The lock ends when its main frame commits a
  document (`Page.frameNavigated`, main frame only) that is a new page, which the
  navigation history tells from a move back or forward: a new page makes a new
  history entry (or reloads the same one), and back and forward land on an entry
  that was already there. A frame inside the page, `history.pushState` and a
  change of the fragment (`Page.navigatedWithinDocument`) commit nothing and do
  not unlock. A move back or forward never unlocks, and one that returns to the
  filled address locks the page again. When the history cannot be read a commit
  is taken for a move through it.
- The person's tab is told by the extension. With
  `chrome.webNavigation.onCommitted` (permission `webNavigation`) it adds
  `currentTab` (the number of the tab the actions go to) and `commits` to the
  `update` message it already sends, and `currentTab` to `hello`: each commit is
  `{tab, documentId, url, isBackForward, count}` for a main frame commit of a tab
  the agent can act in (`transitionQualifiers` includes `forward_back` for a
  move through the history; `count` numbers a tab's commits, so a report that
  arrives late is ignored). `PROTOCOL` stays 5: a server at 5 ignores fields it
  does not know, and a browser that sends no commits leaves a filled tab locked
  (until the server restarts, which forgets the lock and the values with it).
  The lock ends only when the filled tab commits a document that is not a move
  through the history. The same document coming
  back from the history's cache keeps (or restores) the lock, and so does a move
  through the history to an address that was filled. Switching to, opening,
  going back in, or navigating another tab changes nothing about this one, and
  neither does `navigate` or `back` in the filled tab by itself: only the
  browser reporting the commit does. A tab is named by the number the extension
  gives it; a fill whose tab the extension did not name locks every tab until
  the unnamed tab commits. The guarded actions carry `expectedTab`, which only
  the server sets, and the extension refuses a call when the actions go to
  another tab by then.

A page that signs in without loading a new document (a single-page app) stays
locked after the sign-in; navigating to a page of the app loads one and ends it.

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
  Health, a run nobody is watching is allowed when the person started it: a
  schedule they wrote themselves, since signing in on a schedule is why a person
  turns an item on. `list`, `read_note` and the three sign-in actions are refused
  in a run that content started (an inbound mail, a webhook, a goal, a dream, a
  schedule the agent wrote for itself), because what such a run does is steered
  by content the person did not write. A note is not read in a subagent, whose
  answer comes back as a tool result that filing into memory cannot tell from any
  other.
- **Only while the person may use Passwords.** The permission `passwords:use` is
  asked again whenever an item is listed, read or opened to sign in, not only when
  the run began: an owner who lost it, or whose account was disabled, has no item
  the agent can use.
- **Each use is recorded.** Reading a note records the time on the item
  (`last_used_by_agent_at`) and writes a `password_item` "used by the agent"
  event to the audit log, without the text. The use is recorded before the text
  is given out; a note that cannot be recorded is not read. A use does not raise
  the item's revision (a device that has the item open can still save it), so a
  device shows the new time after a full read of the item, not from the changes
  after a revision.
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
