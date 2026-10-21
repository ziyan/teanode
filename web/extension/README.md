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
the tab is on, reading only that site's storage, and refusing the few
DevTools methods that reach past this tab into the whole browser, such as
every site's cookies. One thing is kept out of the conversation rather
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
  than the saved one: a login matches a page only by exact host (`www.` is
  ignored), and never on plain `http` unless it was saved as `http`. A sign-in
  form inside a frame from another site is filled only after the frame asks you
  and you say yes. A page that tries to hide the frame, fade it or cover it
  gets nothing: the extension checks the frame is really in front of you before
  it acts on a click.
- **Saving.** After a sign-in form is sent with a password that is new, or
  changed, the next page shows a bar at the top: *Save this login to TeaNode
  Passwords?* or *Update the password for ...?*. If Passwords is locked, the
  bar opens the window to unlock first, and the login waits in the extension's
  memory for two minutes (never on disk) for that. Nothing is offered in a
  private window.
- **Passkeys.** Chrome gives extensions no passkey provider, so, as other
  password managers do, a script in the page's own world replaces
  `navigator.credentials.create` and `.get` for passkey (`publicKey`) requests
  only; everything else goes to the browser untouched. The extension checks the
  site's relying party id against the page's host, asks you in a window of its
  own ("Save a passkey for example.com as ada?", "Sign in to example.com with
  your passkey?"), makes or uses an ES256 passkey stored in the login for that
  site and user name, and answers the page. *Use the browser instead* hands the
  request back to Chrome. If Passwords is locked, the window asks you to
  unlock it. A site with no passkey here is left to the browser with no
  question when Passwords is open. The browser's own list of passkeys under a
  field (conditional mediation), requests from a frame of another origin than
  its page, and security key requests are left to the browser.
- **The agent.** The agent's `fill_login` and `fill_one_time_code` actions in an
  attached tab type a login or code into fields it names, only on a page whose
  host is the one the server named, and answer without repeating the value.

What it keeps, and where. The opened account key and key pair live only in
`chrome.storage.session`, which the browser holds in memory, never writes to
disk, clears when it closes and hides from web pages and content scripts. They
are dropped after the lock time you set on the dashboard with no use, when the
browser closes, and when you sign out on the options page. Nothing decrypted is
ever stored: the sealed items are fetched from your server, opened in the
extension's service worker for the one thing being done, and let go; a list in
the window is shown from that and not kept. The only thing kept in
`chrome.storage.local` is public: the public keys of people who shared items
with you, so a key that changes later is noticed, and whether Passwords is set
up. The extension talks to your TeaNode server and to nothing else.

The extension asks for the permissions this takes beyond the agent tab: content
scripts on every site (to see sign-in fields, to draw the frame and to carry
passkey requests), the context menu and a keyboard shortcut, alarms (to lock
and to clear the clipboard), and an offscreen document with clipboard access
(a service worker has no clipboard, and clearing a copied password needs to read
it back to see whether it is still there). The extension's text is in English
only, like its options page and panel.
