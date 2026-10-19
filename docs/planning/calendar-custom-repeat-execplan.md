# Custom repeats in the calendar

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md`.

## Purpose / Big Picture

A person can make an event repeat only every day, week, month or year today, on the dashboard and on the iPhone. After this change they can also say how it repeats in detail, as the iPhone's own Calendar does with Custom: every N days, weeks, months or years; on chosen days of the week (Monday to Friday); in a month on its day number or on "the second Tuesday"; and ending never, on a date, or after a number of times. "Every weekday" is offered as one choice of its own, since it is the one people want most. An event with a rule set elsewhere (another calendar program, the agent) opens in the same editor with its parts filled in, instead of the "As it was set" placeholder both editors show today.

To see it working: open an event on the dashboard or the iPhone, choose Repeat, then Custom, tick Monday to Friday, save; the calendar shows it on each weekday, and opening it again shows the same choices and the sentence "Repeats every week on Monday, Tuesday, Wednesday, Thursday and Friday".

## Progress

- [ ] Milestone 1: the rule, read and written, on both clients, with tests.
- [ ] Milestone 2: the dashboard editor.
- [ ] Milestone 3: the iPhone editor, its demo and screenshots.
- [ ] Docs and the agent tool's description; review until a round finds nothing.

## Surprises & Discoveries

None yet.

## Decision Log

- Decision: no server change. The server already takes any recurrence rule as text (`recurrence` on the calendar event mutations in `internal/api/v1api/apigraph/calendar.go`, written by `internal/calendar/build.go`, expanded with `github.com/teambition/rrule-go` in `internal/calendar/occurrence.go`), and the agent tool (`internal/agent/tools/calendar/calendar.go`, argument `repeat`) and the CLI (`teanode calendar ... --repeat`) take the same text. Only the two editors need to build and read the rule. Date: 2026-10-18.
- Decision: "Every weekday" is a preset beside Every day / week / month / year, and Custom opens the detailed editor. Rationale: the person's example is Monday to Friday.
- Decision: the parts a rule may have in the editor are FREQ, INTERVAL, BYDAY (weekly: plain days; monthly: one day with an ordinal such as 2TU or -1FR), BYMONTHDAY (monthly, one day), COUNT and UNTIL. A rule with anything else (BYSETPOS, BYMONTH, several month days, BYHOUR, WKST) still shows its sentence when it can, but is kept unchanged unless the person changes it, and the editor says it was set elsewhere. Rationale: never lose a rule the person meant.
- Decision: UNTIL is written as a date-time in UTC (`YYYYMMDDTHHMMSSZ`, the end of the chosen day in the event's time zone) for a timed event and as a date (`YYYYMMDD`) for an all-day event, as RFC 5545 requires.

## Outcomes & Retrospective

To be written when the work is done.

## Context and Orientation

A recurrence rule ("rule") is the RFC 5545 RRULE text without its property name, for example `FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20261231T235959Z`. The event's own start date and time are the first occurrence; the rule says how the rest follow.

The dashboard is React and TypeScript in `web/`. The event editor is in `web/src/pages/calendar.tsx`: `REPEATS` (near line 142) is the list of named repeats, the Repeat `<select>` near line 1500 shows them and an "other" option for any other rule (`calendar.repeatOther`), and the draft's `recurrence` string is sent as `recurrence` on save (near line 1385). Strings are in `web/src/i18n/en.ts`, `zh.ts` and `ja.ts` (the catalog test fails when a key is missing in one). Dashboard rules for components and layout are in `docs/coding/frontend-design.md`: read it; reuse the existing dialog, field and button components.

The iPhone app is SwiftUI in `ios/TeaNode/Sources/`. The event editor is in `CalendarView.swift` near line 1300 (`Picker("Repeat", selection: $draft.recurrence)` with tags `FREQ=DAILY`...), and `EventDraft` in `CalendarModel.swift` (near line 365) holds `recurrence` and `static let repeats`. Rules for screens are in `ios/DESIGN.md`: read it (system components, a pushed screen of rows like the iPhone Calendar's Custom screen, symbols the color of their words, every string in `ios/TeaNode/Resources/Localizable.xcstrings` in English, Simplified Chinese and Japanese). The debug demo server `ios/TeaNode/Sources/DemoServer.swift` answers the app's requests in screenshots; CI's `shoot` lines in `.github/workflows/ios.yml` take them (look for the calendar ones, `calendar-sheet` and similar, and add `calendar-repeat-custom` in light, dark and largest text).

There is no Swift toolchain on this machine for the app. Code that does not touch UIKit or SwiftUI can live in TeaNodeKit (`ios/TeaNodeKit/Sources/TeaNodeKit/`) and be tested on Linux in Docker (see Concrete Steps); put the rule parsing and writing there, with XCTest tests, so that it is compiled and tested here. Avoid in app code: a ternary mixing nil with a closure or method reference inside a view body, `==` on SwiftUI types that are not Equatable, very long view expressions.

## Plan of Work

Milestone 1, the rule. On each client add one small module that turns rule text into editor parts and back: frequency (daily, weekly, monthly, yearly), interval (1 or more), weekdays (a set, for weekly), the monthly choice (by day number or by an ordinal weekday such as the second Tuesday or the last Friday), the end (never, on a date, after a count), and whether the rule had parts the editor does not show (then it is kept as is unless changed). It also writes the sentence that describes a rule, such as "Repeats every 2 weeks on Monday, Wednesday and Friday, until 31 December 2026", in the person's language, with day and month names from the platform's formatters, not hand-written lists. TypeScript: `web/src/pages/calendarRepeat.ts` with `calendarRepeat.test.ts` (vitest). Swift: `ios/TeaNodeKit/Sources/TeaNodeKit/CalendarRepeat.swift` with `ios/TeaNodeKit/Tests/TeaNodeKitTests/CalendarRepeatTests.swift`. Both test the same table of rules both ways (every preset, weekdays, every 2 weeks on MO,WE,FR, monthly on day 15, monthly on 2TU, monthly on -1FR, yearly, COUNT=10, UNTIL for timed and all-day, a rule with BYSETPOS kept as is, lower-case input, an empty rule).

Milestone 2, the dashboard. Add "Every weekday" and "Custom…" to the Repeat select. Custom opens a dialog (the page's existing dialog component) with Frequency, Every (a number and the unit word), the weekday toggles for weekly (seven buttons in the week's order of the person's locale, each a toggle button with a pressed state, not checkboxes), the monthly choice, and End (never / on a date / after N times), with the sentence under it, live. Saving the dialog sets the draft's recurrence. The select shows "Custom" with the sentence beside it whenever the rule is not one of the presets; the old `calendar.repeatOther` and the raw rule in monospace go.

Milestone 3, the iPhone. Add "Every weekday" and "Custom" to the Repeat picker. Custom pushes a screen like the iPhone Calendar's (the person's reference): a section with Frequency (a menu picker) and Every (a stepper row: "1 week"), the sentence as that section's footer, then for weekly a section of the seven days as rows with a checkmark (the system's trailing checkmark, tint colored, as the Calendar shows), for monthly "Each day 15" versus "On the second Tuesday" (two pickers), and an End Repeat section (Never, On date with a DatePicker, After with a stepper of times). The Repeat row shows the sentence's short form when custom. Demo data: a demo event with `FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR`, and a `-TeaNodeScreen calendar-repeat-custom` that opens the Custom screen for it.

Docs and parity. `docs/reference/command-line.md` (the `--repeat` flag) and the agent tool's `repeat` description get two examples (weekdays, every 2 weeks until a date) so a person and the agent write the same rules the editors do. The docs page for the calendar under `docs/subsystems/` (find it) says the editors offer Custom.

## Concrete Steps

Work in your own clone under `~/.cache/claude-tmp` on branch `feat/calendar-custom-repeat` (it is already pushed with this plan). Checks, from the clone root:

    cd web && npm ci && npx tsc --noEmit -p . && npx vitest run src/pages/calendarRepeat.test.ts src/pages/calendar && npx eslint --max-warnings 0 src/pages/calendar.tsx src/pages/calendarRepeat.ts && cd ..
    git archive HEAD ios/TeaNodeKit | tar -x -C <dir>    (after committing, or copy the working tree's ios/TeaNodeKit)
    cd <dir>/ios/TeaNodeKit && docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD":/kit -w /kit swift:6.1 bash -c 'swift build --build-tests -q && swift test --skip-build --filter CalendarRepeat'
    node ios/scripts/check-button-colors.mjs
    make lint-ci

## Validation and Acceptance

Vitest and the TeaNodeKit tests pass the shared table of rules both ways. On the dashboard (run `npm run build`; the person checks the deployed page), choosing Custom, ticking Monday to Friday and saving stores `FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR`, and the calendar shows the event on the weekdays. On the iPhone, CI's `calendar-repeat-custom` screenshots show the Custom screen with Monday to Friday checked and the sentence in light, dark and largest text, and the app compiles (CI's App on macOS).

## Idempotence and Recovery

No migration and no server change; reverting the commits returns both editors to the five presets. A rule written by the editor is ordinary RRULE text that every other calendar program reads.

## Interfaces and Dependencies

No new libraries. New: `web/src/pages/calendarRepeat.ts` (parse, write, describe) and `TeaNodeKit.CalendarRepeat` (the same in Swift), both pure functions of the rule text, the event's start, its time zone and whether it is all day.
