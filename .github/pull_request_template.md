## What this changes

<!-- What an operator would notice, or "nothing observable" for a refactor. -->

## Why

<!-- The reason, not the diff. If it fixes a bug, what went wrong. -->

## Changelog

<!--
This block becomes the release notes, so write it for whoever reads them: an
operator with a mail server to look after, months from now, working out whether
to upgrade. Say what changed for them, not what changed in the code.

Keep the heading that fits and delete the rest:

  ### Added       something new (a minor release)
  ### Changed     existing behaviour is different (a patch)
  ### Deprecated  still there, going away (a patch)
  ### Removed     gone, and an upgrade may break somebody (a minor release)
  ### Fixed       something that was wrong is not any more (a patch)
  ### Security    a fix somebody should hurry to take (a patch)

A change nobody outside this repository can observe — a refactor, a test, a
comment — needs no entry. Label the pull request `no changelog` and leave this
block as it came.
-->

### Added | Changed | Deprecated | Removed | Fixed | Security

- TODO: replace with what changed, for somebody who has to decide about it.

## How it was checked

<!-- What you ran, and what you saw. "The tests pass" is worth less here than
     the one thing you looked at to be sure. -->

- [ ] `make` passes: formatted, builds, tests
- [ ] `make lint` passes, including mulint if installed
- [ ] A decision record in `docs/decisions/` if this makes a choice worth explaining later
