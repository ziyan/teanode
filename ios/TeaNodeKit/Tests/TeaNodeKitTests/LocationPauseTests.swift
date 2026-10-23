import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// LocationPauseTests cover pausing where the phone records: what the
/// server's pause decodes to, what each call sends, what the phone does with
/// a pause it made itself, and which readings it keeps while one is on.
final class LocationPauseTests: XCTestCase {
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

    private func date(_ text: String) -> Date { ServerTime.date(text)! }

    private func pause(_ id: String = "pause-1", from startedAt: String, to endedAt: String?) -> LocationPause {
        LocationPause(id: id, startedAt: startedAt, endedAt: endedAt)
    }

    // MARK: The pause

    func testAPauseIsInEffectFromItsStartUntilItsEnd() {
        let timed = pause(from: "2024-01-02T14:00:00Z", to: "2024-01-02T15:00:00Z")
        XCTAssertFalse(timed.isInEffect(at: date("2024-01-02T13:59:59Z")))
        XCTAssertTrue(timed.isInEffect(at: date("2024-01-02T14:00:00Z")))
        XCTAssertTrue(timed.isInEffect(at: date("2024-01-02T14:59:59Z")))
        XCTAssertFalse(timed.isInEffect(at: date("2024-01-02T15:00:00Z")))
        XCTAssertFalse(timed.isOpenEnded)
        let open = pause(from: "2024-01-02T14:00:00Z", to: nil)
        XCTAssertTrue(open.isOpenEnded)
        XCTAssertTrue(open.isInEffect(at: date("2030-01-01T00:00:00Z")))
        XCTAssertFalse(open.isInEffect(at: date("2024-01-02T13:00:00Z")))
    }

    func testAPauseDecodesTheTimesTheServerWrites() throws {
        let text = #"{"id":"pause-1","startedAt":"2024-01-02T14:00:00.123456789Z","endedAt":null}"#
        let decoded = try JSONDecoder().decode(LocationPause.self, from: Data(text.utf8))
        XCTAssertEqual(decoded.id, "pause-1")
        XCTAssertEqual(decoded.startedDate, date("2024-01-02T14:00:00Z"))
        XCTAssertNil(decoded.endedDate)
        XCTAssertTrue(decoded.isOpenEnded)
    }

    func testAPersonLocationCarriesThePauseOnlyWhenTheServerSendsOne() throws {
        let paused = #"""
            {"userId":"user-2","username":"sam","name":null,"avatarUrl":null,"avatarColorIndex":1,"shareLevel":"current",
             "latestPoint":null,"openVisit":null,"pause":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":"2024-01-02T15:00:00Z"}}
            """#
        let person = try JSONDecoder().decode(PersonLocation.self, from: Data(paused.utf8))
        XCTAssertNil(person.latestPoint)
        XCTAssertTrue(person.isPaused(at: date("2024-01-02T14:30:00Z")))
        XCTAssertFalse(person.isPaused(at: date("2024-01-02T15:30:00Z")))
        let pauseText = #","pause":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":"2024-01-02T15:00:00Z"}"#
        let older = paused.replacingOccurrences(of: pauseText, with: "")
        let unpaused = try JSONDecoder().decode(PersonLocation.self, from: Data(older.utf8))
        XCTAssertNil(unpaused.pause)
        XCTAssertFalse(unpaused.isPaused(at: date("2024-01-02T14:30:00Z")))
    }

    // MARK: What the choices mean

    func testForAnHourEndsAnHourFromNow() {
        let now = date("2024-01-02T09:30:00Z")
        XCTAssertEqual(LocationPauseChoice.anHour.endDate(from: now), date("2024-01-02T10:30:00Z"))
    }

    func testUntilResumedHasNoEnd() {
        XCTAssertNil(LocationPauseChoice.untilResumed.endDate(from: date("2024-01-02T09:30:00Z")))
    }

    func testUntilTomorrowMorningIsEightTheNextDayInTheZoneGiven() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "Asia/Tokyo"))
        // 23:30 on the 2nd in Tokyo.
        let lateEvening = date("2024-01-02T14:30:00Z")
        XCTAssertEqual(LocationPauseChoice.untilTomorrowMorning.endDate(from: lateEvening, calendar: calendar), date("2024-01-02T23:00:00Z"))
    }

    func testUntilTomorrowMorningBeforeFiveIsThisMorning() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "Asia/Tokyo"))
        // 00:30 on the 2nd in Tokyo means 08:00 the same day.
        let afterMidnight = date("2024-01-01T15:30:00Z")
        XCTAssertEqual(LocationPauseChoice.untilTomorrowMorning.endDate(from: afterMidnight, calendar: calendar), date("2024-01-01T23:00:00Z"))
        // 04:59 is still before the line, 05:00 is not.
        let justBeforeFive = date("2024-01-01T19:59:00Z")
        XCTAssertEqual(LocationPauseChoice.untilTomorrowMorning.endDate(from: justBeforeFive, calendar: calendar), date("2024-01-01T23:00:00Z"))
        let atFive = date("2024-01-01T20:00:00Z")
        XCTAssertEqual(LocationPauseChoice.untilTomorrowMorning.endDate(from: atFive, calendar: calendar), date("2024-01-02T23:00:00Z"))
    }

    // MARK: A pause made on the phone

    func testPausingOnThePhoneAppliesAtOnceAndWaitsForTheServer() {
        let now = date("2024-01-02T14:00:00Z")
        var tracker = LocationPauseTracker()
        XCTAssertFalse(tracker.isPaused(at: now))
        tracker.startPause(until: date("2024-01-02T15:00:00Z"), at: now)
        XCTAssertTrue(tracker.hasPendingChange)
        XCTAssertTrue(tracker.isPaused(at: now))
        XCTAssertTrue(tracker.isPaused(at: date("2024-01-02T14:59:00Z")))
        XCTAssertFalse(tracker.isPaused(at: date("2024-01-02T15:00:00Z")))
        XCTAssertEqual(tracker.endDate(at: now), date("2024-01-02T15:00:00Z"))
        XCTAssertEqual(tracker.pause?.id, LocationPauseTracker.unconfirmedPauseID)
    }

    func testTheServerTakingThePauseReplacesTheLocalOne() throws {
        let now = date("2024-01-02T14:00:00Z")
        var tracker = LocationPauseTracker()
        tracker.startPause(until: nil, at: now)
        let change = try XCTUnwrap(tracker.pendingChange)
        let answer = pause(from: "2024-01-02T14:00:01Z", to: nil)
        tracker.changeWasTaken(change, answer: answer, at: now)
        XCTAssertFalse(tracker.hasPendingChange)
        XCTAssertEqual(tracker.pause, answer)
        XCTAssertTrue(tracker.isPaused(at: date("2024-01-02T20:00:00Z")))
    }

    func testPausingAgainChangesTheEndOfThePauseInEffect() {
        let now = date("2024-01-02T14:30:00Z")
        var tracker = LocationPauseTracker()
        tracker.learn(pause(from: "2024-01-02T14:00:00Z", to: "2024-01-02T15:00:00Z"), at: now)
        tracker.startPause(until: date("2024-01-02T18:00:00Z"), at: now)
        XCTAssertEqual(tracker.pause?.id, "pause-1")
        XCTAssertEqual(tracker.pause?.startedDate, date("2024-01-02T14:00:00Z"))
        XCTAssertEqual(tracker.pause?.endedDate, date("2024-01-02T18:00:00Z"))
        XCTAssertTrue(tracker.isPaused(at: date("2024-01-02T17:00:00Z")))
    }

    func testResumingOnThePhoneEndsThePauseAtOnceAndWaitsForTheServer() throws {
        let now = date("2024-01-02T14:30:00Z")
        var tracker = LocationPauseTracker()
        tracker.learn(pause(from: "2024-01-02T14:00:00Z", to: nil), at: now)
        XCTAssertTrue(tracker.isPaused(at: now))
        tracker.endPause(at: now)
        XCTAssertEqual(tracker.pendingChange, .resume(at: now))
        XCTAssertFalse(tracker.isPaused(at: now))
        // What was measured while it lasted is still inside it.
        XCTAssertTrue(tracker.isPaused(at: date("2024-01-02T14:10:00Z")))
        let change = try XCTUnwrap(tracker.pendingChange)
        tracker.changeWasTaken(change, answer: nil, at: now)
        XCTAssertFalse(tracker.hasPendingChange)
        XCTAssertNil(tracker.pause)
    }

    func testResumingAPauseNotYetAnsweredForStillSendsTheResume() {
        let now = date("2024-01-02T14:00:00Z")
        var tracker = LocationPauseTracker()
        tracker.startPause(until: nil, at: now)
        tracker.endPause(at: date("2024-01-02T14:05:00Z"))
        // The pause may already be on its way, so the resume goes after it.
        XCTAssertEqual(tracker.pendingChange, .resume(at: date("2024-01-02T14:05:00Z")))
        XCTAssertFalse(tracker.isPaused(at: date("2024-01-02T14:06:00Z")))
    }

    func testTheAnswerToAPauseDoesNotUndoAResumeMadeBeforeIt() throws {
        let now = date("2024-01-02T14:00:00Z")
        var tracker = LocationPauseTracker()
        tracker.startPause(until: nil, at: now)
        let sent = try XCTUnwrap(tracker.pendingChange)
        // Resumed while the call was on its way, and the server began the pause a moment after the tap.
        tracker.endPause(at: date("2024-01-02T14:00:01Z"))
        tracker.changeWasTaken(sent, answer: pause(from: "2024-01-02T14:00:02Z", to: nil), at: date("2024-01-02T14:00:03Z"))
        XCTAssertTrue(tracker.hasPendingChange)
        XCTAssertFalse(tracker.isPaused(at: date("2024-01-02T14:00:03Z")))
        // The resume goes next, and the server's answer to it leaves no pause.
        let resume = try XCTUnwrap(tracker.pendingChange)
        tracker.changeWasTaken(resume, answer: nil, at: date("2024-01-02T14:00:04Z"))
        XCTAssertFalse(tracker.hasPendingChange)
        XCTAssertNil(tracker.pause)
    }

    func testAPauseNeverSentIsEndedByTheResumeTheServerAnswersWithNothing() throws {
        let now = date("2024-01-02T14:00:00Z")
        var tracker = LocationPauseTracker()
        tracker.startPause(until: nil, at: now)
        tracker.endPause(at: date("2024-01-02T14:05:00Z"))
        let resume = try XCTUnwrap(tracker.pendingChange)
        tracker.changeWasTaken(resume, answer: nil, at: date("2024-01-02T14:05:01Z"))
        XCTAssertEqual(tracker, LocationPauseTracker())
    }

    func testResumingWhenNotPausedDoesNothing() {
        var tracker = LocationPauseTracker()
        tracker.endPause(at: date("2024-01-02T14:05:00Z"))
        XCTAssertEqual(tracker, LocationPauseTracker())
    }

    func testAChangeWaitingForTheServerWinsOverWhatTheServerLastSaid() {
        let now = date("2024-01-02T14:00:00Z")
        var tracker = LocationPauseTracker()
        tracker.startPause(until: nil, at: now)
        // An answer that left before the pause reached the server.
        tracker.learn(nil, at: date("2024-01-02T14:00:05Z"))
        XCTAssertTrue(tracker.isPaused(at: date("2024-01-02T14:00:05Z")))
        XCTAssertTrue(tracker.hasPendingChange)
    }

    func testAPausePausedElsewhereIsLearnedAndALaterResumeToo() {
        let now = date("2024-01-02T14:30:00Z")
        var tracker = LocationPauseTracker()
        tracker.learn(pause(from: "2024-01-02T14:00:00Z", to: "2024-01-02T16:00:00Z"), at: now)
        XCTAssertTrue(tracker.isPaused(at: now))
        tracker.learn(nil, at: date("2024-01-02T14:45:00Z"))
        XCTAssertFalse(tracker.isPaused(at: date("2024-01-02T14:45:00Z")))
    }

    func testARefusedChangeIsDroppedAndTheServersPauseStands() throws {
        let now = date("2024-01-02T14:30:00Z")
        var tracker = LocationPauseTracker()
        tracker.learn(pause(from: "2024-01-02T14:00:00Z", to: "2024-01-02T15:00:00Z"), at: now)
        tracker.startPause(until: date("2024-01-02T18:00:00Z"), at: now)
        let change = try XCTUnwrap(tracker.pendingChange)
        tracker.changeWasRefused(change)
        XCTAssertFalse(tracker.hasPendingChange)
        XCTAssertEqual(tracker.pause?.endedDate, date("2024-01-02T15:00:00Z"))
    }

    func testANewerChangeSurvivesTheAnswerToAnOlderOne() throws {
        let now = date("2024-01-02T14:00:00Z")
        var tracker = LocationPauseTracker()
        tracker.startPause(until: date("2024-01-02T15:00:00Z"), at: now)
        let older = try XCTUnwrap(tracker.pendingChange)
        tracker.startPause(until: nil, at: date("2024-01-02T14:01:00Z"))
        tracker.changeWasTaken(older, answer: pause(from: "2024-01-02T14:00:00Z", to: "2024-01-02T15:00:00Z"), at: date("2024-01-02T14:01:00Z"))
        XCTAssertTrue(tracker.hasPendingChange)
        XCTAssertNil(tracker.pause?.endedDate)
    }

    func testATimedPauseThatEndedBeforeTheServerHeardIsDropped() {
        var tracker = LocationPauseTracker()
        tracker.startPause(until: date("2024-01-02T15:00:00Z"), at: date("2024-01-02T14:00:00Z"))
        tracker.settle(at: date("2024-01-02T15:00:00Z"))
        XCTAssertFalse(tracker.hasPendingChange)
        XCTAssertFalse(tracker.isPaused(at: date("2024-01-02T15:00:00Z")))
    }

    func testAResumeIsKeptWhenTheServerSaysThereIsNoPause() {
        let now = date("2024-01-02T14:30:00Z")
        var tracker = LocationPauseTracker()
        tracker.learn(pause(from: "2024-01-02T14:00:00Z", to: nil), at: now)
        tracker.endPause(at: now)
        tracker.learn(nil, at: date("2024-01-02T14:31:00Z"))
        // Sent anyway: the server does not need it, and it does no harm.
        XCTAssertTrue(tracker.hasPendingChange)
        XCTAssertFalse(tracker.isPaused(at: date("2024-01-02T14:31:00Z")))
    }

    func testAnAnswerFromBeforeThePauseDoesNotUndoAResume() throws {
        var tracker = LocationPauseTracker()
        tracker.startPause(until: nil, at: date("2024-01-02T14:00:00Z"))
        let sent = try XCTUnwrap(tracker.pendingChange)
        tracker.endPause(at: date("2024-01-02T14:00:01Z"))
        // A report answer that left the server before the pause reached it.
        tracker.learn(nil, at: date("2024-01-02T14:00:02Z"))
        tracker.changeWasTaken(sent, answer: pause(from: "2024-01-02T14:00:03Z", to: nil), at: date("2024-01-02T14:00:04Z"))
        XCTAssertTrue(tracker.hasPendingChange)
        XCTAssertFalse(tracker.isPaused(at: date("2024-01-02T14:00:04Z")))
    }

    // MARK: What is kept while paused

    func testAPositionInsideThePauseIsNotRecordable() {
        var tracker = LocationPauseTracker()
        tracker.learn(pause(from: "2024-01-02T14:00:00Z", to: "2024-01-02T15:00:00Z"), at: date("2024-01-02T14:30:00Z"))
        XCTAssertTrue(tracker.isRecordable(pointMeasuredAt: date("2024-01-02T13:59:00Z")))
        XCTAssertFalse(tracker.isRecordable(pointMeasuredAt: date("2024-01-02T14:00:00Z")))
        XCTAssertFalse(tracker.isRecordable(pointMeasuredAt: date("2024-01-02T14:59:00Z")))
        XCTAssertTrue(tracker.isRecordable(pointMeasuredAt: date("2024-01-02T15:00:00Z")))
    }

    func testAVisitIsJudgedByWhenItBeganOrByWhenItEndedIfTheBeginningWasNotSeen() {
        var tracker = LocationPauseTracker()
        tracker.learn(pause(from: "2024-01-02T14:00:00Z", to: "2024-01-02T15:00:00Z"), at: date("2024-01-02T14:30:00Z"))
        // Began before the pause, ended in it: sent whole, the server ends it at the pause's start.
        XCTAssertTrue(tracker.isRecordable(visitArrivedAt: date("2024-01-02T13:00:00Z"), departedAt: date("2024-01-02T14:20:00Z")))
        XCTAssertFalse(tracker.isRecordable(visitArrivedAt: date("2024-01-02T14:10:00Z"), departedAt: nil))
        XCTAssertFalse(tracker.isRecordable(visitArrivedAt: nil, departedAt: date("2024-01-02T14:10:00Z")))
        XCTAssertTrue(tracker.isRecordable(visitArrivedAt: date("2024-01-02T15:10:00Z"), departedAt: nil))
        XCTAssertTrue(tracker.isRecordable(visitArrivedAt: nil, departedAt: nil))
    }

    func testADepartureInsideThePauseIsToldAtThePausesStart() {
        var tracker = LocationPauseTracker()
        tracker.learn(pause(from: "2024-01-02T14:00:00Z", to: "2024-01-02T15:00:00Z"), at: date("2024-01-02T14:30:00Z"))
        XCTAssertEqual(tracker.departureDate(noticedAt: date("2024-01-02T14:20:00Z")), date("2024-01-02T14:00:00Z"))
        XCTAssertEqual(tracker.departureDate(noticedAt: date("2024-01-02T13:20:00Z")), date("2024-01-02T13:20:00Z"))
        XCTAssertEqual(tracker.departureDate(noticedAt: date("2024-01-02T15:20:00Z")), date("2024-01-02T15:20:00Z"))
    }

    // MARK: Keeping it

    func testTheTrackerSurvivesBeingKeptAndReadBack() throws {
        var tracker = LocationPauseTracker()
        tracker.learn(pause(from: "2024-01-02T14:00:00Z", to: nil), at: date("2024-01-02T14:30:00Z"))
        tracker.endPause(at: date("2024-01-02T14:30:00Z"))
        let read = try LocationPauseTracker.decoded(from: tracker.encoded())
        XCTAssertEqual(read, tracker)
        XCTAssertNotEqual(LocationPauseTracker.defaultsKey(accountKey: "a"), LocationPauseTracker.defaultsKey(accountKey: "b"))
    }

    // MARK: Sending it

    func testRetriesWaitLongerEachTimeUpToAQuarterOfAnHour() {
        XCTAssertEqual(LocationPauseRetry.delaySeconds(afterFailureCount: 1), 15)
        XCTAssertEqual(LocationPauseRetry.delaySeconds(afterFailureCount: 2), 30)
        XCTAssertEqual(LocationPauseRetry.delaySeconds(afterFailureCount: 3), 60)
        XCTAssertEqual(LocationPauseRetry.delaySeconds(afterFailureCount: 40), 900)
    }

    func testOnlyARefusalByTheServerIsFinal() {
        XCTAssertEqual(
            LocationPauseFailure.classify(TeaNodeError.refused("permission denied: location:use")), .refused("permission denied: location:use"))
        XCTAssertEqual(
            LocationPauseFailure.classify(TeaNodeError.refused("invalid arguments: the pause must end in the future")),
            .refused("invalid arguments: the pause must end in the future"))
        XCTAssertEqual(LocationPauseFailure.classify(TeaNodeError.refused("something went wrong")), .retryLater)
        XCTAssertEqual(LocationPauseFailure.classify(URLError(.notConnectedToInternet)), .retryLater)
    }

    func testPausesUntilTheEndGiven() async throws {
        let (connection, fake) = connection(
            answering: #"{"data":{"PauseLocation":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":"2024-01-02T15:00:00Z"}}}"#)
        let made = try await connection.pauseLocation(until: date("2024-01-02T15:00:00Z"))
        XCTAssertEqual(made.id, "pause-1")
        XCTAssertEqual(made.endedDate, date("2024-01-02T15:00:00Z"))
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["endedAt": .string("2024-01-02T15:00:00Z")])
    }

    func testPausesUntilResumedSendsNoEnd() async throws {
        let (connection, fake) = connection(
            answering: #"{"data":{"PauseLocation":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":null}}}"#)
        let made = try await connection.pauseLocation()
        XCTAssertTrue(made.isOpenEnded)
        XCTAssertEqual(try sentVariables(fake.sent[0]), [:])
    }

    func testResumes() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"ResumeLocation":true}}"#)
        let wasPaused = try await connection.resumeLocation()
        XCTAssertTrue(wasPaused)
        XCTAssertEqual(try sentVariables(fake.sent[0]), [:])
        let (otherConnection, _) = self.connection(answering: #"{"data":{"ResumeLocation":false}}"#)
        let wasPausedAgain = try await otherConnection.resumeLocation()
        XCTAssertFalse(wasPausedAgain)
    }

    func testReadsThePausesOfARange() async throws {
        let (connection, fake) = connection(
            answering:
                #"{"data":{"ListLocationPauses":[{"id":"pause-2","startedAt":"2024-01-02T14:00:00Z","endedAt":"2024-01-02T15:00:00Z"},{"id":"pause-1","startedAt":"2024-01-01T14:00:00Z","endedAt":null}]}}"#
        )
        let pauses = try await connection.locationPauses(userId: "user-2", from: date("2024-01-01T00:00:00Z"), until: date("2024-01-03T00:00:00Z"))
        XCTAssertEqual(pauses.map(\.id), ["pause-2", "pause-1"])
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["userId": .string("user-2"), "from": .string("2024-01-01T00:00:00Z"), "until": .string("2024-01-03T00:00:00Z")])
    }

    func testAReportAnswerCarriesThePauseAndWhatItDropped() async throws {
        let (connection, _) = connection(
            answering:
                #"{"data":{"ReportLocation":{"newPointCount":1,"newVisitCount":0,"updatedVisitCount":0,"pausedPointCount":4,"pausedVisitCount":1,"pause":{"id":"pause-1","startedAt":"2024-01-02T14:00:00Z","endedAt":null}}}}"#
        )
        let result = try await connection.reportLocation(LocationBatch(points: [], visits: []))
        XCTAssertEqual(result.pausedPointCount, 4)
        XCTAssertEqual(result.pausedVisitCount, 1)
        XCTAssertEqual(result.pause?.id, "pause-1")
    }

    func testAReportAnswerFromAServerThatCannotPauseDecodes() throws {
        let text = #"{"newPointCount":1,"newVisitCount":1,"updatedVisitCount":0}"#
        let result = try JSONDecoder().decode(ReportLocationResult.self, from: Data(text.utf8))
        XCTAssertNil(result.pause)
        XCTAssertEqual(result.pausedPointCount, 0)
        XCTAssertEqual(result.pausedVisitCount, 0)
    }

    func testTheLocationDocumentsAskForThePause() {
        for name in ["pauseLocation", "resumeLocation", "locationPauses"] {
            XCTAssertNotNil(Documents.locationDocuments[name], name)
        }
        XCTAssertTrue(Documents.location.contains("pause { id startedAt endedAt }"))
        XCTAssertTrue(Documents.sharedLocations.contains("pause { id startedAt endedAt }"))
        XCTAssertTrue(Documents.reportLocation.contains("pausedPointCount"))
    }

    // MARK: The timeline

    private func visit(_ id: String, from arrivedAt: String, to departedAt: String?) -> PlaceVisit {
        let place = LocationPlace(id: "place-1", placeName: "Fixture Park", latitudeDegrees: 10.5, longitudeDegrees: 20.5)
        return PlaceVisit(id: id, place: place, arrivedAt: arrivedAt, departedAt: departedAt, latitudeDegrees: 10.5, longitudeDegrees: 20.5)
    }

    func testAPauseIsListedAmongTheVisitsOfTheDayItBegan() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "UTC"))
        let visits = [
            visit("visit-1", from: "2024-01-02T09:00:00Z", to: "2024-01-02T10:00:00Z"),
            visit("visit-2", from: "2024-01-02T16:00:00Z", to: nil),
            visit("visit-3", from: "2024-01-01T09:00:00Z", to: "2024-01-01T10:00:00Z"),
        ]
        let pauses = [
            pause("pause-1", from: "2024-01-02T12:00:00Z", to: "2024-01-02T13:00:00Z"),
            pause("pause-2", from: "2024-01-03T07:00:00Z", to: "2024-01-03T08:00:00Z"),
        ]
        let days = LocationDays.group(visits, pauses: pauses, calendar: calendar)
        XCTAssertEqual(days.count, 3)
        XCTAssertEqual(days[0].entries.map(\.id), ["pause:pause-2"])
        XCTAssertEqual(days[1].entries.map(\.id), ["visit:visit-2", "pause:pause-1", "visit:visit-1"])
        XCTAssertEqual(days[1].visits.map(\.id), ["visit-2", "visit-1"])
        XCTAssertEqual(days[2].entries.map(\.id), ["visit:visit-3"])
    }

    func testGroupingWithNoPausesIsWhatItWas() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "UTC"))
        let days = LocationDays.group([visit("visit-1", from: "2024-01-02T09:00:00Z", to: nil)], calendar: calendar)
        XCTAssertEqual(days.count, 1)
        XCTAssertEqual(days[0].visits.map(\.id), ["visit-1"])
        XCTAssertEqual(days[0].entries.map(\.id), ["visit:visit-1"])
    }

    func testOneStretchAsATimelineIsNewestFirst() {
        let entries = LocationDays.entries(
            visits: [visit("visit-1", from: "2024-01-02T09:00:00Z", to: nil)],
            pauses: [
                pause("pause-1", from: "2024-01-02T12:00:00Z", to: nil),
                pause("pause-0", from: "2024-01-02T08:00:00Z", to: "2024-01-02T08:30:00Z"),
            ])
        XCTAssertEqual(entries.map(\.id), ["pause:pause-1", "visit:visit-1", "pause:pause-0"])
    }
}
