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
  program that speaks MCP (another program, not the person), a schedule the
  agent wrote for itself, one of the built-in schedules (the daily brief and the
  asset estimates, recorded as written by `built_in`: they read the inbox and the
  web, so mail could steer them, though their prompt is still handed to the run
  as the person's words), or any surface not known to be the person's. A
  schedule made before that was recorded counts as built in too when it still has
  the built-in name and prompt. A schedule saved with no writer, and one from
  before the writer was recorded at all, counts as the person's. A subagent is
  its parent's. The passwords tool's `list` and
  `read_note` keep the same rule.

  Three more edges are closed. A turn that carries on an answer given to a card
  after the turn that raised it had ended is the person's only if the turn that
  raised it was: the card keeps that in `agent_interaction.is_started_by_person`
  (a card from before it was recorded is not the person's), so a question a chat
  app or an MCP program raised and the person answered later does not become
  theirs. A message written while the person's turn runs is handed to that turn
  only when it is itself one the person wrote (`AskRun.IsStartedByPerson`); a
  message from MCP or a chat app that names the same conversation waits for its
  own turn. And a schedule made or reworded from the command line is recorded as
  the agent's, since the agent's shell holds the same token: only a tap on the
  dashboard or in the phone app (`Principal.IsPersonsTap`) writes `person`.
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
  back what is dragged), with the limits in "Copying and selectors" below, and moving the page (`Page.navigate`, `Page.reload`, `Page.getNavigationHistory`,
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

**Copying and selectors.** Two more things are refused on a locked page, in both
browsers and inside `steps`, because each would let the model read a value back
without the exact value ever appearing:

- *Copy, paste and drag.* A key event with editing `commands` (`selectAll`,
  `copy`, `paste`), with a control, alt or meta modifier, or for the Insert key
  (or Shift with Delete), a mouse move with a button held (drag and drop), the
  middle button, any drag command, and the `press` action with a key such as
  `Control+a` are refused. Without them a filled text field (a one-time code
  field, or a password field a show button turned into text) cannot be copied into
  an ordinary input, where the scrubbing would no longer recognize it. Plain
  typing, Shift with a letter, Enter and clicks still work.
- *A CSS selector.* A selector says yes or no about the page, and
  `input[type=password][value^="a"]` matches when the page copies the value into
  the attribute (React does). So a locked page is acted on by ref alone: any
  `selector` argument, and `wait` with `for` set to `selector`, is refused.
- *Input values are not shown.* While a page is locked, the snapshot (headless
  browser: `snapshot.js`) shows no input's, textarea's or editable region's text at
  all ("has text, not shown while the page is locked"), not only the filled ones,
  and a click or hover names such an element by its label alone. A button's value
  is its label and is still shown. This is a second line behind the two above.

**The document is asked, not only told.** Going back can restore the filled
document from the history's cache a moment before the `Page.frameNavigated` event
(or the server's history read) reaches the lock, so `steps: [back, wait, evaluate]`
could land in the gap. Before any headless command that could read the page
(`evaluate`, `screenshot`, `storage`, `cdp`, `cdp_events`, and the snapshot and
element names above) the server asks `Page.getFrameTree` for the main frame's
current loader id and locks the page again if it is a document that was filled
(`Context.CheckCurrentDocument`). If the browser does not answer, those commands
are refused rather than guessed at. `back` itself now returns only once the
history has moved to the earlier entry, and checks again.

**A page the agent scripted is not filled.** The lock looks at a page after the
fill, so it cannot tell that a script the agent ran earlier left a listener that
sends every input value to another site, or changed the form's address. So the
document a script ran in is marked (`FilledValues.NoteScripted`, in
`internal/browser/filled.go`), and `fill_login`, `fill_one_time_code` and
`passkey_sign_in` are refused on it with "This page has run the agent's script, so
it is not filled. Open the sign-in page again (navigate) and fill it before
running any script." What counts as running script on a document:

- `evaluate`, in either browser; and a `navigate` to a `javascript:` or `data:`
  address.
- Any `cdp` method that is not on a short list of ones that only look or give
  input (`internal/agent/tools/browser/script.go`): `Input.*`, `Page.navigate`
  to a web address, `Page.reload`, `Page.captureScreenshot`, `DOM.getDocument`
  and the other `DOM` reads, `DOMSnapshot.*`, `Accessibility.*` and a few more.
  Everything else counts, which is `Runtime.*`, `DOM.set*`, `DOM.remove*`,
  `DOM.insert*`, `Page.setDocumentContent`, `Emulation.setScriptExecutionDisabled`
  and whatever the protocol adds. The list names what is harmless, not what is
  not, so a new method fails closed.
- `Page.addScriptToEvaluateOnNewDocument`, `Runtime.addBinding`, `Debugger.*`,
  `Fetch.*` and `Network.setRequestInterception` put something in every page the
  tab loads from then on, which a navigation does not undo. A tab that ever got
  one is never filled again ("This tab was given a script that runs in every page
  it loads, so nothing is filled in it. Open a new tab (open) and fill the sign-in
  page there before running any script."). A tab opened later is a different tab.

The mark belongs to the document, the way the lock does, in the other direction.
In the headless browser it is exact: the loader id of the document the script ran
in is kept, and the fill asks `Page.getFrameTree` which document the page shows
now, so a reload or a new page is filled and a page restored from the history's
cache is not. If the browser does not say which document a script ran in, the
page counts as scripted for good. For the person's tab the server knows the
document by the commits the extension reports: a new document that is not a move
through the history ends the mark, and the scripted document coming back keeps
it. A script that ran before the server heard the tab's number marks every tab
until the unnamed tab commits. The check is in the server for both browsers;
the extension fills a tab it is asked to fill, and cannot see what the agent ran.

**A form that sends to another site is not filled.** The headless fill checks it
in the same script that types, the way the extension does (`isFormSendingElsewhere`):
the form's `action` and the `formaction` of every control that submits it, which
includes an image input and a control outside the form that names it with
`form=`, resolved against the page's base address. They are read with the
browser's own getters, not from `form.action` or `form.elements`, which a field
named `action` or `elements` replaces. An address on another host or port, or a
secure page sending to a plain-text address, refuses the fill ("the form around
that field sends to another site than this page"). A form with no `action`, a
relative one on the same site, and a field outside any form are filled. A
listener the site's own script adds is the site's, and is not looked for.

**The session is not read back.** Signing in leaves cookies and storage that
are the site's session. Once the agent has filled a password or a one-time code
into a host in a tab, then for the rest of that tab's life (not only until the
next document) the browser tool refuses, for that host, the `storage` action
(the page's cookies and storage), and, in that tab, the DevTools methods that
return cookies, storage or the requests the page makes with them:
`Network.getCookies`, `Network.getAllCookies`, `Network.enable` (its events carry
the `Cookie` and `Set-Cookie` headers), `Storage.*`, `DOMStorage.*`,
`IndexedDB.*`, `CacheStorage.*`, `Page.getCookies` and `Fetch.*`. The methods
can name other sites, so they are closed in the whole tab. The headless browser
has no `storage` or `cdp` action. A user name alone is not a session.

*What this does not stop.* A script in the page still reads `document.cookie` and
`localStorage` and `sessionStorage` once the lock has ended (a new document), and
`evaluate` is allowed then. A cookie the site marked `HttpOnly` is not in
`document.cookie`, which is what the site's own defense is for, but a session
kept in `localStorage` or in a cookie without `HttpOnly` is readable by script,
and the scrubbing only hides the exact password and code, not the session token
the site hands back. Signing in for the agent therefore gives the agent the
session on that site for the rest of the tab, the way the person's own sign-in
would; it does not give it the password.

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
  in a run that content started (an inbound mail, a webhook, a goal, a dream, an
  MCP client, a built-in schedule such as the daily brief, a schedule the agent
  wrote for itself), because what such a run does is steered
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
- **Memory, titles and other runs.** A turn that listed the items, read a note or
  made up a password is a private turn, like one that used Health
  (`tools.IsPrivateToolCall`, `tools.PrivateTurns` in `internal/agent/tools`,
  with `list`, `read_note` and `generate` in the list). When conversations are
  filed into memory, what the agent said and its tools answered in a private turn
  is left out, and the conversation's title and summary are not refreshed from it.
  The `conversation` tool's `read` shows the same messages as "[hidden: from a
  turn that used Health or Passwords]", so a goal, a schedule the agent wrote or
  a run a mail steered cannot read a note or a made-up password out of the
  person's chat. What the person wrote stays. It does not reach text the person
  or the agent repeats later; the tool's guidance tells the agent not to copy
  Passwords into notes, memory or messages.

  The same filter applies wherever else the conversation is read. A run the
  person did not start (a chat app, a mail, a goal, a schedule the agent wrote,
  a command left running, a program speaking MCP, a card answered after one of
  those, the agent speaking first) is given the marker where a private turn was
  when its history is loaded (`historyForRun`), and the person's own runs get the
  whole history so they can go on talking about the note. A compaction writes its
  note from the history with private turns replaced by the marker, so neither the
  note nor the runs that hold each chunk's prompt contain them. The filing runs
  judge a turn by the whole conversation, not only by the part after their mark.
  The conversation tool's `search` and `search_everything`, and the command
  line's search, leave every message of a private turn out
  (`SearchAgentConversations`, flag `isPrivateIncluded`, `internal/agent/privateturn`);
  only a tap on the dashboard or the phone app searches them. The lock-screen
  notification for the answer of a private turn says "Your answer is ready".
- **This computer's files.** `teanode passwords unlock` keeps the opened account
  key and private key, in the clear, in a file only the person can read
  (`passwords-unlock-<profile>.json`, only in `$XDG_RUNTIME_DIR/teanode`, which
  is not on disk; without it `unlock` refuses) for up to 8 hours, and
  `passwords-keys-<profile>.json` holds the confirmed and seen public keys of
  the people shared with. The agent's computer tools run
  as the person, so the file and `teanode passwords show --reveal` would open
  every item whatever "Let the agent use this" says. Two guards stop the obvious
  routes (`internal/passwords/localfiles.go` names the files and the mark once):
  the computer program's filesystem tool refuses to read, write, copy, move or
  edit a file with those names (also through a link) and leaves them out of
  `grep`; and the program sets `TEANODE_AGENT_COMMAND=1` on every shell command
  and terminal session it starts for the agent (last, so a call cannot unset it),
  and the `teanode passwords` commands that open the keys, read or write the
  unlock file or ask for the passphrase, which is every command that shows,
  copies or exports an item, answer "Passwords on this computer are not open to
  the agent's commands." **The limit, plainly:** a command the agent runs as you
  can do what you can. A program it writes can clear the mark, read the file
  with the person's own permissions, or use the clipboard or the terminal the
  person is typing into (a terminal the person attached is theirs, not marked).
  The guard stops the obvious routes, not a determined program; the way to keep
  an unlocked computer from the agent is `teanode passwords lock` when you are
  done, or not unlocking on a computer the agent can run commands on.
- **A terminal the person attached.** The terminal tool can read the screen of a
  terminal the person attached as well as type into it. A command typed there is
  unmarked, and the output of `teanode passwords show --reveal` in that terminal
  is on the screen the model can read. The person should not reveal an item in a
  terminal they have attached to the agent.
- **The person switches, the agent does not.** Turning "Let the agent use this"
  on or off, sharing, erasing and every change to an item need the person's
  device; the server cannot read an item, so the agent has nothing to change.

## What the server limits

The server cannot read an item, so what it can do is bound how much and how
fast, and keep a history that a program with a token cannot erase.

- **Size.** An item's sealed fields are at most 512 KB and any sealed key 8 KB.
  A person has at most 20,000 items (deleted ones included, until they are swept
  after 30 days) and at most 64 MiB of sealed text in the items that are not
  deleted (the fields and the keys). A save, a re-key or a restore that would go
  past it is refused with "Passwords is full: delete items or make notes
  shorter." A change that does not make an item larger is always allowed, so a
  person who is over can shorten things. A list answers every item at once, so
  this bounds what one request costs the server; paging is a follow-up in the
  plan.
- **Earlier versions of an item.** A save or a re-key keeps the version it
  replaces in `password_item_version` only when the item has no kept version
  younger than one hour. A program with a token needs no tap to save an item, so
  25 saves of garbage in a row keep the content from before the first one and
  nothing else. Versions are kept 30 days, at most 60 for an item; when there are
  more, the one just before the newest goes, so the oldest within 30 days stays.
  Nothing in the API reads them: an operator puts one back from the database.
- **Writes.** A person may make 5,000 writes to Passwords in a row (save, re-key,
  delete, restore, share, share keys, "Let the agent use this") and gets 5,000
  an hour back, so that importing a few thousand items fits and one person cannot
  wear out the revision counter that everybody's items share. Their own taps
  (the dashboard and the iPhone app) and every other credential (a command line
  token, the extension, an OAuth grant) have separate counts, so a stolen token
  cannot use up what the person does on their dashboard. The count is in memory,
  for each server instance. The agent recording that it used an item is not
  counted.
- **Unlock tries.** 10 wrong proofs in a row, 30 an hour given back, counted
  separately for the person's taps and for other credentials, so a stolen token
  guessing the passphrase runs out of tries without locking the person out of
  their own dashboard. Every wrong try and every try turned away is in the audit
  log.
- **How long a device stays unlocked.** Making it longer needs the person's tap;
  making it shorter does not.
- **A new item key when access narrows.** Turning "Let the agent use this" off or
  taking somebody out of an item's shares makes the owner's device seal the item
  again under a new item key (`RekeyPasswordItem`), so that a copy of the old
  sealed key opens nothing that is saved later. It needs no tap, because it
  widens nothing. The versions kept from before are still sealed under the old
  key.

### Limits, plainly

- **Changing the passphrase or the recovery key does not change the account
  key.** Each only seals the same account key again. Anyone who holds an older
  copy of the database and the old passphrase (or the old recovery key) can open
  the account key from it, and with it the item keys of items saved afterwards
  when they also get those items. Changing a secret protects against a copy made
  after the change, not against one made before. Rotating the account key means
  sealing every item key again; it is a follow-up in the plan.
- **The audit log names the host the agent signed in to.** When the agent signs
  in with an item, the `password_item` "used by the agent" event holds the item's
  id and kind and the host of the page (never a path, a user name or a
  password). Anyone who may read the audit log, an operator for one, can see
  which sites the agent used a person's items on.

