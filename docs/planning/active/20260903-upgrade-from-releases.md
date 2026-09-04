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

**It restarts itself.** `syscall.Exec` replaces this process's image with the
new binary, keeping the same arguments, the same environment and the same
process. No supervisor is involved, so a server started by hand upgrades itself
as well as one under systemd. This was not the first answer here — see the
Decision Log — and getting it wrong cost the feature most of its reach.

**It upgrades a container by writing outside the image.** The binary inside an
image is on a layer that `docker compose up` throws away, so an upgrade there
does not write to it: it writes to a directory on a mounted volume and execs
that at the next start. What makes this work is one variable, read from the
environment before the database is opened, naming where that directory is.

## Progress

- [x] (2026-09-03) Milestone one: knowing what is out there.
- [x] (2026-09-03) Milestone two: applying one, and refusing when it must not.
- [x] (2026-09-03) Milestone three: the settings, the API and the command line.
- [x] (2026-09-03) Milestone four: the dashboard.
- [x] (2026-09-03) Milestone five: on a schedule.
- [x] (2026-09-03) Milestone six: proved against a real deployment — the
      deployment test asserted the container refusal, and a development server
      pointed at the real release list showed the card, found 0.1.1, and
      refused to upgrade because nothing would start it again.
- [x] (2026-09-03) Milestone seven: both refusals removed. Exec replaces the
      process without a supervisor; a container stages onto its volume and runs
      that at the next start. The deployment test now asserts the opposite of
      what it used to.
- [ ] Milestone eight: the dashboard as one place. The version card gained the
      release link and the notes; the rail gained a refresh button for a stale
      bundle and a dot on Server when there is a release; Setup, Integrations
      and Server became tabs of one `/server`.

## Surprises & Discoveries

- **The optional argument was required.** `GetUpgrade(check: Boolean)` came out
  as `Boolean!` because the field was a plain `bool`, so every caller that
  merely wanted to know what was already known had to pass an argument saying
  so. The deployment test found it, which is the second time this exact shape
  has bitten — the first was every partial domain update being refused. The
  schema test now pins it.

- **The exec was in the wrong place twice, and both were quiet.** First
  before the database was opened but after the config was read, which is after
  `Migrate` — so the image's old binary would revert the new one's migrations
  on every restart and the new one would re-apply them against the schema it
  had just lost. Then at the end of `serve`, which looks like the last thing
  that happens and is not: every closer in that function is a `defer` of its
  callers, so exec skipped the mailer, the queue, the storage client and the
  database pool. Both were found by review rather than by a test, and neither
  would have failed anything visibly.

- **The crash-loop guard had a second door, and closing the first one did not
  close it.** Marking the binary before both execs stopped it being *run*
  twice without being told to. It did not stop it being *installed* twice —
  and installing clears the mark, because a newly staged binary deserves its
  own attempt. So the loop simply went round the other way: mark holds it
  back, the image's binary serves, the scheduled check wakes, sees the same
  release available, stages it over the mark, and runs it again. Installing a
  release that is already staged and already marked is refused now, by
  version, before anything is downloaded — and as an error, so the backoff
  engages and the page says what happened.

- **The crash-loop guard covered one of the two ways a staged binary gets
  run.** The container start writes the marker before exec'ing; the exec an
  upgrade does when it finishes went straight to `syscall.Exec` and did not.
  So an automatic upgrade to a release that crashes before it serves looped
  with no end and no backoff, because nothing had failed — install, exec,
  crash, restart, marker, exec, crash, restart, run the image's binary, check,
  install the same release again, forty-five megabytes a lap. Both paths mark
  it now.

- **Asking a question was changing the filesystem.** "Can this server upgrade
  itself" created the staging directory and reset its mode, on every check and
  on every start — so a deployment with upgrades turned off grew a directory
  it would never use, and an operator who had set a mode on purpose found it
  put back every few minutes. The read path reads now; creating it is the job
  of the one moment something is staged, and a directory anybody else can
  write is refused with a reason rather than quietly corrected.

- **The page kept guessing something the server knew.** Whether pressing
  "Check now" would achieve anything was inferred twice, from two different
  timestamps, and both were wrong somewhere: the time of the last success does
  not move when a check fails, and the time of the last attempt moves when the
  scheduled loop checks, which does not spend the allowance for asking by
  hand. The reply says whether a check started.

- **A pointer into a field the next check overwrites.** `AttemptedAt` was the
  address of the manager's own `lastAttempt`, and the status is copied and read
  after the lock is released — so the dashboard's polling encoded a `time.Time`
  while a check wrote through it. `CheckedAt` had always taken the address of a
  fresh local, which is what made the new field look right beside it.

- **The same bug came back through a symlink.** "The second upgrade of a
  container took the wrong road" was fixed by comparing the target with the
  staged path instead of with this process's executable — and one of those two
  is resolved through its symlinks at startup and the other is not. A data
  directory that is a symlink, or lives under one, put the identical failure
  back: replace in place, no version written, and the next start deleting a
  freshly installed binary as stale while the database carried its migrations.
  Two fixes in a row derived the answer from comparing paths. The function
  that chooses the road now says which road it chose.

- **A remedy has to be checked the way the thing it remedies is checked.** The
  claim "removing the marker will let it try again" was made on a weaker
  question than the start asks — is there a binary, is there a marker — so it
  was offered for a binary that also fails its checksum, where removing the
  marker changes nothing. Both now ask one function.

- **The refusal's own remedy was the data loss it existed to prevent.** The
  message said to set `TEANODE_ALLOW_MIGRATION_REVERT=true` to go back — and in
  the multi-instance case the guard was written for, following that reverts the
  migrations while the other instance is live on the new schema, taking its
  columns out from under it. The remedy that loses nothing is to run the newer
  version here; reverting is the second option, and the message now says what
  it costs and when not to do it at all.

- **A remedy that is only sometimes true is worse than none.** The same
  message offered "remove the pending marker and it will try again" whenever a
  staged binary was present, and a staged binary is left in place for six
  different reasons — an unreadable version, a missing or wrong checksum, a
  file that is not executable, permissions. In five of them removing the
  marker changes nothing, and somebody who follows it and watches it fail has
  no reason to believe the next paragraph, which is the one about losing data.

- **Reverting migrations had to stop being the default.** Three review rounds
  kept finding the same accident under different names, and the third one
  settled it. A start that meets a migration it does not recognise reverts it,
  which is how a downgrade works here and cannot be told apart from an upgrade
  that crashed, a second instance that never got the upgrade, or an operator
  pulling last week's image to test something. Guarding the one case the
  upgrade feature creates was not enough — the multi-instance case has no
  staged binary to notice. So it is opt-in: the start refuses, names the
  migrations, and says to set `TEANODE_ALLOW_MIGRATION_REVERT=true` to go back
  on purpose. A start that does not happen costs minutes, because the queue is
  on disk and senders retry. A dropped column costs what was in it.

- **An upgrade could stage into a directory the next start would refuse.** The
  two ends were asking different questions: staging asked whether it could
  write there, and the start asked whether anybody else could. On a volume
  mounted `dir_mode=0777` the upgrade succeeded, exec'd, and reported success —
  and the next recreate quietly went back to the old binary, with no refusal
  recorded at any point where somebody could have acted on it. Both ends ask
  the same question now, and a directory that is merely loose and ours is
  tightened rather than refused.

- **The exec ran on any shutdown, not the one it asked for.** The new binary
  is in place from the moment the swap succeeds, which is a moment before the
  restart is even requested — so a `docker compose stop` landing in that
  window, or hours later after an upgrade that could not ask for a restart,
  would have exec'd the new binary instead of exiting. The operator asked the
  server to stop and it would have come back.

- **The second upgrade of a container took the wrong road.** `swap` chose
  between replacing in place and staging by asking whether the target differed
  from this process's executable — and once a process has been exec'd out of
  the staging directory, the staged path *is* its executable. So the second
  upgrade replaced in place, wrote no version and no checksum, and left the
  directory describing the release before last. The next recreate refused the
  staged binary over the mismatch and ran the image's old one. The question is
  where the binary goes, not whether that happens to be the file this process
  was started from.

- **Refusing to run a staged binary is safe for the binary and was not safe
  for the schema.** Everything the exec refuses — a crash-loop marker, a
  checksum that does not match — ends with this older binary carrying on and
  opening the database, and `Migrate` reverts what it does not recognise. A
  release that migrated and then crashed before serving would therefore have
  its columns dropped by the very guard that was protecting the server from
  it. So a start that has refused a staged upgrade and finds migrations it
  does not know now stops instead, and says which, where, and the two ways
  out. An ordinary downgrade — nothing staged — still reverts as documented.

- **`teanode config init` and `config import` migrate too.** Both are run with
  `docker compose exec` against a live container, and both would have let the
  image's older binary revert a staged upgrade's schema under a running
  server. They reach past it the same way a start does, without spending the
  crash-loop mark: a command that runs and exits proves nothing about whether
  a release can serve.

- **`git describe` makes every development build look like a candidate.**
  `VERSION ?= $(shell git describe --tags)` produces `0.1.2-9-g6a8860b`, whose
  prerelease field sorts it below the tag it came after. Plain semantic
  versioning therefore calls `0.1.2` an upgrade from it — so a build from a
  checkout would have offered, and with automatic upgrades on installed, the
  release it was already ahead of. The literal `0.0.0-dev` guard did not
  cover it.

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

- **The two refusals were both wrong, and both were removed.** They are kept
  here rather than deleted because the reasoning that produced them is the
  interesting part.

  "It cannot restart itself" came from reading `internal/api.Restarter`, which
  exits and waits for a supervisor, and treating that as the only way. `exec`
  is the other way: same process, new image, no supervisor. So there is no
  unsupervised deployment to refuse — the server run from a shell upgrades
  itself like any other.

  "It cannot upgrade a container" came from the image being read-only, which
  is true and is not the question. The question is where the next start looks,
  and a volume is somewhere both the running container and the next one can
  see. So a container stages there and execs it at startup, and an upgrade
  survives a recreate.

  Both refusals had passed review, been documented, and been asserted by the
  deployment test. What removed them was somebody saying "there is a way to do
  this" — twice.

- **A container never writes over its own executable, even when it can.** The
  overlay layer a container writes to is writable, so the obvious rule —
  replace in place when the directory allows it — silently produces the worst
  outcome for any container running as root: the upgrade works, reports
  success, and is gone at the next `docker compose up`. Being in a container
  decides this, not the permissions.

- **Where a staged binary lives comes from the environment, not the
  configuration.** Every other path this server uses is a setting in the
  database. This one cannot be: the staged binary has to be found and run
  before anything opens the database, because this program reverts migrations
  it does not recognise and an old binary that reached the database first
  would revert the new one's schema — and the data in those columns — seconds
  before handing over to it. So `TEANODE_UPGRADE_DIRECTORY`, read from the
  environment, defaulting to `upgrade` under the data directory the
  environment names, and absent means a deployment that cannot write over its
  own binary is told it cannot upgrade itself.

- **A staged binary is checked before it is run, and the check is not a
  signature.** It must be newer than the running one, it must match the
  checksum recorded when it was staged, and neither it nor its directory may
  be writable by anybody else. That catches a truncated write, a backup
  restoring last month's binary, and another container sharing the volume. It
  does not catch a host root, and nothing here could: the staging directory is
  a bind mount, and an operator who lets something else write into it has
  given it the server. Said plainly in the code and in the reference.

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

1. Refuse when there is nowhere to write: not beside the running binary, and
   no staging directory either.
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

### Milestone seven: the container and the supervisor, reconsidered

Both refusals go. Exec replaces the process; a container stages onto its
volume. `TEANODE_UPGRADE_DIRECTORY` names where, read before the database is
opened. The deployment test asserts the opposite of what it asserted in
milestone six: the container can upgrade itself, and the directory it would
write into exists on the volume and is private to the user the server runs as.

### Milestone eight: the dashboard as one place

The version card gains the release link and the notes, so that "should I?" is
answerable without leaving the page. The rail gains two marks: a refresh button
when the loaded bundle is older than the server it is talking to, and a dot on
Server when a release is waiting. And Setup, Integrations and Server — three
rows for one subject — become tabs of one `/server`.

## Idempotence and Recovery

An interrupted download leaves a temporary file and nothing else. A failed
verification leaves the running binary untouched. A swap that succeeds and a
restart that does not is the one bad case, and it is why the previous binary is
kept next to the new one under a name somebody can guess.

Staging has its own two. A staged binary that crashes on startup would be
exec'd again at every start, for ever, so a marker is written before it is run
and cleared only once the new process is serving: the second start finds the
marker, says so, and runs the binary from the image instead. And a staged
binary that the image has since overtaken — the operator pulled a newer
image — is deleted rather than run, because the alternative is a deployment
that silently goes backwards on every restart.
