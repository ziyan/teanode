package com.teanode.kit.journal

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonDecoder
import kotlinx.serialization.json.JsonNull
import java.net.URI

// The journal as the server answers it (internal/api/v1api/apigraph/journal.go). The keys are the server's json tags. A
// field the server may leave null (a Go pointer) is nullable here; a list is never nullable, since an empty one and a
// null one mean the same to a screen.

/**
 * JournalListSerializer reads a list the server may send as null, or with nulls in it (a Go slice of pointers), as the
 * list without the nulls. A screen never has to tell a missing list from an empty one.
 */
class JournalListSerializer<Element>(private val elementSerializer: KSerializer<Element>) : KSerializer<List<Element>> {
    private val delegate = ListSerializer(elementSerializer)

    override val descriptor: SerialDescriptor get() = delegate.descriptor

    override fun deserialize(decoder: Decoder): List<Element> {
        val jsonDecoder = decoder as JsonDecoder
        val element = jsonDecoder.decodeJsonElement()
        if (element is JsonNull) return emptyList()
        val entries = element as? kotlinx.serialization.json.JsonArray ?: return emptyList()
        return entries.filter { entry -> entry !is JsonNull }.map { entry -> jsonDecoder.json.decodeFromJsonElement(elementSerializer, entry) }
    }

    override fun serialize(encoder: Encoder, value: List<Element>) = delegate.serialize(encoder, value)
}

// MARK: Parts of a day

/** JournalMoodLevel is how a day was, as the person chose it. */
enum class JournalMoodLevel(val moodLevel: Int) {
    Awful(1), Low(2), Okay(3), Good(4), Great(5);

    companion object {
        /** The level a server's number stands for; null for none or one out of range. */
        fun of(moodLevel: Int?): JournalMoodLevel? = entries.firstOrNull { level -> level.moodLevel == moodLevel }
    }
}

/** JournalWeatherKind is the one word the server gives a day's weather. */
enum class JournalWeatherKind(val wireName: String) {
    Clear("clear"), PartlyCloudy("partlyCloudy"), Cloudy("cloudy"), Fog("fog"), Drizzle("drizzle"), Rain("rain"), Snow("snow"),
    Showers("showers"), Thunderstorm("thunderstorm");

    companion object {
        fun of(wireName: String): JournalWeatherKind? = entries.firstOrNull { kind -> kind.wireName == wireName }
    }
}

/** JournalPeriodKind is a stretch of days a period page covers. */
enum class JournalPeriodKind(val wireName: String) {
    Week("week"), Month("month"), Year("year");

    companion object {
        fun of(wireName: String): JournalPeriodKind? = entries.firstOrNull { kind -> kind.wireName == wireName }
    }
}

/** JournalMood is a day's mood. */
@Serializable
data class JournalMood(
    /** 1 (awful) to 5 (great). */
    val moodLevel: Int,
    val moodWords: String,
    /** "person", or "health" for Apple Health's State of Mind. */
    val moodSource: String,
) {
    val isFromHealth: Boolean get() = moodSource == "health"
    val level: JournalMoodLevel? get() = JournalMoodLevel.of(moodLevel)
}

/** JournalWeather is a day's weather where the person spent most of it. */
@Serializable
data class JournalWeather(
    val weatherCode: Int,
    /** clear, partlyCloudy, cloudy, fog, drizzle, rain, snow, showers or thunderstorm. */
    val weatherKind: String,
    val highCelsius: Double,
    val lowCelsius: Double,
    val precipitationMM: Double,
    val placeName: String,
) {
    val kind: JournalWeatherKind? get() = JournalWeatherKind.of(weatherKind)
}

/** JournalFacts is a day's numbers, or a period's sums. */
@Serializable
data class JournalFacts(
    val photoCount: Int,
    val placeCount: Int,
    val walkedMeters: Double,
    val stepCount: Int,
    val sleepMinutes: Int,
    val workoutMinutes: Int,
    val codingSessionCount: Int,
    /** How many projects were worked on, which a period adds up day by day; absent from a server that counts only sessions. */
    val workProjectCount: Int? = null,
    val codingMinutes: Int,
    val meetingCount: Int,
    val meetingMinutes: Int,
    val recordingCount: Int,
    val conversationCount: Int,
    val chatMessageCount: Int,
    val mailReceivedCount: Int,
    val mailSentCount: Int,
    val documentCount: Int,
    val transactionCount: Int,
    val spentMinorUnits: Long,
    val spentCurrency: String,
    val shoppingTripCount: Int,
    val memoryPageCount: Int,
    val mainPlaceName: String,
) {
    /** The projects worked on, none when the server did not say. */
    val projectCount: Int get() = workProjectCount ?: 0
}

/** JournalNote is a note from the person's notes folder, shown read only on its day. */
@Serializable
data class JournalNote(val documentId: String, val title: String, val text: String) {
    val id: String get() = documentId
}

/** JournalWords is the person's text on a day or a period, with its touched-up version. */
@Serializable
data class JournalWords(
    /** What the person wrote. */
    val original: String,
    /** When the original was saved last; sent back with the next save, so that a day saved somewhere else since is refused. */
    val modifiedAt: String? = null,
    val isTouchUpOn: Boolean,
    val touchedUp: String,
    val touchUpHow: String,
    val isTouchedUpStale: Boolean,
    val touchUpPending: Boolean,
    @Serializable(with = JournalListSerializer::class) val journalNotes: List<JournalNote> = emptyList(),
) {
    /** Whether there is anything written to show. */
    val hasOriginal: Boolean get() = original.isNotBlank()

    /** The version to read: the touched-up text once there is one, else the original. */
    val shownText: String get() = if (isTouchUpOn && touchedUp.isNotEmpty()) touchedUp else original

    /** Whether the text shown is the touched-up one. */
    val isTouchedUpShown: Boolean get() = isTouchUpOn && touchedUp.isNotEmpty()
}

/** JournalCorrection is something the person said a narrative got wrong. */
@Serializable
data class JournalCorrection(val correctionText: String, val createdAt: String)

/** JournalRecord is a record of the person's, found again: a link's or a mention's. */
@Serializable
data class JournalRecord(
    val recordKind: String,
    val recordId: String,
    /** How a narrative writes it: kind:id. */
    val reference: String,
    val title: String,
    val happenedAt: String? = null,
    /** Where the dashboard opens it; the app maps it to its own screens. */
    val dashboardPath: String,
    /** Gone, or no longer the person's: it has no title. */
    val isUnavailable: Boolean,
) {
    val id: String get() = reference
}

/** JournalNarrative is what the agent wrote about a day or a period. */
@Serializable
data class JournalNarrative(
    val headline: String,
    /** A few paragraphs separated by blank lines, in which a record is written [[kind:id]]. */
    val narrative: String,
    @Serializable(with = JournalListSerializer::class) val references: List<JournalRecord> = emptyList(),
    val writtenAt: String? = null,
    val isStale: Boolean,
    val narrativePending: Boolean,
    /** Older than the depth dreams write back to: no narrative until the person asks for one. */
    val isBeyondDepth: Boolean,
    val failure: String,
    @Serializable(with = JournalListSerializer::class) val corrections: List<JournalCorrection> = emptyList(),
    val sourceCount: Int,
) {
    val hasNarrative: Boolean get() = narrative.isNotBlank()
}

/** JournalLink is a record a day or a period links to: the agent's choice, or the person's pin. */
@Serializable
data class JournalLink(
    val linkId: String,
    val record: JournalRecord? = null,
    /** "agent" for a choice, "person" for a pin. */
    val linkSource: String,
    val linkReason: String,
    val isHidden: Boolean,
) {
    val id: String get() = linkId
}

// MARK: The timeline

/** JournalDaySummary is a day as the timeline, a week or a month shows it. */
@Serializable
data class JournalDaySummary(
    val day: String,
    val isToday: Boolean,
    val mood: JournalMood? = null,
    val weather: JournalWeather? = null,
    val headline: String,
    /** The start of the words as shown, at most 280 characters. */
    val wordsExcerpt: String,
    val hasWords: Boolean,
    val facts: JournalFacts? = null,
    val coverPhotoId: String,
    @Serializable(with = JournalListSerializer::class) val photoIds: List<String> = emptyList(),
    val hasNarrative: Boolean,
) {
    val id: String get() = day

    /** The photo that stands for the day: the cover, else the first. */
    val shownPhotoId: String? get() = coverPhotoId.ifEmpty { photoIds.firstOrNull() }
}

/** JournalDayPage is a page of the timeline, newest day first. */
@Serializable
data class JournalDayPage(
    @Serializable(with = JournalListSerializer::class) val days: List<JournalDaySummary> = emptyList(),
    val totalCount: Int,
    /** The person's today, YYYY-MM-DD, in their zone. */
    val today: String,
    /** The earliest day with anything in it, empty for none. */
    val firstDay: String,
)

// MARK: One day

/** JournalCodingSession is a stretch of work in Claude Code or Codex. */
@Serializable
data class JournalCodingSession(
    val documentId: String,
    val title: String,
    val directory: String,
    val assistant: String,
    val startedAt: String,
    val endedAt: String? = null,
    /** How many stretches of work (each ended by a pause) the day had in this project; absent from an older server. */
    val stretchCount: Int? = null,
    val linkReason: String,
) {
    val id: String get() = documentId
}

/** JournalShipped is a pull request, merge request or commit of the day. */
@Serializable
data class JournalShipped(
    val documentId: String,
    val title: String,
    /** Where the request lives, on the host that holds the repository. */
    val address: String,
    val happenedAt: String,
    val isDraft: Boolean,
) {
    val id: String get() = "$documentId|$address"

    /**
     * The address as a web page, or null for anything else: the address comes from a remote host, and a link never
     * runs a script or opens a file.
     */
    val webAddress: String?
        get() {
            val uri = try {
                URI(address)
            } catch (_: Exception) {
                return null
            }
            val scheme = uri.scheme?.lowercase()
            return if ((scheme == "https" || scheme == "http") && uri.host != null) address else null
        }
}

/**
 * JournalWorkProject is one project the person worked on in a day: the whole day's coding in a repository or folder,
 * which is what a person reads first. Its coding sessions are the details behind it.
 */
@Serializable
data class JournalWorkProject(
    /** The repository's or folder's name. */
    val name: String,
    val firstAt: String,
    val lastAt: String,
    /** The time spent in it, from the spans of its sessions. */
    val activeMinutes: Int,
    val stretchCount: Int,
    /** The titles of its sessions, each once. */
    @Serializable(with = JournalListSerializer::class) val topics: List<String> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val shipped: List<JournalShipped> = emptyList(),
    /** What the agent wrote was done in it, once the day is written; empty before. */
    val summary: String,
    /** The project's first session. */
    val documentId: String,
) {
    val id: String get() = "$name|$documentId"

    /** The topics to list, and how many more there are. */
    fun shownTopics(limit: Int = TOPIC_LIMIT): ShownTopics {
        val named = topics.filter { topic -> topic.isNotBlank() }
        val shown = named.take(maxOf(0, limit))
        return ShownTopics(shown, named.size - shown.size)
    }

    data class ShownTopics(val shown: List<String>, val hiddenCount: Int)

    companion object {
        /** How many topics are listed before "and N more". */
        const val TOPIC_LIMIT = 4
    }
}

/** JournalDocument is an ingested document of the day that involves the person. */
@Serializable
data class JournalDocument(
    val documentId: String,
    val title: String,
    val sourceName: String,
    val happenedAt: String,
    val isChosen: Boolean,
    val linkReason: String,
) {
    val id: String get() = documentId
}

/** JournalConversation is a conversation with the agent that had messages that day. */
@Serializable
data class JournalConversation(
    val conversationId: String,
    val title: String,
    /** The person's main conversation, which the app names in the person's own language; absent from an older server. */
    val isMain: Boolean? = null,
    val firstAt: String,
    val lastAt: String,
    val messageCount: Int,
) {
    val id: String get() = conversationId
    val isMainChat: Boolean get() = isMain ?: false
}

/** JournalChat is a circle or group chat that had messages that day. */
@Serializable
data class JournalChat(val chatId: String, val chatName: String, val firstAt: String, val lastAt: String, val messageCount: Int) {
    val id: String get() = chatId
}

/** JournalMail is a message that arrived or was sent that day. */
@Serializable
data class JournalMail(
    val itemId: String,
    val fromName: String,
    val subject: String,
    val receivedAt: String,
    val isSent: Boolean,
    val isImportant: Boolean,
    val isChosen: Boolean,
    val linkReason: String,
    /** How many messages of the day it stands for: the same sender and subject arriving again is one row with a count. */
    val repeatCount: Int? = null,
) {
    val id: String get() = itemId
}

/** JournalRecording is a recording made that day. */
@Serializable
data class JournalRecording(val recordingId: String, val title: String, val recordedAt: String? = null, val durationMS: Long, val summary: String) {
    val id: String get() = recordingId
}

/** JournalPhoto is a photo taken that day. */
@Serializable
data class JournalPhoto(val photoId: String, val takenAt: String, val placeName: String, val isVideo: Boolean) {
    val id: String get() = photoId

    /** Where the server serves the photo's small preview. */
    val smallPreviewPath: String get() = smallPreviewPath(photoId)

    companion object {
        fun smallPreviewPath(photoId: String): String = "/api/v1/photos/$photoId/previews/small"
    }
}

/** JournalVisit is a stay at a place that day. */
@Serializable
data class JournalVisit(
    val visitId: String,
    val placeId: String,
    val placeName: String,
    val arrivedAt: String? = null,
    val departedAt: String? = null,
    val latitude: Double,
    val longitude: Double,
) {
    val id: String get() = visitId
}

/** JournalEvent is an occurrence of a calendar event that day. */
@Serializable
data class JournalEvent(
    val eventId: String,
    val title: String,
    val calendarName: String,
    val startsAt: String,
    val endsAt: String,
    val isAllDay: Boolean,
    val location: String,
    /** Names this occurrence: calendar_occurrence:eventId@start. */
    val reference: String,
) {
    val id: String get() = reference
}

/** JournalReminder is a reminder due or done that day. */
@Serializable
data class JournalReminder(val reminderId: String, val title: String, val dueAt: String? = null, val doneAt: String? = null) {
    val id: String get() = reminderId
}

/** JournalTransaction is money in or out that day. */
@Serializable
data class JournalTransaction(
    val transactionId: String,
    val postedOn: String,
    val transactedAt: String? = null,
    val description: String,
    val accountName: String,
    /** Negative for money spent. */
    val amountMinorUnits: Long,
    val currency: String,
) {
    val id: String get() = transactionId
}

/** JournalSleepStage is one stretch of sleep: deep, core, rem, awake or inBed. */
@Serializable
data class JournalSleepStage(val sleepStage: String, val minutes: Int)

/** JournalWorkout is a workout that day. */
@Serializable
data class JournalWorkout(val sampleId: String, val activityType: String, val startedAt: String, val minutes: Int, val meters: Double) {
    val id: String get() = sampleId
}

/** JournalHealth is a day's Health, from the day summaries the phone sent. */
@Serializable
data class JournalHealth(
    val stepCount: Int,
    @Serializable(with = JournalListSerializer::class) val stepsByHour: List<Int> = emptyList(),
    val sleepMinutes: Int,
    @Serializable(with = JournalListSerializer::class) val sleepStages: List<JournalSleepStage> = emptyList(),
    val restingHeartRate: Double,
    @Serializable(with = JournalListSerializer::class) val workouts: List<JournalWorkout> = emptyList(),
)

/** JournalShoppingTrip is a trip to the shops that day. It does not keep what was bought, so it says nothing of it. */
@Serializable
data class JournalShoppingTrip(
    val tripId: String,
    /** The store, when the person named one; empty for none. */
    val storeName: String,
    /** The lists it was for that the person still reaches. */
    @Serializable(with = JournalListSerializer::class) val listNames: List<String> = emptyList(),
    val startedAt: String,
    val finishedAt: String? = null,
    /** Where the dashboard opens it. */
    val dashboardPath: String,
) {
    val id: String get() = tripId

    /** Where the app takes the person: the Shopping part. */
    val opening: JournalRecordOpening get() = JournalRecordOpening.of("shopping_trip", tripId, dashboardPath)
}

/** JournalMemoryPage is a memory page with a fact about that day. */
@Serializable
data class JournalMemoryPage(val nodeId: String, val path: String, val title: String, val factCount: Int) {
    val id: String get() = nodeId
}

/** JournalMoment is one thing in the day's order. */
@Serializable
data class JournalMoment(
    val at: String,
    val recordKind: String,
    val recordId: String,
    val title: String,
    val detail: String,
    val dashboardPath: String,
) {
    val id: String get() = "$at|$recordKind|$recordId"

    /** How a day's pins and choices name the record: kind:id. */
    val reference: String get() = "$recordKind:$recordId"

    /** Whether the moment can be pinned to its day: it names a record, and the record is not among the day's placedReferences already. */
    fun canPin(placedReferences: Set<String>): Boolean = recordKind.isNotEmpty() && recordId.isNotEmpty() && reference !in placedReferences
}

/** JournalDay is one day whole. */
@Serializable
data class JournalDay(
    val day: String,
    val timeZone: String,
    val isToday: Boolean,
    val weekday: String,
    /** The first day of the week this day is in. */
    val weekStart: String,
    val words: JournalWords? = null,
    val mood: JournalMood? = null,
    val weather: JournalWeather? = null,
    val facts: JournalFacts? = null,
    val narrative: JournalNarrative? = null,
    /** The agent's choices (hidden ones included, marked) and the person's pins. */
    @Serializable(with = JournalListSerializer::class) val choices: List<JournalLink> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val pins: List<JournalLink> = emptyList(),
    val coverPhotoId: String,
    /** The day's work by project, what is read first; the coding sessions and documents are its details. */
    @Serializable(with = JournalListSerializer::class) val workProjects: List<JournalWorkProject> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val codingSessions: List<JournalCodingSession> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val documents: List<JournalDocument> = emptyList(),
    val documentTotal: Int,
    @Serializable(with = JournalListSerializer::class) val conversations: List<JournalConversation> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val chats: List<JournalChat> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val mail: List<JournalMail> = emptyList(),
    val mailTotal: Int,
    @Serializable(with = JournalListSerializer::class) val recordings: List<JournalRecording> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val photos: List<JournalPhoto> = emptyList(),
    val photoTotal: Int,
    @Serializable(with = JournalListSerializer::class) val visits: List<JournalVisit> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val events: List<JournalEvent> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val reminders: List<JournalReminder> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val transactions: List<JournalTransaction> = emptyList(),
    val health: JournalHealth? = null,
    @Serializable(with = JournalListSerializer::class) val shoppingTrips: List<JournalShoppingTrip> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val notes: List<JournalRecord> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val memoryPages: List<JournalMemoryPage> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val unavailableSections: List<String> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val moments: List<JournalMoment> = emptyList(),
) {
    /** Whether the touched-up version or the narrative is being made. */
    val isPending: Boolean get() = (words?.touchUpPending ?: false) || (narrative?.narrativePending ?: false)

    /**
     * The records already on the day as a pin, or as a choice the person has not hidden: a moment of one of them has
     * nothing left to pin. A hidden choice can be pinned, and the server then makes it the person's.
     */
    val placedReferences: Set<String>
        get() {
            val references = HashSet<String>()
            for (link in pins) link.record?.let { record -> references.add(record.reference) }
            for (link in choices) if (!link.isHidden) link.record?.let { record -> references.add(record.reference) }
            return references
        }
}

// MARK: Weeks, months and years

/** JournalMonthSummary is a month as a year shows it. */
@Serializable
data class JournalMonthSummary(
    val periodStart: String,
    val headline: String,
    /** The month's days in order, each 0 (no mood) or 1 to 5. */
    @Serializable(with = JournalListSerializer::class) val dayMoods: List<Int> = emptyList(),
    val placeCount: Int,
    val photoCount: Int,
) {
    val id: String get() = periodStart
}

/** JournalChapter is a stretch of a year that belongs together. */
@Serializable
data class JournalChapter(val fromDay: String, val untilDay: String, val title: String, val detail: String)

/** JournalPeriod is one week, month or year. */
@Serializable
data class JournalPeriod(
    /** "week", "month" or "year". */
    val periodKind: String,
    val periodStart: String,
    /** Its last day, included. */
    val periodEnd: String,
    val isCurrent: Boolean,
    val lookingBack: JournalWords? = null,
    val narrative: JournalNarrative? = null,
    @Serializable(with = JournalListSerializer::class) val choices: List<JournalLink> = emptyList(),
    /** The days in it, today and before only; a year leaves them out. */
    @Serializable(with = JournalListSerializer::class) val days: List<JournalDaySummary> = emptyList(),
    @Serializable(with = JournalListSerializer::class) val months: List<JournalMonthSummary> = emptyList(),
    val totals: JournalFacts? = null,
    /** How many days had each mood, awful first; five numbers. */
    @Serializable(with = JournalListSerializer::class) val moodCounts: List<Int> = emptyList(),
    val dayCount: Int,
    @Serializable(with = JournalListSerializer::class) val chapters: List<JournalChapter> = emptyList(),
) {
    val kind: JournalPeriodKind? get() = JournalPeriodKind.of(periodKind)

    val isPending: Boolean get() = (lookingBack?.touchUpPending ?: false) || (narrative?.narrativePending ?: false)
}

// MARK: Changes and settings

/** JournalSettings is what a person decided about their journal. */
@Serializable
data class JournalSettings(
    val isWeatherOn: Boolean,
    val isEveningQuestionOn: Boolean,
    /** "HH:MM", in the person's zone. */
    val eveningQuestionTime: String,
    /** "monday" or "sunday". */
    val weekStartsOn: String,
    val narrativeDepthDays: Int,
    /** The provider the agent's model is run by: what the agent reads of the journal goes there. */
    val modelProviderName: String,
)

/** JournalErased says what an erase removed. */
@Serializable
data class JournalErased(val dayCount: Int, val periodCount: Int, val linkCount: Int)

// MARK: Search

/** JournalSearchHit is a day or a period whose words, headline or story hold what was searched for. */
@Serializable
data class JournalSearchHit(
    /** The day, or the first day of the period. */
    val day: String,
    /** "week", "month" or "year" for a period; empty for a day. */
    val periodKind: String,
    val headline: String,
    /** The matching words with some around them. */
    val snippet: String,
) {
    val id: String get() = "$periodKind|$day"

    /** The kind of period, or null for a day. */
    val kind: JournalPeriodKind? get() = JournalPeriodKind.of(periodKind)
}

/** JournalSearchPage is a page of hits, newest first. */
@Serializable
data class JournalSearchPage(
    @Serializable(with = JournalListSerializer::class) val hits: List<JournalSearchHit> = emptyList(),
    /** How many hits there are in all, of which the pages read so far are some. */
    val totalCount: Int,
)

/** JournalSearch is what the server takes of a search. */
object JournalSearch {
    /** How many hits one read brings. */
    const val PAGE_SIZE = 20

    /** The longest search the server answers, in characters (Unicode scalars, as the server counts them). */
    const val QUERY_CHARACTER_LIMIT = 200

    /** The words to search for: trimmed, and cut to the longest the server takes; null when there is nothing to search for. */
    fun normalized(typedText: String): String? {
        val trimmed = typedText.trim()
        if (trimmed.isEmpty()) return null
        val scalars = trimmed.codePoints().toArray()
        if (scalars.size <= QUERY_CHARACTER_LIMIT) return trimmed
        val cut = String(scalars, 0, QUERY_CHARACTER_LIMIT)
        // The cut may leave a space at its end.
        return cut.trim()
    }

    /** The hits read so far followed by the next page's, without one the pages share (a day written between two reads moves the rest down). */
    fun appending(page: List<JournalSearchHit>, hits: List<JournalSearchHit>): List<JournalSearchHit> {
        val known = hits.map { hit -> hit.id }.toSet()
        return hits + page.filter { hit -> hit.id !in known }
    }
}

// MARK: Long lists

/** JournalRows says how many rows of a long list to show: the first few, and all of them once the person asks. */
object JournalRows {
    /** How many rows a list shows before "Show all". */
    const val LIMIT = 6

    /** How many of the day's moments are shown before "Show all". */
    const val MOMENT_LIMIT = 12

    /** The part of a list to draw. */
    fun <Element> shown(rows: List<Element>, limit: Int = LIMIT, isShowingAll: Boolean): List<Element> =
        if (isShowingAll) rows else rows.take(maxOf(0, limit))

    /** Whether there are more rows than the limit, so that "Show all" has something to show. */
    fun <Element> hasMore(rows: List<Element>, limit: Int = LIMIT): Boolean = rows.size > limit
}
