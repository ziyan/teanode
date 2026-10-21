# The agent extension

A small Chrome extension that puts your TeaNode agent on whatever page
you are on, and attaches the tab you are looking at to it, so the agent
can read and act in that tab with your session while you watch. It is
for what the headless browser cannot do: a portal only you can log into,
your bank's message centre, a form to fill from an email.

It is built with the dashboard (`make web`, or `npm run build:extension`
in `web/`) into `web/extension/dist/`, with the dashboard's own tokens
bundled into its options page. Load that directory unpacked
(`chrome://extensions`, developer mode, *Load unpacked*), open its
options, enter your server's address and press *Sign in*: the server's
authorization page opens — the same page `teanode auth login` uses — you
press Authorize there, and a token comes back to the extension, which
keeps it in the browser's own storage. *Sign out* on the options page
revokes the token.

Signed in, the extension keeps your browser connected to your agent, and
the badge shows a dot. The agent can then open a tab in your browser when a
page needs your session, in a "TeaNode" tab group, and act there; it can
list the tabs it opened, switch between them and close them, never yours.

The extension's button opens a panel on the page: the dashboard's own
drawer, framed from the server and signed in by the extension — the same
conversation you would have on the dashboard. On the dashboard's own
pages the button opens the built-in drawer instead. The panel's bar has
*Attach this tab*; while a tab of yours is attached the badge says *on*,
and the agent reads and acts in that tab, beside any it opens. *Detach*
gives the tab back and leaves the browser connected. An operator can
switch the whole thing off for the server with `agent.browser.attachTabs`.

The agent acts in the tab as you would, with your session, and the
extension does not refuse things on your behalf: filling in a form,
including a password or a card number, is what you attached it for. What
it does hold is the edge of the tab itself — fetching only from the site
the tab is on, reading only that site's storage, sending only DevTools
methods on a list of exact names (nothing that reaches past this tab into
the whole browser, such as every site's cookies, and nothing that puts a
file of yours into a page), and never driving TeaNode itself or this
extension's own pages in your browser. One thing is kept out of the conversation rather
than out of your hands: a password already filled in on the page is
marked in a snapshot and its value left out, because a value read into
the conversation is a value stored and sent onward. A password or a
one-time code that the agent signs in with from Passwords goes further: the
tab is locked against scripts, pictures and the network log until it
loads a new page, no request body is ever kept, and the extension tells the
server when a tab the agent can act in loads a new page (the
`webNavigation` permission), which is what ends the lock.

The agent may open tabs at addresses of its choosing and read them with
your session, the way you could; that is what attaching is for, and it
happens in front of you, in the TeaNode group beside your tab.

## Passwords

The extension is also where your logins and passkeys from **Passwords** are
used in the browser. Passwords is set up once on the dashboard, with a
passphrase and a recovery key; the extension then fills logins, offers to save
one after you sign in, makes passwords, and creates and uses passkeys on
websites. It needs the same sign-in as above (the token it holds is not a tap
on the dashboard, so it cannot set up, share or hand an item to the agent).

- **Unlock.** Press **Ctrl+Shift+L** (**Command+Shift+L** on a Mac) on a page,
  choose *Fill from TeaNode Passwords* from the context menu on a field, or press
  *Open Passwords* on the options page. A small window opens. It asks for the
  passphrase (or the recovery key) once; the server proves it right without ever
  seeing it. The window lists the logins for the page first, then all of them,
  with *Fill*, *Copy password*, *Copy user name*, the one-time code, a password
  generator, *Lock* and a link to Passwords on the dashboard. A copied password
  or code is cleared from the clipboard after 30 seconds if nothing else was
  copied meanwhile.
- **Suggestions.** Click into a user name or password field on a sign-in page
  and a small frame appears under it with the logins saved for that site (or
  *Unlock TeaNode Passwords*). The frame is a page of the extension, not part
  of the website, and only a click you make in it fills anything. Nothing is
  ever filled when a page loads, into a field you cannot see, or on a site other
  than the saved one: a login matches a page only by origin, the same host
  (`www.` is ignored) and the same port, and never on plain `http` unless it was
  saved as `http`. A site saved without a port means the default port of its
  scheme, and one saved with a port matches only that port; the same cases are
  checked by the server's own rule (`internal/passwords/testdata/sitematch.json`).
  A field counts as one you can see only if it is not hidden, not faded below
  half (its own opacity and that of every element around it together), not cut
  off by a clip path, a clip or an overflow, and nothing else is drawn over its
  middle. A suggestion appears only right after a key or a click of yours (the
  browser's user activation), so a page that focuses a field by script gets
  none; pressing on a field that already has the focus asks for it. A sign-in
  form inside a frame from another site, or a form that sends what you type to
  another host than its page's own, is filled only after the frame says so and
  you say yes; the window's Fill does the same. A page that tries to hide the
  frame, fade it or cover it gets nothing: the extension checks the frame is
  really in front of you before it acts on a click.
- **From the keyboard.** The down arrow in a field with the suggestion open
  moves into it (the first login has the focus), the arrow keys move between the
  buttons, Enter fills, Escape returns to the field, and Tab past the last
  button leaves the frame and closes it.
- **Frames and messages.** The suggestion and the save bar are pages of the
  extension in a frame. The content script hands each one end of a
  `MessageChannel` once the frame has loaded (the only message that crosses the
  page's window, addressed to the extension's own origin), and both talk through it
  from then on, so nothing on the page can hear or forge them. A frame that does
  not answer within three seconds is closed.
- **Saving.** After a sign-in form is sent with a password that is new, or
  changed, the next page shows a bar at the top: *Save this login to TeaNode
  Passwords?* or *Update the password for ...?*. A sign-in is remembered only
  if the browser says you just pressed a key or clicked, so a script that
  submits a form or clicks its own button leaves nothing to save. If Passwords
  is locked, the bar opens the window to unlock first, and the login waits in
  the extension's memory for two minutes (never on disk) for that. Nothing is
  offered in a private window. Updating a password keeps the old one on the
  server for 30 days (`password_item_version`, the newest 20), but nothing in the
  extension or the dashboard shows it yet, so the bar does not promise it; it
  can be put back from the database.
- **Passkeys.** Chrome gives extensions no passkey provider, so, as other
  password managers do, a script in the page's own world replaces
  `navigator.credentials.create` and `.get` for passkey (`publicKey`) requests
  only; everything else goes to the browser untouched. The extension checks the
  site's relying party id against the page's host, asks you in a window of its
  own ("Save a passkey for example.com as ada?", "Sign in to example.com with
  your passkey?"), makes or uses an ES256 passkey stored in the login for that
  site and user name, and answers the page. *Use the browser instead* hands the
  request back to Chrome, and is remembered for that site until the browser
  closes. The window also names the page that asked when its host is not the
  relying party id. A passkey says you were *verified* (the user verified flag)
  only if you typed the passphrase in that window or within the last minute;
  otherwise it says only that you were present. A site that insists on
  verification (`userVerification: "required"`) gets no answer until you have
  typed the passphrase, and closing the window answers it with
  `NotAllowedError`. The window opens without taking the keyboard focus when the
  page that asked is not the tab you are looking at. The question ends, and
  nothing is saved, when the page withdraws its request, when the tab goes to
  another address or when it is closed. If Passwords is locked, the window asks
  you to unlock it, but only for a site the extension knows has a passkey here
  (see below); a site with no passkey here is left to the browser with no
  window and no question. The browser's own list of passkeys under a field
  (conditional mediation), silent requests, requests from a frame of another
  origin than its page, and security key requests are left to the browser. A
  relying party id must be the page's host or a domain above it that is not a
  public suffix (a list of the common hosting services, not the browser's full
  list), and is never an address (except `127.0.0.1` for a local test page).
- **The agent.** The agent's `fill_login` and `fill_one_time_code` actions in an
  attached tab type a login or code into fields it names, only on a page whose
  host is the one the server named, and answer without repeating the value.

What it keeps, and where. The opened account key and key pair live only in
`chrome.storage.session`, which the browser holds in memory, never writes to
disk, clears when it closes and hides from web pages and content scripts. They
are dropped after the lock time you set on the dashboard with no use (never
more than a day, whatever the server says), when the browser closes, and when
you sign out on the options page. The service worker also closes
`chrome.storage.local` and `chrome.storage.sync`, which hold the sign-in token
and the server address, to content scripts each time it starts, so only the
extension's own pages and worker can read them. The panel's script, which runs
in the page, is given the token only in the seconds after you press the
extension's button on that tab, and asks the worker for everything else. Nothing decrypted is
ever stored: the sealed items are fetched from your server, opened in the
extension's service worker for the one thing being done, and let go; a list in
the window is shown from that and not kept. The only things kept in
`chrome.storage.local` are public: the public keys of people who shared items
with you, so a key that changes later is noticed, whether Passwords is set up,
and which sites have a passkey here, as salted SHA-256 hashes of the relying
party ids (the salt is stored beside them, so this keeps the list from being
read at a glance and is not a secret), so that a locked Passwords can leave a
site with no passkey to the browser without a window. A shared login whose
owner's key changed is not filled; the window says so and points to the
dashboard. The extension talks to your TeaNode server and to nothing else.

The extension asks for the permissions this takes beyond the agent tab: content
scripts on every site (to see sign-in fields, to draw the frame and to carry
passkey requests), the context menu and a keyboard shortcut, alarms (to lock
and to clear the clipboard), and an offscreen document with clipboard access
(a service worker has no clipboard, and clearing a copied password needs to read
it back to see whether it is still there). The extension's text is in English
only, like its options page and panel.
