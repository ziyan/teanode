import Foundation

// The journal as the server answers it (internal/api/v1api/apigraph/journal.go).
// The keys are the server's json tags. A field the server may leave null (a
// Go pointer) is optional here; a list is never optional, since an empty one
// and a null one mean the same to a screen.

/// JournalList is a list the server may send as null, or with nulls in it
/// (a Go slice of pointers), read as the list without the nulls. A screen
/// never has to tell a missing list from an empty one.
@propertyWrapper
public struct JournalList<Element: Codable & Hashable & Sendable>: Codable, Hashable, Sendable {
    public var wrappedValue: [Element]

    public init(wrappedValue: [Element] = []) {
        self.wrappedValue = wrappedValue
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            wrappedValue = []
            return
        }
        wrappedValue = try container.decode([Element?].self).compactMap { $0 }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(wrappedValue)
    }
}

extension KeyedDecodingContainer {
    /// A list the answer does not have at all is an empty one.
    public func decode<Element>(_ type: JournalList<Element>.Type, forKey key: Key) throws -> JournalList<Element> {
        try decodeIfPresent(type, forKey: key) ?? JournalList(wrappedValue: [])
    }
}

// MARK: Parts of a day

/// JournalMood is a day's mood.
public struct JournalMood: Codable, Hashable, Sendable {
    /// 1 (awful) to 5 (great).
    public let moodLevel: Int
    public let moodWords: String
    /// "person", or "health" for Apple Health's State of Mind.
    public let moodSource: String

    public var isFromHealth: Bool { moodSource == "health" }
    public var level: JournalMoodLevel? { JournalMoodLevel(moodLevel: moodLevel) }
}

/// JournalWeather is a day's weather where the person spent most of it.
public struct JournalWeather: Codable, Hashable, Sendable {
    public let weatherCode: Int
    /// clear, partlyCloudy, cloudy, fog, drizzle, rain, snow, showers or
    /// thunderstorm.
    public let weatherKind: String
    public let highCelsius: Double
    public let lowCelsius: Double
    public let precipitationMM: Double
    public let placeName: String

    public var kind: JournalWeatherKind? { JournalWeatherKind(rawValue: weatherKind) }
}

/// JournalFacts is a day's numbers, or a period's sums.
public struct JournalFacts: Codable, Hashable, Sendable {
    public let photoCount: Int
    public let placeCount: Int
    public let walkedMeters: Double
    public let stepCount: Int
    public let sleepMinutes: Int
    public let workoutMinutes: Int
    public let codingSessionCount: Int
    public let codingMinutes: Int
    public let meetingCount: Int
    public let meetingMinutes: Int
    public let recordingCount: Int
    public let conversationCount: Int
    public let chatMessageCount: Int
    public let mailReceivedCount: Int
    public let mailSentCount: Int
    public let documentCount: Int
    public let transactionCount: Int
    public let spentMinorUnits: Int64
    public let spentCurrency: String
    public let shoppingTripCount: Int
    public let memoryPageCount: Int
    public let mainPlaceName: String
}

/// JournalNote is a note from the person's notes folder, shown read only on
/// its day.
public struct JournalNote: Codable, Hashable, Sendable, Identifiable {
    public let documentId: String
    public let title: String
    public let text: String
    public var id: String { documentId }
}

/// JournalWords is the person's text on a day or a period, with its
/// touched-up version.
public struct JournalWords: Codable, Hashable, Sendable {
    /// What the person wrote.
    public let original: String
    /// When the original was saved last; sent back with the next save, so
    /// that a day saved somewhere else since is refused.
    public let modifiedAt: String?
    public let isTouchUpOn: Bool
    public let touchedUp: String
    public let touchUpHow: String
    public let isTouchedUpStale: Bool
    public let touchUpPending: Bool
    @JournalList public var journalNotes: [JournalNote]

    /// Whether there is anything written to show.
    public var hasOriginal: Bool { !original.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    /// The version to read: the touched-up text once there is one, else the
    /// original.
    public var shownText: String {
        isTouchUpOn && !touchedUp.isEmpty ? touchedUp : original
    }

    /// Whether the text shown is the touched-up one.
    public var isTouchedUpShown: Bool { isTouchUpOn && !touchedUp.isEmpty }
}

/// JournalCorrection is something the person said a narrative got wrong.
public struct JournalCorrection: Codable, Hashable, Sendable {
    public let correctionText: String
    public let createdAt: String
}

/// JournalRecord is a record of the person's, found again: a link's or a
/// mention's.
public struct JournalRecord: Codable, Hashable, Sendable, Identifiable {
    public let recordKind: String
    public let recordId: String
    /// How a narrative writes it: kind:id.
    public let reference: String
    public let title: String
    public let happenedAt: String?
    /// Where the dashboard opens it; the app maps it to its own screens.
    public let dashboardPath: String
    /// Gone, or no longer the person's: it has no title.
    public let isUnavailable: Bool

    public var id: String { reference }
}

/// JournalNarrative is what the agent wrote about a day or a period.
public struct JournalNarrative: Codable, Hashable, Sendable {
    public let headline: String
    /// A few paragraphs separated by blank lines, in which a record is
    /// written [[kind:id]].
    public let narrative: String
    @JournalList public var references: [JournalRecord]
    public let writtenAt: String?
    public let isStale: Bool
    public let narrativePending: Bool
    /// Older than the depth dreams write back to: no narrative until the
    /// person asks for one.
    public let isBeyondDepth: Bool
    public let failure: String
    @JournalList public var corrections: [JournalCorrection]
    public let sourceCount: Int

    public var hasNarrative: Bool { !narrative.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
}

/// JournalLink is a record a day or a period links to: the agent's choice,
/// or the person's pin.
public struct JournalLink: Codable, Hashable, Sendable, Identifiable {
    public let linkId: String
    public let record: JournalRecord?
    /// "agent" for a choice, "person" for a pin.
    public let linkSource: String
    public let linkReason: String
    public let isHidden: Bool

    public var id: String { linkId }
}

// MARK: The timeline

/// JournalDaySummary is a day as the timeline, a week or a month shows it.
public struct JournalDaySummary: Codable, Hashable, Sendable, Identifiable {
    public let day: String
    public let isToday: Bool
    public let mood: JournalMood?
    public let weather: JournalWeather?
    public let headline: String
    /// The start of the words as shown, at most 280 characters.
    public let wordsExcerpt: String
    public let hasWords: Bool
    public let facts: JournalFacts?
    public let coverPhotoId: String
    @JournalList public var photoIds: [String]
    public let hasNarrative: Bool

    public var id: String { day }

    /// The photo that stands for the day: the cover, else the first.
    public var shownPhotoId: String? {
        if !coverPhotoId.isEmpty { return coverPhotoId }
        return photoIds.first
    }
}

/// JournalDayPage is a page of the timeline, newest day first.
public struct JournalDayPage: Codable, Hashable, Sendable {
    @JournalList public var days: [JournalDaySummary]
    public let totalCount: Int
    /// The person's today, YYYY-MM-DD, in their zone.
    public let today: String
    /// The earliest day with anything in it, empty for none.
    public let firstDay: String
}

// MARK: One day

/// JournalCodingSession is a stretch of work in Claude Code or Codex.
public struct JournalCodingSession: Codable, Hashable, Sendable, Identifiable {
    public let documentId: String
    public let title: String
    public let directory: String
    public let assistant: String
    public let startedAt: String
    public let endedAt: String?
    /// How many stretches of work (each ended by a pause) the day had in this
    /// project; absent from an older server, which sent one stretch a row.
    public let stretchCount: Int?
    public let linkReason: String
    public var id: String { documentId }
}

/// JournalDocument is an ingested document of the day that involves the
/// person.
public struct JournalDocument: Codable, Hashable, Sendable, Identifiable {
    public let documentId: String
    public let title: String
    public let sourceName: String
    public let happenedAt: String
    public let isChosen: Bool
    public let linkReason: String
    public var id: String { documentId }
}

/// JournalConversation is a conversation with the agent that had messages
/// that day.
public struct JournalConversation: Codable, Hashable, Sendable, Identifiable {
    public let conversationId: String
    public let title: String
    public let firstAt: String
    public let lastAt: String
    public let messageCount: Int
    public var id: String { conversationId }
}

/// JournalChat is a circle or group chat that had messages that day.
public struct JournalChat: Codable, Hashable, Sendable, Identifiable {
    public let chatId: String
    public let chatName: String
    public let firstAt: String
    public let lastAt: String
    public let messageCount: Int
    public var id: String { chatId }
}

/// JournalMail is a message that arrived or was sent that day.
public struct JournalMail: Codable, Hashable, Sendable, Identifiable {
    public let itemId: String
    public let fromName: String
    public let subject: String
    public let receivedAt: String
    public let isSent: Bool
    public let isImportant: Bool
    public let isChosen: Bool
    public let linkReason: String
    /// How many messages of the day it stands for: the same sender and
    /// subject arriving again is one row with a count.
    public let repeatCount: Int?
    public var id: String { itemId }
}

/// JournalRecording is a recording made that day.
public struct JournalRecording: Codable, Hashable, Sendable, Identifiable {
    public let recordingId: String
    public let title: String
    public let recordedAt: String?
    public let durationMS: Int64
    public let summary: String
    public var id: String { recordingId }
}

/// JournalPhoto is a photo taken that day.
public struct JournalPhoto: Codable, Hashable, Sendable, Identifiable {
    public let photoId: String
    public let takenAt: String
    public let placeName: String
    public let isVideo: Bool
    public var id: String { photoId }

    /// Where the server serves the photo's small preview.
    public var smallPreviewPath: String { JournalPhoto.smallPreviewPath(photoId: photoId) }

    public static func smallPreviewPath(photoId: String) -> String {
        "/api/v1/photos/\(photoId)/previews/small"
    }
}

/// JournalVisit is a stay at a place that day.
public struct JournalVisit: Codable, Hashable, Sendable, Identifiable {
    public let visitId: String
    public let placeId: String
    public let placeName: String
    public let arrivedAt: String?
    public let departedAt: String?
    public let latitude: Double
    public let longitude: Double
    public var id: String { visitId }
}

/// JournalEvent is an occurrence of a calendar event that day.
public struct JournalEvent: Codable, Hashable, Sendable, Identifiable {
    public let eventId: String
    public let title: String
    public let calendarName: String
    public let startsAt: String
    public let endsAt: String
    public let isAllDay: Bool
    public let location: String
    /// Names this occurrence: calendar_occurrence:eventId@start.
    public let reference: String
    public var id: String { reference }
}

/// JournalReminder is a reminder due or done that day.
public struct JournalReminder: Codable, Hashable, Sendable, Identifiable {
    public let reminderId: String
    public let title: String
    public let dueAt: String?
    public let doneAt: String?
    public var id: String { reminderId }
}

/// JournalTransaction is money in or out that day.
public struct JournalTransaction: Codable, Hashable, Sendable, Identifiable {
    public let transactionId: String
    public let postedOn: String
    public let transactedAt: String?
    public let description: String
    public let accountName: String
    /// Negative for money spent.
    public let amountMinorUnits: Int64
    public let currency: String
    public var id: String { transactionId }
}

/// JournalSleepStage is one stretch of sleep: deep, core, rem, awake or
/// inBed.
public struct JournalSleepStage: Codable, Hashable, Sendable {
    public let sleepStage: String
    public let minutes: Int
}

/// JournalWorkout is a workout that day.
public struct JournalWorkout: Codable, Hashable, Sendable, Identifiable {
    public let sampleId: String
    public let activityType: String
    public let startedAt: String
    public let minutes: Int
    public let meters: Double
    public var id: String { sampleId }
}

/// JournalHealth is a day's Health, from the day summaries the phone sent.
public struct JournalHealth: Codable, Hashable, Sendable {
    public let stepCount: Int
    @JournalList public var stepsByHour: [Int]
    public let sleepMinutes: Int
    @JournalList public var sleepStages: [JournalSleepStage]
    public let restingHeartRate: Double
    @JournalList public var workouts: [JournalWorkout]
}

/// JournalShoppingTrip is a trip to the shops that day.
public struct JournalShoppingTrip: Codable, Hashable, Sendable, Identifiable {
    public let tripId: String
    @JournalList public var listNames: [String]
    public let startedAt: String
    public let finishedAt: String?
    public let boughtCount: Int
    public let itemCount: Int
    public var id: String { tripId }
}

/// JournalMemoryPage is a memory page with a fact about that day.
public struct JournalMemoryPage: Codable, Hashable, Sendable, Identifiable {
    public let nodeId: String
    public let path: String
    public let title: String
    public let factCount: Int
    public var id: String { nodeId }
}

/// JournalMoment is one thing in the day's order.
public struct JournalMoment: Codable, Hashable, Sendable, Identifiable {
    public let at: String
    public let recordKind: String
    public let recordId: String
    public let title: String
    public let detail: String
    public let dashboardPath: String

    public var id: String { "\(at)|\(recordKind)|\(recordId)" }
}

/// JournalDay is one day whole.
public struct JournalDay: Codable, Hashable, Sendable {
    public let day: String
    public let timeZone: String
    public let isToday: Bool
    public let weekday: String
    /// The first day of the week this day is in.
    public let weekStart: String
    public let words: JournalWords?
    public let mood: JournalMood?
    public let weather: JournalWeather?
    public let facts: JournalFacts?
    public let narrative: JournalNarrative?
    /// The agent's choices (hidden ones included, marked) and the person's
    /// pins.
    @JournalList public var choices: [JournalLink]
    @JournalList public var pins: [JournalLink]
    public let coverPhotoId: String
    @JournalList public var codingSessions: [JournalCodingSession]
    @JournalList public var documents: [JournalDocument]
    public let documentTotal: Int
    @JournalList public var conversations: [JournalConversation]
    @JournalList public var chats: [JournalChat]
    @JournalList public var mail: [JournalMail]
    public let mailTotal: Int
    @JournalList public var recordings: [JournalRecording]
    @JournalList public var photos: [JournalPhoto]
    public let photoTotal: Int
    @JournalList public var visits: [JournalVisit]
    @JournalList public var events: [JournalEvent]
    @JournalList public var reminders: [JournalReminder]
    @JournalList public var transactions: [JournalTransaction]
    public let health: JournalHealth?
    @JournalList public var shoppingTrips: [JournalShoppingTrip]
    @JournalList public var notes: [JournalRecord]
    @JournalList public var memoryPages: [JournalMemoryPage]
    @JournalList public var unavailableSections: [String]
    @JournalList public var moments: [JournalMoment]

    /// Whether the touched-up version or the narrative is being made.
    public var isPending: Bool {
        (words?.touchUpPending ?? false) || (narrative?.narrativePending ?? false)
    }
}

// MARK: Weeks, months and years

/// JournalMonthSummary is a month as a year shows it.
public struct JournalMonthSummary: Codable, Hashable, Sendable, Identifiable {
    public let periodStart: String
    public let headline: String
    /// The month's days in order, each 0 (no mood) or 1 to 5.
    @JournalList public var dayMoods: [Int]
    public let placeCount: Int
    public let photoCount: Int
    public var id: String { periodStart }
}

/// JournalChapter is a stretch of a year that belongs together.
public struct JournalChapter: Codable, Hashable, Sendable {
    public let fromDay: String
    public let untilDay: String
    public let title: String
    public let detail: String
}

/// JournalPeriod is one week, month or year.
public struct JournalPeriod: Codable, Hashable, Sendable {
    /// "week", "month" or "year".
    public let periodKind: String
    public let periodStart: String
    /// Its last day, included.
    public let periodEnd: String
    public let isCurrent: Bool
    public let lookingBack: JournalWords?
    public let narrative: JournalNarrative?
    @JournalList public var choices: [JournalLink]
    /// The days in it, today and before only; a year leaves them out.
    @JournalList public var days: [JournalDaySummary]
    @JournalList public var months: [JournalMonthSummary]
    public let totals: JournalFacts?
    /// How many days had each mood, awful first; five numbers.
    @JournalList public var moodCounts: [Int]
    public let dayCount: Int
    @JournalList public var chapters: [JournalChapter]

    public var kind: JournalPeriodKind? { JournalPeriodKind(rawValue: periodKind) }

    public var isPending: Bool {
        (lookingBack?.touchUpPending ?? false) || (narrative?.narrativePending ?? false)
    }
}

// MARK: Changes and settings

/// JournalTouchUp is the touching up of a day or a period after a change.
public struct JournalTouchUp: Codable, Hashable, Sendable {
    public let day: String
    public let periodKind: String
    public let words: JournalWords?
}

/// JournalSettings is what a person decided about their journal.
public struct JournalSettings: Codable, Hashable, Sendable {
    public let isWeatherOn: Bool
    public let isEveningQuestionOn: Bool
    /// "HH:MM", in the person's zone.
    public let eveningQuestionTime: String
    /// "monday" or "sunday".
    public let weekStartsOn: String
    public let narrativeDepthDays: Int
    /// The provider the agent's model is run by: what the agent reads of the
    /// journal goes there.
    public let modelProviderName: String
}

/// JournalErased says what an erase removed.
public struct JournalErased: Codable, Hashable, Sendable {
    public let dayCount: Int
    public let periodCount: Int
    public let linkCount: Int
}
