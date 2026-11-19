import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AgentFeedTests cover the agent's feed: what the server's cards decode to,
/// what each call sends, and that the documents name the server's operations.
/// Every card here is invented.
final class AgentFeedTests: XCTestCase {
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

    private let pageJSON = #"""
        {"data":{"ListAgentFeed":{"unreadCount":2,"nextCursor":"cursor-2","feedItems":[
          {"feedItemId":"card-1","feedItemKind":"update","feedSourceKind":"schedule","feedSourceId":"schedule-1","feedSourceTitle":"Morning weather",
           "runConversationId":"run-1","title":"Sunny and mild","body":"Highs near **21 degrees**. See [the forecast](https://weather.example.com/town).",
           "pictureSymbol":"cloud.sun",
           "factRows":[{"factLabel":"High","factValue":"21 degrees"},{"factLabel":"Rain","factValue":"10 percent"}],
           "sourceLinks":[{"sourceTitle":"Town forecast","sourceUrl":"https://weather.example.com/town"}],
           "choices":[],"chosenChoiceKey":null,"chosenAt":null,"outcomeText":"","isOutcomePending":false,"feedUrgency":"whenever",
           "isLiked":true,"isRead":false,"isDismissed":false,"postedAt":"2030-03-01T07:00:00Z","updatedAt":"2030-03-01T08:30:00.123456789Z"},
          {"feedItemId":"card-2","feedItemKind":"decision","feedSourceKind":"alert","feedSourceId":"alert-9","feedSourceTitle":"",
           "runConversationId":"run-2","title":"New sign-in on a laptop","body":"A laptop signed in at 06:12.","pictureSymbol":"lock.shield",
           "factRows":null,"sourceLinks":null,
           "choices":[{"choiceKey":"mine","choiceLabel":"That's me","choiceRole":"dismiss"},
                      {"choiceKey":"secure","choiceLabel":"Secure my account","choiceRole":"action"}],
           "chosenChoiceKey":"secure","chosenAt":"2030-03-01T06:20:00Z","outcomeText":"Signed out the other session.","isOutcomePending":true,
           "feedUrgency":"now","isLiked":false,"isRead":true,"isDismissed":false,"postedAt":"2030-03-01T06:13:00Z","updatedAt":"2030-03-01T06:20:00Z"},
          null
        ]}}}
        """#

    // MARK: The documents

    func testTheDocumentsNameTheServersOperations() {
        let operations: [String: String] = [
            "agentFeed": "ListAgentFeed", "agentFeedStack": "ListAgentFeedStack", "chooseAgentFeedItem": "ChooseAgentFeedItem", "likeAgentFeedItem": "LikeAgentFeedItem",
            "showLessLikeAgentFeedItem": "ShowLessLikeAgentFeedItem", "dismissAgentFeedItem": "DismissAgentFeedItem",
            "markAgentFeedRead": "MarkAgentFeedRead", "discussAgentFeedItem": "DiscussAgentFeedItem", "agentFeedEvents": "AgentFeedChanged",
        ]
        XCTAssertEqual(Set(Documents.agentFeedDocuments.keys), Set(operations.keys))
        for (name, operation) in operations {
            XCTAssertTrue(Documents.agentFeedDocuments[name]?.contains(operation) == true, "\(name) does not call \(operation)")
        }
        XCTAssertTrue(Documents.agentFeedEvents.hasPrefix("subscription"))
        XCTAssertTrue(Documents.markAgentFeedRead.contains("[String!]!"))
    }

    // MARK: Decoding

    func testAPageDecodesItsCardsAndCounts() async throws {
        let (connection, fake) = connection(answering: pageJSON)
        let page = try await connection.agentFeed()
        XCTAssertEqual(page.unreadCount, 2)
        XCTAssertEqual(page.nextCursor, "cursor-2")
        // The null card is left out.
        XCTAssertEqual(page.feedItems.map(\.feedItemId), ["card-1", "card-2"])
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["first"], .number(30))
        XCTAssertNil(variables["after"])
        XCTAssertNil(variables["includeDismissed"])
    }

    func testAWeatherCardKeepsItsFactsAndSources() async throws {
        let (connection, _) = connection(answering: pageJSON)
        let card = try await connection.agentFeed().feedItems[0]
        XCTAssertEqual(card.title, "Sunny and mild")
        XCTAssertEqual(card.factRows, [AgentFeedFact(factLabel: "High", factValue: "21 degrees"), AgentFeedFact(factLabel: "Rain", factValue: "10 percent")])
        XCTAssertEqual(card.sourceLinks.map(\.sourceUrl), ["https://weather.example.com/town"])
        XCTAssertEqual(card.feedSourceTitle, "Morning weather")
        XCTAssertTrue(card.isLiked)
        XCTAssertFalse(card.isRead)
        XCTAssertFalse(card.isChosen)
        XCTAssertFalse(card.isWaitingForAnswer)
        // The date shown is when it was last replaced, fractions dropped.
        XCTAssertEqual(card.shownAt, ServerTime.date("2030-03-01T08:30:00Z"))
    }

    func testADecisionCardKnowsWhatWasChosen() async throws {
        let (connection, _) = connection(answering: pageJSON)
        let card = try await connection.agentFeed().feedItems[1]
        XCTAssertEqual(card.choices.map(\.choiceRole), [.dismiss, .action])
        XCTAssertEqual(card.chosenChoice?.choiceLabel, "Secure my account")
        XCTAssertEqual(card.outcomeText, "Signed out the other session.")
        XCTAssertTrue(card.isOutcomePending)
        XCTAssertTrue(card.isChosen)
        // Null lists are empty ones.
        XCTAssertEqual(card.factRows, [])
        XCTAssertEqual(card.sourceLinks, [])
        XCTAssertFalse(card.isWaitingForAnswer)
    }

    func testACardWithMissingFieldsStillDecodes() throws {
        let json = #"{"feedItemId":"card-3","title":"Bare card"}"#
        let card = try JSONDecoder().decode(AgentFeedItem.self, from: Data(json.utf8))
        XCTAssertEqual(card.title, "Bare card")
        XCTAssertEqual(card.body, "")
        XCTAssertEqual(card.feedUrgency, "whenever")
        XCTAssertEqual(card.choices, [])
        XCTAssertFalse(card.isDismissed)
        XCTAssertNil(card.shownAt)
    }

    func testAnUnansweredQuestionIsWaiting() throws {
        let json = #"""
            {"feedItemId":"card-4","title":"Which style?","choices":[{"choiceKey":"option:1","choiceLabel":"Plain","choiceRole":"action"},
             {"choiceKey":"future","choiceLabel":"Later","choiceRole":"snooze"}]}
            """#
        let card = try JSONDecoder().decode(AgentFeedItem.self, from: Data(json.utf8))
        XCTAssertTrue(card.isWaitingForAnswer)
        // A role this app does not know answers the agent, as an action does.
        XCTAssertEqual(card.choices[1].choiceRole, .action)
    }

    func testACodingSessionCardNamesItsSession() throws {
        let json = #"{"feedItemId":"card-5","title":"Claude Code on laptop asks","feedSourceKind":"coding_session","feedSourceId":"laptop/p2"}"#
        let card = try JSONDecoder().decode(AgentFeedItem.self, from: Data(json.utf8))
        XCTAssertEqual(card.codingSessionLocation?.computer, "laptop")
        XCTAssertEqual(card.codingSessionLocation?.paneId, "p2")
        let other = AgentFeedItem(feedItemId: "card-6", feedSourceKind: "goal", feedSourceId: "goal-1/x", title: "Not a session")
        XCTAssertNil(other.codingSessionLocation)
    }

    func testAPictureIsASymbolAnEmojiOrTheNewspaper() {
        XCTAssertEqual(AgentFeedPictureKind.kind(of: "cloud.sun"), .symbol("cloud.sun"))
        XCTAssertEqual(AgentFeedPictureKind.kind(of: " chart.line.uptrend.xyaxis "), .symbol("chart.line.uptrend.xyaxis"))
        XCTAssertEqual(AgentFeedPictureKind.kind(of: "\u{1F324}"), .emoji("\u{1F324}"))
        XCTAssertEqual(AgentFeedPictureKind.kind(of: ""), .fallback)
        XCTAssertEqual(AgentFeedPictureKind.kind(of: "a picture of a cloud"), .fallback)
    }

    func testAnEventDecodes() throws {
        let json = #"{"feedItemId":"card-1","feedChange":"settled","unreadCount":3}"#
        let event = try JSONDecoder().decode(AgentFeedEvent.self, from: Data(json.utf8))
        XCTAssertEqual(event.feedChange, "settled")
        XCTAssertEqual(event.unreadCount, 3)
    }

    func testAStackTopDecodesItsCounts() throws {
        let json = #"{"feedItemId":"card-7","title":"Robots","feedStackKey":"goal:goal-1","stackItemCount":3,"stackUnreadCount":2}"#
        let card = try JSONDecoder().decode(AgentFeedItem.self, from: Data(json.utf8))
        XCTAssertTrue(card.isStack)
        XCTAssertEqual(card.stackMoreCount, 2)
        XCTAssertEqual(card.stackUnreadCount, 2)
        // A card without the fields stands alone.
        let alone = try JSONDecoder().decode(AgentFeedItem.self, from: Data(#"{"feedItemId":"card-8","title":"Alone"}"#.utf8))
        XCTAssertFalse(alone.isStack)
        XCTAssertEqual(alone.stackItemCount, 1)
        XCTAssertEqual(alone.stackUnreadCount, 0)
        XCTAssertEqual(alone.feedStackKey, "")
        XCTAssertEqual(alone.feedStackTitle, "")
        XCTAssertNil(alone.stackPosition)
        XCTAssertEqual(alone.stackDisplayTitle, "")
    }

    func testAnOrderedTitledStackCardKnowsItsPlace() throws {
        let json = #"""
            {"feedItemId":"card-9","title":"Weather","feedSourceTitle":"Daily brief","feedStackKey":"brief:1","stackItemCount":5,
             "feedStackTitle":"Morning brief, Wed Nov 19","stackPosition":1}
            """#
        let card = try JSONDecoder().decode(AgentFeedItem.self, from: Data(json.utf8))
        XCTAssertEqual(card.stackPosition, 1)
        XCTAssertEqual(card.stackDisplayTitle, "Morning brief, Wed Nov 19")
        // Without a title of its own, the source names the stack.
        let untitled = AgentFeedItem(feedItemId: "card-10", feedSourceTitle: "Daily brief", title: "x")
        XCTAssertEqual(untitled.stackDisplayTitle, "Daily brief")
    }

    func testReadingAStackSendsItsKeyAndCursor() async throws {
        let (connection, fake) = connection(answering: pageJSON.replacingOccurrences(of: "ListAgentFeed", with: "ListAgentFeedStack"))
        let page = try await connection.agentFeedStack("goal:goal-1", after: "cursor-1", pageSize: 20)
        XCTAssertEqual(page.feedItems.count, 2)
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["feedStackKey"], .string("goal:goal-1"))
        XCTAssertEqual(variables["first"], .number(20))
        XCTAssertEqual(variables["after"], .string("cursor-1"))
    }

    // MARK: Layouts

    private func layoutCard(_ layout: String, _ content: String) throws -> AgentFeedItem {
        let escaped = String(decoding: try JSONEncoder().encode(content), as: UTF8.self)
        let json = #"{"feedItemId":"card-11","title":"Brief","feedCardLayout":"\#(layout)","layoutContent":\#(escaped)}"#
        return try JSONDecoder().decode(AgentFeedItem.self, from: Data(json.utf8))
    }

    func testAListLayoutDecodesItsRows() throws {
        let card = try layoutCard(
            "list",
            #"{"listRows":[{"rowTitle":"A story","rowMeta":"Example News","rowSymbol":"newspaper","rowUrl":"https://news.example.com/a"},{"rowTitle":"A mail","rowRecordReference":{"itemId":"mail-1","subject":"Hello"}},{"rowTitle":"Odd","rowUrl":"javascript:alert(1)"}]}"#)
        guard case .list(let rows) = card.layout else { return XCTFail("not a list") }
        XCTAssertEqual(rows.map(\.rowTitle), ["A story", "A mail", "Odd"])
        XCTAssertEqual(rows[0].url?.absoluteString, "https://news.example.com/a")
        XCTAssertEqual(rows[1].rowRecordReference?.itemId, "mail-1")
        XCTAssertTrue(rows[1].isOpenable)
        // Only http and https are followed.
        XCTAssertNil(rows[2].url)
        XCTAssertFalse(rows[2].isOpenable)
    }

    func testAWeatherLayoutDecodesNumbersAndText() throws {
        let card = try layoutCard(
            "weather",
            #"{"placeName":"Riverside","currentTemperature":16,"temperatureUnit":"C","conditionText":"Mostly sunny","conditionSymbol":"cloud.sun","highTemperature":"21","lowTemperature":11.5,"hourlyForecast":[{"forecastHour":"2030-03-01T08:00:00Z","forecastTemperature":16,"forecastSymbol":"sun.max","precipitationChancePercent":0},{"forecastHour":"2030-03-01T20:00:00Z","forecastTemperature":15,"precipitationChancePercent":60}]}"#)
        guard case .weather(let weather) = card.layout else { return XCTFail("not weather") }
        XCTAssertEqual(weather.currentTemperature, "16")
        XCTAssertEqual(weather.shown(weather.currentTemperature), "16°C")
        XCTAssertEqual(weather.shown(weather.highTemperature), "21°C")
        XCTAssertEqual(weather.lowTemperature, "11.5")
        XCTAssertEqual(weather.hourlyForecast.count, 2)
        XCTAssertEqual(weather.hourlyForecast[1].precipitationChancePercent, 60)
        XCTAssertNotNil(weather.hourlyForecast[0].date)
    }

    func testAnAgendaLayoutDecodesItsEntriesAndKinds() throws {
        let card = try layoutCard(
            "agenda",
            #"{"agendaEntries":[{"startsAt":"2030-03-01T09:00:00Z","endsAt":"2030-03-01T09:30:00Z","agendaTitle":"Stand-up","agendaEntryKind":"meeting","agendaDetail":"Room 4"},{"startsAt":"2030-03-01T12:30:00Z","agendaTitle":"Lunch","agendaEntryKind":"break"},{"startsAt":"2030-03-01T15:00:00Z","agendaTitle":"Odd","agendaEntryKind":"surprise"}]}"#)
        guard case .agenda(let entries) = card.layout else { return XCTFail("not an agenda") }
        XCTAssertEqual(entries.map(\.agendaEntryKind), [.meeting, .break, .task])
        XCTAssertNotNil(entries[0].endDate)
        XCTAssertNil(entries[1].endDate)
    }

    func testALayoutThatCannotBeReadFallsBackToText() throws {
        XCTAssertEqual(try layoutCard("list", "not json").layout, .text)
        XCTAssertEqual(try layoutCard("list", #"{"listRows":[]}"#).layout, .text)
        XCTAssertEqual(try layoutCard("weather", #"{"placeName":"Nowhere"}"#).layout, .text)
        XCTAssertEqual(try layoutCard("agenda", #"{"agendaEntries":"soon"}"#).layout, .text)
        XCTAssertEqual(try layoutCard("carousel", #"{"x":1}"#).layout, .text)
        let plain = try JSONDecoder().decode(AgentFeedItem.self, from: Data(#"{"feedItemId":"card-12","title":"Plain"}"#.utf8))
        XCTAssertEqual(plain.feedCardLayout, "text")
        XCTAssertEqual(plain.layout, .text)
    }

    // MARK: Calls

    func testReadingTheNextPageSendsTheCursor() async throws {
        let (connection, fake) = connection(answering: pageJSON)
        _ = try await connection.agentFeed(after: "cursor-2", pageSize: 10, includeDismissed: true)
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["first"], .number(10))
        XCTAssertEqual(variables["after"], .string("cursor-2"))
        XCTAssertEqual(variables["includeDismissed"], .bool(true))
    }

    func testChoosingSendsTheCardAndTheChoice() async throws {
        let card = #"{"feedItemId":"card-2","title":"New sign-in on a laptop","chosenChoiceKey":"mine"}"#
        let (connection, fake) = connection(answering: #"{"data":{"ChooseAgentFeedItem":\#(card)}}"#)
        let settled = try await connection.chooseAgentFeedItem("card-2", choiceKey: "mine")
        XCTAssertEqual(settled.chosenChoiceKey, "mine")
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["feedItemId"], .string("card-2"))
        XCTAssertEqual(variables["choiceKey"], .string("mine"))
    }

    func testLikingShowingLessAndDismissingAnswerWithTheCard() async throws {
        let card = #"{"feedItemId":"card-1","title":"Sunny and mild","isLiked":true,"isDismissed":true}"#
        let (connection, fake) = connection(
            answering: #"{"data":{"LikeAgentFeedItem":\#(card),"ShowLessLikeAgentFeedItem":\#(card),"DismissAgentFeedItem":\#(card)}}"#)
        let liked = try await connection.likeAgentFeedItem("card-1", isLiked: true)
        XCTAssertTrue(liked.isLiked)
        XCTAssertEqual(try sentVariables(fake.sent[0])["isLiked"], .bool(true))
        let lessLikeThis = try await connection.showLessLikeAgentFeedItem("card-1")
        XCTAssertTrue(lessLikeThis.isDismissed)
        let dismissed = try await connection.dismissAgentFeedItem("card-1")
        XCTAssertTrue(dismissed.isDismissed)
        XCTAssertEqual(try sentVariables(fake.sent[2])["feedItemId"], .string("card-1"))
    }

    func testMarkingReadSendsTheIdsAndReturnsHowManyWereNew() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"MarkAgentFeedRead":2}}"#)
        let newlyRead = try await connection.markAgentFeedRead(["card-1", "card-2"])
        XCTAssertEqual(newlyRead, 2)
        XCTAssertEqual(try sentVariables(fake.sent[0])["feedItemIds"], .array([.string("card-1"), .string("card-2")]))
    }

    func testDiscussingReturnsTheMainConversation() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"DiscussAgentFeedItem":"conversation-main"}}"#)
        let conversationId = try await connection.discussAgentFeedItem("card-1")
        XCTAssertEqual(conversationId, "conversation-main")
        XCTAssertEqual(try sentVariables(fake.sent[0])["feedItemId"], .string("card-1"))
    }
}
