# Unified iPhone device setup execution plan

## Purpose and user-visible behavior

Account settings, device sync and Mail programs open the same native service selector.
The person chooses available Mail, Calendar, Reminders and Contacts before creating a
one-use installation profile. Calendar, Reminders and Contacts work without a mailbox.
Mail uses a mailbox app password; collections use an account sync credential. The
profile combines those existing credential boundaries without displaying secrets.

## Progress

- [x] Inspect existing sync/password/profile components and shared backend contract.
- [x] Isolate this work from the recording increment.
- [x] Add portable capability/profile contracts and failure/selection tests.
- [x] Add a native selector and prominent shared entry points, retaining manual setup.
- [ ] Review code, run schema/package/lint checks and fix findings.
- [ ] Open a draft implementation PR and inspect actual simulator captures in light,
  dark and largest text. Report installation checks separately.

## Implementation and contracts

The backend owns GetDeviceSetupCapabilities and CreateDeviceSetupProfile. The native
client consumes owned usable mailboxes and explicit service flags. Generic account
setup initially selects available collections and leaves Mail off; a mailbox shortcut
selects only its owned mailbox plus available collections. Unavailable services are
omitted and a failed refresh cannot silently broaden the selected permissions.

A common DeviceSetupView uses the existing Form, Toggle, Picker, NavigationLink and
system profile-download behavior. Creation returns a short-lived link and separate
revocation metadata. Repeated taps are disabled while the request is running. The
ready profile can be shared through the system share sheet or opened in Safari, then the person installs it under Profile Downloaded in
Settings. An app cannot install a configuration profile itself. Existing manual mail
and sync password controls remain secondary, with existing separate revocation APIs.

Owned files are native API/selection definitions, the common view, account/sync/mail
entry points, translations, portable tests and demo/screenshot routes. Backend,
web, CLI and tool changes stay with their respective workstreams.

## Validation and acceptance

Portable tests use fake responses and invented fixtures: unavailable scopes and
foreign mailbox choices must fail before a mutation; mailbox-free requests send no
mailbox ID; errors remain errors; profile receipts cannot substitute a foreign link;
mailbox and sync revocation identities stay separate. Validate native GraphQL against
the real built schema. Run the Swift package suite and full project lint, including
local naming and secret checks. Review before pushing.

macOS CI must build the app, widget and share targets and capture service selection,
mailbox-free selection and installation guidance in light, dark and largest text.
Inspect actual images and fix layout findings. Simulator build/capture evidence does
not prove signed device profile installation or native Mail/DAV sync. Physical-device
installation, selected collection access and separate revocation remain explicit
validation gaps until evidence exists.

## Coordination, recovery and status

This increment depends on the shared device-setup backend contract and does not
change existing APIs. A failed profile request retains the choices and displays the
server error. Expired links cannot be downloaded; retain the receipt and its separate password-management links. A new credential pair is never created implicitly.
Parent integration owns merge and deployment after review and required checks.
Recording work remains on its own branch. No device installation or deployed outcome
is currently claimed.

Portable package validation passed 291 tests, including 5 focused device setup tests.
The actual app-document/schema check passed against the shared backend contract.
Native compilation, screenshots and signed-device installation remain pending.

Review retained the existing sync-profile sharing option. Creation shows installation
guidance and the primary Download action without automatically consuming the one-use
link; the system Share action remains available until download starts.

The first native SDK run rejected a titled Section initializer combined with a
footer. Both service-selection and installation sections now use the existing
content/header/footer initializer. Fresh SDK validation is pending.

The corrected native Section forms built successfully with the real simulator SDK.
Fifteen actual setup images were inspected across light, dark and largest text;
selection and receipt content remained readable. The largest-text Download label
was shortened to reduce wrapping; the system still hyphenates it at the largest size. No physical-device installation is claimed.

Independent review found expired receipt removal, receipt hiding when all services
turn off, and stale unavailable selections. Receipt rendering now precedes capability
checks and retains the returned mailbox/sync identities; expired links
are disabled without creating new passwords. An issued unexpired profile keeps its
existing secure download behavior when services are later disabled. Capability
refresh prunes unavailable choices only before creation without selecting newly available services. Two regressions bring the portable
suite to 293 passing tests. Fresh SDK and expired/unavailable receipt captures remain
pending for the updated code.

The next exact SDK run and all 21 setup captures passed and were visually inspected.
The expired receipt still displayed an upcoming-expiry label, which is now hidden
when expiration passes. Independent Connection review also found that an expired
issued response could be discarded before publishing its credential receipt. URL
origin/path and exact scopes remain validated at creation, while download alone
rejects expiry; the receipt is retained. A Connection-level regression exercises
that response, not just DTO decoding. Fresh exact-head checks remain required.

The late-response Connection regression passes with the full 294-test portable
suite. Independent rereview found no remaining confirmed issue in the response
validation and retained receipt changes. Fresh SDK and screenshot evidence remains
pending for this final focused fix.

The late-response build and portable checks passed, but the first ready-profile
simulator capture rendered blank and correctly failed its content assertion. The
preceding nine selector images passed. The expiry date now uses the existing static
LabeledContent date/time pattern outside the action timer; the timer still prevents
expired downloads and sharing. Screenshot failures now retain app runtime logs to
distinguish a rendering failure from an initial launch problem. Fresh exact-head
SDK/capture verification remains required; the blank screen is not marked resolved
without that evidence.
