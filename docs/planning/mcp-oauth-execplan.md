# The MCP endpoint is an OAuth protected resource

## Why this matters

Today a program that wants to use somebody's TeaNode agent tools has to be
handed an API token by hand. The person opens a terminal, runs `teanode token
create`, copies a secret string, and pastes it into the other program's
configuration. Every coding agent, editor and harness that speaks the Model
Context Protocol already knows how to do this differently: it asks the server
who authorizes it, registers itself, sends the person to a consent page, and
comes back holding a token nobody typed.

TeaNode answers that question with a web page today, so the conversation ends
before it starts. A harness pointed at the endpoint gets `401` with no hint of
where to authorize, asks for the two standard metadata documents, receives the
dashboard's HTML because an unmatched path falls through to the single page
application, fails to parse it, and reports that the server needs an
interactive login. That report is wrong, and it is the server's fault for not
saying otherwise.

After this change, a person adds their own TeaNode to a harness by giving it
one address and nothing else. The harness discovers the authorization server,
registers itself, opens a browser, the person approves once, and the tools
appear. No token is ever copied, and the person can see and revoke what they
approved.

The term of art here is "Model Context Protocol", abbreviated MCP: a
request-and-answer format that lets a program offer a list of tools and run
one on request. TeaNode already answers it at `POST /api/v1/mcp`; the file is
`internal/api/v1api/apigraph/agent_mcp.go`, and the protocol itself, with no
HTTP in it, is `internal/mcpserve`.

## What "following the standard" means, precisely

Four published specifications are involved. None of them needs to be read to
follow this plan; everything required is written out below in plain language.

**A protected resource** is the thing being guarded, here the MCP endpoint. It
does not check passwords. It accepts a token issued by somebody it trusts, and
when it refuses a request it must say where to go to get one. The saying-where
is a response header named `WWW-Authenticate`, and the address it carries
points at a small JSON document describing the resource. That arrangement is
RFC 9728.

**An authorization server** is what issues tokens. It publishes its own JSON
document listing its endpoints: where to send somebody to approve, where to
swap an approval for a token, and where a new program can register itself.
That document is RFC 8414, and it lives at a conventional address beginning
`/.well-known/`.

**Dynamic client registration** is a program introducing itself without a human
arranging it in advance. It posts its name and its redirect address, and gets
back an identifier to use. That is RFC 7591, and it is the part that makes the
whole thing automatic rather than merely standard.

**PKCE**, pronounced "pixy", stops a stolen approval code from being useful.
The program invents a random secret, sends a hash of it when it asks for
approval, and sends the original when it swaps the code for a token. A thief
who captures the code in transit does not have the secret and gets nothing.

**Resource indicators**, RFC 8707, bind a token to the thing it was issued for,
so a token minted for one server cannot be replayed against another. The MCP
specification requires this, and it is the difference between a token and a
skeleton key.

TeaNode already implements every one of these, on the other side. The file
`internal/mcp/oauth.go` is a complete OAuth client: `Discover` fetches both
metadata documents, `Register` performs dynamic client registration, `Begin`
builds an authorization address with PKCE, and `Exchange` and `Refresh` swap
codes and renew. This plan is that file's mirror image, and reading it first is
the fastest way to understand what the shapes look like.

## The state of the tree before this work

The MCP endpoint is registered at `internal/api/v1api/apigraph/apigraph.go:109`
and serves `POST` only. Authentication happens earlier, in middleware:
`internal/web/auth_middleware.go` refuses anything under `/api/` whose caller it
cannot name, and `internal/web/session.go:225` is where a caller is named. That
function reads the `Authorization` header first and only then falls back to a
browser cookie, which is why a bearer token works and why the 401 has nothing to
do with sessions.

Tokens are `internal/models/token.go`. A token belongs to an account, acts as
that account, carries an optional expiry, and can be revoked without being
deleted so a list can still show it. The string a holder sends is two halves:
an identifier that appears in logs and a secret that is stored only as a hash.
The prefix is `tnt_`, set at `internal/web/credential.go:23`.

Unmatched paths reach `internal/web/static.go:22`, which serves the dashboard
for anything it does not recognize. This is why the two metadata addresses
currently answer with HTML and a `200`, which is worse than a `404`: a client
cannot tell a missing document from a present one.

There is a precedent for what this plan needs. `PathBimiLogo` in
`internal/api/path.go:98` is served under `/.well-known/`, outside the API
prefix and outside the session check, because the thing fetching it is a remote
mail system that has no session and never will. The OAuth metadata documents
are the same shape of problem and take the same shape of answer.

## Decisions taken before implementation begins

**The access token an approval produces is an ordinary TeaNode token.** The
alternative is a second kind of credential with its own table, its own
validation path and its own revocation story, sitting beside a token type that
already does all of that and is already audited. Reusing `models.Token` means
`authenticateBearer` needs no new branch, revocation already works, expiry
already works, and the audit log already names the account. The OAuth layer
becomes a way of *obtaining* a token rather than a new thing to *be* one.

Two columns are added to support it: the client that holds the token, so a
person can see "the editor on my laptop" rather than an opaque row, and the
resource it was issued for, so a token minted for the MCP endpoint cannot be
replayed elsewhere. A token with no client is exactly what it is today.

**Approval is a dashboard page, not a new login form.** The authorization
endpoint requires a signed-in person. If nobody is signed in it sends them to
the existing dashboard login and returns them afterwards. Writing a second
place that accepts a password would be a second place to get it wrong, and the
dashboard already supports passkeys and single sign-on, which a new form would
not.

**Registration is open but bounded.** Dynamic client registration accepts
anybody, because that is what makes it automatic, and a registered client can
do nothing at all until a person approves it. What registration must not become
is unbounded writes from strangers: it is rate limited, the stored fields are
length capped, and a client nobody approves within a day is swept.

**Scopes are not invented.** TeaNode's permissions live on accounts and groups,
and a token acts as its account. Inventing a parallel vocabulary of scope names
that must then be mapped onto real permissions would create two answers to
"what may this caller do" and eventually a disagreement. A single scope named
`mcp` is published, meaning "the tools this account can reach", which is the
truth about what the token grants.

## Milestone 1: the endpoint says where to authorize

At the end of this milestone, a harness pointed at the MCP endpoint stops
seeing HTML and starts seeing an authorization server, though it cannot yet
register with it.

This milestone must not be released on its own, and the first draft of this
plan was wrong to say it could be. See `Surprises & Discoveries`: the
authorization server document has to name endpoints that milestones 2 through
4 build, and naming an endpoint that answers with the dashboard's HTML leaves
a client worse off than naming nothing at all. The whole flow ships together.

Add to `internal/api/path.go` three constants beside `PathBimiLogo`, which is
the existing example of a `.well-known` address served outside the session
check:

    PathOAuthProtectedResource = "/.well-known/oauth-protected-resource"
    PathOAuthProtectedResourceMCP = "/.well-known/oauth-protected-resource/api/v1/mcp"
    PathOAuthAuthorizationServer = "/.well-known/oauth-authorization-server"

Two addresses serve the same protected-resource document because clients differ
in which they ask for, and `internal/mcp/oauth.go:262` is the proof: TeaNode's
own client builds both `origin + "/.well-known/" + document + path` and
`origin + path + "/.well-known/" + document` and tries them in turn. A server
that answers only one will work with some clients and not others, and the
failure looks like a parse error rather than a missing document.

Add all three to `PublicPaths()` in the same file. They must be reachable
without a credential: a client fetches them precisely because it does not have
one yet.

Create `internal/api/v1api/apioauth/metadata.go` serving the two documents.
The protected-resource document names the resource, the authorization server,
the one scope and the fact that the token travels in a header:

    {
      "resource": "https://mail.example.com:10443/api/v1/mcp",
      "authorization_servers": ["https://mail.example.com:10443"],
      "scopes_supported": ["mcp"],
      "bearer_methods_supported": ["header"]
    }

The authorization-server document names the endpoints the later milestones
build, and may be written now because the addresses are decided now:

    {
      "issuer": "https://mail.example.com:10443",
      "authorization_endpoint": "https://mail.example.com:10443/oauth/authorize",
      "token_endpoint": "https://mail.example.com:10443/oauth/token",
      "registration_endpoint": "https://mail.example.com:10443/oauth/register",
      "revocation_endpoint": "https://mail.example.com:10443/oauth/revoke",
      "response_types_supported": ["code"],
      "grant_types_supported": ["authorization_code", "refresh_token"],
      "code_challenge_methods_supported": ["S256"],
      "token_endpoint_auth_methods_supported": ["none"],
      "scopes_supported": ["mcp"]
    }

The origin in both is not a setting to invent. It is the address the server
already knows itself by, the same one `internal/cmd/agent_mcp.go:112` uses when
it builds a redirect back to the dashboard. Read it from the same place; a
metadata document that disagrees with the address the client used is rejected
by a conforming client, and `internal/mcp/oauth.go:246` is TeaNode's own copy of
that check.

`token_endpoint_auth_methods_supported` is `["none"]` because a program running
on somebody's laptop cannot keep a secret, so it is not issued one. PKCE is what
protects it, which is why `code_challenge_methods_supported` lists `S256` and
nothing else. Offering the weaker `plain` alongside it would let a client choose
the one that does not protect anything.

Then make the refusal informative. In `internal/web/auth_middleware.go`, where a
request under `/api/` with no credential is turned away, set the header before
writing the body:

    WWW-Authenticate: Bearer resource_metadata="https://mail.example.com:10443/.well-known/oauth-protected-resource"

Only for the MCP path. The GraphQL endpoint is used by the dashboard, and a
browser that receives a `WWW-Authenticate: Bearer` header on a fetch behaves in
ways the dashboard does not want; the mail and media endpoints are fetched by
the browser too. Scoping the header to the one endpoint that is spoken to by
programs keeps a protocol-level improvement from becoming a user-visible
regression.

To verify, with the server running:

    curl -si https://mail.example.com:10443/api/v1/mcp -X POST \
        -H 'Content-Type: application/json' -d '{}' | head -5

The response must be `401` and must carry the `WWW-Authenticate` line. Then:

    curl -s https://mail.example.com:10443/.well-known/oauth-protected-resource

must return the JSON above with `content-type: application/json`, not HTML.
Before this milestone that same command returns a `200` with
`Content-Type: text/html; charset=utf-8`, which is the bug.

Tests go in `internal/api/v1api/apioauth/metadata_test.go`: that both
protected-resource addresses return the same document, that the issuer matches
the address asked, and that the documents parse as the client in
`internal/mcp/oauth.go` parses them. That last one is the valuable test, because
it checks the two halves against each other rather than against a guess.

## Milestone 2: a program can introduce itself

At the end of this milestone a harness can register and receive an identifier,
which it cannot yet use for anything.

Add migration `internal/db/migrations/0101_oauth_client.sql` with its reverse.
Follow `docs/coding/database-migrations.md`; the convention is visible in the
neighbouring files and the number follows `0100_source_generation.sql`.

The table holds an identifier, the name the program gave itself, its permitted
redirect addresses, when it registered, and when it was last approved by
anybody. No secret: clients here are public, per the decision above.

Serve `POST /oauth/register` in `internal/api/v1api/apioauth/register.go`,
accepting the RFC 7591 body and answering with the identifier. The fields that
matter are `client_name` and `redirect_uris`; everything else may be accepted
and ignored.

Validate the redirect addresses, and be strict, because this is the one place a
mistake is exploitable. A redirect address must be `https`, or `http` on a
loopback host, which is how a program on somebody's own machine receives the
answer. It may not carry a fragment. Registration answers `400` otherwise. The
loopback rule is already written once in this tree, at
`internal/mcp/oauth.go:232`; the check belongs in one place, so move it somewhere
both sides can reach rather than writing a second copy that will drift.

Rate limit registration per address. The login limiter at
`internal/web/session.go` is the existing pattern.

## Milestone 3: a person approves

At the end of this milestone a person can complete an approval in a browser and
the harness receives a code, which it cannot yet swap.

Add migration `0102_oauth_authorization.sql` for approval codes: the code's
identifier and hashed secret, the client, the account, the redirect address it
was asked for, the PKCE challenge, the resource, and an expiry. Codes are short
lived, five minutes, and single use. Deleting on use is what makes a replayed
code fail, and the delete and the token mint must be in one transaction so a
crash between them cannot leave a code that was spent but issued nothing.

Serve `GET /oauth/authorize`. It checks the client exists, that the redirect
address is one the client registered, and that a PKCE challenge is present and
`S256`. If any of those fail it renders an error page rather than redirecting,
because redirecting to an address that has not been verified is how open
redirects are built. Everything else is reported back to the client's redirect
address as the specification requires.

If nobody is signed in, send them to the dashboard login and return them here
afterwards. The dashboard already does this dance for other addresses.

The approval page itself is a dashboard page under `web/src/pages/`. It names
the program, names the account, says what is being granted in the plain terms
the decision above settled on, and offers approve and refuse. Follow
`docs/coding/frontend-design.md`, and audit it on a phone as well as a desktop:
this is a page people will see, and it is the only part of this feature that is
not invisible.

## Milestone 4: the code becomes a token

At the end of this milestone the whole flow works and tools appear in a harness.

Add migration `0103_token_client.sql` adding the two columns decided above to
the token table, both nullable so every token that exists today keeps working.

Serve `POST /oauth/token` handling two grant types. For `authorization_code`:
find the code, verify it has not expired, verify the client and redirect match
what it was issued against, verify the PKCE verifier hashes to the stored
challenge, delete the code and mint a token in one transaction. For
`refresh_token`: verify and issue a replacement, retiring the old one.

Mint through the existing token path so the new token is an ordinary token with
an account, an expiry and a revocation story. Set the two new columns. Answer
in the shape the specification requires and `internal/mcp/oauth.go:408` already
parses.

Bind the audience. A token carrying a resource is accepted only for that
resource; `internal/web/session.go` is where that check belongs, beside the
expiry and revocation checks that are already there. Without this the token is
a skeleton key and the resource indicator is decoration.

The person must be able to see and undo this. The dashboard's existing token
list is where an approved program should appear, named by its client, with the
same revoke it already offers.

## Milestone 5: proof against a real client

A specification followed from reading is a specification followed
approximately. This milestone proves it against something that was not written
alongside it.

Add TeaNode to a harness using only its address, with no token:

    claude mcp add --transport http teanode https://mail.example.com:10443/api/v1/mcp

Observe the browser open, approve, and see the tools appear. Capture the
transcript into `Surprises & Discoveries` below, because what a real client does
differently from what the documents say is the single most valuable thing this
plan can record for whoever comes next.

Then point TeaNode's own client at TeaNode, by declaring it as a connected
server with `auth: oauth` in `agent.mcp.servers`, and run
`teanode agent mcp connect`. The two halves are independent implementations of
the same four specifications, written months apart, and each one passing the
other is the strongest evidence available that the shapes are right.

Update `docs/subsystems/mcp.md`, whose "Inward" section currently states that
the endpoint is authenticated by an ordinary API token and that there is no
session. The first half stops being the whole truth here.

## Progress

- [x] Research: the four specifications, and the existing client in
      `internal/mcp/oauth.go` that already implements all of them outbound.
- [x] Confirm the defect against the running server: both metadata addresses
      answer `200` with `text/html`, and the `401` carries no
      `WWW-Authenticate` header.
- [x] Milestone 1: metadata documents and the `WWW-Authenticate` header.
      Four tests in `internal/api/v1api/apioauth/metadata_test.go` and two in
      `internal/web/auth_middleware_test.go`, all passing under `-race`.
- [ ] Milestone 2: dynamic client registration.
- [ ] Milestone 3: the approval page.
- [ ] Milestone 4: the token endpoint and audience binding.
- [ ] Milestone 5: proof against a real client, and the documentation.

## Surprises & Discoveries

The server answers `200` on the metadata addresses rather than `404`, because
an unmatched path falls through to the dashboard. A client cannot distinguish a
server that has no OAuth support from one whose document failed to parse, which
is why the report that came back said "interactive login" rather than "no
authorization server":

    $ curl -sD - https://mail.example.com:10443/.well-known/oauth-protected-resource | head -3
    HTTP/1.1 200 OK
    Cache-Control: no-store
    Content-Type: text/html; charset=utf-8

The `401` carries no `WWW-Authenticate` header at all, so even a client that
handled the HTML gracefully has nowhere to go next.

**Milestone 1 cannot be released on its own, which the plan originally
assumed it could be.** The authorization server document is required to name
its authorization, token and registration endpoints, and those are built by
milestones 2 through 4. Publishing the document before they exist advertises
three addresses that fall through to the dashboard and answer with HTML and a
`200`. A harness that reads it would get further than today and then fail
harder, having registered nothing and with no way to tell that the endpoint it
posted to was a web page. There is no honest partial document to publish
instead: a metadata document without a token endpoint is not a metadata
document. So the milestones stay on one branch and the branch merges once.

The value of keeping milestone 1 separate is unchanged, it is just a
reviewing and testing boundary rather than a shipping one.

TeaNode already contains a complete, working implementation of the client half
of all four specifications, in `internal/mcp/oauth.go`. This is a larger gift
than it first appears: it settles which well-known layouts to serve (line 262
builds both), which issuer check a conforming client applies (line 246), and
what response shapes the token endpoint must produce (line 408). The plan's
riskiest guesses are answered by code already in the tree.

## Decision Log

**The issued access token is an ordinary `models.Token`.** A second credential
type would duplicate validation, revocation and expiry, all of which exist and
are audited. The OAuth endpoints become a way to obtain a token rather than a
new kind of one, and `authenticateBearer` needs no new branch.

**Approval reuses the dashboard's sign-in.** A second password form would be a
second place to get authentication wrong, and would silently lose passkeys and
single sign-on.

**One scope, named `mcp`.** TeaNode's permissions live on accounts and groups,
and a token acts as its account. A parallel vocabulary of scope names would
create a second answer to "what may this caller do", and eventually the two
answers would disagree.

**Clients are public and hold no secret.** A program on somebody's laptop cannot
keep one. PKCE with `S256` is what protects the exchange, and `plain` is not
offered because offering it lets a client pick the option that protects nothing.

**`WWW-Authenticate` is set only on the MCP path.** Browsers react to that
header on a fetch, and the GraphQL, mail and media endpoints are fetched by the
dashboard. Scoping it to the endpoint that only programs speak to keeps this
from becoming a visible regression.

**The loopback check is moved, not copied.** `internal/mcp/oauth.go:232` already
decides whether a host is loopback. Registration needs the same rule, and two
copies of a security check drift.

**The origin comes from the request, not from a setting.** A conforming client
refuses a metadata document whose issuer disagrees with the address it asked,
and TeaNode's own client does exactly that at `internal/mcp/oauth.go:246`. A
configured address is one more thing to set correctly and is wrong on every
other address the server answers on. The scheme is read through
`api.IsSecure`, which consults `X-Forwarded-Proto` only from a trusted proxy,
because a server behind one that terminates TLS sees no TLS on the request and
would otherwise advertise `http://` for an `https://` server.

**The trusted proxy list is threaded into the authentication middleware.** It
had no access to configuration, and the alternative was reading `request.TLS`
alone, which is wrong behind a terminating proxy in exactly the case above.
`MakeSecurityHeadersMiddleware` already takes the list as a function, so the
shape was decided; this follows it.

## Outcomes & Retrospective

To be written as milestones land.
