# The operator holds the provider keys, the person holds the finance source

- Status: accepted
- Date: 2026-09-29
- Deciders: the-owner

## Context

Reading someone's accounts at a bank, card issuer, brokerage or lender
takes a provider that signs in to the institution on their behalf. The two
that fit a self-hosted server are built differently. Plaid issues a client
id and secret to a developer who has agreed to its terms, and every call
carries them. SimpleFIN has no developer account: the person pays its
bridge, makes a setup token there, and hands the token to the application,
which exchanges it once for a URL that carries its own credentials.

A key built into the binary was not an option. It would be one key for
every install, it would break Plaid's terms, and whoever extracted it could
use it anywhere.

## Decision

Both providers are supported behind one interface, and the operator decides
which are offered (`finance.offeredProviders`).

The operator holds the Plaid client id and secret in the agent settings.
The secret is a field tagged `secret:"true"`, sealed with the server secret
like the other provider keys, and never returned by any read.

The person holds every finance source: their link to one login at one
institution. They link it themselves, from the dashboard or the command
line. A finance source is one of their agent's sources, of the kind
`finance`, so it is granted, scheduled, synced, switched off and deleted
the way every source is. Its credential is a secret of that source, sealed
like every other source secret. Deleting the finance source, or the agent,
removes it at the provider and deletes everything it brought in. The
operator sees neither the credential nor the transactions.

## Consequences

An operator who offers Plaid pays for it, and is bound by their plan's
limits for everyone on the server. A free Plaid Trial plan allows ten
finance sources for the life of the account, and deleting one does not give
its slot back, so it suits a household rather than a larger install.
SimpleFIN costs the operator nothing and each person pays their own way.

Deleting a person's agent has to call the provider for each Plaid finance
source before the rows go, or the operator keeps paying for finance
sources nobody can reach.

The Plaid secret and the credentials are opened with a key derived from the
server secret. When the operator keeps the server secret in a file, as
`20260929-the-server-secret-can-be-kept-out-of-the-database.md` allows, a
dump of the database opens none of them; while it is still in the
database, a dump opens all of them, as it does every other sealed secret.

The operator can still turn a provider off. Finance sources through it stop
syncing and say why, and they keep their data until the person deletes
them.
