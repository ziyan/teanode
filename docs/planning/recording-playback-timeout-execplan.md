# Recording playback request timeout

This narrow execution plan follows the [meeting recording plan](meeting-recording-execplan.md)
and [playback surface plan](recording-playback-surfaces.md). It changes the CLI
request allowance, without changing backend decoding, credentials, permissions,
provider calls or storage policy.

## Confirmed behavior

The Go client defaults to one minute for the entire HTTP request. Standalone
playback preparation and recording downloads previously used that default.
Preparation runs synchronously under the HTTP request context, with a ten-minute
backend ceiling. The original is copied and decoded before a retained preview
is reserved and published. A disconnected request cancels descriptor readers;
it cannot continue partial decoding through a later read or retry.

A controlled loopback GraphQL response delayed beyond one minute reproduced
client timeout and server request cancellation. The same delayed response
completed with the existing extended-request pattern. This is transport evidence,
not a claim that any particular recording takes that long to decode.

## Implementation

- [x] Trace CLI, web, native and in-process tool timeout/cancellation paths.
- [x] Demonstrate the default timeout and parent-context cancellation locally.
- [x] Use existing SetTimeout for standalone preparation and recording downloads.
- [x] Verify actual CLI preparation, preview download and slow original transfer.
- [x] Review the focused change and run required lint/race/build checks.
- [ ] Open the separate draft increment and verify exact-head CI.

Allow eleven minutes per request for these CLI paths, giving the backend's
existing ten-minute preparation ceiling room to return its response. Metadata
operations retain normal defaults. Download and explicit preparation preserve
owner checks, read-only refusal, matching receipts, bounded hashing and atomic
file publication. Download still has a finite total request limit.

Native preparation receives its own per-request allowance in the native stream.
Web fetch has no explicit request timeout here, so no web change is justified.
Built-in LLM recording calls use the turn context through the in-process API;
the configured model request timeout bounds provider calls rather than these
tool calls. Existing turn/job/user cancellation remains effective. No configured
safeguard is bypassed and this change does not make preparation a durable job.

## Validation and limits

Run a controlled delayed server with synthetic response data and execute the
actual CLI standalone preparation, explicit preview download and slow original
transfer beyond the former one-minute bound. Assert matching downloaded bytes
and read-only preparation refusal before transport. Reuse existing receipt,
ownership and destination-integrity regressions under the race detector. Run
full naming lint and build the actual CLI. Review the small timeout diff against
existing long-request commands; no getter-only test or slow permanent test is
needed to repeat the controlled transport evidence.

No production call or deployment belongs to this stream. Reverse proxies may
apply shorter timeouts; no particular deployed proxy policy has been verified.
A longer client timeout cannot override a shorter inherited server context or
proxy connection lifetime. Physical iPhone behavior remains separately reported.

## Current evidence

The actual built CLI completed standalone preparation, explicit preview
preparation/download and a slowly streamed original beyond the previous request
bound. Both downloaded files matched their synthetic receipts. Read-only
preparation made zero transport calls. Full lint reports zero naming issues;
client, CLI and recording-tool race tests and the CLI build pass. These checks
used only loopback fixtures, with no provider or production access.
