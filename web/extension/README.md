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

The extension's button opens a panel on the page: the dashboard's own
drawer, framed from the server and signed in by the extension — the same
conversation you would have on the dashboard. On the dashboard's own
pages the button opens the built-in drawer instead. The panel's bar has
*Attach this tab*; while a tab is attached the badge says *on*, and the
agent can read and act in it. It may also open tabs beside yours, which
sit in a "TeaNode" tab group; it can list them, switch between them and
close the ones it opened, never yours. An operator can switch attaching
off for the whole server with `agent.browser.attachTabs`.

What the extension refuses on its own, whatever the agent asks: typing
into a password or payment field; submitting a form that pays or changes
credentials without your word; fetching from any site but the one the
current tab is on; reading storage of any site but that one. The agent
may open tabs at addresses of its choosing and read them with your
session, the way you could; that is what attaching is for, and it
happens in front of you, in the TeaNode group beside your tab.
