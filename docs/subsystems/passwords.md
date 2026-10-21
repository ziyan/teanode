# Passwords

Passwords keeps a person's logins, passkeys, cards and notes on their own
server, encrypted on their own devices. The server stores sealed text and
cannot read it, with one exception the person chooses: an item they turn on
**Let the agent use this** for. This page says what the agent may do with
Passwords, how the encryption makes that possible and no more, who may have an
item and how a device checks it, the rules the agent's tool keeps, and what the
server limits. The whole design, with the format of every sealed value,
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

## Who may have an item

The server holds the list of shares and the "usable by the agent" switch, but a
device never takes the server's word for either. Four things make that so.

**Safety codes, optional.** A person's public key has a safety code: eight groups
of four digits, worked out from the public key alone (SHA-256 of a label and the
65 key bytes; nothing the server supplies goes into it, so it cannot choose a user
id to make a matching code easier to find). Every surface shows the person's own
code, worked out from the key pair the device opened when it unlocked, never from
what the server reports: the dashboard's and the iPhone's Passwords settings, and
`teanode passwords code`. Two people can compare codes by each reading their own
off their own screen ("Ask Ada to open Passwords settings on their own device and
read you their safety code."), in person or on a call they trust. Comparing is
always available (the share screen's row for each person) and never required:
nothing waits for it and no screen asks the person to confirm a key.

**Trust on first use.** Each device keeps one known key for each person, by user
id: the first key it saw for them, from any source (sharing with them, an item
they shared, a waiting share). It seals for that key at once, with no dialog.
Sharing with somebody whose key the device has never seen seals for the key the
server gives and records it as known. If the server later gives a different key
for a known person (they erased Passwords and set it up again; a person has one
key for all their devices, so a new device never changes it), the device accepts
it, seals for it, opens that person's items with it, records it, and shows a
notice that stays until dismissed: "Ben's key changed. This happens when they
erase Passwords and set it up again. If you did not expect it, compare safety
codes with them." It never mentions new devices: a person has one key for all
their devices, so signing in on another device cannot change it. The notice has a Compare codes action showing the old
and new codes and the person's own; nothing waits on it. Comparing marks the key
"Codes compared" for display only. Known keys are per device, and a device
records a person's key as known after an item from that owner opened with it. A
recipient who has not set up Passwords has no key yet and is left waiting. A
waiting share completes by itself only on a device that sealed the item for that
person before, or where the person chose them directly (named them in the share).
Every other case waits for the person: a circle member who joins later, or a
device that never saw the item. The person finishes it with "Finish sharing" on
the dashboard and in the iPhone app (for one item, or for all of them at once
from the list), or with `teanode passwords share --waiting` (all items) or
`teanode passwords rekey <item>` on the command line. A recipient the server
reports as having too much shared with them (see the server limits) is named and
skipped instead of tried again on every pass. A prompt
would not protect anyone, because nobody compares codes before sharing and anyone
asked would press yes. What keeps the server from adding people is the signed
access record below, not the key.

**The access record.** Inside the sealed fields of every item is an `access`
record: who the item is given to (`grantees`, each a person's `userId` or a
circle's `circleId`) and `isUsableByAgent`. Only the owner's devices write it,
and they write it again whenever shares or agent use change. A device seals an
item key for a recipient only when the record reaches them (a person by their user
id, a circle member when the server says they are in that circle). The server's
list of shares only tells a device who is waiting. A device makes an agent copy of the key only when the record says
the agent may use the item, and the server's agent opener also requires it.

The record is signed so that anyone holding an item key (a recipient, or the
server through the agent key) cannot write a wider one. It carries a
`recordVersion` and a `recordMac`, an HMAC-SHA256 over the item's id and kind,
the version, the agent switch and the sorted grantees, under a key derived from
the account key (HKDF with the label `teanode passwords access record v1`). Only
the owner's devices hold the account key. Each device remembers, for each item,
the highest version it has seen with a valid signature (the dashboard in
`localStorage`, the iPhone in the Passwords keychain group, the command line in
its keys file), and each change by an owner's device writes one more than the
highest seen. A record that is missing, unsigned or wrongly signed reaches nobody
and gets no agent copy; the item says "Sharing could not be checked". A record
with a lower version than the device has seen is an old one shown again: the
device does nothing automatic with the item and says "This item's sharing was
changed back by the server". Either way the person's tap on "Set sharing again"
writes a fresh record. Recipients cannot check the signature (the key is the
owner's) and ignore the record. The server's agent opener cannot check it either,
so the protection against a forged consent is that the owner's devices never make
an agent copy from a record they cannot verify.

**A new key when access narrows.** Taking somebody off an item, ending the agent's
use of it, a member leaving or being removed from a circle, a circle being
deleted, an account being deleted (for the items shared with it), and deleting an item that had other keys all leave a copy of the old
sealed item key in the wrong hands. The server marks such an item
`needsNewKey` in the same transaction, and only `RekeyPasswordItem` clears the
mark. An owner's device that sees the mark (the dashboard and the iPhone on open
and before a save, the command line before `edit`, `share`, `unshare` and `agent`,
or when asked with `teanode passwords rekey`) seals the fields again under a new
item key and seals that key for whoever keeps access. The extension does not
re-key: it refuses to save over such an item and says to open Passwords on the
dashboard or the iPhone first. A re-key the person did not start never widens
access. It seals only for people in the verified record who already had a key for
the item when this device last saw it, and for the agent only when the record
says so and the item already had an agent copy; anyone else the record reaches
is sealed for when the device completes waiting shares (which is not a re-key,
and which a device does by itself only for people it sealed for before or the
person chose directly) or the person asks for a re-key. An item
is re-keyed automatically at most once in 10 minutes, and a session makes at most
20 automatic re-keys; past that the item is shown as needing attention. No device
ever cuts the record on the server's word. Only the person's own action narrows
it. The versions kept from before a re-key are still sealed under the old key.

Every share key names the item key it was sealed from (`itemKey`), and the server
refuses one whose item key is no longer the item's ("changed on another device").
A key row a tap stored is never overwritten except by a re-key; a row stored by a
token may be replaced by a later tap.

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
  spoke **with their own tap** (a dashboard session or the iPhone app's token,
  `Principal.IsPersonsTap`, never the surface name the client sends), or a
  schedule they wrote themselves, which is what a scheduled sign-in uses. The
  server carries into the run whether the request was a tap and whether an
  operator made it as the person (`AskSettings.IsPersonsTap` and
  `IsOperatorActingAsPerson`). A command line token, an app password, an OAuth
  grant of an app other than the TeaNode app, the extension's token, a program that speaks MCP and any operator acting as the
  person start no such run, whatever surface they name: a token that says
  `drawer` still cannot list or read anything, and the refusal says "Ask in the
  TeaNode app or on the dashboard". A run that content started is refused too: an inbound mail (research), a chat
  app, a webhook, a goal, a dream, a command left running, a restart resumed, a
  program that speaks MCP (another program, not the person), a schedule the
  agent wrote for itself, one of the built-in schedules (the daily brief and the
  asset estimates, recorded as written by `built_in`: they read the inbox and the
  web, so mail could steer them, though their prompt is still handed to the run
  as the person's words), or any surface not known to be the person's. A
  schedule made before that was recorded counts as built in too when it still has
  the built-in name and prompt. A schedule saved with no writer, and one from
  before the writer was recorded at all, counts as the person's. A subagent is
  its parent's, but it is given no Passwords answer worth keeping: `list`,
  `read_note` and `generate` are refused in a subagent, since what it finds comes
  back to its parent as a tool result that is kept like any other. The passwords
  tool's `list` and `read_note` keep the same rule about who started the run. A card answered
  after the run that raised it had ended resumes a run that counts as the person's
  only when the person's run raised the card **and** the answer was the person's own
  tap (`ResumeSettings.IsAnswerFromPersonsTap`, from `Principal.IsPersonsTap`): the
  same late answer made with a command line token, an app password or by an operator
  acting as the person resumes the run, but it may not use Passwords.

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
  dashboard or in the phone app (`Principal.IsPersonsTap`) writes `person`, and
  only a tap's save that includes the schedule's prompt takes a schedule over.
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

**A site the agent ran script on is not filled.** The lock looks at a page after
the fill, so it cannot tell that a script the agent ran earlier left a listener
that sends every input value to another site, or changed the form's address. So
`fill_login`, `fill_one_time_code` and `passkey_sign_in` are refused on any page
of a site the agent ran script on in that browser, until the person or a reset
clears it ("A script the agent ran keeps a site from being filled", below). Sign
in before running any script on a site.

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
  with `list`, `read_note` and `generate` in the list). So is a turn whose
  subagent made such a call: the subagent's answer says so
  (`has_private_call`), and the turn that received it is private. When conversations are
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
  only a tap on the dashboard or the phone app searches them, and a turn whose
  subagent read a note counts as private there too. The lock-screen
  notification for the answer of a private turn says "Your answer is ready".
- **This computer's files.** `teanode passwords unlock` keeps the opened account
  key and private key, in the clear, in a file only the person can read
  (`passwords-unlock-<profile>.json`, only in `$XDG_RUNTIME_DIR/teanode`, which
  is not on disk; without it `unlock` refuses) for up to 8 hours, and
  `passwords-keys-<profile>.json` holds the known public keys of
  the people shared with, the highest access record version seen for each item,
  and who held a key for it when the command line last saw it. The agent's computer tools run
  as the person, so the file and `teanode passwords show --reveal` would open
  every item whatever "Let the agent use this" says. Two guards stop the obvious
  routes (`internal/passwords/localfiles.go` names the files and the mark once):
  the computer program's filesystem tool refuses to read, write, copy, move or
  edit a file with those names (also through a link) and leaves them out of
  `grep`; the names are the two files above and an export,
  `passwords-export-*.json` or `.csv`, which is what `teanode passwords export`
  writes when it is given no file name; and the tool refuses any file that
  exists and begins as an export does (a JSON object with an `items` array holding items
  with an `itemKind` or carrying `version` and `exportedAt`, in any key order, or
  a CSV file whose header row is one `import csv` reads: Chrome, Apple Passwords
  or Bitwarden), whatever its name; and the program sets `TEANODE_AGENT_COMMAND=1` on every shell command
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
  done, or not unlocking on a computer the agent can run commands on. The mark
  does not reach every route the agent has to a shell on this computer: a
  coding-session pane of herdr (a pane the person attached or the agent opened
  through the herdr tool) is a terminal that was not started by the program, so
  its commands are not marked and can open Passwords like the person's own.
  Run `teanode passwords lock` before the agent works in such a pane, and do not
  leave an export file lying about.
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
fast, and keep a history that a program with a token cannot erase. A **tap** is
the person's own action on the dashboard or in the iPhone app
(`Principal.IsPersonsTap`); a command line token, the extension and an OAuth
grant are not taps. The one exception is the TeaNode iPhone app's own sign-in: a
token has the app's kind (`ClientKindApp`, which `IsPersonsTap` accepts) only when
the person approved the sign-in with a tap and the client is registered with
exactly the app's return address, `com.teanode.app:/oauth` (or the token came from
a connect code made with a tap, collected by a client registered that way). Any
other app can register an address of its own and ask for the whole API, and is
signed in, but its token is not a tap, so it cannot set up, erase, share, restore
or let the agent use an item. That holds at every step: a connect code made with
a tap, collected by another native app, gives an ordinary token, and a refresh
keeps the mark only while the token's client is still registered with exactly the
app's address (migration 0251 took the mark off the tokens of other clients that
an earlier migration had marked).

  **A known limit of the app mark.** The mark rests on the app's return address,
  a private-use scheme (`com.teanode.app:`). Nothing stops another app on the same
  iPhone from also claiming that scheme: if the person approves a sign-in they
  did not start, and the system hands the return address to that other app, it
  collects a token that is a tap. Code on the server cannot close this, so the
  consent page for the TeaNode app says "Approve only if you just started signing
  in to the TeaNode app on your iPhone." (English, Chinese and Japanese). The real
  fix is a return address the system gives only to the app that owns it: an HTTPS
  callback tied to the app's associated domain (a universal link), or App Attest
  on the sign-in. Both need work in the iPhone app and the server together; it is
  a Follow-up in `docs/planning/passwords-execplan.md`.

- **Size.** An item's sealed fields are at most 512 KB, and a sealed share key or
  agent copy is exactly the size of a sealed 32-byte item key, written the one way
  every client writes it (no white space, the fields in the order `version`,
  `ephemeralPublicKey`, `nonce`, `ciphertext`, 232 bytes, at most 400): a padded
  or reordered key is refused, and a key replaces a stored row only if it is no
  longer than the row, so a replacement cannot take a person past the bounds
  below without being counted. A person has at
  most 20,000 items that are not deleted and two byte allowances, checked apart:
  - Items that are not deleted hold at most 64 MiB of sealed text (the fields and
    the keys). That is the only limit that stops a save, by whomever it comes
    from, so deleting an item frees its room at once. A save, a re-key or a
    restore that would go past it is refused with "Passwords is full: delete items
    or make notes shorter." A change that does not make an item larger is always
    allowed, so a person who is over can shorten things.
  - Deleted items and the versions that saves replaced hold at most 128 MiB. When
    that is full, a change that is not a tap and would add to it (a save that
    would keep a version, a delete, a re-key of a deleted item) is refused with
    "Passwords is keeping too many deleted items and old versions". A tap's save,
    delete or restore always goes ahead (a tap's save then does not keep the
    version it replaces), so a token cannot fill it with junk and leave the
    person unable to save. `PurgePasswordItem` (delete forever, a tap only)
    removes a deleted item at once with its old versions; it is the only way to
    free this allowance before the 30 days are up, and the dashboard and the
    iPhone offer it in Recently deleted and `teanode passwords delete --forever`
    says where to do it.

  So one person can hold up to 192 MiB of sealed text for up to 30 days.
- **What sharing can add.** An owner's items reach at most 50,000 item and
  recipient pairs; a share that would go past it is refused with "Your items are
  shared with too many people", and a change that does not add pairs is always
  allowed. What others share with one person is at most 64 MiB of sealed text
  and 20,000 items, of which at most 8 MiB and 5,000 items may come from any one
  owner, so one member of a circle cannot use up another's room, with a few large
  items or with many tiny ones. A key that would push a recipient past any of
  those bounds is not stored for them: the recipient comes back with
  `hasTooMuchShared`, the owner's devices show "<name> has too much shared with
  them", and the waiting list leaves them out (a device completing waiting shares
  does not try for them on every pass). An
  item that grows (a save that makes its sealed text larger) is refused when a
  person it is shared with would go past those bounds; a change that does not
  make it larger is always allowed. A list answers the oldest keys first up to
  the bound. `share --waiting` prints the number of keys the server stored.
- **The most rows.** A person has at most 40,000 items counting deleted ones, so
  a program that saves and deletes cannot make a first read grow without bound.
  A save that is not a tap stops making new items at 36,000, so the last 4,000
  places are the person's: a token cannot take them all and leave the dashboard
  unable to add an item.
- **One list at a time.** A list answers every item at once, so the server lets
  one `ListPasswordItems` or `ListWaitingPasswordShares` run for a person at a
  time, with one place for the person's taps and another for every other
  credential, and holds it until the answer has been written to the reader (with
  a minute to write it), not only until the resolver returns. A second one is
  turned away with "Passwords is already being read for you. Try again in a
  moment." The waiting list answers at most 2,000 entries, newest items first,
  and sets `hasMoreWaiting` on every entry when more wait; the devices say so
  ("more are waiting: run this again"), and finishing the ones listed shows the
  rest. Paging is a follow-up in the plan.
- **Earlier versions of an item.** A save or a re-key keeps the version it
  replaces in `password_item_version` only when the item has no kept version
  younger than one hour. A program with a token needs no tap to save an item, so
  25 saves of garbage in a row keep the content from before the first one and
  nothing else. A save that is not a tap also always keeps the version it
  replaces when that version was saved by a tap, up to 20 of them for an item, so
  the person's newest edit is not lost to a token within the hour. Versions are
  kept 30 days, at most 60 of the others for an item; when there are more, the one
  just before the newest goes, so the oldest within 30 days stays. The person
  reads them and puts one back with their tap (`ListPasswordItemVersions` and
  `RestorePasswordItemVersion`, neither open to a token or an operator): the
  version's fields and item key become the item's, what they replace is kept as
  a version whatever the hour rule says, the keys given to other people and the
  agent's copy are dropped, and the item is marked `needsNewKey`. Restoring brings
  back the content only: the device checks that what came back is the version
  the person opened, writes the sharing record it held before the restore into
  the restored fields (so who has the item, and whether the agent may use it,
  stay as they were), and gives the item a new key at once for those people.
  The agent's copy comes back through Finish sharing. The date a version shows is
  when a later save replaced it.
- **Locks and one document.** Every change of Passwords holds the circle
  membership lock and the revision lock until its request commits. A document may
  name at most 25 Passwords mutations (it is refused whole, before any runs), a
  change waits at most 5 seconds in all for its locks (one deadline across the
  circle membership lock, the revision lock and any row lock, each wait getting
  what is left of it) and is then told "Passwords is
  busy with other changes. Try again in a moment." with nothing changed (every
  change takes its locks under a savepoint, so the request can still commit; once
  one change of a document has timed out, the rest of that document's changes are
  refused at once, so a document waits once and not for each change; leaving a
  circle and deleting a circle or an account give the same answer when ending
  their Passwords shares waited that long, and a person with no items, shares or
  keys has nothing to end and never waits), and
  how much a person holds is summed from two numbers the database keeps for each
  item (`item_bytes`, `versions_bytes`) so that the sum does not read their
  versions while the locks are held.
- **Writes.** A person may make 5,000 writes to Passwords in a row (save, re-key,
  delete, purge, restore, share, share keys, "Let the agent use this") and gets
  5,000 an hour back, so that importing a few thousand items fits and one person
  cannot wear out the revision counter that everybody's items share. A call that
  names several items counts one for each. Taps and every other credential have
  separate buckets, so a stolen token cannot use up what the person does on their
  dashboard. The count is in memory, for each server instance. The agent recording
  that it used an item is not counted. A refused call draws no revision number.
  Revisions are 64-bit (`Int64` in GraphQL), and a variable that is a fraction or
  past what a float holds exactly is refused, not cut.
- **Unlock tries.** 10 wrong proofs in a row, 30 an hour given back, counted
  separately for the person's taps and for other credentials, so a stolen token
  guessing the passphrase runs out of tries without locking the person out of
  their own dashboard. Every wrong try and every try turned away is in the audit
  log.
- **How long a device stays unlocked.** Making it longer needs the person's tap;
  making it shorter does not.
- **A new key when access narrows.** `RekeyPasswordItem` seals an item again
  under a new item key, so that a copy of the old sealed key opens nothing that is
  saved later. It needs no tap, because it widens nothing: the server only keeps,
  replaces or ends the agent's copy, never begins one. The server marks an item
  `needsNewKey` wherever access narrows, and the mark is cleared only by a
  re-key. "Who may have an item" above says what the devices do about it.

### Limits, plainly

- **The dashboard is code the server delivers.** A server operator who changes
  the dashboard's code could capture a passphrase typed there, and with it
  everything the person opens on that page. The iPhone app, the command line and
  the Chrome extension do not load their code from the server, so they do not
  have this weakness. A person who does not trust the operator of their server
  with that should open Passwords only in the iPhone app, the command line or
  the extension.

- **Changing the passphrase or the recovery key does not change the account
  key.** Each only seals the same account key again. Anyone who holds an older
  copy of the database and the old passphrase (or the old recovery key) can open
  the account key from it, and with it the item keys of items saved afterwards
  when they also get those items. Changing a secret protects against a copy made
  after the change, not against one made before. Rotating the account key means
  sealing every item key again; it is a follow-up in the plan.
- **Losing `passwords:use` does not remove keys.** A person who loses the
  permission, while others hold keys for their items or while they hold keys for
  other people's items, keeps those key rows until the next re-key of the item.
  Meanwhile the query that decides who is reached leaves the pair out, so nothing
  is listed or opened for them, and a person who gets the permission back has the
  keys they had. Nothing marks the owner's items `needsNewKey` when it is lost.
- **A key a token stored fills a waiting place.** A key row stored by a token (the
  command line) makes the recipient count as having a key. Only a later tap, or a
  re-key, replaces it, and nothing makes a device do that on its own.
- **The audit log names the host the agent signed in to.** When the agent signs
  in with an item, the `password_item` "used by the agent" event holds the item's
  id and kind and the host of the page (never a path, a user name or a
  password). Anyone who may read the audit log, an operator for one, can see
  which sites the agent used a person's items on.


### What a run the person did not start can still see

- **A compaction inside a private turn.** Which messages belong to a private
  turn is judged over the whole stored conversation before the compacted part is
  dropped (`historyOf`), and each message carries it (`IsFromPrivateTurn`), so an
  answer that quotes a note right after the cut is still hidden from a run the
  person did not start and from the next compaction note.
- **The phone push of the agent's own turns.** The answer of a turn that called
  a private tool (a schedule the person wrote that reads a note) is sent as
  "Your answer is ready", judged over the whole conversation like the above.
- **The agent's shell.** The command line sends the surface `agent_command`
  instead of `cli` when `TEANODE_AGENT_COMMAND` is set. The server keeps it as
  given and it is not on the list of surfaces that count as the person's, so a
  turn the agent starts from its own shell cannot use Passwords.
- **A schedule the agent wrote.** A run it wakes is refused with words that say
  the schedule was written by the agent and that the person can open it on the
  dashboard or in the app (Agent, Schedules) and save it themselves to let its
  runs use Passwords. Only a tap's save that includes the prompt makes it theirs. The schedule tool says the same when the agent adds or rewords a
  schedule whose prompt is about signing in, a login or a password, so that the
  agent tells the person.

## A script the agent ran keeps a site from being filled

A script can leave something behind that outlives its page: a window it opened
(which reads the sign-in page through `opener.document`), a worker, a handler on
another page of the site. A mark on one document is not enough, and neither is a
mark that the page replaced while a script was on its way. So the mark is the
site's.

- **What counts as running script** (`internal/agent/tools/browser/script.go`):
  `evaluate`, in either browser; a `navigate` to a `javascript:` or `data:`
  address; any `cdp` method that is not on a short list of ones that only look or
  give input (`Input.*`, `Page.navigate` to a web address, `Page.reload`,
  `Page.captureScreenshot`, `DOM.getDocument` and the other `DOM` reads,
  `DOMSnapshot.*`, `Accessibility.*` and a few more), which means `Runtime.*`,
  `DOM.set*`, `DOM.remove*`, `DOM.insert*`, `Page.setDocumentContent`,
  `Emulation.setScriptExecutionDisabled` and whatever the protocol adds (the list
  names what is harmless, so a new method fails closed); and
  `Page.addScriptToEvaluateOnNewDocument`, `Runtime.addBinding`, `Debugger.*`,
  `Fetch.*` and `Network.setRequestInterception`, which put something in every
  page the tab loads from then on. A tab that ever got one of those last is never
  filled again ("This tab was given a script that runs in every page it loads, so
  nothing is filled in it. Open a new tab (open) and fill the sign-in page there
  before running any script."). A tab opened later is a different tab.
- **Marked before and after.** `Context.Evaluate` marks the document before the
  script, listens for the main frame's commits while it runs, and marks again when
  it returns: the document shown then and every document committed meanwhile. The
  person's tab marks the same way: the extension names the document the script ran
  in with its answer (`{scriptRun: {documentId}, value}`, which the server unwraps
  so the model reads only the value) and the server marks that id. In the headless
  browser the mark is exact by loader id; if the browser does not say which
  document a script ran in, the page counts as scripted for good.
- **The mark is the site's.** Once a script ran on a site (scheme, host without
  `www.`, port: `OriginOf` in Go, `originOf` in `tabDocuments.ts`), no fill, code
  or passkey is done on any page of it. Headless: for the life of the page, until
  the action `reset`, which throws it away and opens a fresh browser context (no
  cookie, storage, window or worker from before; values already typed stay
  hidden) and tells the model to sign in first. The person's browser: the
  extension keeps the sites in `chrome.storage.local` under `scriptedOrigins`
  (origins only), marks them before and after each script, and refuses
  `fill_login` and `fill_one_time_code` on them itself, in the words of
  `ScriptedSiteInTabWords`. The server keeps no copy to decide on, which would go
  stale when the person allows a site again; the list rides on the `hello` message
  (`scriptedOrigins`) for a server that wants it. The options page's button
  "Allow the agent to fill on these sites again" sends
  `passwords:clear-scripted-origins`; `passwords:scripted-origins` answers the
  list. Both are answered only for the extension's own options page.
- **Windows.** After a script, the headless page marks the site of every other
  target of its browser context, and every fill (and passkey) first closes every
  target but the page. The extension marks the site of each tab whose opener is
  the scripted tab.
- **Which commands count** as a script in the person's tab is the server's list
  (`scriptRunOf`); it sends the result as `isScript` with the action (the model
  cannot set it), and an extension that is not told treats a `cdp` command as a
  script.
- Tests: `internal/browser/scripted_site_test.go` (the race with the sign-in page
  on the same site and on another, the popup, the fresh browser, windows closed
  before a fill) against a real Chrome, and `agentTabActions.test.ts` for the
  extension. The headless snapshot and element names also treat an input drawn
  with `-webkit-text-security` as a secret field.

## The agent's input and the person's autofill

- **The gap.** The agent's real input (the DevTools protocol's mouse events)
  looks to a page like the person's: it can click a field, open the suggestion
  frame and click a row, and the fill that follows is read back from the field
  with `Runtime.evaluate`, an item the agent may not use. Audited in Chrome 154
  against the built extension.
- **The rule.** `web/extension/src/agentActivity.ts` records, per tab, the time of
  the agent's last command (`background.js` notes every `act` on a tab, and the
  tab it opens, before it is done, so any input it sends is noted before the page
  can ask for a fill because of it) and the time of the person's last input (what
  the dashboard sends while they hold a tab they took over, which the server
  passes on only then). `passwords/background.ts` answers a page's
  `passwords:suggest`, `fill`, `fill-code`, `generate`, `captured` and the bar's
  `save` as if there were nothing to offer, or with "The agent is working in this
  tab", while the agent's last command in that tab is less than 60 seconds old,
  unless the person's input is newer than that command and under 10 seconds old.
  The times are kept in the session storage, so a stopped worker does not forget
  them. The window's own list (the person clicking Fill in the extension window)
  is not affected.
- **Not "while attached".** The debugger stays attached for ten idle minutes, but
  every input the agent can send is itself a command, and is noted first, so the
  minute after the last command is what matters. Refusing for the whole of an idle
  attachment would only have stopped the person's autofill in a tab for ten
  minutes after the agent was last there.
- **Proved in Chrome 154** (`scenario_autofill`): with the check off the agent's
  two clicks fill the field and `Runtime.evaluate` reads
  `{user: "ada", password: ...}` back; with it on both are empty. With a take-over
  (`cast` then `pointer` messages) the person's click on the row fills, and the
  agent's next click on the same row does not.
- **The suggestion frame and the dynamic address.** With `use_dynamic_url` in the
  manifest, `chrome.runtime.getURL` names a dynamic id while the frame's document
  is at the fixed one, so the connect message addressed to the frame's address was
  never delivered and the suggestion stayed empty. It is now addressed to
  `chrome-extension://<fixed id>` (`frameOriginOf`), and the manifest no longer
  asks for dynamic addresses: in Chrome 154 the debugger is detached from the tab
  (`target_closed`) the moment such a frame opens in it, which ended the
  take-over's stream and the agent's debugging session at the person's first click
  into a field. The cost is that a page can tell the extension is installed (by
  asking for a frame page it knows the id of); it cannot read anything from the
  frame, which takes only the channel the content script hands it.
- **Words.** A changed key of an owner shows as a notice in the window, not a
  button that gates use: that owner's shared items keep opening with the new key,
  and the window can show both codes for comparing. The question before filling a
  login into another site names the whole origin of the page and of each saved site, so a
  different scheme or port is seen.
- **Seen, not fixed here.** The `evaluate` action fails in a tab with an
  `EvalError`: `inPage` builds a function from the expression with
  `new Function`, and the extension's content security policy refuses
  `unsafe-eval` in the isolated world it runs in. It belongs to the take-over
  work.

## The attached tab's reach

The person's own tab carries their session, so what the agent may send to it is
narrowed to the page itself (`internal/agent/tools/browser/reach.go` on the
server and `web/extension/src/reach.js` in the extension, which a test keeps
equal; `docs/subsystems/devices.md` has the whole account).

- **An exact list of DevTools methods.** The `cdp` action sends only methods on a
  list of exact names: real input, reading the page, watching what it asks the
  network for, and moving it. Whatever the protocol adds later is refused until it
  is added by hand. The methods that would put a file of the person's machine into
  a page for it to read back are refused by name: no `DOM.setFileInputFiles`, no
  `Input.dispatchDragEvent`, no file chooser interception.
- **No TeaNode or extension pages.** `open`, `navigate`, `switch`, `fetch` and
  `Page.navigate` refuse an address on a host the server is reached by or one that
  is not http or https, and every other action refuses a tab that shows such a
  page or a `chrome-extension://` one. A request made by script from the server's
  own origin carries the person's session cookie, which the server takes for the
  person's own tap. The server refuses first, so a deploy closes it before anyone
  reloads the extension, and the extension refuses again against where the tab is
  at that moment.
- **No input into the person's TeaNode panel.** The panel the extension puts on a
  page is the dashboard's drawer, signed in as the person. The extension refuses a
  mouse, touch, tap or scroll gesture whose point is inside it, keys while its
  frame has focus, and `click`, `hover`, `type` and `select` on an element it
  covers. Its sign-in token is handed to its frame once, by the extension, and is
  in no address. The protocol is also kept from reading through frames (`pierce`,
  a named context or frame, `DOMSnapshot`, `Page.captureSnapshot`,
  `Page.getFrameTree`).
- **Nothing filled or suggested in a tab the agent is driving.** The extension
  records the time of the agent's last command in each tab
  (`web/extension/src/agentActivity.ts`) and, for 60 seconds after it, answers a
  page's request for a suggestion, a fill, a code, a generated password, a captured
  login or a save as if there were nothing to offer, unless the person holds the
  tab and their own input is newer than the agent's command and under 10 seconds
  old. The agent's real input looks to a page like the person's: it could click a
  field, open the suggestion frame, click a row, and read the fill back with
  `Runtime.evaluate`. This is separate from the agent's own fill actions, which
  the lock, the origin check and the scripted-site rule govern.

## On the devices

- **The dashboard** keeps its opened keys in memory, shows the person's own safety
  code in Passwords settings, seals by the access record and the known keys,
  shows a notice when a person's key changed, completes waiting shares by itself
  only for people it sealed for before or the person chose directly (the rest wait
  for "Finish sharing"), and re-keys items the server marks. Its remembered record versions and key holders are in `localStorage` for
  each signed-in person. A conflicting save keeps what the person typed. Exports
  leave the access record out. Recently deleted offers Delete forever.
- **The iPhone** keeps the person's known keys, the highest record
  versions and the key holders in the Passwords keychain group, which the share
  extension and the widgets cannot write (one keychain item for each, so two
  processes cannot undo each other); the cache is written under a file
  coordinator. AutoFill does not open the items to draw its list. The app writes a
  sealed summary of the logins (name, user name, sites, the sites of passkeys, who
  shared it, whether it has a password and a code) with every list capped and the
  whole summary held to 1 MiB, the person's own logins first and shared ones the
  first dropped; opening the summary and then one item at a time keeps the
  extension under its memory limit however many logins circle members share. A
  login somebody shared is labeled **Shared by** the owner's name, and a login for
  a related site (a parent or child domain) is asked about like any other
  before it is filled.
- **The command line** unlocks only under `$XDG_RUNTIME_DIR/teanode`. `teanode
  passwords code` prints the person's own safety code; `share`,
  `share --waiting` and re-keys ask nothing about keys: they seal for the known or
  first-seen key and print one line for each first-seen or changed key ("first key
  for ben recorded", "ben's key changed: compare codes with teanode passwords
  code"); `trust <user name> <code>` is the optional way to mark codes compared,
  records the key only if the 32 digits match what the server shows, never prints
  the expected code, and gates nothing; `rekey <item>` gives an item a new key (for
  everyone the signed record reaches); before it seals anything it says who gets
  the key and whether the agent gets a copy, and asks for `yes` at a terminal, or
  needs `--yes` without one, and `list` and `show` name the same people; `delete
  --forever` removes an item that is already deleted, which needs a tap, so it
  says where to do it. `edit`, `share`, `unshare` and `agent` re-key first when
  the server marks the item, for people and the agent this computer last saw
  holding a key, at most 20 times in a command and once in 10 minutes for an item;
  `list` and `show` say when an item needs a new key.
- **The extension** shows who shared a login and asks before filling another site's
  login, naming the whole origin of the page and of each saved site. It skips a
  malformed shared passkey, hides masked fields from snapshots and shows a stored
  `http` address as a problem. It does not re-key or share: it refuses to save over
  an item the server marks `needsNewKey` and says to open Passwords on the
  dashboard or the iPhone first, and it keeps the access record through edits.
