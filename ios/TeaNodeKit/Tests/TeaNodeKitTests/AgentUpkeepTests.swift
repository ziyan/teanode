import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AgentUpkeepTests cover the agent page's upkeep tabs: reading the
/// server's answers, the rules the dashboard applies to them, and what the
/// calls send.
final class AgentUpkeepTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    // MARK: Sources

    func testReadsTheSourcesAndWhatBecameOfTheirFiles() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{
                  "ListAgentKnowledgeSources":[
                    {"id":"source-1","kind":"computer","name":"Projects",
                     "specification":{"type":"","settings":null,"computer":"laptop","path":"~/projects","format":"files","mailboxId":"","readEveryCheckout":false},
                     "rootPath":"projects","enabled":true,"cron":"","lastRunAt":"2030-01-02T03:04:05.123456789Z","lastError":"",
                     "documentCount":120,"chunkCount":900,"refusedCount":2,"more":true,"checkoutsKeptToProfile":3,"filesKeptToProfile":40},
                    {"id":"source-2","kind":"finance","name":"A bank","specification":{},"enabled":false}
                  ],
                  "ListAgentSourceAttachments":[{"sourceId":"source-1","undecided":4,"declined":1,"described":7}]
                }}
                """#)
        let details = try await connection.knowledgeSourceDetails()
        XCTAssertEqual(details.sources.map(\.id), ["source-1", "source-2"])
        let projects = details.sources[0]
        XCTAssertEqual(projects.specification.computer, "laptop")
        XCTAssertEqual(projects.documentCount, 120)
        XCTAssertTrue(projects.more)
        XCTAssertEqual(projects.shape, .files)
        XCTAssertFalse(projects.isFinance)
        XCTAssertEqual(projects.whereItReads(mailboxNames: [:]), "~/projects · laptop")
        XCTAssertTrue(details.sources[1].isFinance)
        XCTAssertFalse(details.sources[1].enabled)
        XCTAssertEqual(details.attachments.first?.declined, 1)
        XCTAssertTrue(details.attachments[0].hasAny)
    }

    func testAListOfOnlyIdsAndNamesStillReads() throws {
        let sources = try JSONDecoder().decode([KnowledgeSource].self, from: Data(#"[{"id":"source-1","name":"Notes"}]"#.utf8))
        XCTAssertEqual(sources[0].name, "Notes")
        XCTAssertEqual(sources[0].kind, "")
        XCTAssertTrue(sources[0].enabled)
        XCTAssertEqual(sources[0].documentCount, 0)
    }

    func testSaysWhereATypedSourceAndSentMailRead() {
        let typed = KnowledgeSource(
            id: "source-1", name: "Wiki", kind: "computer",
            specification: .init(type: "wiki", settings: .string(#"{"baseURL":"https://wiki.example.com","spaces":["team"],"isArchived":false,"limit":""}"#)))
        XCTAssertEqual(typed.whereItReads(mailboxNames: [:]), "Base URL: https://wiki.example.com · Spaces: team")
        let sent = KnowledgeSource(id: "source-2", name: "Sent", kind: "sent", specification: .init(mailboxId: "mailbox-1"))
        XCTAssertEqual(sent.shape, .sent)
        XCTAssertEqual(sent.whereItReads(mailboxNames: ["mailbox-1": "robin@example.com"]), "robin@example.com")
        let journal = KnowledgeSource(id: "source-3", name: "Notes", kind: "archive", specification: .init(format: "journal"))
        XCTAssertEqual(journal.shape, .journal)
        XCTAssertEqual(KnowledgeSource.Shape.records.kind, "archive")
        XCTAssertEqual(KnowledgeSource.Shape.sent.format, "")
    }

    func testOffersTheReadableTypesBuiltInReadersFirst() {
        let types = [
            KnowledgeSourceType(name: "tracker", reader: ""),
            KnowledgeSourceType(name: "broken", reader: "files", readable: false),
            KnowledgeSourceType(name: "web pages", reader: "web"),
            KnowledgeSourceType(name: "folder", reader: "files"),
            KnowledgeSourceType(name: "chat", reader: ""),
        ]
        XCTAssertEqual(KnowledgeSourceType.offered(types).map(\.name), ["folder", "web pages", "chat", "tracker"])
        XCTAssertTrue(types[0].isReadOnComputer)
        XCTAssertFalse(types[2].isReadOnComputer)
    }

    func testNamesSettingsAsLabels() {
        XCTAssertEqual(KnowledgeSourceType.label("maxAttachmentMB"), "Max attachment MB")
        XCTAssertEqual(KnowledgeSourceType.label("baseURL"), "Base URL")
        XCTAssertEqual(KnowledgeSourceType.label("apiKey"), "API key")
        XCTAssertEqual(KnowledgeSourceType.label("path"), "Path")
        XCTAssertEqual(KnowledgeSourceType.asSentence("the folder to read"), "The folder to read.")
        XCTAssertEqual(KnowledgeSourceType.asSentence("Already one."), "Already one.")
        XCTAssertEqual(KnowledgeSourceType.choices(in: "^(mine|public|all)$"), ["mine", "public", "all"])
        XCTAssertNil(KnowledgeSourceType.choices(in: "^[a-z]+$"))
        XCTAssertNil(KnowledgeSourceType.choices(in: "^(one)$"))
    }

    func testChecksAndSendsTheSettingsATypeDeclares() {
        let sourceType = KnowledgeSourceType(
            name: "tracker", reader: "",
            settings: [
                .init(name: "project", isRequired: true),
                .init(name: "limit", settingType: "integer", defaultValue: .number(50), minimum: 1, maximum: 500),
                .init(name: "labels", settingType: "array", itemPattern: "^[a-z]+$"),
                .init(name: "isClosedIncluded", settingType: "boolean", defaultValue: .bool(true)),
                .init(name: "state", pattern: "^(open|closed)$"),
            ])
        var draft = sourceType.draft(from: ["limit": .number(20), "labels": .array([.string("bug"), .string("idea")])])
        XCTAssertEqual(draft.texts["limit"], "20")
        XCTAssertEqual(draft.texts["labels"], "bug, idea")
        XCTAssertEqual(draft.switches["isClosedIncluded"], true)
        XCTAssertEqual(sourceType.firstProblem(in: draft)?.problem, .missing)
        XCTAssertNil(sourceType.firstProblem(in: draft, skipping: ["project"]))
        draft.texts["project"] = "website"
        draft.texts["limit"] = "900"
        XCTAssertEqual(sourceType.firstProblem(in: draft)?.problem, .tooLarge(500))
        draft.texts["limit"] = "ten"
        XCTAssertEqual(sourceType.firstProblem(in: draft)?.problem, .notWhole)
        draft.texts["limit"] = ""
        draft.texts["labels"] = "bug, Not Allowed"
        XCTAssertEqual(sourceType.firstProblem(in: draft)?.problem, .itemRefused("Not Allowed"))
        draft.texts["labels"] = "bug"
        draft.texts["state"] = "maybe"
        XCTAssertEqual(sourceType.firstProblem(in: draft)?.problem, .refused)
        draft.texts["state"] = "open"
        XCTAssertNil(sourceType.firstProblem(in: draft))
        draft.switches["isClosedIncluded"] = false
        XCTAssertEqual(
            sourceType.values(from: draft),
            ["project": .string("website"), "labels": .array([.string("bug")]), "isClosedIncluded": .bool(false), "state": .string("open")])
    }

    func testSavesASourceWithOnlyTheFieldsGiven() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SaveAgentKnowledgeSource":{"id":"source-9","name":"Notes"}}}"#)
        let sourceId = try await connection.saveKnowledgeSource(
            KnowledgeSourceChange(name: "Notes", computer: "laptop", rootPath: "notes", type: "folder", settings: ["path": .string("~/notes")]))
        XCTAssertEqual(sourceId, "source-9")
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(
            variables,
            [
                "name": .string("Notes"), "computer": .string("laptop"), "rootPath": .string("notes"), "type": .string("folder"),
                "settings": .object(["path": .string("~/notes")]),
            ])
    }

    func testGroupsDeclinedFilesUnderTheirReason() {
        let files = [
            DeclinedAttachment(documentId: "document-1", name: "a.png", declined: "A screenshot of a menu"),
            DeclinedAttachment(documentId: "document-2", name: "b.png", channel: "general", thread: "Plans", declined: "Too large"),
            DeclinedAttachment(documentId: "document-3", name: "c.png", declined: "A screenshot of a menu"),
        ]
        let groups = DeclinedAttachment.groupedByReason(files)
        XCTAssertEqual(groups.map(\.declinedReason), ["A screenshot of a menu", "Too large"])
        XCTAssertEqual(groups[0].files.map(\.documentId), ["document-1", "document-3"])
        XCTAssertEqual(files[1].placeText, "Plans · general")
    }

    // MARK: Dreams

    func testSaysWhatADreamDid() throws {
        let json = #"""
            {"id":"dream-1","jobId":"job-1","startedAt":"2030-01-02T01:00:00Z","finishedAt":"2030-01-02T01:42:20Z",
             "digested":12,"filed":3,"merged":0,"rewritten":0,"moved":0,"dormant":1,"embedded":0,"backlog":40,"coarse":false,
             "strengthened":0,"associated":0,"rehearsed":5,"gaps":0,"unknown":2,"revised":0,"overviewsWritten":0,
             "themesMade":0,"themesUpdated":0,"reflectionsWritten":1,"lastError":"","cost":0.42,"currency":"USD"}
            """#
        let dream = try JSONDecoder().decode(AgentDream.self, from: Data(json.utf8))
        XCTAssertEqual(
            dream.outcomes.map(\.outcomeKind), [.read, .filed, .retired, .rehearsed, .gaps, .unknown, .reflectionsWritten])
        XCTAssertEqual(dream.outcomes.first { $0.outcomeKind == .gaps }?.outcomeCount, 0)
        XCTAssertEqual(dream.minutesTaken, 42)
    }

    func testNeverSaysTheReadingIsDoneWhileSomethingWaits() {
        XCTAssertEqual(ReadingProgress(waiting: 1, read: 999).readPercent, 99)
        XCTAssertEqual(ReadingProgress(waiting: 0, read: 10).readPercent, 100)
        XCTAssertEqual(ReadingProgress(waiting: 0, read: 0).readPercent, 0)
        XCTAssertEqual(ReadingProgress(waiting: 300, read: 100).readPercent, 25)
        XCTAssertNil(ReadingProgress(waiting: 10, read: 0).timeLeft)
        XCTAssertEqual(ReadingProgress(waiting: 10, read: 0, perHour: 100, hoursLeft: 0.1).timeLeft, .minutes(6))
        XCTAssertEqual(ReadingProgress(waiting: 10, read: 0, perHour: 1, hoursLeft: 10.4).timeLeft, .hours(10))
        XCTAssertEqual(ReadingProgress(waiting: 10, read: 0, perHour: 1, hoursLeft: 100).timeLeft, .days(4))
    }

    func testAsksForBootstrappingAndRereading() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"DreamAgentNow":true,"RereadAgentDocuments":17}}"#)
        try await connection.setDreamBootstrap(true)
        let putBack = try await connection.rereadDocuments(minutes: 60)
        XCTAssertEqual(putBack, 17)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["on": .bool(true)])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["minutes": .number(60)])
    }

    func testChangesOnlyTheHoursGiven() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"UpdateAgent":{"agent":{"id":"agent-1","enabled":true,"operatorDisabledAt":null,"dreamFrom":"02:30","dreamUntil":"",
                  "dreamBootstrap":false,"isAlertsEnabled":true,"alertQuietStart":"","alertQuietEnd":"","alertDailyMost":0}}}}
                """#)
        let preferences = try await connection.updateAgentUpkeep(dreamFrom: "02:30")
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["dreamFrom": .string("02:30")])
        XCTAssertEqual(preferences?.shownDreamFrom, "02:30")
        XCTAssertEqual(preferences?.shownDreamUntil, "06:00")
        XCTAssertEqual(preferences?.shownAlertQuietStart, "22:00")
        XCTAssertEqual(preferences?.shownAlertDailyMost, 5)
        XCTAssertEqual(preferences?.shownLookAroundWeekday, "thursday")
        XCTAssertEqual(preferences?.shownLookAroundTime, "17:00")
        XCTAssertEqual(preferences?.isRunning, true)
        XCTAssertEqual(UpkeepClock.hourAndMinute("07:05")?.minute, 5)
        XCTAssertNil(UpkeepClock.hourAndMinute("25:00"))
        XCTAssertEqual(UpkeepClock.text(hour: 7, minute: 5), "07:05")
    }

    func testChangesTheLookAroundDayAndTime() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"UpdateAgent":{"agent":{"id":"agent-1","enabled":true,"operatorDisabledAt":null,"dreamFrom":"","dreamUntil":"",
                  "dreamBootstrap":false,"isAlertsEnabled":true,"alertQuietStart":"","alertQuietEnd":"","alertDailyMost":0,
                  "lookAroundWeekday":"friday","lookAroundTime":"18:30"}}}}
                """#)
        let preferences = try await connection.updateAgentUpkeep(lookAroundWeekday: "friday", lookAroundTime: "18:30")
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["lookAroundWeekday": .string("friday"), "lookAroundTime": .string("18:30")])
        XCTAssertEqual(preferences?.shownLookAroundWeekday, "friday")
        XCTAssertEqual(preferences?.shownLookAroundTime, "18:30")
        XCTAssertEqual(AgentUpkeepPreferences(id: "agent-1", lookAroundWeekday: "someday", lookAroundTime: "late").shownLookAroundWeekday, "thursday")
        XCTAssertEqual(AgentUpkeepPreferences(id: "agent-1", lookAroundWeekday: "someday", lookAroundTime: "late").shownLookAroundTime, "17:00")
    }

    // MARK: Schedules

    func testReadsAndWritesTheBriefsTimes() async throws {
        XCTAssertEqual(DailyBrief.time(fromCron: "5 8 * * 1-5"), "08:05")
        XCTAssertEqual(DailyBrief.time(fromCron: nil), "07:30")
        XCTAssertEqual(DailyBrief.dayNumbers(fromCron: "30 7 * * 6,0"), [6, 7])
        XCTAssertEqual(DailyBrief.dayNumbers(fromCron: "30 7 * * 1-5"), [1, 2, 3, 4, 5])
        XCTAssertEqual(DailyBrief.dayNumbers(fromCron: nil), [1, 2, 3, 4, 5])
        XCTAssertEqual(DailyBrief.dayNumbers(fromCron: "30 7 * * */2"), [1, 2, 3, 4, 5])
        let schedules = [AgentSchedule(id: "schedule-1", name: "Round-up", cron: "0 17 * * 5"), AgentSchedule(id: "schedule-2", name: "Daily brief", cron: "30 7 * * 1-5")]
        XCTAssertEqual(DailyBrief.schedule(in: schedules)?.id, "schedule-2")

        let (connection, fake) = connection(answering: #"{"data":{"SetAgentBrief":{"id":"schedule-2"}}}"#)
        try await connection.setDailyBrief(isEnabled: true, at: "07:30", days: [1, 2])
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["enabled": .bool(true), "at": .string("07:30"), "days": .array([.number(1), .number(2)])])
    }

    func testReadsTheDaysOfACronLineForTheDayToggles() {
        XCTAssertEqual(ScheduleWeekday.dayNumbers(fromCronField: "*"), [1, 2, 3, 4, 5, 6, 7])
        XCTAssertEqual(ScheduleWeekday.dayNumbers(fromCronField: "1-5"), [1, 2, 3, 4, 5])
        XCTAssertEqual(ScheduleWeekday.dayNumbers(fromCronField: "5,1,3"), [1, 3, 5])
        // Cron has Sunday as both 0 and 7; the server counts it as 7.
        XCTAssertEqual(ScheduleWeekday.dayNumbers(fromCronField: "0"), [7])
        XCTAssertEqual(ScheduleWeekday.dayNumbers(fromCronField: "0-2"), [1, 2, 7])
        XCTAssertEqual(ScheduleWeekday.dayNumbers(fromCronField: "6,7,0"), [6, 7])
        XCTAssertNil(ScheduleWeekday.dayNumbers(fromCronField: "*/2"))
        XCTAssertNil(ScheduleWeekday.dayNumbers(fromCronField: "5-1"))
        XCTAssertNil(ScheduleWeekday.dayNumbers(fromCronField: "8"))
        XCTAssertNil(ScheduleWeekday.dayNumbers(fromCronField: "1,"))
        XCTAssertNil(ScheduleWeekday.dayNumbers(fromCronField: "MON"))
    }

    func testOrdersAndNamesTheDaysAsTheLocaleDoes() {
        XCTAssertEqual(ScheduleWeekday.weekOrder(firstWeekday: 2), [1, 2, 3, 4, 5, 6, 7])
        XCTAssertEqual(ScheduleWeekday.weekOrder(firstWeekday: 1), [7, 1, 2, 3, 4, 5, 6])
        for dayNumber in 1...7 {
            XCTAssertEqual(ScheduleWeekday.dayNumber(fromCalendarWeekday: ScheduleWeekday.calendarWeekday(fromDayNumber: dayNumber)), dayNumber)
        }
        let english = Locale(identifier: "en_US")
        XCTAssertEqual(ScheduleWeekday.name(1, locale: english), "Monday")
        XCTAssertEqual(ScheduleWeekday.name(7, locale: english), "Sunday")
        XCTAssertEqual(ScheduleWeekday.shortName(5, locale: english), "Fri")
        XCTAssertNotEqual(ScheduleWeekday.name(5, locale: Locale(identifier: "ja_JP")), "Friday")
    }

    func testNamesSomeDaysForTheBriefsRow() {
        let english = Locale(identifier: "en_US")
        XCTAssertEqual(ScheduleWeekday.shortNamesText([1, 3, 5], locale: english), "Mon, Wed, and Fri")
        XCTAssertEqual(ScheduleWeekday.shortNamesText([5, 1], locale: english), "Mon and Fri")
        XCTAssertEqual(ScheduleWeekday.shortNamesText([7], locale: english), "Sun")
        XCTAssertEqual(ScheduleTiming.dayPattern(for: [1, 2, 3, 4, 5, 6, 7]), .everyDay)
        XCTAssertEqual(ScheduleTiming.dayPattern(for: [1, 2, 3, 4, 5]), .weekdays)
        XCTAssertEqual(ScheduleTiming.dayPattern(for: [6, 7]), .weekends)
        XCTAssertEqual(ScheduleTiming.dayPattern(for: [3]), .oneDay(3))
        XCTAssertEqual(ScheduleTiming.dayPattern(for: [1, 3]), .someDays([1, 3]))
        XCTAssertEqual(ScheduleTiming.dayPattern(for: []), .someDays([]))
    }

    func testDescribesAPlainCronLineAndLeavesTheRest() throws {
        let weekdays = try XCTUnwrap(ScheduleTiming(cron: "30 7 * * 1-5"))
        XCTAssertEqual(weekdays.dayPattern, .weekdays)
        XCTAssertEqual(weekdays.hour, 7)
        XCTAssertEqual(weekdays.minute, 30)
        XCTAssertEqual(ScheduleTiming(cron: "0 17 * * 5")?.dayPattern, .oneDay(5))
        XCTAssertEqual(ScheduleTiming(cron: "0 9 * * *")?.dayPattern, .everyDay)
        XCTAssertEqual(ScheduleTiming(cron: "0 9 * * 0")?.dayPattern, .oneDay(7))
        XCTAssertEqual(ScheduleTiming(cron: "0 9 * * 1,2,3,4,5,6,0")?.dayPattern, .everyDay)
        XCTAssertEqual(ScheduleTiming(cron: "0 10 * * 6,0")?.dayPattern, .weekends)
        XCTAssertEqual(ScheduleTiming(cron: "0 9 * * 1,3,5")?.dayPattern, .someDays([1, 3, 5]))
        for unplainCron in ["*/15 * * * *", "0 9 1 * *", "0 9 * 6 1", "0 9 * * 1-5/2", "@in 20m", "@at 2030-01-02 09:00", "60 9 * * *", "0 24 * * *", "0 +9 * * *", "0 9 * *"] {
            XCTAssertNil(ScheduleTiming(cron: unplainCron), unplainCron)
        }

        // A time the locale's way. Some locales put a narrow space before
        // AM, so spaces are compared as spaces.
        func spaced(_ text: String) -> String { text.replacingOccurrences(of: "\u{202F}", with: " ") }
        XCTAssertEqual(spaced(weekdays.timeText(locale: Locale(identifier: "en_US"))), "7:30 AM")
        XCTAssertEqual(spaced(ScheduleTiming(cron: "0 17 * * 5")?.timeText(locale: Locale(identifier: "en_US")) ?? ""), "5:00 PM")
        XCTAssertEqual(ScheduleTiming(cron: "0 17 * * 5")?.timeText(locale: Locale(identifier: "en_GB")), "17:00")
    }

    func testTurnsAScheduleOffWithoutTouchingTheRest() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SaveAgentSchedule":{"id":"schedule-1"}}}"#)
        try await connection.saveAgentSchedule(scheduleId: "schedule-1", isEnabled: false)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["scheduleId": .string("schedule-1"), "enabled": .bool(false)])
    }

    // MARK: Alerts and connections

    func testReadsAlertsAndMutes() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{
                  "ListAgentAlerts":[{"id":"alert-1","alertText":"Your card was charged twice.","sentAt":"2030-01-02T08:00:00Z","subjectKey":"card",
                    "isUrgent":true,"covered":[{"mailId":"","subject":"Receipt","fromAddress":"shop@example.com"}],
                    "muteChoices":[{"muteScope":"sender","muteTarget":"shop@example.com"},{"muteScope":"domain","muteTarget":"example.com"}]}],
                  "ListAgentAlertMutes":[{"id":"mute-1","muteScope":"kind","muteTarget":"budget","createdAt":"2030-01-01T00:00:00Z"}]
                }}
                """#)
        let answer = try await connection.agentAlerts()
        XCTAssertEqual(answer.alerts[0].muteChoices.first?.muteScope, "sender")
        XCTAssertEqual(answer.alerts[0].covered[0].fromAddress, "shop@example.com")
        XCTAssertEqual(answer.mutes[0].muteTarget, "budget")
    }

    func testReadsWhatTheAgentFollowsNearestDayFirst() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListAgentFollowUps":[
                  {"id":"parcel-1","followUpKind":"parcel","followUpTitle":"Garden shears","payee":null,"carrier":"Example Post","amount":null,
                   "currencyCode":null,"trackingNumber":"EX123456789","dueOn":null,"place":null,"followUpState":"open"},
                  {"id":"bill-1","followUpKind":"bill","followUpTitle":"Power bill","payee":"Example Power","carrier":null,"amount":"61.00",
                   "currencyCode":"USD","trackingNumber":null,"dueOn":"2030-01-20","place":null,"followUpState":"overdue"},
                  {"id":"event-1","followUpKind":"event","followUpTitle":"Street fair","payee":null,"carrier":null,"amount":null,
                   "currencyCode":null,"trackingNumber":null,"dueOn":"2030-01-05","place":"Elm Street","sourceUrl":"https://events.example.org/fair","followUpState":"open"}
                ]}}
                """#)
        let followUps = try await connection.agentFollowUps()
        XCTAssertEqual(followUps.map(\.id), ["event-1", "bill-1", "parcel-1"])
        XCTAssertTrue(followUps[1].isOverdue)
        XCTAssertEqual(followUps[0].sourcePageURL?.host, "events.example.org")
        XCTAssertNil(followUps[1].sourcePageURL)
        XCTAssertFalse(followUps[0].isStalled)
        XCTAssertNil(followUps[2].dueDate())
        let day = try XCTUnwrap(followUps[0].dueDate(calendar: Calendar(identifier: .gregorian)))
        let parts = Calendar(identifier: .gregorian).dateComponents([.year, .month, .day], from: day)
        XCTAssertEqual([parts.year, parts.month, parts.day], [2030, 1, 5])
        let sentBody = try XCTUnwrap(fake.sent[0].httpBody)
        XCTAssertTrue(String(decoding: sentBody, as: UTF8.self).contains("isOpenOnly: true"))
    }

    func testStopsAFollowUpAndMutesAKind() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"StopAgentFollowUp":{"id":"bill-1","followUpState":"stopped"},"MuteAgentAlert":{"id":"mute-1"}}}"#)
        try await connection.stopAgentFollowUp("bill-1")
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["followUpId": .string("bill-1")])
        try await connection.muteAgentAlertKind("parcel")
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["muteTarget": .string("parcel")])
        XCTAssertEqual(AgentFollowUp.muteKinds.first, "follow_up")
    }

    func testAddsAFollowUpWithOnlyTheFieldsItsKindUses() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"AddAgentFollowUp":{"id":"parcel-2","followUpTitle":"Garden shears"}}}"#)
        var parcel = NewAgentFollowUp()
        parcel.followUpKind = "parcel"
        parcel.followUpTitle = " Garden shears "
        parcel.carrier = "Example Post"
        // Typed while the kind was a bill, then the kind was changed.
        parcel.payee = "Example Water"
        parcel.amount = "84.20"
        XCTAssertTrue(parcel.canAdd)
        try await connection.addAgentFollowUp(parcel)
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["followUpKind": .string("parcel"), "followUpTitle": .string("Garden shears"), "carrier": .string("Example Post")])
    }

    func testAFollowUpNeedsWhatTheServerNeeds() {
        var bill = NewAgentFollowUp()
        bill.followUpTitle = "Water bill"
        XCTAssertFalse(bill.canAdd)
        bill.dueOn = "2030-01-20"
        XCTAssertFalse(bill.canAdd)
        bill.amount = "84.20"
        XCTAssertTrue(bill.canAdd)
        var event = NewAgentFollowUp()
        event.followUpKind = "event"
        event.followUpTitle = "Street fair"
        XCTAssertFalse(event.canAdd)
        let day = Calendar(identifier: .gregorian).date(from: DateComponents(year: 2030, month: 1, day: 5))!
        XCTAssertEqual(NewAgentFollowUp.dayText(day, calendar: Calendar(identifier: .gregorian)), "2030-01-05")
    }

    func testSaysWhyAServerRefusedTheCredential() async throws {
        let (connection, _) = connection(answering: #"{"data":{"ConnectAgentServer":{"name":"tracker","status":"error","lastError":"bad token","tools":0}}}"#)
        let refusal = try await connection.connectServer("tracker", credential: "a-credential")
        XCTAssertEqual(refusal, "bad token")
        let server = AgentConnectedServer(
            name: "tracker", transport: "http", auth: "oauth", oauthRedirect: "server", headless: false, enabled: true, status: "", lastError: nil,
            tools: 3)
        XCTAssertTrue(server.isPersonal)
        XCTAssertTrue(server.doesAuthorizeInBrowser)
        XCTAssertFalse(server.isConnected)
    }

    func testSetsAChatBotWithoutAnEmptyToken() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SetAgentChannel":null}}"#)
        try await connection.setChatChannel(kind: "telegram", token: "", isEnabled: false)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["kind": .string("telegram"), "enabled": .bool(false)])
    }

    // MARK: Activity

    func testReadsAPageOfRunsNarrowedToADream() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListAgentRuns":{"total":120,"runs":[{"id":"run-1","title":"Read a source","jobKind":"ingest","lastAt":"2030-01-02T01:00:00Z",
                  "usage":{"promptTokens":1000,"cacheReadTokens":500,"completionTokens":200,"cost":0.01}}]}}}
                """#)
        let page = try await connection.agentRuns(offset: 50, jobId: "job-1", jobKinds: ["dream"], query: "  source ")
        XCTAssertEqual(page.total, 120)
        XCTAssertEqual(page.runs[0].tokenCount, 1700)
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["first": .number(50), "offset": .number(50), "jobId": .string("job-1"), "kinds": .array([.string("dream")]), "query": .string("source")])
    }

    func testKnowsWhichWorkCanBeStoppedOrOpened() {
        XCTAssertTrue(AgentBackgroundWork(id: "work-1", workStatus: "running").isStoppable)
        XCTAssertFalse(AgentBackgroundWork(id: "work-1", workStatus: "running").isOpenable)
        XCTAssertTrue(AgentBackgroundWork(id: "work-2", workStatus: "failed").isOpenable)
        XCTAssertFalse(AgentBackgroundWork(id: "work-3", workStatus: "stopped").isOpenable)
    }

    // MARK: Memory

    func testScoresOverTimeLeaveGapsAndRoundFirst() {
        let runs = [
            MemoryCheckRun(id: "run-3", startedAt: "2030-01-03T00:00:00Z"),
            MemoryCheckRun(
                id: "run-2", startedAt: "2030-01-02T00:00:00Z", finishedAt: "2030-01-02T00:10:00Z",
                sourceScores: [MemoryCheckScore(answerFrom: "memory", answeredCount: 10, scorePercent: 72.6)]),
            MemoryCheckRun(
                id: "run-1", startedAt: "2030-01-01T00:00:00Z", finishedAt: "2030-01-01T00:10:00Z",
                sourceScores: [
                    MemoryCheckScore(answerFrom: "memory", answeredCount: 10, scorePercent: 70.4),
                    MemoryCheckScore(answerFrom: "sources", answeredCount: 0, scorePercent: 0),
                ]),
        ]
        let history = MemoryCheckRun.scoreHistory(runs, answerFrom: "memory")
        XCTAssertEqual(history.map(\.runId), ["run-1", "run-2"])
        XCTAssertEqual(MemoryCheckRun.scoreChangePoints(history), 3)
        XCTAssertTrue(MemoryCheckRun.scoreHistory(runs, answerFrom: "sources").isEmpty)
        XCTAssertNil(MemoryCheckRun.scoreChangePoints(Array(history.prefix(1))))
    }

    func testSendsOnlyWhatAnEditChanged() async throws {
        let question = MemoryCheckQuestion(id: "question-1", questionKind: "changed", questionText: "Where do I live?", expectedAnswer: "Riverton")
        let change = MemoryCheckQuestionChange(
            from: question,
            to: MemoryCheckQuestionChange(
                questionKind: "changed", questionText: "Where do I live?", expectedAnswer: "Hillford", outdatedAnswer: "Riverton", questionState: "confirmed"))
        XCTAssertEqual(change.variables, ["expectedAnswer": .string("Hillford"), "outdatedAnswer": .string("Riverton")])
        XCTAssertTrue(MemoryCheckQuestionChange(from: question, to: MemoryCheckQuestionChange(questionText: "Where do I live?")).isEmpty)

        let (connection, fake) = connection(answering: #"{"data":{"UpdateAgentEvaluationQuestion":null}}"#)
        try await connection.updateMemoryCheckQuestion("question-1", change: MemoryCheckQuestionChange(questionState: "dropped"))
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["questionId": .string("question-1"), "questionState": .string("dropped")])
    }

    func testHidesDroppedQuestionsAndPicksMissedAnswers() {
        let questions = [MemoryCheckQuestion(id: "question-1"), MemoryCheckQuestion(id: "question-2", questionState: "dropped")]
        XCTAssertEqual(MemoryCheckQuestion.visible(questions, isShowingDropped: false).map(\.id), ["question-1"])
        XCTAssertEqual(MemoryCheckQuestion.visible(questions, isShowingDropped: true).count, 2)
        XCTAssertTrue(MemoryCheckAnswer(id: "answer-1", questionId: "question-1", answerVerdict: "stale").isMissedOrWrong)
        XCTAssertFalse(MemoryCheckAnswer(id: "answer-2", questionId: "question-1", answerVerdict: "not_known").isMissedOrWrong)
        XCTAssertTrue(MemoryCheckAnswer(id: "answer-2", questionId: "question-1", answerVerdict: "not_known").isRight)
    }

    func testReadsWhatTheAgentLearnedLately() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"ListAgentLearned":[{"fact":{"id":"fact-1","number":3,"kind":"preference","text":"Likes tea","happenedAt":null,
                  "inferred":true,"evidence":[{"kind":"conversation","id":"conversation-1","quote":"I always drink tea"}],"audiences":null},
                  "path":"people/robin","name":"Robin"}]}}
                """#)
        let facts = try await connection.learnedFacts()
        XCTAssertEqual(facts[0].fact.number, 3)
        XCTAssertEqual(facts[0].path, "people/robin")
        XCTAssertTrue(facts[0].fact.inferred)
    }
}
