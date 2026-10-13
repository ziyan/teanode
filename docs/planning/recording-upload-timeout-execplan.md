# Recording import and resume request timeout

This follows the [playback timeout plan](recording-playback-timeout-execplan.md)
and [meeting recording plan](meeting-recording-execplan.md). Reuse the existing
bounded CLI media allowance for fresh import and explicit resume, without
changing shared client defaults, upload protocol, native behavior or backend
limits.

## Confirmed behavior

The CLI import entry opens the normal client. Neither ImportRecordingFile nor
ResumeRecordingFile changes its one-minute total HTTP deadline. An upload part
is bounded to 8 MiB; a valid slowly transferred part can exceed one minute while
remaining within the server's existing five-minute request-body read bound.
A controlled actual CLI upload reproduced that timeout. It retained the local
source and did not claim whole-file completion after losing the part response.

## Work and validation

- [x] Trace import/resume and shared client timeout handling.
- [x] Confirm a slow 8 MiB part exceeds the prior actual CLI deadline.
- [x] Reuse the existing eleven-minute per-request CLI media allowance.
- [x] Check actual slow fresh import and explicit resume with matching receipts.
- [x] Check unknown-response recovery, mismatch refusal and read-only behavior.
- [x] Review, run full naming lint, focused races and build the actual CLI.
- [ ] Open the separate draft increment and verify exact-head CI.

The allowance applies to this command's connection, including its manifest,
part and completion requests. Ordinary standalone metadata commands keep their
normal default. Eleven minutes is still a total per-request ceiling, not an
unbounded whole upload. The server's request-body limit and caller cancellation
remain effective; no configured limit, quota, permission or receipt is bypassed.

Use a synthetic loopback receiver that slowly reads an 8 MiB part, then validate
fresh import and resume through the actual rebuilt CLI. A deliberately lost
part response must remain unknown to the caller; retry must read the stored
matching receipt and skip that part. A mismatched receipt must stop completion.
Read-only initial import must make no transport call. Every outcome must retain
the unchanged original. Existing source/manifest/receipt and bounded-memory
regressions run under the race detector instead of adding a getter-only timeout
test or permanent minute-long CI fixture.

Native URLSession has different idle-timeout semantics; no native change is
established by this Go total-timeout finding. No web or LLM capability is added.
This stream makes no production call or deployment. External proxy limits are
not verified and a longer CLI allowance cannot override shorter inherited
contexts or server connection limits.

## Current evidence

Actual rebuilt CLI fresh import and explicit resume completed controlled slow
8 MiB transfers beyond the old deadline with verified whole-file receipts. A
deliberately lost part response stayed unknown; stable retry skipped its stored
matching receipt and sent no duplicate part. Mismatched receipts refused
completion; read-only initial import made no transport call. The unchanged
source was retained. Full lint reports zero naming issues; focused client, CLI
and recording-tool race tests and the CLI build pass. No production or provider
call was made by these checks.
