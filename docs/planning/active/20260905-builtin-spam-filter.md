# A spam filter inside the server

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. This repository keeps its planning conventions in
`~/.claude/PLAN.md`; this document is maintained in accordance with it.

## Purpose / Big Picture

Today, scoring a message for spam requires a second program. `internal/mx`
opens a TCP connection to a SpamAssassin daemon — a long-running Perl process
that speaks a line protocol on port 783 — and asks it to score each message.
That daemon is not part of this repository. It arrives as a third-party
container image named in `deploy/docker-compose.yml`, and on 2026-09-05 the
image that file named was deleted from Docker Hub by its author, which stopped
`docker compose up -d` from working at all: Compose pulls every service before
starting any of them, so a missing image for the optional spam scanner took
down the mail server with it.

After this change, a new deployment scores spam with code inside
`teanode-server` and needs no second program. An operator who prefers the
external daemon can still have it, by changing one setting. The built-in
filter is the default.

You can see it working like this: start the server with no spam daemon
anywhere, send a message through it, and open that message in the dashboard.
The "Spam filter" panel shows a score, and beneath it the named checks that
produced the score — for example `SPF_FAIL`, `DNSBL_SPAMHAUS`,
`NO_REVERSE_DNS` — each with the points it contributed. Today, with no daemon
running, that panel is empty and the score is absent.

## Naming, and a word about what this is not

The built-in filter is called **the strainer**, and its code lives in
`internal/strainer`. A strainer is the thing that holds the leaves back when
you pour the tea, which is what this does to a message stream, and it fits the
name of the program. In the dashboard and in the configuration it is described
as the built-in filter.

It is deliberately **not** presented as SpamAssassin, and must not be
described as SpamAssassin, an implementation of it, or compatible with it, in
the user interface, the documentation or the configuration. It is a different
program with different behaviour.

That naming choice does not remove an attribution obligation. Milestone four
consumes rule files published by the Apache SpamAssassin project. Those files
are licensed under the Apache License 2.0, which permits use and
redistribution but requires that the licence and attribution notices travel
with them. If and when that milestone ships, a `NOTICE` file at the repository
root must name the Apache SpamAssassin project as the source of the rule data,
and `docs/configuration.md` must say where the rules come from. This is a
condition of using the data, not a matter of preference, and it is compatible
with not naming the product after it.

## Context and Orientation

Read this section even if you know the repository; it names every file the
plan touches.

**How a message is scored today.** `internal/mx/exchange.go` is the object
that accepts an incoming message. Its constructor, `mx.Open`, takes a
`spamc.Client` among its arguments and stores it on the struct as the field
`spamc`. When a message arrives, the exchange calls that client, and writes
what comes back into the message's `AuthenticationResults.SpamFilter` field.

**The client.** `internal/util/spamc/spamc.go` is 140 lines. It defines:

    type Settings struct {
        Host string
        Port uint16
    }

    type Client interface {
        Close() error
        Check(ctx context.Context, reader io.Reader) (*Result, error)
    }

    func Open(settings *Settings) (Client, error)

`Check` writes the message to the daemon and parses the reply. This narrow
interface is the seam the whole plan hangs on: anything that can score a
message and return a `*Result` can be passed to `mx.Open`, and nothing else in
the server needs to know which one it got.

**Where the result is shown.** `internal/models/mail.go` defines:

    type SpamFilterResult struct {
        Score   float64  `json:"score"`
        Symbols []string `json:"symbols,omitempty"`
        Result  string   `json:"result,omitempty"`
    }

`Symbols` is a list of short names for the checks that fired. The dashboard
already renders this. A "symbol" is simply the name of one check — an
identifier like `SPF_FAIL` — and is the vocabulary this plan uses throughout.

**The setting today.** `internal/config/config.go` has:

    type Antispam struct {
        Enabled bool   `yaml:"enabled"`
        Host    string `yaml:"host"`
        Port    uint16 `yaml:"port"`
    }

Configuration is stored in PostgreSQL, not in a file. `internal/configdb` owns
that. Every field must be documented in `docs/configuration.md` or the check
in `scripts/check-config-docs.bash` fails the build; `make lint-ci` runs it.

**Signals the server already computes and currently throws away for this
purpose.** These are inventoried in the next section, which is the heart of
this plan. `internal/models/mail.go` defines `AuthenticationResults`, which by
the time a message is scored already holds: `SPF` (did the connecting address
have permission to send for this domain), `DKIMs` (did the cryptographic
signatures verify), `DMARC` (does the domain's published policy consider this
message aligned), `ARC` (the chain of custody through forwarders), `SenderMX`
and `FromMX` (what the sending host's DNS looks like), and, when enabled, a
GeoIP location. None of this reaches the spam score today, because the score
comes from a separate process that never sees it.

**A DNS resolver already exists.** `internal/resolver` is passed into
`mx.Open`. Milestone two needs nothing more than this.

## Do not compute anything twice

This is the governing rule of the whole plan, and the reason the built-in
filter can be cheaper than the daemon it replaces rather than merely equal to
it.

By the time a message is scored, this server has already established almost
every fact a spam filter wants, at a cost it has already paid. An external
daemon cannot be told any of it: it is handed a stream of bytes on a socket
and has to work everything out again from scratch — resolving the same names,
verifying the same signatures, parsing the same message. The built-in filter
must never do that. It reads what is already there.

Here is the complete inventory, each verified in the source. Nothing in this
table may be recomputed by `internal/strainer`.

The connection facts come from `internal/util/smtpd/smtpd.go`, which resolves
them before the SMTP session begins and puts them on a `mailparse.Envelope`
(see the struct built near the end of that file):

    envelope.IP        the connecting address
    envelope.RDNS      its reverse DNS name, already resolved by checkIp();
                       empty when it has none
    envelope.Location  the GeoIP location, already looked up by locator.Locate()
    envelope.Hello     the name the sender gave in HELO or EHLO

`internal/mx/exchange_incoming.go` already copies `RDNS` and `Location` off
that envelope, so they are in hand at the point of scoring.

The authentication facts come from the checks in
`internal/mx/exchange_utils.go` and arrive as a `models.AuthenticationResults`:
`SPF`, `DKIMs`, `DMARC`, `ARC`, `SenderMX` and `FromMX`. Re-verifying DKIM
would be the most expensive mistake available here — it is public-key
cryptography per signature, over the message body — and the answer is already
computed a few microseconds away.

The message itself is already parsed. `mailparse.Split` has separated it into
`headers []string` and `body []byte` before any check runs, and both are
passed to every `check...` function. The strainer takes those directly.

There is one piece of pure waste visible today, and it is instructive.
`checkSpam` in `internal/mx/exchange_utils.go` calls `mailparse.Unsplit` to
glue the headers and body back into one buffer, borrowed from a pool, for no
reason other than that the daemon needs a byte stream on a socket. The
built-in filter must not do this. Keep `Unsplit` in the `spamd` adapter, where
it is genuinely required, and let the strainer work from the parsed form.

So milestone one performs **no lookups at all**. Every symbol it produces is
arithmetic over values already in memory. That is worth stating plainly to
whoever implements it: if you find yourself calling a resolver, verifying a
signature, or re-parsing a message in milestone one, stop, because the answer
already exists and you are about to make the server slower than it was.

Milestones two and three do introduce genuinely new work — DNS block list
queries and classifier lookups — because those facts do not exist yet. Even
there the rule holds in spirit: the block list checks take the connecting
address from the envelope rather than re-deriving it, and the classifier
tokenises the already-parsed body.

## What the research showed, and why the plan is shaped this way

Before writing this plan, the SpamAssassin 4.0.2 rule corpus was measured
directly, by reading the `.cf` rule files out of a running container. The
numbers matter because they decide what is worth reimplementing.

The corpus holds 3,032 rules that are pure regular expressions, 1,538 "meta"
rules (boolean combinations of other rules' outcomes), and 887 rules
implemented by Perl plugins rather than by a pattern.

Of the 3,032 regular expressions, 373 — about 12% — use constructs that Go's
standard `regexp` package cannot compile: lookahead, lookbehind and
backreferences. Go's engine guarantees linear-time matching and deliberately
omits these. That sounds like a blocker and is not: those 373 rules carry
about 2% of the total scoring weight. The remaining 88% compile as-is.

SpamAssassin ships four sets of scores for the same rules, chosen by whether
Bayesian classification and network lookups are available. Score set 0 is the
one for "neither", and is therefore the calibration a self-contained
implementation would use. Measured against score set 0, the weight divides as:
regular-expression rules 47%, meta rules 22%, plugin-backed rules 8%, and
rules whose definitions live outside the files measured 20%.

**The conclusion that shaped this plan:** score set 0 is SpamAssassin's
*weakest* configuration. Most of its real-world accuracy comes from Bayesian
classification trained on the recipient's own mail, and from network lookups —
exactly the two things score set 0 excludes. So porting the static rule corpus
first would be the most work for the least accuracy. The milestones below are
therefore ordered by value, not by resemblance to SpamAssassin: signals the
server already has, then network reputation, then Bayes, and only then the
public rule files.

## The configuration this plan introduces

The whole surface is defined here so that later milestones do not change the
shape of it. Fields arrive progressively — a milestone adds only what it
implements — but the names and nesting are fixed now.

    antispam:
      enabled: true

      # Which filter scores messages: "builtin" or "spamd".
      engine: builtin

      # Points at or above which a message is treated as spam.
      threshold: 5.0

      # Used when engine is "spamd": an external SpamAssassin daemon.
      spamd:
        host: spamassassin
        port: 783

      builtin:
        # Checks derived from what the server already knows about the
        # message: authentication results, the sending host's DNS, and how
        # the sending host behaved during the SMTP conversation.
        signals:
          enabled: true

        # Reputation lookups in public DNS block lists.
        dns:
          enabled: true
          timeout: 5s
          # Lists consulted for the connecting address.
          addressLists:
            - zone: zen.spamhaus.org
              weight: 3.0
            - zone: bl.spamcop.net
              weight: 2.0
          # Lists consulted for domains found in the message body.
          domainLists:
            - zone: dbl.spamhaus.org
              weight: 3.0

        # Classification learned from this server's own mail.
        bayes:
          enabled: true
          minimumMessages: 200
          weight: 3.0

        # Public rule files: pattern rules published as a signed update
        # channel, downloaded once into the database and evaluated in
        # process. There is deliberately no directory setting; see
        # "Nothing lives on one instance's disk".
        rules:
          enabled: false
          channels:
            - updates.spamassassin.org
          updateInterval: 24h
          maximumEvaluationTime: 2s

`engine` is the choice the operator makes. Because configuration already
exists in databases in the field, an unset `engine` must be resolved rather
than defaulted blindly: if `engine` is empty and `spamd.host` (or the legacy
top-level `host`) is set, the engine is `spamd`; if `engine` is empty and no
host is set, the engine is `builtin`. This means an existing deployment that
is talking to a daemon keeps talking to it after an upgrade, without a
migration, and a new deployment gets the built-in filter. State this rule in
`docs/configuration.md`.

The legacy `antispam.host` and `antispam.port` fields must keep working and
must keep their meaning. Mark them deprecated in their doc comments, have
`config export` write the new `spamd` block, and accept either on import.

## Nothing lives on one instance's disk

This server is designed to run as several instances against one PostgreSQL.
The README says so, the compose file has a `cluster` profile for it, and
`TEANODE_INSTANCE_ID` exists to tell them apart. Everything this plan adds is
therefore shared state in the database, and no part of it may be a directory
on one machine.

This rules out the obvious design for the rule files, which is to download
them into a folder under the data directory. Three instances would each
download their own copy, at their own times, and drift: the same message would
score differently depending on which instance received it, and an operator
looking at the dashboard would have no way to see that had happened. Worse,
the object store that *is* shared is optional — it only exists in the cluster
profile — so it cannot be relied on either. The database is the only thing
guaranteed to be present and shared, which is where the configuration already
lives for exactly this reason.

So: rule files are stored in the database, Bayesian counts are stored in the
database, and an instance keeps only a parsed in-memory copy, refreshed when
the stored version changes. The data directory is untouched by this plan.

## Data model

Everything added is listed here. The repository separates two layers, and both
must be edited for each new table:

- `internal/models/` holds the domain types, plain structs with `json` tags
  and no persistence concerns.
- `internal/db/database_*.go` holds a matching GORM model with `gorm` tags and
  a `TableName()` method, plus functions that convert between the two — see
  `mailModel` and `getMailFromMailModel` in `internal/db/database_mail.go` for
  the pattern to copy.

Schema changes are `.sql` files in `internal/db/migrations/`, numbered in
sequence, each with a `.reverse.sql` beside it; the loader panics if the
reverse file is missing. Follow `docs/coding/database-migrations.md` exactly.
The existing files carry a comment explaining *why* the change exists, and new
ones should too.

### Milestone one: the score breakdown, with no migration

The purpose section promises that the dashboard shows each check with the
points it contributed. Today `models.SpamFilterResult` cannot express that —
`Symbols` is a list of bare names, which is all the daemon's protocol
provides. Add to `internal/models/mail.go`:

    // SpamFilterCheck is one check that fired, and what it cost.
    type SpamFilterCheck struct {
        // Symbol is the check's short name, for example SPF_FAIL.
        Symbol string `json:"symbol"`

        // Score is the points this check contributed. Negative for checks
        // that vouch for a message rather than accuse it.
        Score float64 `json:"score"`

        // Description is a sentence for a human reading the dashboard.
        Description string `json:"description,omitempty"`
    }

and a field on the existing `SpamFilterResult`:

        // Checks is the breakdown behind Score. Empty for a message scored by
        // the external daemon, whose protocol reports names without points.
        Checks []SpamFilterCheck `json:"checks,omitempty"`

Keep `Symbols` and keep populating it, so nothing that reads it breaks and so
both engines fill the same field.

**No migration is needed for this**, which is worth understanding before
reaching for one. `AuthenticationResults` is not a set of columns; it is
serialised to a single `jsonb` column, declared in `internal/db/database_mail.go`
as:

    AuthenticationResults []byte `gorm:"type:jsonb"`

Adding a field to a struct that is marshalled into that column is backward
compatible in both directions: messages stored before this change unmarshal
with `Checks` nil and render as they do today, and a message stored after it
is readable by an instance still running the old code, which ignores the field
it does not know.

### Milestone three: the Bayesian classifier

Two tables. The first holds what has been learned, keyed by token:

    -- What the classifier has learned. One row per token, shared by every
    -- instance, because a classifier that differed between instances would
    -- score the same message differently depending on which one received it.
    CREATE TABLE "spam_token" (
        "token"       varchar(64)  NOT NULL,
        "spam_count"  bigint       NOT NULL DEFAULT 0,
        "ham_count"   bigint       NOT NULL DEFAULT 0,
        "modified_at" timestamptz  NOT NULL,
        PRIMARY KEY ("token")
    );

with the reverse being `DROP TABLE "spam_token";`.

The token is stored as text rather than as a hash. The alternative was
considered and rejected: a hash would make the table meaningless to look at
when the classifier misbehaves, and it protects little, because this same
database already holds every message's subject, sender and recipients in the
clear. Cap the length at 64 characters and discard longer tokens, which are
not words.

Training must be atomic, because several instances may train at once. Never
read-modify-write; use one statement:

    INSERT INTO "spam_token" ("token", "spam_count", "ham_count", "modified_at")
    VALUES ($1, $2, $3, now())
    ON CONFLICT ("token") DO UPDATE
    SET "spam_count"  = "spam_token"."spam_count" + EXCLUDED."spam_count",
        "ham_count"   = "spam_token"."ham_count"  + EXCLUDED."ham_count",
        "modified_at" = now();

The second table records which messages have been used for training, which
solves three problems at once: marking the same message twice must not count
it twice, un-marking must be able to subtract exactly what was added, and the
corpus totals that `bayes.minimumMessages` is compared against are simply the
row counts.

    -- Which messages taught the classifier, so that training is idempotent
    -- and reversible. Without this, pressing "this is spam" twice would count
    -- the message twice and quietly bias the corpus.
    CREATE TABLE "spam_training" (
        "mail_id"     varchar(32)  NOT NULL,
        "label"       varchar(16)  NOT NULL,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        PRIMARY KEY ("mail_id")
    );
    CREATE INDEX "spam_training_label" ON "spam_training" ("label");

`label` is `spam` or `ham`. Changing a message's label must subtract its
tokens from one count and add them to the other, inside one transaction with
the row update, or a crash between the two leaves the corpus wrong.

In `internal/models/`, add a `SpamTraining` type carrying `MailID`, `Label`,
`CreatedAt` and `ModifiedAt`; the classifier's counts do not need a domain
type, since nothing outside `internal/strainer` reads them.

### Milestone four: rule files in the database

The rule corpus is a few megabytes of text. It goes in the database, for the
reasons in the section above.

    -- One row per configured update channel, per instance-independent
    -- ruleset. Version is what the channel published; instances compare it
    -- against what they have parsed in memory and reload when it moves.
    CREATE TABLE "spam_rule_set" (
        "channel"       varchar(256) NOT NULL,
        "version"       varchar(64)  NOT NULL,
        "content"       bytea        NOT NULL,
        "rules_loaded"  integer      NOT NULL DEFAULT 0,
        "rules_skipped" integer      NOT NULL DEFAULT 0,
        "updated_at"    timestamptz  NOT NULL,
        "error"         text         NOT NULL DEFAULT '',
        PRIMARY KEY ("channel")
    );

`content` is the verified archive as downloaded, so that a later version of
the parser can re-read it without fetching again, and so that what was
verified is what is stored. `rules_loaded` and `rules_skipped` are what the
dashboard shows, which is how "monitor the rulesets" becomes something an
operator can actually do.

Only one instance should download. Take a PostgreSQL advisory lock around the
fetch — `SELECT pg_try_advisory_lock($1)` with a constant derived from the
channel name — and if it is not granted, do nothing and let the instance that
holds it publish the result. Every instance then notices the new `version` and
reparses from `content`. This mirrors how the configuration is already
handled: `db.Database` exposes `ConfigurationVersion()` so an instance can
tell whether its copy is stale, and the same shape should be used here.

Add to the `db.Database` interface in `internal/db/db.go`:

    // SpamRuleSetVersion is what the stored ruleset is at, for an instance
    // checking whether its parsed copy is stale.
    SpamRuleSetVersion(channel string) (string, error)

    // LoadSpamRuleSet reads the stored archive for one channel.
    LoadSpamRuleSet(channel string) (*models.SpamRuleSet, error)

    // SaveSpamRuleSet replaces it after a verified download.
    SaveSpamRuleSet(ruleSet *models.SpamRuleSet) error

and matching methods for the classifier:

    // LearnSpamTokens applies token deltas atomically.
    LearnSpamTokens(deltas []models.SpamTokenDelta) error

    // LoadSpamTokens reads counts for the tokens of one message.
    LoadSpamTokens(tokens []string) (map[string]models.SpamTokenCount, error)

    // CountSpamTraining returns how many messages carry each label.
    CountSpamTraining() (spam int64, ham int64, err error)

Reading counts for every token of every message on every delivery would be a
query per message with a large `IN` list. Keep an in-process cache in
`internal/strainer` with a short expiry, and state plainly in the code why it
is there. If that proves too slow under load, the fallback is to hold the
whole table in memory per instance and refresh it periodically; do not design
for that until it is measured.

## Milestone one: the seam, and scoring from what we already know

At the end of this milestone the server scores messages with no external
program, using only information it already computes, and the operator can
switch between the two engines with one setting.

The work is in three parts.

First, generalise the seam. In `internal/util/spamc`, the `Client` interface
is the right shape but the wrong name for a thing that is no longer only a
SpamAssassin client. Create `internal/spamfilter` defining:

    package spamfilter

    // Filter scores one message. Implementations must be safe for concurrent
    // use and must respect the context's deadline: a filter that hangs holds
    // an SMTP transaction open.
    type Filter interface {
        Close() error
        Check(ctx context.Context, message *Message) (*models.SpamFilterResult, error)
    }

    // Message is everything a filter may look at. Every field is a value the
    // server has already computed; a filter must not derive any of them
    // again. See "Do not compute anything twice" above.
    type Message struct {
        // Headers and Body are the message as mailparse.Split already
        // produced it. Filters read these directly rather than re-parsing,
        // and rather than gluing them back together — only the spamd adapter
        // needs to do that, because a socket takes bytes.
        Headers []string
        Body    []byte

        // Authentication is what the server already established: SPF, DKIM,
        // DMARC, ARC and the sending host's mail servers. May be nil for a
        // message that took a path where those checks do not run.
        Authentication *models.AuthenticationResults

        // The connection the message arrived on, resolved by
        // internal/util/smtpd before the session began and carried on the
        // mailparse.Envelope.
        RemoteAddress netip.Addr
        ReverseName   string          // envelope.RDNS; empty when there is none
        Location      *geoip.Location // envelope.Location
        HelloName     string          // envelope.Hello
    }

Note that `Message` carries more than the raw bytes. This is the reason the
built-in filter can be better than an external daemon rather than merely
equal to it: a separate process is handed a message and knows nothing about
the conversation that delivered it.

Then write two implementations. `spamfilter.NewSpamd(settings)` wraps the
existing `internal/util/spamc` client unchanged and ignores the extra fields.
`internal/strainer` provides `strainer.New(settings, resolver)` returning a
`Filter`.

For this milestone the strainer implements only the signal checks, each
returning a symbol and a weight. Every one of them is a read of something
already computed — the right-hand column names where the value comes from, and
implementing any of these with a fresh lookup or a fresh verification is a
defect, not an optimisation. Take the default weights from the table below,
which are chosen to be conservative: no single signal should condemn a message
on its own, given the threshold of 5.0.

    symbol                weight  read from
    SPF_FAIL                 3.0   Authentication.SPF
    SPF_SOFTFAIL             1.5   Authentication.SPF
    DKIM_INVALID             2.0   Authentication.DKIMs
    DKIM_VALID              -1.0   Authentication.DKIMs
    DMARC_FAIL               3.0   Authentication.DMARC
    DMARC_PASS              -1.0   Authentication.DMARC
    ARC_PASS                -1.0   Authentication.ARC
    NO_CONFIRMED_REVERSE_DNS 1.5   ReverseName is empty
    HELO_NOT_FQDN            1.0   HelloName
    HELO_IS_OUR_NAME         2.5   HelloName against the server's own name

There is deliberately only one reverse-DNS symbol, and the reason is a good
illustration of the rule at the top of this plan. `checkIp` in
`internal/util/smtpd/smtpd.go` does not merely look up the PTR record: it
resolves each name it gets back to an address, and returns the name only if
one of those addresses equals the connecting address. That is a
forward-confirmed reverse DNS check, done in full, before the session begins.

So `ReverseName` is non-empty exactly when the address has a confirmed name,
and empty in both of the failing cases — no PTR record at all, and a PTR
record that does not resolve back. A separate `REVERSE_DNS_MISMATCH` symbol
cannot be produced from what is in hand, and producing it would mean issuing
the same two lookups a second time to learn something worth one point.
One symbol, no lookups.

Negative weights matter as much as positive ones: without them, legitimate
mail from a well-configured sender scores the same as mail from a sender with
no opinion, and the threshold has to be raised until it catches nothing.

Two notes on where these come from, both verified in the code rather than
assumed.

The reverse DNS lookup is already done. `internal/util/smtpd/smtpd.go` calls
`checkIp(ip, resolver, 5*time.Second)` before the session starts and keeps the
result on the session as `rdns`. Pass it through to the `Message` rather than
looking it up a second time.

There is deliberately no `EARLY_TALKER` symbol, and it is worth saying why,
because it looks like an obvious signal to add. A host that transmits before
it is greeted is *already* refused: the same file applies a greeting delay to
untrusted senders, and on detecting early data it logs `failed to delay
connection ...: smtpd: received data too early` and returns, dropping the
connection before a session exists. No message is created, so there is nothing
to score. It is a rejection, not a signal, and it should stay one — scoring it
would mean accepting mail the server currently refuses.

Third, wire it up — and this is the part with a real obstacle in it, so read
it before starting.

In `internal/cmd/server/run.go`, where the `spamc` client is constructed
today, choose the implementation from `antispam.engine` using the resolution
rule above, and pass the resulting `Filter` to `mx.Open`. Change
`mx.Exchange`'s field and constructor parameter from `spamc.Client` to
`spamfilter.Filter`.

**The obstacle: the spam check currently runs at the same time as the checks
whose answers it now needs.** All the checks are fanned out as concurrent
goroutines. In `internal/mx/exchange_incoming.go` the sequence is:

    authenticator := newAuthenticator(ctx)
    self.checkDmarcSpfDkim(authenticator, ...)
    self.checkArc(authenticator, ...)
    self.checkVirus(authenticator, ...)
    self.checkSpam(authenticator, ...)
    authenticationResults, results, err := authenticator.wait()

`authenticator.do` (in `internal/mx/exchange_utils.go`) starts a goroutine per
check and `wait()` merges the results as they arrive. So at the moment
`checkSpam` runs, `Authentication` is still being computed by its siblings and
is not available to it. Passing it in is not a matter of plumbing; the order
has to change.

Restructure each of the three call sites — `exchange_incoming.go`,
`exchange_outgoing.go` and `exchange_bounce.go`, all of which follow this
shape — into two phases:

    authenticator := newAuthenticator(ctx)
    self.checkDmarcSpfDkim(authenticator, ...)   // phase one, concurrent
    self.checkArc(authenticator, ...)
    self.checkVirus(authenticator, ...)
    authenticationResults, results, err := authenticator.wait()

    self.checkSpam(ctx, &authenticationResults, ...)   // phase two

Note that `exchange_outgoing.go` runs only the virus and spam checks, and
`exchange_bounce.go` does not run `checkDmarcSpfDkim`; the two-phase shape
still applies, with a possibly-nil `Authentication`, which the strainer must
tolerate rather than assume.

This costs latency: scoring no longer overlaps with authentication. For
milestone one that cost is close to nothing, because every strainer check is
arithmetic over values already in memory — no lookups, no parsing, no
cryptography. It becomes real in milestone two, when DNS block list queries
arrive, and the fix belongs there rather than here: those queries need only
the connecting address, which is known before any of these checks begin, so
they can be started at the top of phase one and their results collected in
phase two. Do not build that machinery now; note it, and keep phase two cheap.

Keep `checkSpam` as the function name and its position in the file, so the
diff reads as a change of ordering rather than a rewrite.

A filter that fails must not reject mail. Wrap the call so that an error is
logged and recorded on the message, and delivery proceeds unscored. Spam
scoring is advisory; a broken scorer that bounces mail is worse than no
scorer.

Acceptance. Run `make test`. Then start a server with no spam daemon running
anywhere and `antispam.engine` unset, deliver a message from a host with no
reverse DNS, and open it in the dashboard: the spam panel shows a non-zero
score and lists `NO_REVERSE_DNS` among its symbols. Set `antispam.engine` to
`spamd` with no daemon reachable, deliver another message, and confirm the
message is still delivered and the log records the failure. New unit tests in
`internal/strainer` must cover each symbol: construct a `Message` with the
condition present and assert the symbol and weight, and construct one without
and assert its absence.

## Milestone two: reputation from DNS

At the end of this milestone the built-in filter consults public block lists,
which is the single largest accuracy gain available, and still needs no
service of its own — a block list is queried with an ordinary DNS lookup.

A DNS block list works by arithmetic on a name. To ask `zen.spamhaus.org`
about `198.51.100.4`, reverse the octets and append the zone, then look up
`4.100.51.198.zen.spamhaus.org`. An answer means listed; `NXDOMAIN` means not
listed. The address in the answer, usually in `127.0.0.0/8`, says *why* it is
listed, and different codes deserve different weights. Domain lists work the
same way without the reversal: `example.com.dbl.spamhaus.org`.

Implement in `internal/strainer`:

    // Lookup asks one list about one address or domain. A miss and an error
    // are different: an error must not be scored as a miss, because a
    // resolver outage would then silently make every sender look reputable.
    func (self *strainer) lookup(ctx context.Context, zone, subject string) (listed bool, codes []netip.Addr, err error)

Requirements. Query the lists concurrently, bounded by `dns.timeout`, and
cache results for the TTL the resolver reports, because a bulk sender opens
many connections and the same address will be asked about repeatedly. Extract
domains for the domain lists from the message body's links and from the
envelope sender. Cap how many domains are looked up per message — ten is
enough — or a message with a thousand links becomes a thousand DNS queries.

Two warnings to put in `docs/configuration.md`. First, the public lists have
terms of use: they are free for low volume and require a paid subscription
above it, and a server behind a large shared resolver may be refused service
entirely. Second, the default list set must be chosen so that a server which
cannot reach them degrades to milestone one's behaviour rather than failing.

Acceptance. Unit tests with a fake resolver: an address the fake reports as
listed scores the list's weight and gains a symbol naming the zone; an address
reported as `NXDOMAIN` scores nothing; a resolver error scores nothing and is
logged. Then, on a real server, deliver a message from an address known to be
listed and observe the symbol in the dashboard.

## Milestone three: learning from this server's own mail

At the end of this milestone the filter improves as it is used, which is the
part of any spam filter that does most of the work.

Implement a naive Bayesian classifier over message tokens. The mathematics is
standard and small: split the message into tokens, hold a count per token of
how often it has appeared in messages marked spam and in messages marked not
spam, and combine the per-token probabilities into one. The classifier
contributes a symbol and a weight scaled by `bayes.weight`, and must not
contribute at all until it has seen `bayes.minimumMessages` examples, because
a classifier trained on four messages is confidently wrong.

Storage is the two tables specified in "Data model" above — `spam_token` and
`spam_training` — added by a migration in `internal/db/migrations` following
`docs/coding/database-migrations.md` exactly, including the reverse SQL that
file requires. Token counts, never message contents. Because they live in
PostgreSQL, every instance shares one trained classifier, which is the only
correct behaviour when instances share a mail stream.

Training needs a way to say "this is spam". The dashboard's message view gains
two actions, and the API gains the mutation behind them. Marking a message
also re-scores nothing retroactively; it teaches the classifier for next time.

Acceptance. A test that trains the classifier on a small labelled corpus
committed under `internal/strainer/testdata` and asserts that held-out spam
scores above the threshold and held-out legitimate mail below it. In the
dashboard, mark several messages as spam, then deliver a similar one and see
the Bayes symbol appear.

## Milestone four: public rule files

At the end of this milestone the filter can also evaluate the public
pattern-rule corpus, which adds breadth on the kinds of spam the earlier
milestones do not catch.

This is the milestone the research says to do last and the one an
enthusiastic reader will want to do first. Resist that.

Scope precisely. Implement the rule types `header`, `body`, `rawbody`, `full`,
`uri`, `meta`, `score`, `describe` and `tflags`, and the `ifplugin` / `if` /
`endif` conditionals — where every `ifplugin` block must be skipped, since
this program has none of those plugins. Rules whose patterns do not compile
with Go's `regexp` are skipped and counted, not fatal: the measurement says
that is about 12% of them, carrying about 2% of the weight. Log the count once
at load so an operator can see it. Do not add a backtracking regular
expression library to recover those rules; a backtracking engine running
attacker-supplied patterns against attacker-supplied text is a denial of
service waiting to happen, and the weight recovered does not justify it.

Meta rules need a small expression evaluator over the boolean outcomes of
other rules, supporting `&&`, `||`, `!`, parentheses, comparison and addition
of rule hit counts. Write it by hand; it is perhaps 200 lines and one test
file.

Use score set 0 — the first of the four numbers on a `score` line — because
this implementation has neither the plugins nor the network tests the other
sets are calibrated for.

Safety is not optional here. Thousands of patterns run against text an
attacker chose. Enforce `rules.maximumEvaluationTime` across the whole rule
pass and abandon the pass if it is exceeded, scoring what completed. Cap the
body size scanned. Both caps must be tested with a deliberately hostile input.

The update channel is a set of files published over HTTP with a detached
signature, and the version to fetch is published in DNS. Fetching, verifying
and unpacking that is a substantial piece of work on its own; if it proves
large, split it into its own milestone and ship the evaluator first, loading a
ruleset that an operator has stored deliberately — through the command line or
the dashboard, into the `spam_rule_set` table described in "Data model", never
into a directory on one instance's disk. Never evaluate rules from an
unverified download.

This milestone is where the operator's knobs for rules live: `rules.enabled`
to use them at all, `rules.channels` to choose which sources, and
`rules.updateInterval` to say how often to look. Ship with `rules.enabled`
false, so that a server upgrading into this milestone does not silently start
downloading and executing rule files it was never asked for. The dashboard
should show, per channel, when it last updated, how many rules loaded and how
many were skipped, so "monitor" is a thing an operator can actually do rather
than a promise.

Acceptance. A test that loads a small rule file from `testdata`, evaluates a
message against it, and asserts the expected symbols and score, including one
meta rule and one rule that fails to compile and is skipped. A test proving
the evaluation deadline is honoured. On a real server with a channel
configured, the dashboard shows the rule count and the last update time.

## Milestone five: retiring the dependency

At the end of this milestone the default deployment has no spam service in it.

Remove the `spamassassin` service from the default stack in
`deploy/docker-compose.yml` and move it behind a Compose profile named
`spamd`, alongside a comment explaining that it is only needed when
`antispam.engine` is `spamd`. Update `docs/reference/deployment.md` to say the
same. This is the milestone that actually delivers the original motivation, so
do not skip it, and do not do it earlier: until milestone one ships, removing
the service removes spam scoring.

Note for whoever does this: ClamAV cannot be treated the same way. There is no
Go virus engine and its signature database is about a gigabyte. The antivirus
service stays.

## Concrete Steps

Work from the root of this repository.

Before starting, get a working build and a database:

    make test

This starts a PostgreSQL container and needs Docker running. Expect every
package to report `ok`.

Read these files before editing anything, in this order:
`internal/util/spamc/spamc.go`, `internal/mx/exchange.go` (the `Open`
function and wherever `self.spamc` is used), `internal/models/mail.go` (the
types `AuthenticationResults` and `SpamFilterResult`),
`internal/config/config.go` (the type `Antispam`), and
`internal/cmd/server/run.go` (where the client is constructed).

After every change: `make lint-ci`, which must end with `0 issues.` and with
`every configuration field is documented`. That last check fails if a new
configuration field has no entry in `docs/configuration.md`, so add the
documentation in the same commit as the field.

Commit frequently, and update the `Progress` section here at every stopping
point.

## Validation and Acceptance

The plan is complete when all of the following hold.

A server started with no spam daemon reachable and no `antispam.engine` set
scores incoming mail, and the dashboard shows named symbols for the checks
that fired.

Setting `antispam.engine` to `spamd` and pointing `antispam.spamd.host` at a
daemon restores the previous behaviour exactly, and an existing configuration
that names a host keeps working across the upgrade with no edit.

With the daemon configured but unreachable, mail is still delivered, and the
failure appears in the log rather than as a bounce.

`make test` passes and `make lint-ci` reports `0 issues.`

`docker compose up -d` with `deploy/docker-compose.yml` brings up a working
mail server with no spam scanning service running.

## Idempotence and Recovery

Every milestone is additive and reversible by setting `antispam.engine` back
to `spamd`, which is why the external path is kept working rather than
deleted. Keep it working until milestone five, and keep it after.

The database migration in milestone three must carry reverse SQL, as
`docs/coding/database-migrations.md` requires; a migration without it fails the
build. Re-running any milestone's steps is safe: they add types, settings and
tables that either exist or do not.

If a milestone proves larger than expected, split it and record the split in
`Progress` rather than half-finishing it.

## Interfaces and Dependencies

No new third-party Go dependencies are required or permitted for milestones
one through three. Milestone four's update channel needs signature
verification, for which `golang.org/x/crypto/openpgp` or a maintained
equivalent may be vendored; the repository vendors its dependencies, so run
`go mod vendor` and commit `vendor/`.

At the end of milestone one these must exist:

In `internal/spamfilter/spamfilter.go`:

    type Filter interface {
        Close() error
        Check(ctx context.Context, message *Message) (*models.SpamFilterResult, error)
    }

    func NewSpamd(settings *Settings) (Filter, error)

In `internal/strainer/strainer.go`:

    func New(settings *config.AntispamBuiltin, resolver resolver.Resolver, database db.Database) (spamfilter.Filter, error)

The database is not needed until milestone three, but take it from the start:
adding it later means changing the constructor and all three call sites again,
and the strainer's learned state and rule sets both live there.

In `internal/config/config.go`, the `Antispam` type carries `Engine`,
`Threshold`, a nested `Spamd`, and a nested `Builtin`, with the legacy `Host`
and `Port` retained and marked deprecated.

`internal/mx.Open` takes a `spamfilter.Filter` in place of a `spamc.Client`.

## Progress

- [x] (2026-09-05) Measured the SpamAssassin 4.0.2 corpus to decide what is
      worth reimplementing; numbers recorded above and in `Surprises &
      Discoveries`.
- [x] (2026-09-05) Confirmed the integration seam is narrow: one 140-line
      client behind an interface, one field on `mx.Exchange`.
- [x] (2026-09-05) Specified the data model: `SpamFilterCheck` on the
      existing jsonb column with no migration, `spam_token` and
      `spam_training` for the classifier, `spam_rule_set` for the rules — all
      in PostgreSQL, none on an instance's disk.
- [x] (2026-09-05) Inventoried every signal the server already computes, and
      established that milestone one needs no lookups at all. Found that the
      checks run concurrently, so milestone one is a reordering rather than
      plumbing.
- [x] (2026-09-05) Milestone one: the seam, and scoring from signals the
      server already has. `internal/spamfilter` with two implementations,
      `internal/strainer` for the built-in one, `antispam.engine` to choose,
      and the three call sites in `internal/mx` reordered into two phases.
- [x] (2026-09-05) Milestone two: DNS block list reputation, cached, with
      refusal codes distinguished from listings.
- [x] (2026-09-05) Milestone three: Bayesian classification, tables
      `spam_token` and `spam_training`, and the dashboard buttons that teach
      it.
- [x] (2026-09-05) Milestone four: the rule format parsed and evaluated, the
      meta expression language, and `spam_rule_set` in the database.
      Remaining: the signed update channel, deliberately not built — see the
      decision below.
- [x] (2026-09-05) Milestone five: the spam daemon is behind a compose
      profile, so nothing in the default path depends on a third-party image.
- [x] (2026-09-05) Proved end to end: `make test-deployment` passes 104 checks
      against a stack with no spam daemon in it, including that a message is
      scored and carries a per-check breakdown.
- [ ] The signed update channel for rule files, when the OpenPGP dependency
      question is settled.

## Surprises & Discoveries

- Observation: Go's regular expression engine is far less of an obstacle than
  expected. Only 12% of the corpus needs a backtracking engine, and those
  rules carry about 2% of the scoring weight.
  Evidence: of 3,032 pattern rules, 373 contain lookahead, lookbehind or
  backreferences; summing score-set-0 weights by rule category gives
  regex-compatible 46.9%, meta 22.2%, plugin-backed 8.4%, needs-backtracking
  2.1%.

- Observation: the rule corpus is the least valuable part of SpamAssassin to
  reimplement, which inverts the obvious plan.
  Evidence: SpamAssassin ships four score sets; set 0 is the no-Bayes,
  no-network calibration, and is the weakest configuration it offers. The
  strength of the product is Bayes plus network lookups, neither of which is
  in the rule files.

- Observation: a default value can defeat a resolution rule silently, and did
  so on a live server.
  Evidence: `config.Default()` set `engine: builtin`; the stored section is
  unmarshalled over the defaults, so `Antispam.Engine` was never empty and
  `ResolvedEngine()` never reached the "spamd when a host is configured"
  branch. The server restarted and logged
  `spam filter: the built-in filter` while its configuration still named a
  daemon.

- Observation: a block list answering successfully does not mean "listed".
  Evidence: the lists reserve 127.255.255.0/24 to say they will not answer —
  a query through a public resolver, or too many of them. Treating those as
  listings puts the list's full weight on every sender at once.

- Observation: the deployment test found two scoring faults that no unit test
  would have, because both only appear when a real message crosses a real
  threshold.
  Evidence: an ordinary test message was refused with
  `550 5.7.26 Spam check failed`, first because SPF and DMARC were both
  scored for one fact, and then because a seeded domain's threshold was zero.

- Observation: the score breakdown the dashboard needs costs no migration.
  Evidence: `AuthenticationResults` is stored as one `jsonb` column
  (`internal/db/database_mail.go`), so adding `Checks` to the marshalled
  struct is compatible in both directions — old rows read back with it empty,
  and an instance on older code ignores a field it does not know.

- Observation: the spam check runs concurrently with the checks whose answers
  it needs, so the results are not merely unused — they do not exist yet at
  that moment. This turns milestone one from plumbing into a reordering.
  Evidence: `authenticator.do` in `internal/mx/exchange_utils.go` starts a
  goroutine per check; all three call sites fan out `checkDmarcSpfDkim`,
  `checkArc`, `checkVirus` and `checkSpam` together and then call `wait()`.

- Observation: the reverse DNS name the server holds is already
  forward-confirmed, which removes a symbol and two lookups from the design.
  Evidence: `checkIp` in `internal/util/smtpd/smtpd.go` resolves each PTR name
  back to an address and returns the name only if it matches the connecting
  address. An empty `rdns` therefore already means "no confirmed name", and a
  separate mismatch symbol could only be produced by repeating both lookups.

- Observation: there is measurable waste in the current path that exists
  solely to feed the external process.
  Evidence: `checkSpam` calls `mailparse.Unsplit` to glue the
  already-separated headers and body back into a pooled buffer, because a
  socket takes bytes. An in-process filter reads the parsed form and skips
  this entirely.

- Observation: the server already computes signals the external daemon cannot
  see, and currently discards them for scoring purposes.
  Evidence: `AuthenticationResults` holds SPF, DKIM, DMARC and ARC outcomes;
  `internal/util/smtpd/smtpd.go` resolves the connecting address's reverse DNS
  name and its GeoIP location before the session begins and carries both on
  the `mailparse.Envelope`, which `internal/mx/exchange_incoming.go` already
  copies. None of it reaches the score, because the score comes from a process
  that never sees any of it.

- Observation: the obvious "sent data before being greeted" signal cannot be
  scored, because such a connection is already dropped.
  Evidence: in `internal/util/smtpd/smtpd.go`, `delay()` returns
  `smtpd: received data too early`, and the caller logs
  `failed to delay connection ...` and returns without creating a session. The
  first draft of this plan listed `EARLY_TALKER` as a scoring symbol; it was
  removed once the control flow was read.

- Observation: the dependency this plan removes failed in the strongest
  possible way, and the failure was not confined to spam scanning.
  Evidence: `tiredofit/spamassassin` returns 404 from Docker Hub and its
  source repository is archived; because Compose pulls all images before
  starting any service, `docker compose up -d` failed entirely.

## Decision Log

Decisions are the repository owner's. Entries below record what was decided
and why, so a later reader can see the reasoning rather than guess at it.

- Decision: the built-in filter is the default; the external SpamAssassin
  daemon remains fully supported, chosen with `antispam.engine: spamd`.
  Rationale: the operator wants the choice, and the local one should be the
  default. Keeping the external path working is also what makes every
  milestone reversible.
  Date/Author: 2026-09-05, Ziyan

- Decision: never present the built-in filter as SpamAssassin in the
  interface, the configuration or the documentation.
  Rationale: it is a different program with different behaviour, and claiming
  otherwise sets an expectation it will not meet.
  Date/Author: 2026-09-05, Ziyan

- Decision: the filter reads the signals the server has already computed and
  recomputes none of them.
  Rationale: the authentication results, the forward-confirmed reverse DNS
  name, the GeoIP location and the parsed message all exist by the time
  scoring happens. An external daemon cannot be told any of it and must redo
  it from bytes; doing the same thing in-process would give up the main
  advantage of being in-process. See "Do not compute anything twice".
  Date/Author: 2026-09-05, Ziyan

- Decision: the operator gets explicit control over rule sources — which
  channels are used, how often they update, and what the dashboard reports
  about them.
  Rationale: rules that download and execute themselves on a mail server
  should be visible and switchable, not implicit.
  Date/Author: 2026-09-05, Ziyan

- Decision: every piece of state this plan adds lives in PostgreSQL, and
  nothing lives in a directory on one instance's disk.
  Rationale: this server supports several instances against one database. Rule
  files downloaded per instance would drift, so the same message would score
  differently depending on which instance received it, invisibly. The object
  store is not an alternative, because it is optional and exists only in the
  cluster profile; the database is the only shared thing guaranteed to be
  there, which is already why the configuration lives in it.
  Date/Author: 2026-09-05, Ziyan

- Decision: store Bayesian tokens as text, not as hashes.
  Rationale: a hashed table is impossible to inspect when the classifier
  misbehaves, and hashing protects little here — the same database already
  holds every message's subject, sender and recipients in the clear.
  Date/Author: 2026-09-05, Ziyan

- Decision: record which messages were used for training, rather than only
  keeping counts.
  Rationale: it makes marking idempotent, makes un-marking exact, and makes
  the corpus totals a row count rather than a number that can drift away from
  reality.
  Date/Author: 2026-09-05, Ziyan

- Decision: resolve an unset `engine` from whether a host is configured,
  rather than defaulting it blindly.
  Rationale: configuration already lives in deployed databases. This keeps
  existing daemon users working with no migration and no edit, while new
  installations get the built-in filter.
  Date/Author: 2026-09-05, Ziyan

### Decisions taken during implementation

- Decision: the signal checks are capped, together, below the rejection
  threshold.
  Rationale: each is a statement about how the sender is configured, and
  legitimate senders are misconfigured constantly. Crossing the threshold
  should take corroboration from something that looked at the message. The
  deployment test refused an ordinary message before this existed.
  Date/Author: 2026-09-05, Ziyan

- Decision: when a domain's DMARC policy reaches a verdict, SPF is not scored
  separately.
  Rationale: the DMARC verdict is largely the answer to whether SPF aligned,
  so scoring both counts one fact twice and doubles the penalty for a single
  misconfiguration.
  Date/Author: 2026-09-05, Ziyan

- Decision: authenticated submission is scored on content only.
  Rationale: the connection checks ask whether a host is entitled to send
  mail, which a credential answers. Their honest answers are also wrong for a
  laptop, whose home address is in the block lists on purpose.
  Date/Author: 2026-09-05, Ziyan

- Decision: ship milestone four without the signed update channel, and load
  rule sets deliberately instead.
  Rationale: rules are patterns run against every message, so an unattended
  fetch has to verify the publisher's signature, and the OpenPGP package that
  would do it is deprecated upstream. Adding a frozen cryptography dependency
  to a mail server is a decision to take on its own.
  Date/Author: 2026-09-05, Ziyan

### Recommendations awaiting a decision

These are proposals from the research, not settled matters. They are recorded
here so the owner can accept, change or reject them; an implementer should ask
rather than assume.

- Proposed: call the built-in filter "the strainer", in `internal/strainer`.
  Rationale: it needs a name that is not SpamAssassin, and a strainer holds
  the leaves back when you pour. Any other name works as well; the constraint
  is only that it not borrow the other project's.

- Proposed: order the milestones by accuracy gained per unit of work — own
  signals, then DNS reputation, then Bayes, then rule files.
  Rationale: measurement showed the rule corpus carries the least weight in
  the only calibration available to a local filter, and the most ongoing
  maintenance. The alternative order, starting with the rules because they
  are the visible part of SpamAssassin, would spend the most effort first for
  the least gain.

- Proposed: do not vendor a backtracking regular expression engine to recover
  the 12% of rules Go cannot compile.
  Rationale: those rules carry about 2% of the weight, and running a
  backtracking engine over attacker-chosen text with attacker-reachable
  patterns is a denial-of-service surface. The trade looks bad in both
  directions, but it is a judgement call about accuracy against risk.

- Proposed: ship milestone four with `rules.enabled` false.
  Rationale: an upgrade should not silently begin downloading and executing
  rule files the operator did not ask for.

## Outcomes & Retrospective

**All five milestones are implemented.** A new deployment scores spam with no
second program: `deploy/docker-compose.yml` starts no spam service, and
`make test-deployment` proves a message is scored anyway, with the breakdown
that only the built-in filter can produce. An operator can still choose the
daemon, and one that was already using it keeps using it without being asked.

What the end-to-end test found, which no unit test would have:

The first version rejected an ordinary message at the SMTP door. SPF and
DMARC were both scored, which counts one fact twice — a DMARC verdict is
largely the answer to whether SPF aligned — and the total crossed the
threshold on configuration faults alone. Both are fixed, and the signal
checks are now capped below the threshold on purpose: every one of them is a
statement about how a sender is configured, and legitimate senders are
misconfigured all the time.

A domain seeded from the environment carried a spam threshold of zero, which
means "reject anything the filter has any opinion about". That was latent for
as long as scoring required a daemon and was off by default. Turning the
built-in filter on by default would have turned an upgrade into a mail server
that refused almost everything.

Deploying to a live server found the worst one. `config.Default()` set
`engine: builtin`, and the stored configuration is unmarshalled over the
defaults, so the field was never empty and the resolution rule never fired.
The server restarted, logged "spam filter: the built-in filter", and stopped
using the daemon it had been configured with. The rule existed precisely to
prevent that, and a default defeated it silently.

Two more came out of reviewing the result rather than running it. A block
list answers in 127.0.0.0/8 and uses the top of that range to say it will not
answer — through a public resolver, or too often — and reading those as
listings would have put the full weight on every sender at once. And the
connection checks were being applied to authenticated submission, where they
are both meaningless and wrong: a laptop has no reverse DNS and its home
address is in the block lists deliberately.

What remains is the signed update channel for rule files. Rules are patterns
this server runs against every message, so fetching them unattended means
verifying the publisher's signature, and the package that would do it is
deprecated upstream. Loading a set deliberately works today.
