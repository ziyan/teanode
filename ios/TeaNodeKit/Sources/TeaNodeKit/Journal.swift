import Foundation

// The journal's GraphQL (internal/api/v1api/apigraph/journal.go). A shared list
// of fields is written out in full, with no other shared list inside it, since
// the server's test reads each document out of this file and fills in only one
// level of them.

extension Documents {
    static let journalMoodFields = """
        moodLevel moodWords moodSource
        """
    static let journalWeatherFields = """
        weatherCode weatherKind highCelsius lowCelsius precipitationMM placeName
        """
    static let journalFactsFields = """
        photoCount placeCount walkedMeters stepCount sleepMinutes workoutMinutes codingSessionCount codingMinutes
        meetingCount meetingMinutes recordingCount conversationCount chatMessageCount mailReceivedCount mailSentCount
        documentCount transactionCount spentMinorUnits spentCurrency shoppingTripCount memoryPageCount mainPlaceName
        """
    static let journalWordsFields = """
        original modifiedAt isTouchUpOn touchedUp touchUpHow isTouchedUpStale touchUpPending
        journalNotes { documentId title text }
        """
    static let journalRecordFields = """
        recordKind recordId reference title happenedAt dashboardPath isUnavailable
        """
    static let journalNarrativeFields = """
        headline narrative
        references { recordKind recordId reference title happenedAt dashboardPath isUnavailable }
        writtenAt isStale narrativePending isBeyondDepth failure
        corrections { correctionText createdAt }
        sourceCount
        """
    static let journalLinkFields = """
        linkId linkSource linkReason isHidden
        record { recordKind recordId reference title happenedAt dashboardPath isUnavailable }
        """

    public static let journalDays = """
        query ($fromDay: String, $untilDay: String, $first: Int, $offset: Int) {
          ListJournalDays(fromDay: $fromDay, untilDay: $untilDay, first: $first, offset: $offset) {
            days {
              day isToday
              mood { \(journalMoodFields) }
              weather { \(journalWeatherFields) }
              headline wordsExcerpt hasWords
              facts { \(journalFactsFields) }
              coverPhotoId photoIds hasNarrative
            }
            totalCount today firstDay
          }
        }
        """
    public static let journalDay = """
        query ($day: String!) {
          ReadJournalDay(day: $day) {
            day timeZone isToday weekday weekStart
            words { \(journalWordsFields) }
            mood { \(journalMoodFields) }
            weather { \(journalWeatherFields) }
            facts { \(journalFactsFields) }
            narrative { \(journalNarrativeFields) }
            choices { \(journalLinkFields) }
            pins { \(journalLinkFields) }
            coverPhotoId
            codingSessions { documentId title directory assistant startedAt endedAt linkReason }
            documents { documentId title sourceName happenedAt isChosen linkReason }
            documentTotal
            conversations { conversationId title firstAt lastAt messageCount }
            chats { chatId chatName firstAt lastAt messageCount }
            mail { itemId fromName subject receivedAt isSent isImportant isChosen linkReason }
            mailTotal
            recordings { recordingId title recordedAt durationMS summary }
            photos { photoId takenAt placeName isVideo }
            photoTotal
            visits { visitId placeId placeName arrivedAt departedAt latitude longitude }
            events { eventId title calendarName startsAt endsAt isAllDay location reference }
            reminders { reminderId title dueAt doneAt }
            transactions { transactionId postedOn transactedAt description accountName amountMinorUnits currency }
            health {
              stepCount stepsByHour sleepMinutes
              sleepStages { sleepStage minutes }
              restingHeartRate
              workouts { sampleId activityType startedAt minutes meters }
            }
            shoppingTrips { tripId listNames startedAt finishedAt boughtCount itemCount }
            notes { \(journalRecordFields) }
            memoryPages { nodeId path title factCount }
            unavailableSections
            moments { at recordKind recordId title detail dashboardPath }
          }
        }
        """
    public static let journalPeriod = """
        query ($periodKind: String!, $day: String!) {
          ReadJournalPeriod(periodKind: $periodKind, day: $day) {
            periodKind periodStart periodEnd isCurrent
            lookingBack { \(journalWordsFields) }
            narrative { \(journalNarrativeFields) }
            choices { \(journalLinkFields) }
            days {
              day isToday
              mood { \(journalMoodFields) }
              weather { \(journalWeatherFields) }
              headline wordsExcerpt hasWords
              facts { \(journalFactsFields) }
              coverPhotoId photoIds hasNarrative
            }
            months { periodStart headline dayMoods placeCount photoCount }
            totals { \(journalFactsFields) }
            moodCounts dayCount
            chapters { fromDay untilDay title detail }
          }
        }
        """
    public static let journalSettings = """
        query {
          ReadJournalSettings { isWeatherOn isEveningQuestionOn eveningQuestionTime weekStartsOn narrativeDepthDays modelProviderName }
        }
        """
    public static let setJournalWords = """
        mutation ($day: String!, $words: String!, $baseModifiedAt: DateTime) {
          SetJournalWords(day: $day, words: $words, baseModifiedAt: $baseModifiedAt) { day }
        }
        """
    public static let setJournalMood = """
        mutation ($day: String!, $moodLevel: Int, $moodWords: String) {
          SetJournalMood(day: $day, moodLevel: $moodLevel, moodWords: $moodWords) { day }
        }
        """
    public static let setJournalLookingBack = """
        mutation ($periodKind: String!, $day: String!, $lookingBack: String!, $baseModifiedAt: DateTime) {
          SetJournalLookingBack(periodKind: $periodKind, day: $day, lookingBack: $lookingBack, baseModifiedAt: $baseModifiedAt) { periodStart }
        }
        """
    public static let touchUpJournalWords = """
        mutation ($day: String!, $periodKind: String, $how: String) {
          TouchUpJournalWords(day: $day, periodKind: $periodKind, how: $how) { day periodKind }
        }
        """
    public static let stopJournalTouchUp = """
        mutation ($day: String!, $periodKind: String) {
          StopJournalTouchUp(day: $day, periodKind: $periodKind) { day periodKind }
        }
        """
    public static let correctJournalNarrative = """
        mutation ($day: String!, $periodKind: String, $correctionText: String!) {
          CorrectJournalNarrative(day: $day, periodKind: $periodKind, correctionText: $correctionText) { narrativePending }
        }
        """
    public static let removeJournalCorrection = """
        mutation ($day: String!, $periodKind: String, $correctionIndex: Int!) {
          RemoveJournalCorrection(day: $day, periodKind: $periodKind, correctionIndex: $correctionIndex) { narrativePending }
        }
        """
    public static let writeJournalNarrative = """
        mutation ($day: String!, $periodKind: String) {
          WriteJournalNarrative(day: $day, periodKind: $periodKind) { narrativePending }
        }
        """
    public static let updateJournalSettings = """
        mutation ($isWeatherOn: Boolean, $isEveningQuestionOn: Boolean, $eveningQuestionTime: String) {
          UpdateJournalSettings(isWeatherOn: $isWeatherOn, isEveningQuestionOn: $isEveningQuestionOn, eveningQuestionTime: $eveningQuestionTime) {
            isWeatherOn isEveningQuestionOn eveningQuestionTime weekStartsOn narrativeDepthDays modelProviderName
          }
        }
        """
    public static let eraseJournal = """
        mutation { EraseJournal { dayCount periodCount linkCount } }
        """
}

/// JournalTarget is what a change to the journal's words, touching up or
/// narrative is about: one day, or the week, month or year that contains a day.
public enum JournalTarget: Hashable, Sendable {
    case day(String)
    case period(JournalPeriodKind, containing: String)

    /// The arguments the server names a day or a period by.
    var variables: [String: JSONValue] {
        switch self {
        case .day(let day):
            return ["day": .string(day)]
        case .period(let periodKind, let day):
            return ["day": .string(day), "periodKind": .string(periodKind.rawValue)]
        }
    }
}

extension Connection {
    /// A page of the timeline, newest day first. Days with nothing in them
    /// are left out by the server, and so are days to come.
    public func journalDays(fromDay: String? = nil, untilDay: String? = nil, first: Int = 31, offset: Int = 0) async throws -> JournalDayPage {
        struct Answer: Decodable, Sendable { let ListJournalDays: JournalDayPage? }
        var variables: [String: JSONValue] = ["first": .number(Double(first)), "offset": .number(Double(offset))]
        if let fromDay { variables["fromDay"] = .string(fromDay) }
        if let untilDay { variables["untilDay"] = .string(untilDay) }
        guard let page = try await execute(Documents.journalDays, variables: variables, as: Answer.self).ListJournalDays else {
            throw TeaNodeError.unreadableAnswer
        }
        return page
    }

    /// One day whole.
    public func journalDay(_ day: String) async throws -> JournalDay {
        struct Answer: Decodable, Sendable { let ReadJournalDay: JournalDay? }
        guard let read = try await execute(Documents.journalDay, variables: ["day": .string(day)], as: Answer.self).ReadJournalDay else {
            throw TeaNodeError.unreadableAnswer
        }
        return read
    }

    /// The week, month or year that contains a day.
    public func journalPeriod(_ periodKind: JournalPeriodKind, containing day: String) async throws -> JournalPeriod {
        struct Answer: Decodable, Sendable { let ReadJournalPeriod: JournalPeriod? }
        let variables: [String: JSONValue] = ["periodKind": .string(periodKind.rawValue), "day": .string(day)]
        guard let read = try await execute(Documents.journalPeriod, variables: variables, as: Answer.self).ReadJournalPeriod else {
            throw TeaNodeError.unreadableAnswer
        }
        return read
    }

    public func journalSettings() async throws -> JournalSettings {
        struct Answer: Decodable, Sendable { let ReadJournalSettings: JournalSettings? }
        guard let read = try await execute(Documents.journalSettings, as: Answer.self).ReadJournalSettings else {
            throw TeaNodeError.unreadableAnswer
        }
        return read
    }

    // MARK: Writing

    /// Saves the person's original words for a day or a period. baseModifiedAt
    /// is the modifiedAt the words were read with, sent back as it came: a
    /// save over somebody else's since is refused.
    public func setJournalWords(for target: JournalTarget, words: String, baseModifiedAt: String?) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable {}
            let SetJournalWords: Saved?
            let SetJournalLookingBack: Saved?
        }
        var variables = target.variables
        variables["baseModifiedAt"] = baseModifiedAt.map { JSONValue.string($0) } ?? JSONValue.null
        switch target {
        case .day:
            variables["words"] = .string(words)
            _ = try await execute(Documents.setJournalWords, variables: variables, as: Answer.self)
        case .period:
            variables["lookingBack"] = .string(words)
            _ = try await execute(Documents.setJournalLookingBack, variables: variables, as: Answer.self)
        }
    }

    /// Sets a day's mood; nil clears it. moodWords is sent only when given.
    public func setJournalMood(day: String, moodLevel: Int?, moodWords: String? = nil) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable {}
            let SetJournalMood: Saved?
        }
        let moodLevelValue: JSONValue = moodLevel.map { JSONValue.number(Double($0)) } ?? JSONValue.null
        var variables: [String: JSONValue] = ["day": .string(day), "moodLevel": moodLevelValue]
        if let moodWords { variables["moodWords"] = .string(moodWords) }
        _ = try await execute(Documents.setJournalMood, variables: variables, as: Answer.self)
    }

    /// Asks for the words to be touched up; how is what to change, nil keeps
    /// the last instruction. The touched-up text arrives within a minute or so.
    public func touchUpJournalWords(for target: JournalTarget, how: String?) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable {}
            let TouchUpJournalWords: Saved?
        }
        var variables = target.variables
        variables["how"] = how.map { JSONValue.string($0) } ?? JSONValue.null
        _ = try await execute(Documents.touchUpJournalWords, variables: variables, as: Answer.self)
    }

    /// Turns touching up off: the touched-up text is deleted and the original
    /// shows again.
    public func stopJournalTouchUp(for target: JournalTarget) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable {}
            let StopJournalTouchUp: Saved?
        }
        _ = try await execute(Documents.stopJournalTouchUp, variables: target.variables, as: Answer.self)
    }

    // MARK: The narrative

    /// Adds the person's correction to a narrative, which is written again.
    public func correctJournalNarrative(for target: JournalTarget, correctionText: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable {}
            let CorrectJournalNarrative: Saved?
        }
        var variables = target.variables
        variables["correctionText"] = .string(correctionText)
        _ = try await execute(Documents.correctJournalNarrative, variables: variables, as: Answer.self)
    }

    /// Removes one correction, by its place in the list from 0.
    public func removeJournalCorrection(for target: JournalTarget, correctionIndex: Int) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable {}
            let RemoveJournalCorrection: Saved?
        }
        var variables = target.variables
        variables["correctionIndex"] = .number(Double(correctionIndex))
        _ = try await execute(Documents.removeJournalCorrection, variables: variables, as: Answer.self)
    }

    /// Asks for the narrative to be written again now.
    public func writeJournalNarrative(for target: JournalTarget) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable {}
            let WriteJournalNarrative: Saved?
        }
        _ = try await execute(Documents.writeJournalNarrative, variables: target.variables, as: Answer.self)
    }

    // MARK: Settings

    /// Changes what the person decided about their journal; a setting left
    /// nil keeps its value.
    public func updateJournalSettings(isWeatherOn: Bool? = nil, isEveningQuestionOn: Bool? = nil, eveningQuestionTime: String? = nil) async throws -> JournalSettings {
        struct Answer: Decodable, Sendable { let UpdateJournalSettings: JournalSettings? }
        var variables: [String: JSONValue] = [:]
        if let isWeatherOn { variables["isWeatherOn"] = .bool(isWeatherOn) }
        if let isEveningQuestionOn { variables["isEveningQuestionOn"] = .bool(isEveningQuestionOn) }
        if let eveningQuestionTime { variables["eveningQuestionTime"] = .string(eveningQuestionTime) }
        guard let saved = try await execute(Documents.updateJournalSettings, variables: variables, as: Answer.self).UpdateJournalSettings else {
            throw TeaNodeError.unreadableAnswer
        }
        return saved
    }

    /// Erases the whole journal: every day, period, link, word, mood,
    /// narrative and correction. What it linked to stays.
    public func eraseJournal() async throws -> JournalErased {
        struct Answer: Decodable, Sendable { let EraseJournal: JournalErased? }
        guard let erased = try await execute(Documents.eraseJournal, as: Answer.self).EraseJournal else {
            throw TeaNodeError.unreadableAnswer
        }
        return erased
    }
}
