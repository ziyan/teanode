import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AgentSettingsTests cover the agent's settings: what the server's answers
/// decode to, and what each form sends, as the dashboard sends it.
final class AgentSettingsTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(statusCode: Int = 200, answering page: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (statusCode, page) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    /// The variables of the one request the fake server was sent.
    private func sentVariables(_ fake: FakeServer) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: fake.sent.first?.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    /// An agent page as the server answers it: one mailbox granted with a
    /// policy, one never granted, a calendar and an address book.
    private let readAnswer = #"""
        {"data":{"ReadAgent":{
          "agent":{"id":"agent-1","name":"Juniper","enabled":true,"instructions":"Sign as R.","language":"","knowledgeLanguage":"en",
            "askModel":"","speechVoice":"","operatorDisabledAt":null,"confirm":["calendar","mail_send"],"unattendedAllowedRisks":["money"],
            "isMemoryCheckEnabled":true,"isIdeasEnabled":false,
            "voice":{"tone":"formal","length":null,"greeting":"Hello","signoff":null},
            "categories":[{"name":"club","description":"The sailing club"}]},
          "sources":[
            {"mailboxId":"mailbox-1","name":"Robin","addresses":["robin@example.com"],"policy":{"granted":true,"draftReplies":true,"search":false,"research":false,
              "triage":{"enabled":true,"backfill":"","replyExpectation":"any"},"summaries":null,
              "autoReply":{"enabled":false,"guidance":null,"scope":null,"allow":null,"never":null,"categories":null,"when":null,"hours":null,"holdMinutes":null,"dailyLimit":null},
              "alerts":null}},
            {"mailboxId":"mailbox-2","name":"Club","addresses":["club@example.org","info@example.org"],"policy":null}],
          "collections":[{"id":"calendar-1","name":"Home","kind":"calendar","granted":true,"items":12},
            {"id":"book-1","name":"Contacts","kind":"addressBook","granted":false,"items":40}],
          "allowed":{"enabled":true,"triage":true,"summaries":true,"draftReplies":true,"search":false,"research":true,"autoReply":true,"ask":true,
            "schedules":true,"browser":false,"connectedServers":false},
          "budget":{"used":1200,"limit":100000,"resetsAt":"2030-01-02T00:00:00Z","cost":0.25,"costLimit":0,"currency":"USD"},
          "choices":["model-a","model-b"],"timezone":"Europe/Berlin","language":"en","knowledgeLanguage":"en",
          "categories":["personal","work","newsletter","notification","receipt","promotion","social","invitation","phishing","junk","other","club"]}}}
        """#

    /// The same page, as the answer to a change named operation.
    private func answer(as operation: String) -> String {
        readAnswer.replacingOccurrences(of: "\"ReadAgent\"", with: "\"\(operation)\"")
    }

    // MARK: Reading

    func testDecodesTheAgentPage() async throws {
        let (connection, fake) = connection(answering: readAnswer)
        let settings = try await connection.agentSettings()
        let agent = try XCTUnwrap(settings.agent)
        XCTAssertEqual(agent.name, "Juniper")
        XCTAssertTrue(agent.enabled)
        XCTAssertFalse(agent.isOperatorDisabled)
        XCTAssertEqual(agent.confirm, ["calendar", "mail_send"])
        XCTAssertEqual(agent.voice?.tone, "formal")
        XCTAssertEqual(agent.categories.first?.name, "club")
        XCTAssertEqual(settings.sources.count, 2)
        XCTAssertTrue(settings.source("mailbox-1")?.isGranted == true)
        XCTAssertEqual(settings.source("mailbox-1")?.policy?.triage?.replyExpectation, "any")
        XCTAssertFalse(settings.source("mailbox-2")?.isGranted == true)
        XCTAssertTrue(settings.collections[0].isCalendar)
        XCTAssertEqual(settings.collections[1].items, 40)
        XCTAssertFalse(settings.allowed.search)
        XCTAssertEqual(settings.budget?.limit, 100000)
        XCTAssertEqual(settings.choices, ["model-a", "model-b"])
        XCTAssertEqual(settings.fixedCategoryNames.count, 9)
        XCTAssertEqual(fake.sent.first?.url?.path, "/api/v1/graphql")
    }

    func testNoAgentYetIsReadAsNone() async throws {
        let (connection, _) = connection(answering: #"""
            {"data":{"ReadAgent":{"agent":null,"sources":[],"collections":[],"allowed":{"enabled":true,"triage":true,"summaries":true,"draftReplies":true,"search":true,"research":true,"autoReply":true,"ask":true,"schedules":true,"browser":true,"connectedServers":true},"budget":null,"choices":[],"timezone":"UTC","language":"en","knowledgeLanguage":"en","categories":[]}}}
            """#)
        let settings = try await connection.agentSettings()
        XCTAssertNil(settings.agent)
        XCTAssertTrue(settings.allowed.enabled)
    }

    func testAnAnswerWithLessIsReadAsOfferingNothing() async throws {
        let (connection, _) = connection(answering: #"{"data":{"ReadAgent":{"timezone":"UTC","categories":[]}}}"#)
        let settings = try await connection.agentSettings()
        XCTAssertNil(settings.agent)
        XCTAssertFalse(settings.allowed.enabled)
        XCTAssertTrue(settings.sources.isEmpty)
    }

    // MARK: Changing the agent

    func testAChangeSendsOnlyWhatItChanges() async throws {
        let (connection, fake) = connection(answering: answer(as: "UpdateAgent"))
        _ = try await connection.updateAgent(AgentChange(enabled: false))
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables, ["enabled": .bool(false)])
    }

    func testTheNameAndLanguagesAreTrimmedButNotTheInstructions() async throws {
        let (connection, fake) = connection(answering: answer(as: "UpdateAgent"))
        _ = try await connection.updateAgent(AgentChange(name: "  Juniper ", instructions: "  Sign as R.\n", language: " ja ", knowledgeLanguage: ""))
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["name"], .string("Juniper"))
        XCTAssertEqual(variables["instructions"], .string("  Sign as R.\n"))
        XCTAssertEqual(variables["language"], .string("ja"))
        XCTAssertEqual(variables["knowledgeLanguage"], .string(""))
        XCTAssertNil(variables["enabled"])
    }

    func testTheVoiceSaysEveryPart() async throws {
        let (connection, fake) = connection(answering: answer(as: "UpdateAgent"))
        var draft = AgentVoiceDraft(AgentSettings.Voice(tone: "formal", greeting: "Hello"))
        draft.length = "short"
        _ = try await connection.updateAgent(AgentChange(voice: draft.voice))
        let variables = try sentVariables(fake)
        XCTAssertEqual(
            variables["voice"], .object(["tone": .string("formal"), "length": .string("short"), "greeting": .string("Hello"), "signoff": .string("")]))
    }

    func testCategoriesWithoutANameAreLeftOut() async throws {
        let (connection, fake) = connection(answering: answer(as: "UpdateAgent"))
        let categories = [
            AgentSettings.Category(name: "club", description: "The sailing club"), AgentSettings.Category(name: "  ", description: "nothing"),
            AgentSettings.Category(name: "garden", description: nil),
        ]
        _ = try await connection.updateAgent(AgentChange(categories: categories))
        let variables = try sentVariables(fake)
        XCTAssertEqual(
            variables["categories"],
            .array([
                .object(["name": .string("club"), "description": .string("The sailing club")]),
                .object(["name": .string("garden"), "description": .string("")]),
            ]))
    }

    func testForgettingSendsForget() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"UpdateAgent":{"agent":null,"allowed":{"enabled":true,"triage":true,"summaries":true,"draftReplies":true,"search":true,"research":true,"autoReply":true,"ask":true,"schedules":true,"browser":true,"connectedServers":true}}}}"#)
        let settings = try await connection.forgetAgent()
        XCTAssertNil(settings.agent)
        XCTAssertEqual(try sentVariables(fake), ["forget": .bool(true)])
    }

    func testARefusalComesBackAsItsReason() async throws {
        let (connection, _) = connection(answering: #"{"errors":[{"message":"category 0: \"work\" is already a category"}],"data":null}"#)
        do {
            _ = try await connection.updateAgent(AgentChange(categories: [AgentSettings.Category(name: "work")]))
            XCTFail("expected a refusal")
        } catch let error as TeaNodeError {
            XCTAssertEqual(error, .refused("category 0: \"work\" is already a category"))
        }
    }

    // MARK: Ask me first

    func testTheConfirmListKeepsItsOrderAndNamesEachOnce() {
        var draft = AgentConfirmDraft(confirm: ["calendar", "mail_send", "calendar"])
        XCTAssertEqual(draft.askFirstNames, ["calendar", "mail_send"])
        draft.setAskingFirst("shell", isAskingFirst: true)
        draft.setAskingFirst("calendar", isAskingFirst: false)
        draft.setAskingFirst("shell", isAskingFirst: true)
        XCTAssertEqual(draft.askFirstNames, ["mail_send", "shell"])
    }

    func testAToolAsksFirstByItsOwnNameOrItsFamilys() {
        let draft = AgentConfirmDraft(confirm: ["calendar"])
        let calendarTool = AgentTool(name: "calendar_add", family: "calendar", risk: "", description: "", confirms: false, core: false, actions: nil)
        let mailTool = AgentTool(name: "mail_read", family: "mail", risk: "", description: "", confirms: false, core: false, actions: nil)
        XCTAssertTrue(draft.doesToolAskFirst(calendarTool))
        XCTAssertFalse(draft.doesToolAskFirst(mailTool))
    }

    func testToolsDecodeAndGroupByFamilyInOrder() async throws {
        let (connection, _) = connection(answering: #"""
            {"data":{"ListAgentTools":[
              {"name":"mail_read","family":"mail","risk":"","description":"Reads mail.","confirms":false,"core":true,"actions":null},
              {"name":"calendar","family":"calendar","risk":"outward","description":"Your calendar.","confirms":true,"core":false,"actions":["list","add"]},
              {"name":"mail_send","family":"mail","risk":"outward","description":"Sends mail.","confirms":true,"core":false,"actions":[]}]}}
            """#)
        let tools = try await connection.agentTools()
        XCTAssertEqual(AgentTool.families(tools), ["mail", "calendar"])
        XCTAssertEqual(tools[1].actions, ["list", "add"])
    }

    // MARK: When you are not there

    func testUnattendedRisksAreSentInTheSettingsOrder() async throws {
        let (connection, fake) = connection(answering: answer(as: "UpdateAgent"))
        var draft = AgentUnattendedDraft(nil)
        draft.setAllowed("listed", isAllowed: true)
        draft.setAllowed("outward", isAllowed: true)
        draft.setAllowed("money", isAllowed: true)
        draft.setAllowed("money", isAllowed: false)
        _ = try await connection.updateAgent(AgentChange(unattendedAllowedRisks: draft.orderedAllowedRisks))
        XCTAssertEqual(try sentVariables(fake)["unattendedAllowedRisks"], .array([.string("outward"), .string("listed")]))
    }

    // MARK: Mailboxes

    func testGrantingWithoutAPolicyLeavesItToTheServer() async throws {
        let (connection, fake) = connection(answering: answer(as: "GrantAgentMailbox"))
        _ = try await connection.grantAgentMailbox("mailbox-2")
        XCTAssertEqual(try sentVariables(fake), ["mailboxId": .string("mailbox-2")])
    }

    func testANeverGrantedMailboxStartsFromTheDashboardsDefault() {
        let policy = AgentSettings.MailboxPolicy.defaultPolicy
        XCTAssertFalse(policy.granted)
        XCTAssertTrue(policy.draftReplies)
        XCTAssertTrue(policy.search)
        XCTAssertFalse(policy.research)
        XCTAssertTrue(policy.isAlertsEnabled)
    }

    func testAPolicyIsSentGrantedAndWithoutAnEmptyAnswering() async throws {
        let (connection, fake) = connection(answering: answer(as: "GrantAgentMailbox"))
        var policy = AgentSettings.MailboxPolicy.defaultPolicy
        policy.triage = .init(enabled: true, backfill: "none")
        policy.alerts = .init(enabled: false)
        policy.autoReply = .init(enabled: false)
        _ = try await connection.grantAgentMailbox("mailbox-1", policy: policy)
        let variables = try sentVariables(fake)
        guard case .object(let sent) = variables["policy"] else { return XCTFail("no policy") }
        XCTAssertEqual(sent["granted"], .bool(true))
        XCTAssertEqual(sent["triage"], .object(["enabled": .bool(true), "backfill": .string("none"), "replyExpectation": .string("")]))
        XCTAssertEqual(sent["alerts"], .object(["enabled": .bool(false)]))
        XCTAssertNil(sent["autoReply"])
        XCTAssertNil(sent["summaries"])
    }

    func testAnsweringOutsideHoursStartsFromWorkingHours() {
        var reply = AgentSettings.MailboxPolicy.AutoReply(enabled: true)
        reply.when = "outsideHours"
        var policy = AgentSettings.MailboxPolicy.defaultPolicy
        policy.autoReply = reply
        XCTAssertEqual(policy.cleaned.autoReply?.hours, .workingHours)

        policy.autoReply?.hours = AgentSettings.MailboxPolicy.Hours(from: "08:00", until: "16:00", days: [1, 2])
        policy.autoReply?.when = "always"
        XCTAssertNil(policy.cleaned.autoReply?.hours)
    }

    func testAnsweringWithGuidanceAloneIsKept() {
        var reply = AgentSettings.MailboxPolicy.AutoReply(enabled: false)
        reply.guidance = "Confirm appointments."
        var policy = AgentSettings.MailboxPolicy.defaultPolicy
        policy.autoReply = reply
        XCTAssertEqual(policy.cleaned.autoReply?.guidance, "Confirm appointments.")
    }

    func testTheAnsweringIsSentWhole() {
        var reply = AgentSettings.MailboxPolicy.AutoReply(enabled: true)
        reply.scope = "list"
        reply.allow = AgentSettings.splitList("a@example.com, example.org\n b@example.net ,")
        reply.when = "outsideHours"
        reply.holdMinutes = AgentSettings.wholeNumber("15")
        reply.dailyLimit = AgentSettings.wholeNumber("many")
        var policy = AgentSettings.MailboxPolicy.defaultPolicy
        policy.autoReply = reply
        guard case .object(let sent) = policy.cleaned.variables, case .object(let answering) = sent["autoReply"] else {
            return XCTFail("no answering")
        }
        XCTAssertEqual(answering["allow"], .array([.string("a@example.com"), .string("example.org"), .string("b@example.net")]))
        XCTAssertEqual(answering["holdMinutes"], .number(15))
        XCTAssertEqual(answering["dailyLimit"], .number(0))
        XCTAssertEqual(
            answering["hours"],
            .object(["from": .string("09:00"), "until": .string("17:30"), "days": .array([1, 2, 3, 4, 5].map { .number(Double($0)) })]))
    }

    func testRevokingNamesTheMailbox() async throws {
        let (connection, fake) = connection(answering: answer(as: "RevokeAgentMailbox"))
        let settings = try await connection.revokeAgentMailbox("mailbox-1")
        XCTAssertEqual(try sentVariables(fake), ["mailboxId": .string("mailbox-1")])
        XCTAssertNotNil(settings.agent)
    }

    func testACollectionIsGrantedByItsKindAndId() async throws {
        let (reading, _) = connection(answering: readAnswer)
        let calendar = try await reading.agentSettings().collections[0]
        let (connection, fake) = connection(answering: answer(as: "GrantAgentSource"))
        _ = try await connection.grantAgentCollection(calendar, isGranted: false)
        XCTAssertEqual(try sentVariables(fake), ["kind": .string("calendar"), "id": .string("calendar-1"), "granted": .bool(false)])
    }

    // MARK: Lists, numbers and times

    func testListsAreTypedWithCommas() {
        XCTAssertEqual(AgentSettings.joinList(["a@example.com", "example.org"]), "a@example.com, example.org")
        XCTAssertEqual(AgentSettings.joinList(nil), "")
        XCTAssertEqual(AgentSettings.splitList(" , \n"), [])
    }

    func testClockTimesRoundTrip() {
        let parts = AgentSettings.clockParts("09:05")
        XCTAssertEqual(parts?.hour, 9)
        XCTAssertEqual(parts?.minute, 5)
        XCTAssertNil(AgentSettings.clockParts("25:00"))
        XCTAssertNil(AgentSettings.clockParts("nine"))
        XCTAssertEqual(AgentSettings.clockText(hour: 7, minute: 30), "07:30")
        XCTAssertEqual(AgentSettings.clockText(hour: 17, minute: 0), "17:00")
    }

    // MARK: Speaking voice

    func testTheVoicesDecode() async throws {
        let (connection, _) = connection(answering: #"""
            {"data":{"ReadAgentVoice":{"isVoiceAvailable":true,"sampleRate":24000,"speechVoice":"voice-b","speechVoices":["voice-a","voice-b"],"serverSpeechVoice":"voice-a"}}}
            """#)
        let voices = try await connection.speechVoices()
        XCTAssertTrue(voices.isVoiceAvailable)
        XCTAssertEqual(voices.speechVoices, ["voice-a", "voice-b"])
        XCTAssertEqual(voices.serverSpeechVoice, "voice-a")
    }

    func testASampleIsAskedForByVoice() async throws {
        let (connection, fake) = connection(answering: "RIFF")
        let sample = try await connection.speechVoiceSample("voice-b")
        XCTAssertEqual(sample, Data("RIFF".utf8))
        let address = try XCTUnwrap(fake.sent.first?.url)
        XCTAssertEqual(address.path, "/api/v1/agent/voice/sample")
        XCTAssertEqual(address.query, "speechVoice=voice-b")
        XCTAssertEqual(fake.sent.first?.value(forHTTPHeaderField: "Authorization"), "Bearer tnt_a")
    }

    func testARefusedSampleSaysWhy() async throws {
        let (connection, _) = connection(statusCode: 403, answering: #"{"error":"voice is off on this server"}"#)
        do {
            _ = try await connection.speechVoiceSample("voice-b")
            XCTFail("expected a refusal")
        } catch let error as TeaNodeError {
            XCTAssertEqual(error, .refused("voice is off on this server"))
        }
    }
}
