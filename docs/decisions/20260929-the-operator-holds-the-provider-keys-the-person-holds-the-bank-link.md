# The operator holds the provider keys, the person holds the bank link

- Status: accepted
- Date: 2026-09-29
- Deciders: the-owner

## Context

Reading someone's bank transactions takes a provider that signs in to banks
on their behalf. The two that fit a self-hosted server are built
differently. Plaid issues a client id and secret to a developer who has
agreed to its terms, and every call carries them. SimpleFIN has no
developer account: the person pays its bridge, makes a setup token there,
and hands the token to the application, which exchanges it once for a URL
that carries its own credentials.

A key built into the binary was not an option. It would be one key for
every install, it would break Plaid's terms, and whoever extracted it could
use it anywhere.

## Decision

Both providers are supported behind one interface, and the operator decides
which are offered (`banking.offeredProviders`).

The operator holds the Plaid client id and secret in the agent settings.
The secret is a field tagged `secret:"true"`, sealed with the server secret
like the other provider keys, and never returned by any read.

The person holds every bank connection. They link it themselves, from their
own agent page, through Plaid's window or with their own SimpleFIN token.
A connection is one of their agent's sources, of the kind `bank`, so it is
granted, scheduled, synced, switched off and deleted the way every source
is. Its credential (a Plaid access token, or a SimpleFIN access URL) is a
secret of that source, sealed like every other source secret. Deleting the
source, or the agent, removes the connection at the provider and deletes
everything it brought in. The operator sees neither the credential nor the
transactions.

## Consequences

An operator who offers Plaid pays for it, and is bound by their plan's
limits for everyone on the server. A free Plaid Trial plan allows ten
connections for the life of the account, and removing one does not give
its slot back, so it suits a household rather than a larger install.
SimpleFIN costs the operator nothing and each person pays their own way.

Removing a person's agent has to call the provider for each Plaid
connection before the rows go, or the operator keeps paying for
connections nobody can reach.

The operator's settings and the people's credentials are opened with a key
derived from the server secret, which is stored in the same database. A
full dump of the database is enough to read them. This is the same
exposure as every other sealed secret on the server, and the security
review records it.

The operator can still turn a provider off. Existing connections through it
stop syncing and say why, and they keep their data until the person
unlinks them.
