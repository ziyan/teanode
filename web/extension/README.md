# The agent tab extension

A small Chrome extension that attaches the tab you are looking at to your
TeaNode agent, over the dashboard's websocket, so the agent can read and
act in that tab with your session while you watch. It is for what the
headless browser cannot do: a portal only you can log into, your bank's
message centre, a form to fill from an email.

Load it unpacked from this directory (`chrome://extensions`, developer
mode, *Load unpacked*), open its options and enter your dashboard's
address, sign in to the dashboard in the same browser, then click the
extension's button on a tab. The badge says *on* while the tab is
attached; click again to detach. An operator can switch attaching off for
the whole server with `agent.browser.attachTabs`.

What the extension refuses on its own, whatever the agent asks: typing
into a password or payment field; submitting a form that pays or changes
credentials without your word; fetching from any site but the attached
page's own; reading storage of a site you did not attach.
