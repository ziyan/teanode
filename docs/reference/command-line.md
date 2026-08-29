# The command line tool

`teanode` is both the server and the tool that administers it.

Anything that changes configuration goes through the running server's API
rather than writing to the database directly, so that a change made from the
shell behaves exactly like the same change made in the dashboard — validated
the same way, with the same side effects. See
`docs/decisions/20260818-the-cli-goes-through-the-api.md` for why.

## Which server it talks to

**On the server itself**, nothing has to be set up beyond the environment the
server already has. The tool reads `TEANODE_DATABASE_URL`, finds the server
secret in the stored configuration, mints a token signed with it, and connects
over the loopback interface:

    set -a; . /opt/teanode/.env; set +a
    teanode user list

In a container deployment the environment is already there:

    docker compose exec teanode teanode user list

**From anywhere else**, give it a URL and a token:

    teanode token create laptop --lifetime 720h    # run this on the server

    export TEANODE_URL=https://mail.example.com
    export TEANODE_TOKEN=tnt_...
    teanode user list

The token can also go in `~/.config/teanode/token`, which is read when
`TEANODE_TOKEN` is not set, or be passed as `--token`.

A token belongs to an account and acts as that person. Removing the account
revokes its tokens with it.

## Commands

| Command | What it does |
| --- | --- |
| `teanode run` | run the server |
| `teanode config env` | write a starter environment file |
| `teanode config init` | migrate the database and store what the environment describes |
| `teanode config show\|validate` | inspect and check the stored configuration |
| `teanode config import\|export` | load a `teanode.yaml` into the database, or write one out |
| `teanode user list\|add\|password\|remove\|reset` | the accounts that administer this server |
| `teanode token create\|list\|revoke` | API tokens for administering it from elsewhere |
| `teanode credential add\|list\|remove` | SMTP credentials for sending mail through it |
| `teanode dkim show\|generate` | the keys that sign outgoing mail |
| `teanode tls self-signed` | a certificate for local development |
| `teanode password` | hash a password for an account entry |
| `teanode api list\|describe\|call\|graphql` | everything else |

Restarting an instance and reading its status are API operations like any
other, so they work from the command line too:

    teanode api call GetServerStatus --select "{ instance version uptimeSeconds pendingRestart supervision }"
    teanode api call RestartServer --select "{ started supervision }"

## Reaching the whole API

The commands above cover the common tasks. `teanode api` covers every
operation the server offers, including any added since, because it works off
the schema the server reports rather than a hand written list:

    teanode api list                    # every operation
    teanode api list domain             # the ones about domains
    teanode api describe CreateDomain   # arguments, input shapes, return fields

    teanode api call ListDomains
    teanode api call GetDomain domainId=01ABC...
    teanode api call CreateDomain domainParameters:='{"domain":"example.com","subdomain":"mail"}'

Arguments are `name=value`. Use `name:=<json>` for a number, boolean, list or
object. Values whose type the schema declares as a number or boolean are
converted for you, so `first=10` arrives as `10`.

The reply carries every field that can be asked for without arguments, three
levels deep. `--depth` changes that, and `--select` replaces the generated
selection entirely:

    teanode api call ListDomains --select "{ id domain }"

For anything the generated query cannot express, write the query:

    teanode api graphql '{ ListDomains { id domain records { records { type name verified } } } }'
    teanode api graphql --file query.graphql --variables '{"domainId":"01ABC..."}'

`teanode api` always prints JSON. The typed commands print a table by default
and JSON with `--json`, so either is usable from a script.

## Working without a running server

Reads fall back to the configuration file when the server is not running,
because a read cannot lose anybody's change and the file is current either way.
This is what makes the first run work: `teanode dkim show example.com` prints
the DNS record to publish before the server has ever started.

Writes do not fall back. When the server is down the command fails and says so,
rather than making a change the server would overwrite the next time anything
was saved from the dashboard.

The exception is `teanode user --offline`, for a server that will not start or
that nobody can log into:

    teanode user reset --offline        # remove every account
    teanode user add ziyan --offline    # add one back

It edits the file directly and refuses when the server is reachable, which is
the case it exists to avoid.
