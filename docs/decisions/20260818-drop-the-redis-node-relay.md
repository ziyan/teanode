# The Redis node relay is deleted

- Status: accepted
- Date: 2026-08-18
- Deciders: Ziyan Zhou

## Context

`internal/node` proxied websocket streams between backend instances over Redis
pub/sub, with an accompanying model, database table, GraphQL resolvers and two
HTTP views. It existed to let several backends of a hosted service reach each
other. Redis was, with PostgreSQL, one of only two services the binary
required.

## Decision

Delete `internal/node`, `internal/util/redisclient`, `internal/util/mux` (a
yamux multiplexer used only by the relay), the node API views and resolvers,
the node model, and the node database table.

## Consequences

- Redis is no longer a dependency. A deployment needs PostgreSQL and nothing
  else.
- Running several instances that can address each other is no longer possible.
  Nothing in a single-server mail forwarder needed it; if it is ever wanted
  again it should be reintroduced deliberately rather than carried along.
