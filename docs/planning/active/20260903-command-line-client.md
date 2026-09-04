# A command line client for the whole API, signed in from a browser

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It follows the ExecPlan conventions described in
`~/.claude/PLAN.md` (not checked into this repository).

## Purpose / Big Picture

Today `teanode` is one binary that is both the mail server and the tool that
administers it. The tool covers a handful of tasks with a command of their own
(`user`, `token`, `credential`, `dkim`) and reaches everything else through
`teanode api call <Operation> name=value`, which works but makes the operator
learn the GraphQL schema to add a domain. Signing in from a laptop means
running `teanode token create` on the server and pasting the secret into an
environment variable.

After this change there are two programs:

- `teanode-server` runs the mail server and does the things only the server's
  own host can do: create the database schema, import or export the stored
  configuration, generate a development certificate, and recover accounts
  when nobody can log in.
- `teanode` is the client. It administers a server over its API, from
  anywhere. `teanode auth login --url https://mail.example.com` opens a
  browser, the operator signs into the dashboard and presses one button, and
  the client receives a token over a loopback connection and saves it as a
  profile. From then on `teanode domain list`, `teanode mail list`,
  `teanode template render example.com welcome --variable name=Ann` and the
  rest work without any environment set up. Every resource the API exposes
  has a command group: `domain`, `alias`, `credential`, `user`, `token`,
  `session`, `passkey`, `settings`, `server`, `mail`, `delivery`, `report`,
  `template`, `layout`, plus `dkim` and the raw `api` escape hatch.

A person can see it working by building both binaries with `make build`,
running `build/teanode-server run` against the development database, running
`build/teanode auth login --url http://127.0.0.1:10081` and authorising in the
browser, and then running `build/teanode domain list`, which prints a table of
the configured domains.

## Progress

- [x] (2026-09-03 12:00Z) Read the existing command line code, the API
      resolvers, the dashboard and the deployment scripts. Wrote this plan.
- [x] (2026-09-03 20:10Z) Milestone 1: two binaries. `cmd/teanode/main.go` and
      `cmd/teanode-server/main.go`; subcommands move to `internal/cmd`
      (client) and `internal/cmd/server` (server); Makefile, Dockerfile,
      release script, CI, dev scripts and docs updated; `make build` produces
      `build/teanode` and `build/teanode-server`.
- [ ] Milestone 2: profiles and `teanode auth`. `~/.config/teanode/profiles.json`,
      `auth login` with the browser loopback flow and `--token`, `logout`,
      `status`, `list`, `switch`, `remove`; the dashboard's `/cli` page; the
      content security policy widened for that page only; tests.
- [ ] Milestone 3: configuration commands. `domain`, `alias`, `credential`
      (with `update`), `user` (with `update`), `server`, `settings`.
- [ ] Milestone 4: data commands. `mail` (list, get, content, download, opens,
      count, send), `delivery`, `report`, `session`, `passkey`.
- [ ] Milestone 5: content commands. `template`, `layout`.
- [ ] Milestone 6: the deployment test drives the new commands; documentation
      (`docs/reference/command-line.md`, project structure, local
      development, README, getting started, changelog); a decision record for
      the split; this plan moves to `docs/planning/done/`.

## Surprises & Discoveries

- Observation: the client has to read the database even after the split.
  Evidence: `internal/client/local.go` mints a console token from the server
  secret, which lives in the stored configuration, and reads the listen
  address from the same place. Without that path `docker compose exec teanode
  teanode user list` would need a token issued first. The decision record
  `docs/decisions/20260818-the-cli-goes-through-the-api.md` calls this
  zero-setup console path deliberate, so the client keeps `internal/configdb`
  as a dependency and stays a "client" in what it does — never writing the
  database — rather than in what it links.

## Decision Log

- Decision: two binaries, `teanode` (client) and `teanode-server` (server),
  built from `cmd/teanode` and `cmd/teanode-server`, with the subcommand
  implementations in `internal/cmd` (client, package `cmd`) and
  `internal/cmd/server` (server, package `server`).
  Rationale: the owner asked for the rename. Two packages rather than one so
  that the client binary does not link the mail path, the AWS SDK, the
  embedded dashboard and everything else `run` needs; Go links a whole
  package, so one package with both sets would give the client the server's
  size. The subcommands leave the root `cmd/` directory because the
  conventional Go layout puts one `main` per subdirectory there, and mixing a
  library package with two `main` subdirectories under it reads badly.
  Date/Author: 2026-09-03, the implementing session.

- Decision: the dividing line is who writes the database. The server binary
  owns every command that writes the stored configuration or the schema
  directly: `run`, `config env|init|validate|show|import|export`, `tls
  self-signed`, `password`, and `user list|add|password|remove|reset` (the
  offline account recovery, formerly `teanode user --offline`). The client
  owns every command that goes through the API. The client still reads the
  database for the console token and for the read-only fallback that lets
  `teanode dkim show` work before the server has started.
  Rationale: "the running server is the only writer" is the existing
  invariant; putting the two exceptions into the server's own binary makes
  the line legible. The client cannot corrupt anything.
  Date/Author: 2026-09-03.

- Decision: profiles live in `~/.config/teanode/profiles.json` (mode 0600),
  one per server, with an active one. `--profile NAME` and `TEANODE_PROFILE`
  choose another for one command. `--url` plus `--token` (or `TEANODE_URL`
  and `TEANODE_TOKEN`) bypass profiles for scripts. The old
  `~/.config/teanode/token` file is no longer read.
  Rationale: this is the shape of `gh`, `kubectl` and every other tool people
  already know. The token file
  existed for three days in one release; a profile carries everything it did
  and the server's URL besides.
  Date/Author: 2026-09-03.

- Decision: when nothing selects a server — no `--url`, no `--profile`, no
  active profile — the client falls back to the console path: it reads the
  server's environment, mints a token from the stored secret, and connects
  over loopback. A profile named `local` is reserved and forces that path,
  for a console that also has profiles for other servers.
  Rationale: keeps `docker compose exec teanode teanode user list` working
  with nothing set up, which the getting-started guide relies on.
  Date/Author: 2026-09-03.

- Decision: the browser flow is a loopback handshake. The client listens on `127.0.0.1:<random>`, opens
  `<server>/cli?port=…&state=…&name=…&lifetime=…`, and the dashboard page —
  once the reader is signed in and presses Authorize — calls the existing
  `CreateToken` mutation and posts the secret to
  `http://127.0.0.1:<port>/callback` with the state nonce. The client checks
  the nonce, confirms the token with `GetCurrentUser`, and saves the profile.
  If the browser cannot reach the loopback the page shows the full
  `teanode auth login --url … --token …` command to paste.
  Rationale: no token crosses the clipboard or the shell history in the
  common case; the server needs no new API, only a page and a one-route
  change to the content security policy (`connect-src` gains
  `http://127.0.0.1:*` and `http://localhost:*` for `/cli` only).
  Date/Author: 2026-09-03.

- Decision: command verbs are `list`, `get`, `create`, `update`, `delete`,
  matching the API's `List…`, `Get…`, `Create…`, `Update…`, `Delete…`.
  `add` and `remove` remain as aliases where a released command used them
  (`user add`, `credential add`, `credential remove`), and `revoke` stays the
  verb for tokens and sessions because that is what the API calls it.
  Rationale: `CONTRIBUTING.md` says the same thing is named the same way
  everywhere; the API is the thing being named. Aliases keep the release
  notes and scripts written against 0.1 working.
  Date/Author: 2026-09-03.

- Decision: `settings set <section> key=value …` is generic. The keys and
  their types come from the server's own schema (the `…ParametersInput`
  object for that section), so a setting added to the API is settable the
  day it appears, with values coerced the way `teanode api call` already
  coerces arguments.
  Rationale: eight sections with five to eight fields each would be forty
  flags to write and keep in step; the introspection code already exists.
  Date/Author: 2026-09-03.

## Outcomes & Retrospective

To be written as milestones land.

## Context and Orientation

TeaNode is a mail server written in Go with a React dashboard compiled into
the binary. Its management API is one GraphQL endpoint, `POST /api/v1/graphql`,
whose schema is generated by reflection over Go interfaces in
`internal/api/v1api/apigraph/`. Each file there declares a `…Query` and a
`…Mutation` interface (for example `DomainQuery` in `domain.go`), and the
methods on those interfaces are the operations: `ListDomains`, `CreateDomain`,
and so on. Each method's argument struct (`CreateDomainArguments`) names the
GraphQL arguments through its `json` tags. Reading the interfaces in that
directory is the complete list of what the API can do.

Every operation except `GetSession`, `Login`, `CreateFirstAccount` and the
passkey sign-in checks that the caller is an operator: a request carrying a
session cookie from the dashboard, or an `Authorization: Bearer` header with
either a token from the `CreateToken` mutation (prefix `tnt_`) or a "local"
token (prefix `tnl_`) minted from the server secret by a process that can read
the stored configuration. That second kind is how the command line tool works
on the server's own console with nothing set up: `internal/client/local.go`
reads the configuration from the database the environment points at
(`TEANODE_DATABASE_URL`), mints a five minute token, and connects to
`127.0.0.1` on the configured HTTP port.

`internal/client/` is the Go side of that API for the command line tool:
`client.go` posts queries, `operations.go` holds hand written queries for the
handful of commands that exist, and `introspect.go` reads the schema from a
running server so that `teanode api call` can build a query for any operation.

The command line tool itself is `main.go` at the repository root plus one file
per subcommand in `cmd/` (package `cmd`), built on `github.com/urfave/cli/v3`.
`cmd/client.go` has `openClient`, which decides how to reach a server;
`cmd/local.go` opens the database the environment names; `cmd/api.go` has the
generic `api` command and shared helpers `printJSON` and `JSONFlag`.

The dashboard is `web/src/`, React with no router library beyond
`react-router-dom`; `app.tsx` shows a login form until `GetSession` says the
reader is signed in, then renders the routes. Text is translated through
`web/src/i18n/`: every key is added to `en.ts` and must also be added to
`ja.ts` and `zh.ts`, or the TypeScript build fails; `web/scripts/check-catalogs.mjs`
(run by `make check-catalogs`, part of `make lint-ci`) checks placeholders.
`internal/web/middlewares.go` sets a content security policy on every page;
`connect-src 'self'` is what would stop the `/cli` page reaching the client's
loopback listener.

The build: `make build` produces `build/teanode`; `make web` builds the
dashboard into `internal/frontend/static/`, which the Go build embeds.
`deploy/Dockerfile` builds both and ships one static binary.
`.github/scripts/release.sh` cross-compiles the release binaries and
`.github/workflows/release.yml` publishes them. `scripts/dev-config.bash`
and `scripts/test-deployment.bash` run `build/teanode`. Tests run with
`make test` (starts PostgreSQL in Docker) or `go test -mod=vendor ./...` per
package. Lint is `make lint-ci`.

The shape of the client is the familiar one: profiles in a JSON file, a
browser loopback handshake for sign-in, one command group per resource with
`list|get|create|update|delete` verbs, tables by default and `--json` on
request.

Terms used below: a *profile* is a saved pairing of a server URL and a token;
the *console path* is authenticating with a local token minted from the
server secret; a *typed command* is one with flags of its own, as opposed to
`teanode api call`.

## Plan of Work

### Milestone 1: two binaries

Move the subcommand package. `git mv cmd/*.go internal/cmd/` keeps the
package name `cmd` and every file. Then create `internal/cmd/server/` and move
into it what the server owns: `run.go`, `config.go`, `import.go`, `tls.go`,
`password.go`, and a new `user.go` that is the `--offline` half of the old
`cmd/user.go` with the flag removed (the client's `user.go` loses the offline
branches). These files change package to `server`, and what they used from
package `cmd` is exported from `internal/cmd`: `OpenLocalStore`,
`OpenBootstrapDatabase`, `LoadLocalConfiguration`, `UpdateLocalConfiguration`,
`ReadPassword`, `PrintJSON`, `JSONFlag`, `SetupLogging`, `SetLogLevel`, and
the `NewVersionCommand(program string)` constructor, which prints the name it
is given. `internal/cmd/local.go` keeps the database-opening code because the
client needs it for the console path.

Create `cmd/teanode/main.go` (the client: global flags `--url`, `--token`,
`--profile`, `--insecure`, `--log-level`; commands `auth`, `domain`, …,
`api`, `version`) and `cmd/teanode-server/main.go` (the server: `run`,
`config`, `tls`, `user`, `password`, `version`; global flag `--log-level`).
Delete the root `main.go`.

Update the build. In `Makefile`: `GOPACKAGES` becomes `./cmd/... ./internal/...`;
`build` produces `$(BUILD_DIR)/teanode` from `./cmd/teanode` and
`$(BUILD_DIR)/teanode-server` from `./cmd/teanode-server`; `SERVER_BINARY`
replaces `BINARY` where the server is meant; `dev-backend` runs
`teanode-server run`; the help text says both. `deploy/Dockerfile` builds
both, copies both to `/usr/local/bin`, and its entrypoint is
`teanode-server` with `CMD ["run"]`. `.github/scripts/release.sh` builds
`teanode-server-<os>-<arch>` for linux/amd64 and linux/arm64 and
`teanode-<os>-<arch>` for those plus darwin/amd64 and darwin/arm64 (a client
belongs on a laptop; the server does not). `.github/workflows/release.yml`
lists the new files; `ci.yml` vets `./cmd/... ./internal/...` and its
container check stays `docker run --rm teanode:ci version`, which now runs
`teanode-server version`. `scripts/dev-config.bash` and
`scripts/test-deployment.bash` name `build/teanode-server` where they start
the server or use `config init`, and `build/teanode` where they use the
client.

Acceptance: `make build` writes both binaries; `build/teanode-server version`
prints `teanode-server 0.x`; `build/teanode version` prints `teanode 0.x`;
`build/teanode --help` lists no `run`; `make test` and `make lint-ci` pass.

### Milestone 2: profiles and `teanode auth`

`internal/cmd/profile.go`: the `Profile` struct (`Name`, `URL`, `Token`,
`TokenID`, `Username`, `Insecure`), the `Profiles` file (`Active` plus a map
by name), `LoadProfiles`, `Save` (0700 directory, 0600 file, written through
`internal/util/atomicfile`), `Active(override)`, and `ProfilesPath`, which
honours `XDG_CONFIG_HOME` and defaults to `~/.config/teanode/profiles.json`.

`internal/cmd/client.go`: `openClient` resolves in this order: `--url` (with
`--token`, or the token of a saved profile whose URL matches, or an error
saying to run `auth login`); `--profile`; the active profile; the console
path. `--profile local` is the console path by name. `--insecure` sets
`client.Options.Insecure`, which `internal/client/client.go` turns into a
transport that skips certificate verification.

`internal/cmd/auth.go`: the `auth` group. `login` takes `--url`, `--name`
(default: the host name in the URL), `--token` (a value, or `-` to read one
without echo), `--no-browser`, `--insecure`, `--lifetime`. Without `--token`
it runs `browserLogin` from `internal/cmd/loopback.go`: listen on
`127.0.0.1:0`, make a 16 byte hex nonce, serve `/callback` accepting `POST`
JSON `{state, token, tokenId, username}` with the CORS and
`Access-Control-Allow-Private-Network` headers a browser needs to post from a
public origin to a loopback address, open the browser with `xdg-open`,
`open` or `rundll32` by platform, print the URL either way, and wait up to
five minutes. Either way the token is then checked with `GetCurrentUser`
and the profile saved and made active. `logout [name]` revokes the token
with `DeleteToken` (when the profile knows its id) and removes the profile;
`--keep-token` skips the revocation. `status` prints the profile that would
be used and whether the server answers. `list` is a table with the active
one starred. `switch <name>` changes the active profile. `remove <name>`
forgets a profile without revoking.

The dashboard: `web/src/pages/cli.tsx`, routed at `/cli` inside the signed-in
shell (the shell already shows the login form first and keeps the URL, so
the flow resumes on this page). It reads `port`, `state`, `name` and
`lifetime` from the query; shows who is signed in and the token name it will
create (`teanode CLI (<name>)`); on Authorize calls `CreateToken`, posts to
the loopback, and shows "done, close this tab" or, if the post fails, the
full command to paste. Strings go in all three catalogues.

`internal/web/middlewares.go`: `MakeSecurityHeadersMiddleware` gains a second
policy whose `connect-src` is `'self' http://127.0.0.1:* http://localhost:*`,
used when the request path is exactly `/cli`. A test in
`internal/web/web_test.go` (or a new file) checks both.

Tests: `internal/cmd/profile_test.go` (round trip, permissions, active
resolution, XDG); `internal/cmd/loopback_test.go` (a posted result with the
right nonce is delivered, a wrong nonce is refused, `OPTIONS` answers the
preflight); `internal/cmd/client_test.go` for the resolution order using a
temporary profiles file.

Acceptance: against a running development server, `build/teanode auth login
--url http://127.0.0.1:10081` opens the browser, and after Authorize prints
`Signed in to http://127.0.0.1:10081 as <user>; saved profile "127.0.0.1"`.
`build/teanode auth list` shows it starred. `build/teanode user list` works
from a shell with no `TEANODE_*` variables. `build/teanode auth login --url
… --token -` accepts a pasted token. `build/teanode auth logout` revokes it
and `build/teanode user list` then says to sign in.

### Milestone 3: configuration commands

`internal/client/` grows one file per resource with hand written queries and
the structs they decode into: `domain.go` (list, get, create, update, delete,
check, server addresses, outgoing identity), `alias.go`, `credential.go`
(adds update), `user.go` (adds update), `server.go` (status, restart),
`settings.go` (get, update with a raw `map[string]any`). `operations.go`
shrinks to what is left.

`internal/cmd/` gains `domain.go`, `alias.go`, `server.go`, `settings.go`,
and `credential.go` and `user.go` grow. Tables come from a small helper in
`internal/cmd/output.go`: `printTable(headers, rows)` over `text/tabwriter`,
and `confirm(command, prompt)` honouring `--force`. Domains are named by
their domain name on the command line and resolved to identifiers with the
existing `requireDomain`. Aliases, credentials and the rest are named by
identifier, which `list` prints in its first column.

`settings set <section> key=value …` uses `client.Introspect` to find the
input object for the section (`S3ParametersInput` for `s3`, and so on),
coerces each value by the field's declared type (reusing the coercion in
`api.go`, generalised from arguments to input fields), and sends
`UpdateSettings` with that one section. `settings show` prints every section
as `section.key: value` lines, or JSON.

Acceptance: `teanode domain create example.net --comment "second"` prints
the DNS records to publish; `teanode domain list` shows both domains;
`teanode alias create example.net --pattern '^hello$' --kind email --email
me@example.org` then `teanode alias match example.net hello` names it;
`teanode settings set antispam enabled=true host=127.0.0.1 port=783`
followed by `teanode settings show antispam` shows the change and
`teanode server status` lists `antispam` under pending restart.

### Milestone 4: data commands

`internal/client/mail.go`, `delivery.go`, `report.go`, `session.go`,
`passkey.go`; `internal/cmd/mail.go`, `delivery.go`, `report.go`,
`session.go`, `passkey.go`.

`mail list [--domain D] [--first N] [--status S] [--kind K] [--from F]
[--subject text] [--sender S]` builds an aggregation pipeline of equality
matches (`contains` for subject) and prints a table; `mail get <id>` prints
the envelope, the authentication results and the deliveries; `mail content
<id> [--html] [--headers]` prints the decoded body; `mail download <id>
[--output path]` fetches `/api/v1/mail/<id>/raw` with the bearer token
through a new `client.Download`; `mail opens <id>`; `mail count --by field
[--domain]`; `mail send <domain> --from a@d --to b --subject s (--text file
| --html file | --template name [--variable k=v …]) [--attach file …]`,
where `-` reads a file from standard input.

`delivery list (--domain D | --mail M) [--first N]`, `delivery pending
[--domain D]`, `delivery get <id>`, `delivery retry <id>`. `report list
[--domain D] [--first N]`, `report get <id>`. `session list [--user U]
[--revoked]`, `session revoke <id>`, `session revoke-all`. `passkey list`,
`passkey rename <id> <name>`, `passkey delete <id>`; registering one needs a
browser, which the help says.

Acceptance: after `swaks` delivers a message to the development server,
`teanode mail list` shows it, `teanode mail content <id>` prints its text,
`teanode mail download <id>` writes `<id>.eml`, and `teanode delivery
pending` shows the delivery waiting when sending is disabled.

### Milestone 5: content commands

`internal/client/template.go`, `layout.go`; `internal/cmd/template.go`,
`layout.go`. Templates are named `<domain> <name>` on the command line, which
is how `GetTemplate` can look them up; layouts by identifier. `create` and
`update` take `--comment`, `--locale`, `--subject`, `--html file`, `--text
file`, `--layout id`, and `--translation locale=file.json` for each extra
locale, or `--from-file parameters.json` carrying the whole parameters object
(which is what `get --json` prints, so export and import round-trip).
`update` loads the stored template first and changes only what was given,
because `ModifyTemplate` replaces every field. `render <domain> <name>
[--locale] [--variable k=v …] [--html|--text]` prints the rendered subject
and text by default.

Acceptance: `teanode template create example.com welcome --subject "Hello
{{ name }}" --text body.txt` then `teanode template render example.com
welcome --variable name=Ann` prints `Hello Ann`.

### Milestone 6: deployment test, documentation, decision record

`scripts/test-deployment.bash` `check_cli` uses the typed commands beside the
`api call` checks: `domain list`, `alias match`, `auth login --token` into a
temporary `XDG_CONFIG_HOME`, and `auth logout`. `docs/reference/command-line.md`
is rewritten around the two programs and profiles; `docs/reference/project-structure.md`,
`docs/reference/local-development.md`, `docs/getting-started.md`,
`docs/configuration.md` and `README.md` say `teanode-server` where they mean
the server. `docs/decisions/20260903-two-binaries.md` records the split.
`CHANGELOG.md` gets the entries under Unreleased: Added (the client, the
browser sign-in, every command group), Changed (the server binary is
`teanode-server`; the token file is replaced by profiles).

## Concrete Steps

All commands run from the repository root of the worktree.

    make build                       # both binaries
    make test                        # unit tests, PostgreSQL in Docker
    make lint-ci                     # gofmt, secrets, catalogues, golangci-lint
    make web                         # after changing the dashboard

For a live check:

    make dev-up
    set -a; . ./dev/.env; set +a
    build/teanode-server config init
    build/teanode-server tls self-signed
    build/teanode-server run &
    build/teanode user add operator          # console path
    build/teanode auth login --url http://127.0.0.1:10081
    build/teanode domain list

## Validation and Acceptance

Each milestone above states what to run and what to observe. Overall
acceptance: `make lint-ci` and `make test` pass; `scripts/test-deployment.bash`
passes with the new client checks; the live check above produces a table of
domains after a browser authorisation with no environment variables set in
the client's shell.

## Idempotence and Recovery

Every step is a source change under version control; `git checkout -- .`
restores any file. `auth login` overwrites a profile of the same name and
issues a new token each time, leaving the old token valid until it expires
or is revoked with `teanode token revoke`. Profiles are written atomically,
so an interrupted save leaves the previous file.

## Artifacts and Notes

Kept short here; the code and its tests are the artifact. Notable transcripts
are added as milestones land.

## Interfaces and Dependencies

No new module dependencies. `golang.org/x/term` (already vendored) reads the
pasted token without echo.

In `internal/cmd/profile.go`:

    type Profile struct {
        Name     string `json:"name"`
        URL      string `json:"url"`
        Token    string `json:"token,omitempty"`
        TokenID  string `json:"tokenId,omitempty"`
        Username string `json:"username,omitempty"`
        Insecure bool   `json:"insecure,omitempty"`
    }
    type Profiles struct {
        Active   string              `json:"active"`
        Profiles map[string]*Profile `json:"profiles"`
    }
    func LoadProfiles() (*Profiles, error)
    func (self *Profiles) Save() error
    func (self *Profiles) Find(name string) *Profile
    func (self *Profiles) Set(profile *Profile)
    func (self *Profiles) Remove(name string)

In `internal/client/client.go`, `Options` gains `Insecure bool`, and
`client.go` gains `func (self *Client) Download(ctx, path string) (*http.Response, error)`
for the raw message endpoint.

In `internal/web/middlewares.go`, `MakeSecurityHeadersMiddleware` keeps its
signature; the path check is internal.

Every new command group is a `New<Group>Command() *cli.Command` in
`internal/cmd`, listed in `cmd/teanode/main.go` in this order: `auth`,
`domain`, `alias`, `credential`, `dkim`, `user`, `token`, `session`,
`passkey`, `settings`, `server`, `mail`, `delivery`, `report`, `template`,
`layout`, `api`, `version`.
