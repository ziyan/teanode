# Recordings are passive knowledge sources

- Status: accepted
- Date: 2026-10-12
- Deciders: project maintainers

## Context

Voice conversations already turn speech into commands. Meeting capture must instead
preserve media and attributed evidence, survive outages and learn through existing
knowledge and dreaming machinery without asking for learning approval.

## Decision

Use an optional transcription capability on existing provider clients and registry.
Require explicit speaker/reference capabilities and preserve normalized segments
plus raw usage. Keep original media in existing storage, version transcripts and
store user corrections separately. Provider speaker labels are request-local;
only user-confirmed identities can seed automatically curated voice references.

Recording/transcript versions become knowledge sources. Corrections and deletion
reconcile dependent memories through existing evidence and revision mechanisms.
Recorded words never enter the user-turn or tool-authorization path.

## Consequences

A provider may charge for a full reprocessing pass. Model replacement needs
capability validation, not a name substitution. Device background capture and
actual billing require observed validation before being claimed as supported.

Plan: [meeting recording](../planning/meeting-recording-execplan.md).
