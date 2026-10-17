# Places are named from OpenStreetMap, once, and the person can always name them

- Status: proposed
- Date: 2026-10-17
- Deciders: the-owner

## Context

A list of coordinates is not a history. "Elm Library for 40 minutes" is. Something
has to say what is at a position, and every source of that has terms.

Google Places allows only its identifier to be stored and its results to be shown
on its own map. Mapbox requires its own map. Apple's server interface allows
caching for a short time only. OpenStreetMap's data (ODbL) may be stored and shown
to anybody with credit, and it can be asked for from a public server
(Nominatim) under a usage policy, or from a server the operator runs (Photon).
The server must also work with no model and no agent, and with nothing sent
anywhere at all, for an operator who wants that.

## Decision

A place is named from OpenStreetMap through a **place provider** the operator
chooses in the server settings (`places.providerKind`: `nominatim`, the default,
or `photon`; `places.providerUrl` for the address). A new place is named after its
nearest city from a table built into the server, which needs nothing from anyone,
and right after the report that made it is stored the server asks the provider
once what is at its position and keeps the answer. It is asked about a place once;
a failure leaves the place waiting for the agent's periodic job to try again. The
check-in list and the agent's search use the same provider.

The person's own name for a place always wins over the provider's, and a person
can rename, label, move or resize any place. The provider is a default, not an
authority.

All requests go through one limiter of one a second for each instance, with a
User-Agent that names the server, as the public server's policy asks; a caller
that would wait more than a few seconds is turned away.

The provider is on by default, and the person is told where they set Location up,
and the operator beside the switch: "Place names come from OpenStreetMap: your
server sends the position of each new place to the place provider to look up its
name." Setting the provider empty sends nothing anywhere.

The map is drawn from the style at `places.mapStyleUrl`, fetched through the
server for signed-in accounts that may use Location, so that the browser's content
security policy, which allows only this server because it shows mail written by
strangers, does not change.

## Consequences

The position of every new place, home included, goes to a third party unless the
operator turns the provider off or runs their own. That is the cost of readable
names with no key to manage and no service to pay for, and it is why the sentence
above is shown where it is decided.

The public server's policy is for light use. A server with many people, or several
instances (the limit is per instance), should run its own Photon.

Names are written by strangers on a public map. Everywhere they reach a model they
are cleaned and marked as data, and they are never trusted as instructions.

Moving a place by more than its radius clears what the provider said about the old
position, so it is named again for the new one.
