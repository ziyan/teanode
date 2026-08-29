# Publish TeaNode: a clean history, and an audit that says why

## Purpose / Big Picture

This repository has been private since 2021 and is about to be public. Two
things stand in the way, and they are different in kind.

The first is the history. Five hundred and seventy-seven commits carry the
closed-source service this program grew out of: personal addresses, real
recipients in `.eml` fixtures, bounce addresses under domains that belong to
somebody, and a DKIM key from a 2021 unit test. None of it is reachable from
the current tree — the restructure deleted all of it — but every byte is still
in `.git`, and publishing a repository publishes its history. The fix was
decided when `scripts/check-secrets.bash` was written, whose own comment says
"the published history is a single fresh commit": the public repository starts
at one commit, and the old history stays private.

The second is everything the tree still says about the machine it runs on.
A published repository is read by people who will run this on their own
hardware, and a document describing the owner's gateway, their CDN and their
port forwards teaches them nothing and tells a stranger the shape of one
particular deployment. `make check-secrets` already catches hostnames and
keys. It cannot catch a paragraph.

This plan does the audit, fixes what it finds, resets the history and
publishes.

## Progress

- [x] (2026-09-03) Audited the history: every blob that has ever been
      committed, scanned for private keys, AWS credentials and personal
      addresses. Findings under `Surprises & Discoveries`.
- [x] (2026-09-03) Audited the tree: `make check-secrets` passes; the leaks
      that remain are prose, not patterns.
- [x] (2026-09-03) Settled the two questions with the owner. See `Decision Log`.
- [ ] Milestone one: the tree tells the truth.
- [ ] Milestone two: nothing in the tree is about one deployment.
- [ ] Milestone three: one commit.
- [ ] Milestone four: published, and proved from a fresh clone.

## Surprises & Discoveries

- **No production key was ever committed.** The only PEM block with real bytes
  in the whole history is `testPrivateKeyPEM` in a 2021 DKIM unit test. Every
  other match is the phrase in code that checks for the phrase. This is the
  best possible answer and it was not the expected one — the working
  assumption was that something would need rotating.

- **What the history does carry is people.** Personal addresses at consumer
  providers, in old forwarder code and in `.eml` test fixtures captured from
  real mail. Sixteen blobs. That alone settles the history question: those
  addresses belong to people who did not agree to be in a public repository.

- **The `mail_servers` column has never been written.** Found while auditing,
  fixed in `67d5501`: the reading half of the domain row mapping had the
  field and the writing half did not. Worth recording here because it is the
  shape of thing an audit is for — nothing failed, nothing logged, and the
  live deployment looked correct because it was deriving the same names by
  accident.

- **The security review has gone stale in the right direction.** SEC-5 says
  the ACME retry loop has no ceiling. It has had per-certificate exponential
  backoff, five minutes doubling to a day, since `2026-09-02`. A published
  review that overstates a defect is as wrong as one that hides it.

- **The changelog stops before today.** Everything since the compose merge —
  pictures in mail, opens, the picture host, three API fixes — is absent, on a
  project whose CI refuses a pull request that changes code without touching
  `CHANGELOG.md`. The guard only runs on pull requests; this work went
  straight to `master`.

## Decision Log

- **The old history is kept, not deleted, and not force-pushed over.**
  `ziyan/teanode` is renamed to `ziyan/teanode-private` — it keeps its
  history, its issues and its URL, and stays private. The public repository is
  created fresh at `ziyan/teanode` and receives one commit. Nothing is
  destroyed at any point, which is worth more than the tidiness of reusing the
  name in place.

- **Five of the six ExecPlans are published; `20260818-production-parity.md`
  is not.** It exists to prove one specific deployment could be replaced by
  this program, and every line of it is about that deployment. It moves to the
  server, next to `DEPLOYMENT.md`, where the rest of that story already lives.

- **The security review is published, open findings and all.** A mail server
  that says "two hardening items are open, here they are" is more trustworthy
  than one that says nothing. SEC-5 is corrected first, because it is fixed.

## Context and Orientation

The audit was run over `git rev-list --objects --all`, every blob, excluding
vendored code and binaries, against the same patterns `check-secrets.bash`
uses: PEM private keys, AWS access key ids, AWS credential files, and personal
addresses at consumer mail providers. Thirty-one blobs matched. Fifteen are
`scripts/test-deployment.bash` and `check-secrets.bash` matching their own
patterns, three are the AWS SDK's own source in `vendor/`, and the rest are
listed above.

The current tree passes every guard the project has: `make check-secrets`,
`make check-catalogs`, `make check-config-docs`, `make lint-ci`, 619 unit
tests, and an 84-check deployment test against a real stack in Docker.

## Plan of Work

### Milestone one: the tree tells the truth

The three places where the published tree would say something untrue.

- `CHANGELOG.md` gains the work since the compose merge, in the same voice as
  the rest: pictures in a template and the media store behind them; the
  per-message address and what an open is worth; the picture host; and the
  three fixes — the partial domain update the schema refused, the mail server
  names that were never stored, and the per-message address a CDN could cache.
- `docs/security/security-review.md`: SEC-5 becomes fixed, naming the backoff
  and where it lives. Its "see the cutover audit" reference goes, because that
  document is not published.
- `docs/planning/active/20260818-open-source-restructure.md`: milestones six
  to ten are done and the progress list still says otherwise.

Acceptance: `make lint-ci` passes; the changelog's `[Unreleased]` describes
every feature a reader of the tree can find.

### Milestone two: nothing in the tree is about one deployment

- `20260818-production-parity.md` leaves the repository for the server.
- `20260903-media-and-opens.md` loses the paragraphs naming a gateway, its
  controller's certificate and a CDN. What stays is the thing a reader needs:
  where mail arrives and where HTTPS answers are different questions, and
  `linkHost` is the answer.
- A sweep for the same shape of leak in the other four plans and in `docs/`.

Acceptance: no tracked file names a device, a CDN, a port forward or an
address belonging to the owner's network.

### Milestone three: one commit

- Rename the GitHub repository to `ziyan/teanode-private`. The history, the
  issues and every old clone URL keep working; it stays private.
- `git bundle create` the whole thing to a file outside the repository as well,
  because one copy is not a backup.
- `git checkout --orphan`, `git add -A`, one commit, `git branch -M master`.
  `.gitignore` already excludes `.claude/`, `dev/` and `deploy/test/`, which is
  what makes `git add -A` safe here — but the staged list is read before the
  commit, by a person, because a previous session nearly committed a directory
  of private keys with exactly this command.
- `git gc --prune=now --aggressive`, and the repository stops being 122 MB.

Acceptance: `git log --oneline` is one line; `du -sh .git` is single-digit
megabytes; `git rev-list --objects --all | wc -l` is the tree and nothing else.

### Milestone four: published, and proved from a fresh clone

- Create `ziyan/teanode` public, push, set the description and topics.
- Clone it into a clean directory and run `make lint-ci`, `make test` and
  `make test-deployment` there. Not because they passed here — because a
  fresh clone is what a stranger gets, and the deployment test builds the
  image from the tree.

Acceptance: a clone of the public URL builds, tests, and brings the stack up
end to end.

## Idempotence and Recovery

Everything before milestone three is ordinary editing. Milestone three is the
irreversible step, and it is arranged so that it is not: the old history is on
GitHub under a different name and in a bundle on disk before the orphan branch
is made, and the orphan branch is made in this working copy, so `git checkout
master` gets everything back until the branch is moved.

The published repository is new. There is no force push anywhere in this plan.
