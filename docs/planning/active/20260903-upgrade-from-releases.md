# Upgrading from a release, on demand and on a schedule

## Purpose / Big Picture

A mail server that is behind is a mail server with a known bug in it. This one
now publishes releases — binaries, checksums and a container image — and the
running server has no idea they exist. Somebody has to read a changelog they
were not told about.

So: the server checks what has been released, says so on its own page, upgrades
when asked, and — for whoever wants it — upgrades on a schedule without being
asked.

The whole feature is one long argument about trust and about who is allowed to
restart a mail server, and the code is the small part. Three things decide the
shape of it:

**It replaces the code it is running.** Whatever it downloads runs as the user
that receives mail for every domain on the box. The check that the download is
genuine is the feature; the rest is plumbing.

**It cannot restart itself.** `internal/api.Restarter` already says this
plainly: there is no restart in place, only exiting and letting a supervisor
start a new one. An upgrade is therefore a swap followed by an exit, and an
exit is only safe when something will start the process again.

**It cannot upgrade a container.** The binary inside an image is on a read-only
layer, and replacing it would be undone by the next `docker compose up`. The
honest answer there is to say what is available and stop, because what needs
replacing is the image and this program is not the thing that replaces it.

## Progress

- [x] (2026-09-03) Milestone one: knowing what is out there.
- [x] (2026-09-03) Milestone two: applying one, and refusing when it must not.
- [x] (2026-09-03) Milestone three: the settings, the API and the command line.
- [x] (2026-09-03) Milestone four: the dashboard.
- [x] (2026-09-03) Milestone five: on a schedule.
- [x] (2026-09-03) Milestone six: proved against a real deployment — the
      deployment test asserts the container refusal, and a development server
      pointed at the real release list showed the card, found 0.1.1, and
      refused to upgrade because nothing would start it again.

## Surprises & Discoveries

- **The optional argument was required.** `GetUpgrade(check: Boolean)` came out
  as `Boolean!` because the field was a plain `bool`, so every caller that
  merely wanted to know what was already known had to pass an argument saying
  so. The deployment test found it, which is the second time this exact shape
  has bitten — the first was every partial domain update being refused. The
  schema test now pins it.

- **The test for applying an upgrade was itself racy.** `Restarter.Request`
  runs its trigger in a goroutine, so checking a channel immediately
  afterwards is a race, and it failed one run in three. Waiting with a timeout
  is the fix; the negative case asks `Requested()`, which is set before the
  goroutine starts.

## Decision Log

- **Checksums over TLS, from a repository fixed at build time.** The upgrader
  fetches `SHA256SUMS` and the asset from the same release and checks the hash.
  Said plainly, because a security control that oversells itself is worse than
  none: this proves the bytes are the bytes GitHub is serving for that release,
  and nothing more. Anyone who can publish a release to that repository can
  publish a binary, and this will install it. What it does defeat is a
  corrupted download and a mirror that is not GitHub. The repository is
  compiled in rather than configured, so a stolen dashboard session cannot
  point a mail server at somebody else's build.

  Signing was the alternative — cosign against the workflow's OIDC identity, so
  that a build which never ran in Actions cannot be installed. It is the
  stronger answer and it is not what was chosen here; if that changes, the
  verification step is one function.

- **Automatic upgrades take any newer release, and are off until turned on.**
  Not patch-only: a rule that stops at a minor version is a rule that quietly
  stops upgrading, and the operator who turned this on wanted to stop thinking
  about it. Off by default because a release can break mail delivery, and
  nobody installs a mail server expecting it to change underneath them.

- **A container is told, not upgraded.** Detected through the supervision
  `Restarter` already reports. The dashboard says a new version exists and that
  the image is what to replace.

- **An unsupervised process is told too.** If nothing recognisable would start
  the process again, exiting leaves the server down and the upgrade has taken
  mail down to fix a bug in mail. It refuses, and says why. A development
  server run from a shell looks exactly like this, which is the case that
  proves the rule.

## Interfaces and Dependencies

`internal/upgrade`, new, with no dependency on anything above it:

    // What is running and what is available.
    type Status struct {
        Current    string     // the running version
        Latest     string     // the newest release, empty until a check succeeds
        Available  bool       // Latest is newer than Current
        Notes      string     // the release notes for Latest
        CheckedAt  *time.Time
        Error      string     // why the last check failed, for the page to show
        Applicable bool       // whether this deployment can apply it at all
        Reason     string     // and if not, why not
    }

    type Manager interface {
        Status() Status
        Check(ctx context.Context) (Status, error)
        Apply(ctx context.Context) error   // download, verify, swap, restart
        Close() error
    }

It is given the release repository at construction, the `api.Restarter` to
finish with, and a `config.Store` to read the schedule from.

The configuration gains one section:

    upgrade:
      enabled: true          # check for releases at all
      automatic: false       # and install them without being asked
      checkInterval: 6h      # how often to look
      window: ""             # "02:00-04:00", local time; empty means any time

`enabled: true` by default. Knowing that a version exists is not the same as
installing it, and an operator who is never told is an operator running last
year's bugs. The check is one HTTPS request every six hours to a public
endpoint and carries nothing about the deployment.

## Plan of Work

### Milestone one: knowing what is out there

`GET https://api.github.com/repos/<owner>/<repo>/releases/latest`, the tag
parsed as a semantic version and compared with `version.Version()`. Cached in
memory with the time of the check, refreshed by a `periodic` loop like every
other background task here. A failure is remembered rather than logged and
forgotten, because "it has not checked since Tuesday" is what the page has to
be able to say.

A development build reports `0.0.0-dev`, which is older than everything. It
never claims an upgrade is available, because a developer's own build is not
behind the release it was built from.

Acceptance: a unit test over the comparison, including the development
version, a prerelease tag, and a release older than what is running.

### Milestone two: applying one, and refusing when it must not

In order, and stopping at the first refusal:

1. Refuse in a container, or when nothing would restart the process.
2. Refuse when the running executable's directory cannot be written.
3. Download the asset for this `GOOS/GOARCH` and `SHA256SUMS` from the release.
4. Verify the hash. Refuse on any mismatch, and say which file.
5. Write the new binary beside the current one, fsync it, and rename it over —
   atomic within a filesystem, which is why it is written beside rather than
   in a temporary directory that may be elsewhere.
6. Keep the previous binary as `<path>.previous`, so a rollback is a rename
   somebody can do with one command and no download.
7. Ask the `Restarter` to restart.

Acceptance: a test with a local HTTP server standing in for the release, one
run through to the swap and one where the checksum is wrong; the second must
leave the original binary exactly as it was.

### Milestone three: the settings, the API and the command line

`GetUpgrade` returns the status; `ApplyUpgrade` starts one. The command line
reaches both through `api call`, which is the point of that design.

### Milestone four: the dashboard

On the Server settings page, beside what is already there about restarting: the
running version, what is available, the notes, and a button — or the reason the
button is not there. The words say what an upgrade will do, which is replace
the binary and restart the process, because that is the sentence somebody needs
before pressing it rather than after.

### Milestone five: on a schedule

A loop that checks and, when `automatic` is on and a window allows it, applies.
Refusals are logged once, not once a cycle. When the upgrade succeeds, the
process exits and comes back new: the first thing the new one logs is which
version it is and which it replaced.

### Milestone six: proved against a real deployment

`make test-deployment` gains a check that the status is reported and that an
upgrade is refused inside a container, which is what that stack is. The live
deployment is a container too, so the honest end of this is the dashboard
showing the version and saying that the image is what to replace.

## Idempotence and Recovery

An interrupted download leaves a temporary file and nothing else. A failed
verification leaves the running binary untouched. A swap that succeeds and a
restart that does not is the one bad case, and it is why the previous binary is
kept next to the new one under a name somebody can guess.
