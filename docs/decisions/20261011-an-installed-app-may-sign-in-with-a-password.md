# An app installed on a device may sign in to the whole account through the consent page

- Status: accepted
- Date: 2026-10-11
- Deciders: the-owner

## Context

The TeaNode phone app signed in only with a connect code: made where the
person is signed in, shown as a QR code, good for ten minutes and one phone.
The consent page, which any program can send a person to, granted only the
agent tools, because a link to it is where phishing aims.

That left no way in without a second screen, and no credential that lasts:
App Review needs an account it can sign in to days later, with a username and
a password, and a person who has only their phone has nothing to scan.

## Decision

The consent page grants the whole account to one kind of program: an app
installed on a device, every one of whose registered return addresses is its
own private-use scheme (`com.example.app:/oauth`), that asks for the API by
naming its path as the resource. It gets what a connect code would give it.
The page says so in other words than for the agent tools: the whole account,
and only if the person just opened the app on this device to sign in.

Every other program, and the same app when it names no resource or another
one, is still given the agent tools alone.

## Consequences

A link to the consent page can now ask for the whole account. It cannot
deliver it anywhere but the device it is opened on: the approval returns to a
private-use scheme, which a browser hands to the app on that device and to
nothing on the network, and collecting it takes the PKCE verifier the program
that started the request holds. Somebody who sends the link from elsewhere
holds the verifier but never sees the approval. An app on the person's own
device that registered the same scheme could collect it; such an app could
already ask for a connect code the same way.

A desktop program registered with a scheme of its own keeps getting the agent
tools: it has to name the API path to get more, and the page tells the person
what that means.

The connect code stays the way to connect a phone from the dashboard; this is
the way for a phone alone, and for review.
