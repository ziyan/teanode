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

The extension's button opens a dropdown: the logins for the site you are on,
and a *Chat with TeaNode* button. That opens Chrome's side panel, beside the page
and not on it: the dashboard's own drawer, framed from the server and signed in
by the extension, the same conversation you would have on the dashboard. The
side panel's bar has *Attach this tab*, which acts on the tab in front; while a
tab of yours is attached the badge says *on*, and the agent reads and acts in
that tab, beside any it opens. *Detach* gives the tab back and leaves the browser
connected. An operator can switch the whole thing off for the server with
`agent.browser.attachTabs`. The side panel needs Chrome 127 or later.

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

- **The dropdown.** Press the TeaNode button, press **Ctrl+Shift+L**
  (**Command+Shift+L** on a Mac) on a page, or choose *Fill from TeaNode
  Passwords* from the context menu on a field: the button's dropdown opens,
  hanging from the toolbar, where no website can draw over it or click it. It
  asks for the passphrase (or the recovery key) once; the server proves it right
  without ever seeing it. It stays open until the lock time passes without use
  (at most a day), also across a restart of Chrome, and Lock closes it. If the
  shortcut does nothing, another extension had it when this one was installed:
  the dropdown then says so, with a button to the page where shortcuts are set.
  Where Chrome cannot show the dropdown (a window without a toolbar), the same
  page opens in a small window.
- **This site's logins, and Enter.** The dropdown's search field starts with the
  page's host, selected, and lists only the logins saved for the page. The first
  is chosen: Enter fills its user name and password into the page and closes the
  dropdown, so **Ctrl+Shift+L, Enter** signs you in. The arrow keys choose
  another; typing replaces the host and searches every login by name, user name
  and site; clearing the field lists them all. Each login also has *Fill*, *Copy
  password*, *Copy user name* and its one-time code, and the dropdown has a
  password generator (whose *Fill* puts the password into the page's new
  password field and its confirmation), *Lock* and a link to Passwords on the
  dashboard. A copied password or code is cleared from the clipboard after 30
  seconds if nothing else was copied meanwhile.
- **Which form.** The dropdown fills the frame where you last focused a sign-in
  field (or, from the shortcut, the frame holding the focus), else the first
  frame showing a password field. Nothing is ever filled when a page loads, into
  a field you cannot see, or on a site other than the saved one without asking:
  a login matches a page only by origin, the same host (`www.` is ignored) and
  the same port, and never on plain `http` unless it was saved as `http`. A site
  saved without a port means the default port of its scheme, and one saved with
  a port matches only that port; the same cases are checked by the server's own
  rule (`internal/passwords/testdata/sitematch.json`). A login picked for a page
  it is not saved for is filled only after the dropdown asks and you say yes. A
  field counts as one you can see only if it is not hidden, not faded below half
  (its own opacity and that of every element around it together), not cut off
  by a clip path, a clip or an overflow, and nothing else is drawn over its
  middle. A sign-in form inside a frame from another site, or a form that sends
  what you type to another host than its page's own, is filled only after the
  dropdown says so and you say yes.
- **Nothing on the page.** The extension draws nothing into websites: no
  suggestion under a field, no bar at the top. Everything you see and choose is
  in the dropdown or the side panel.
- **Saving.** After a sign-in form is sent with a password that is new, or
  changed, the TeaNode button gets a **+** on the next page, and its tooltip
  says *save the login you used on ...?* or *update the saved password for
  ...?*. The dropdown then shows the offer at the top, with *Save* (or *Update*)
  and *Not now*. A sign-in is remembered only if the browser says you just
  pressed a key or clicked, so a script that submits a form or clicks its own
  button leaves nothing to save. If Passwords is locked, the dropdown unlocks it
  first, and the login waits in the extension's memory for two minutes (never on
  disk) for that. Nothing is offered in a private window. Updating a password
  keeps the old one on the server for 30 days (`password_item_version`, the
  newest 20), but nothing in the extension or the dashboard shows it yet, so the
  offer does not promise it; it can be put back from the database.
- **Passkeys.** Chrome gives extensions no passkey provider, so, as other
  password managers do, a script in the page's own world replaces
  `navigator.credentials.create` and `.get` for passkey (`publicKey`) requests
  only; everything else goes to the browser untouched. The extension checks the
  site's relying party id against the page's host, asks you in the button's
  dropdown ("Save a passkey for example.com as ada?", "Sign in to example.com
  with your passkey?"; in a small window instead when the page that asked is not
  the tab you are looking at), makes or uses an ES256 passkey stored in the login for that
  site and user name, and answers the page. *Use the browser instead* hands the
  request back to Chrome, and is remembered for that site until the browser
  closes. The question also names the page that asked when its host is not the
  relying party id. A passkey says you were *verified* (the user verified flag)
  only if you typed the passphrase in that question or within the last minute;
  otherwise it says only that you were present. A site that insists on
  verification (`userVerification: "required"`) gets no answer until you have
  typed the passphrase, and closing the question answers it with
  `NotAllowedError`. When the page that asked is not the tab you are looking at,
  the question opens in a small window that does not take the keyboard focus.
  The question ends, and nothing is saved, when the page withdraws its request, when the tab goes to
  another address or when it is closed. If Passwords is locked, the question asks
  you to unlock it, but only for a site the extension knows has a passkey here
  (see below); a site with no passkey here is left to the browser with no
  question. The browser's own list of passkeys under a field
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
`chrome.storage.local`, which holds the sign-in token and the server address
beside it, to content scripts each time it starts, so only the extension's own
pages and worker can read it. The address is never in `chrome.storage.sync`:
whatever could write there from another browser would otherwise choose the
server the token is sent to, so each browser is given the address once, as it
is signed in once. The side panel, which is a page of the
extension and not a script in a website, is the only one the worker gives the
token to, and posts it to the dashboard's frame with the server's address as the
target. Nothing decrypted is ever stored: the sealed items are fetched from your server, opened in the
extension's service worker for the one thing being done, and let go; a list in
the dropdown is shown from that and not kept. The only things kept in
`chrome.storage.local` are public: the public keys of people who shared items
with you, so a key that changes later is noticed, whether Passwords is set up,
and which sites have a passkey here, as salted SHA-256 hashes of the relying
party ids (the salt is stored beside them, so this keeps the list from being
read at a glance and is not a secret), so that a locked Passwords can leave a
site with no passkey to the browser without a question. A shared login whose
owner's key changed is still filled; the dropdown notes the change and can
show both safety codes. The extension talks to your TeaNode server and to nothing else.

The extension asks for the permissions this takes beyond the agent tab: content
scripts on every site (to see sign-in fields, to draw the frame and to carry
passkey requests), the context menu and a keyboard shortcut, alarms (to lock
and to clear the clipboard), and an offscreen document with clipboard access
(a service worker has no clipboard, and clearing a copied password needs to read
it back to see whether it is still there). The extension's text is in English
only, like its options page and side panel.
