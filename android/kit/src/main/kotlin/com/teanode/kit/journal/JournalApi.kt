package com.teanode.kit.journal

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive

// The journal's GraphQL (internal/api/v1api/apigraph/journal.go): the same text as the iPhone app's. A shared list of
// fields is written out in full, with no other shared list inside it, since the server's test reads each document out of
// the iPhone app's file and fills in only one level of them.

object JournalDocuments {
    private const val MOOD_FIELDS = "moodLevel moodWords moodSource"
    private const val WEATHER_FIELDS = "weatherCode weatherKind highCelsius lowCelsius precipitationMM placeName"
    private const val FACTS_FIELDS = """photoCount placeCount walkedMeters stepCount sleepMinutes workoutMinutes codingSessionCount workProjectCount codingMinutes
meetingCount meetingMinutes recordingCount conversationCount chatMessageCount mailReceivedCount mailSentCount
documentCount transactionCount spentMinorUnits spentCurrency shoppingTripCount memoryPageCount mainPlaceName"""
    private const val WORDS_FIELDS = """original modifiedAt isTouchUpOn touchedUp touchUpHow isTouchedUpStale touchUpPending
journalNotes { documentId title text }"""
    private const val RECORD_FIELDS = "recordKind recordId reference title happenedAt dashboardPath isUnavailable"
    private const val NARRATIVE_FIELDS = """headline narrative
references { recordKind recordId reference title happenedAt dashboardPath isUnavailable }
writtenAt isStale narrativePending isBeyondDepth failure
corrections { correctionText createdAt }
sourceCount"""
    private const val LINK_FIELDS = """linkId linkSource linkReason isHidden
record { recordKind recordId reference title happenedAt dashboardPath isUnavailable }"""
    private const val DAY_SUMMARY_FIELDS = """day isToday
mood { $MOOD_FIELDS }
weather { $WEATHER_FIELDS }
headline wordsExcerpt hasWords
facts { $FACTS_FIELDS }
coverPhotoId photoIds hasNarrative"""

    const val DAYS = """query (${'$'}fromDay: String, ${'$'}untilDay: String, ${'$'}first: Int, ${'$'}offset: Int) {
  ListJournalDays(fromDay: ${'$'}fromDay, untilDay: ${'$'}untilDay, first: ${'$'}first, offset: ${'$'}offset) {
    days {
      $DAY_SUMMARY_FIELDS
    }
    totalCount today firstDay
  }
}"""
    const val DAY = """query (${'$'}day: String!) {
  ReadJournalDay(day: ${'$'}day) {
    day timeZone isToday weekday weekStart
    words { $WORDS_FIELDS }
    mood { $MOOD_FIELDS }
    weather { $WEATHER_FIELDS }
    facts { $FACTS_FIELDS }
    narrative { $NARRATIVE_FIELDS }
    choices { $LINK_FIELDS }
    pins { $LINK_FIELDS }
    coverPhotoId
    workProjects {
      name firstAt lastAt activeMinutes stretchCount topics
      shipped { documentId title address happenedAt isDraft }
      summary documentId
    }
    codingSessions { documentId title directory assistant startedAt endedAt stretchCount linkReason }
    documents { documentId title sourceName happenedAt isChosen linkReason }
    documentTotal
    conversations { conversationId title isMain firstAt lastAt messageCount }
    chats { chatId chatName firstAt lastAt messageCount }
    mail { itemId fromName subject receivedAt isSent isImportant isChosen linkReason repeatCount }
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
    shoppingTrips { tripId storeName listNames startedAt finishedAt dashboardPath }
    notes { $RECORD_FIELDS }
    memoryPages { nodeId path title factCount }
    unavailableSections
    moments { at recordKind recordId title detail dashboardPath }
  }
}"""
    const val PERIOD = """query (${'$'}periodKind: String!, ${'$'}day: String!) {
  ReadJournalPeriod(periodKind: ${'$'}periodKind, day: ${'$'}day) {
    periodKind periodStart periodEnd isCurrent
    lookingBack { $WORDS_FIELDS }
    narrative { $NARRATIVE_FIELDS }
    choices { $LINK_FIELDS }
    days {
      $DAY_SUMMARY_FIELDS
    }
    months { periodStart headline dayMoods placeCount photoCount }
    totals { $FACTS_FIELDS }
    moodCounts dayCount
    chapters { fromDay untilDay title detail }
  }
}"""
    const val SETTINGS = """query {
  ReadJournalSettings { isWeatherOn isEveningQuestionOn eveningQuestionTime weekStartsOn narrativeDepthDays modelProviderName }
}"""
    const val SET_WORDS = """mutation (${'$'}day: String!, ${'$'}words: String!, ${'$'}baseModifiedAt: DateTime) {
  SetJournalWords(day: ${'$'}day, words: ${'$'}words, baseModifiedAt: ${'$'}baseModifiedAt) { day words { modifiedAt } }
}"""
    const val SET_MOOD = """mutation (${'$'}day: String!, ${'$'}moodLevel: Int, ${'$'}moodWords: String) {
  SetJournalMood(day: ${'$'}day, moodLevel: ${'$'}moodLevel, moodWords: ${'$'}moodWords) { day }
}"""
    const val SET_LOOKING_BACK = """mutation (${'$'}periodKind: String!, ${'$'}day: String!, ${'$'}lookingBack: String!, ${'$'}baseModifiedAt: DateTime) {
  SetJournalLookingBack(periodKind: ${'$'}periodKind, day: ${'$'}day, lookingBack: ${'$'}lookingBack, baseModifiedAt: ${'$'}baseModifiedAt) {
    periodStart lookingBack { modifiedAt }
  }
}"""
    const val TOUCH_UP_WORDS = """mutation (${'$'}day: String!, ${'$'}periodKind: String, ${'$'}how: String) {
  TouchUpJournalWords(day: ${'$'}day, periodKind: ${'$'}periodKind, how: ${'$'}how) { day periodKind }
}"""
    const val STOP_TOUCH_UP = """mutation (${'$'}day: String!, ${'$'}periodKind: String) {
  StopJournalTouchUp(day: ${'$'}day, periodKind: ${'$'}periodKind) { day periodKind }
}"""
    const val CORRECT_NARRATIVE = """mutation (${'$'}day: String!, ${'$'}periodKind: String, ${'$'}correctionText: String!) {
  CorrectJournalNarrative(day: ${'$'}day, periodKind: ${'$'}periodKind, correctionText: ${'$'}correctionText) { narrativePending }
}"""
    const val REMOVE_CORRECTION = """mutation (${'$'}day: String!, ${'$'}periodKind: String, ${'$'}correctionIndex: Int!) {
  RemoveJournalCorrection(day: ${'$'}day, periodKind: ${'$'}periodKind, correctionIndex: ${'$'}correctionIndex) { narrativePending }
}"""
    const val WRITE_NARRATIVE = """mutation (${'$'}day: String!, ${'$'}periodKind: String) {
  WriteJournalNarrative(day: ${'$'}day, periodKind: ${'$'}periodKind) { narrativePending }
}"""
    const val DELETE_NARRATIVE = """mutation (${'$'}day: String!, ${'$'}periodKind: String) {
  DeleteJournalNarrative(day: ${'$'}day, periodKind: ${'$'}periodKind) { narrativePending }
}"""
    const val PIN_RECORD = """mutation (${'$'}day: String!, ${'$'}recordKind: String!, ${'$'}recordId: String!, ${'$'}linkReason: String) {
  PinJournalRecord(day: ${'$'}day, recordKind: ${'$'}recordKind, recordId: ${'$'}recordId, linkReason: ${'$'}linkReason) { $LINK_FIELDS }
}"""
    const val REMOVE_LINK = """mutation (${'$'}linkId: String!) {
  RemoveJournalLink(linkId: ${'$'}linkId)
}"""
    const val SEARCH = """query (${'$'}query: String!, ${'$'}first: Int, ${'$'}offset: Int) {
  SearchJournal(query: ${'$'}query, first: ${'$'}first, offset: ${'$'}offset) {
    hits { day periodKind headline snippet }
    totalCount
  }
}"""
    const val UPDATE_SETTINGS = """mutation (${'$'}isWeatherOn: Boolean, ${'$'}isEveningQuestionOn: Boolean, ${'$'}eveningQuestionTime: String) {
  UpdateJournalSettings(isWeatherOn: ${'$'}isWeatherOn, isEveningQuestionOn: ${'$'}isEveningQuestionOn, eveningQuestionTime: ${'$'}eveningQuestionTime) {
    isWeatherOn isEveningQuestionOn eveningQuestionTime weekStartsOn narrativeDepthDays modelProviderName
  }
}"""
    const val ERASE = """mutation { EraseJournal { dayCount periodCount linkCount } }"""
}

/** JournalTarget is what a change to the journal's words, touching up or narrative is about: one day, or the week, month or year that contains a day. */
sealed class JournalTarget {
    data class Day(val day: String) : JournalTarget()

    data class Period(val periodKind: JournalPeriodKind, val containing: String) : JournalTarget()

    /** The arguments the server names a day or a period by. */
    val variables: Map<String, JsonElement>
        get() = when (this) {
            is Day -> mapOf("day" to JsonPrimitive(day))
            is Period -> mapOf("day" to JsonPrimitive(containing), "periodKind" to JsonPrimitive(periodKind.wireName))
        }
}

@Serializable
private class DaysAnswer(val ListJournalDays: JournalDayPage? = null)

@Serializable
private class DayAnswer(val ReadJournalDay: JournalDay? = null)

@Serializable
private class PeriodAnswer(val ReadJournalPeriod: JournalPeriod? = null)

@Serializable
private class SettingsAnswer(val ReadJournalSettings: JournalSettings? = null)

@Serializable
private class UpdateSettingsAnswer(val UpdateJournalSettings: JournalSettings? = null)

@Serializable
private class WordsStamp(val modifiedAt: String? = null)

@Serializable
private class SavedDay(val words: WordsStamp? = null)

@Serializable
private class SavedPeriod(val lookingBack: WordsStamp? = null)

@Serializable
private class SetWordsAnswer(val SetJournalWords: SavedDay? = null)

@Serializable
private class SetLookingBackAnswer(val SetJournalLookingBack: SavedPeriod? = null)

@Serializable
private class EmptySaved

@Serializable
private class SetMoodAnswer(val SetJournalMood: EmptySaved? = null)

@Serializable
private class TouchUpAnswer(val TouchUpJournalWords: EmptySaved? = null)

@Serializable
private class StopTouchUpAnswer(val StopJournalTouchUp: EmptySaved? = null)

@Serializable
private class NarrativeChangeAnswer(
    val CorrectJournalNarrative: EmptySaved? = null,
    val RemoveJournalCorrection: EmptySaved? = null,
    val WriteJournalNarrative: EmptySaved? = null,
    val DeleteJournalNarrative: EmptySaved? = null,
)

@Serializable
private class PinAnswer(val PinJournalRecord: JournalLink? = null)

@Serializable
private class RemoveLinkAnswer(val RemoveJournalLink: Boolean? = null)

@Serializable
private class SearchAnswer(val SearchJournal: JournalSearchPage? = null)

@Serializable
private class EraseAnswer(val EraseJournal: JournalErased? = null)

private fun text(value: String?): JsonElement = if (value == null) JsonNull else JsonPrimitive(value)

/** A page of the timeline, newest day first. Days with nothing in them are left out by the server, and so are days to come. */
suspend fun Connection.journalDays(fromDay: String? = null, untilDay: String? = null, first: Int = 31, offset: Int = 0): JournalDayPage {
    val variables = hashMapOf<String, JsonElement>("first" to JsonPrimitive(first), "offset" to JsonPrimitive(offset))
    if (fromDay != null) variables["fromDay"] = JsonPrimitive(fromDay)
    if (untilDay != null) variables["untilDay"] = JsonPrimitive(untilDay)
    return execute<DaysAnswer>(JournalDocuments.DAYS, variables).ListJournalDays ?: throw TeaNodeError.UnreadableAnswer
}

/** One day whole. */
suspend fun Connection.journalDay(day: String): JournalDay =
    execute<DayAnswer>(JournalDocuments.DAY, mapOf("day" to JsonPrimitive(day))).ReadJournalDay ?: throw TeaNodeError.UnreadableAnswer

/** The week, month or year that contains a day. */
suspend fun Connection.journalPeriod(periodKind: JournalPeriodKind, containing: String): JournalPeriod =
    execute<PeriodAnswer>(
        JournalDocuments.PERIOD, mapOf("periodKind" to JsonPrimitive(periodKind.wireName), "day" to JsonPrimitive(containing)),
    ).ReadJournalPeriod ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.journalSettings(): JournalSettings =
    execute<SettingsAnswer>(JournalDocuments.SETTINGS).ReadJournalSettings ?: throw TeaNodeError.UnreadableAnswer

/**
 * Saves the person's original words for a day or a period, and answers the time they were saved at, which the next
 * save sends as its base. baseModifiedAt is the modifiedAt the words were read with, sent back as it came: a save over
 * somebody else's since is refused.
 */
suspend fun Connection.setJournalWords(target: JournalTarget, words: String, baseModifiedAt: String?): String? {
    val variables = HashMap(target.variables)
    variables["baseModifiedAt"] = text(baseModifiedAt)
    return when (target) {
        is JournalTarget.Day -> {
            variables["words"] = JsonPrimitive(words)
            execute<SetWordsAnswer>(JournalDocuments.SET_WORDS, variables).SetJournalWords?.words?.modifiedAt
        }
        is JournalTarget.Period -> {
            variables["lookingBack"] = JsonPrimitive(words)
            execute<SetLookingBackAnswer>(JournalDocuments.SET_LOOKING_BACK, variables).SetJournalLookingBack?.lookingBack?.modifiedAt
        }
    }
}

/** Sets a day's mood; null clears it. moodWords is sent only when given. */
suspend fun Connection.setJournalMood(day: String, moodLevel: Int?, moodWords: String? = null) {
    val variables = hashMapOf<String, JsonElement>("day" to JsonPrimitive(day), "moodLevel" to if (moodLevel == null) JsonNull else JsonPrimitive(moodLevel))
    if (moodWords != null) variables["moodWords"] = JsonPrimitive(moodWords)
    execute<SetMoodAnswer>(JournalDocuments.SET_MOOD, variables)
}

/** Asks for the words to be touched up; how is what to change, null keeps the last instruction. The touched-up text arrives within a minute or so. */
suspend fun Connection.touchUpJournalWords(target: JournalTarget, how: String?) {
    execute<TouchUpAnswer>(JournalDocuments.TOUCH_UP_WORDS, target.variables + ("how" to text(how)))
}

/** Turns touching up off: the touched-up text is deleted and the original shows again. */
suspend fun Connection.stopJournalTouchUp(target: JournalTarget) {
    execute<StopTouchUpAnswer>(JournalDocuments.STOP_TOUCH_UP, target.variables)
}

/** Adds the person's correction to a narrative, which is written again. */
suspend fun Connection.correctJournalNarrative(target: JournalTarget, correctionText: String) {
    execute<NarrativeChangeAnswer>(JournalDocuments.CORRECT_NARRATIVE, target.variables + ("correctionText" to JsonPrimitive(correctionText)))
}

/** Removes one correction, by its place in the list from 0. */
suspend fun Connection.removeJournalCorrection(target: JournalTarget, correctionIndex: Int) {
    execute<NarrativeChangeAnswer>(JournalDocuments.REMOVE_CORRECTION, target.variables + ("correctionIndex" to JsonPrimitive(correctionIndex)))
}

/** Asks for the narrative to be written again now. */
suspend fun Connection.writeJournalNarrative(target: JournalTarget) {
    execute<NarrativeChangeAnswer>(JournalDocuments.WRITE_NARRATIVE, target.variables)
}

/** Removes the story of a day or a period. The corrections stay, and the next dream writes a new story. */
suspend fun Connection.deleteJournalNarrative(target: JournalTarget) {
    execute<NarrativeChangeAnswer>(JournalDocuments.DELETE_NARRATIVE, target.variables)
}

/**
 * Pins a record to a day, and answers the pin. A record already pinned there is answered as it is; a choice of the
 * agent becomes the person's, and is shown again if they had hidden it.
 */
suspend fun Connection.pinJournalRecord(day: String, recordKind: String, recordId: String, linkReason: String? = null): JournalLink {
    val variables = hashMapOf<String, JsonElement>("day" to JsonPrimitive(day), "recordKind" to JsonPrimitive(recordKind), "recordId" to JsonPrimitive(recordId))
    if (linkReason != null) variables["linkReason"] = JsonPrimitive(linkReason)
    return execute<PinAnswer>(JournalDocuments.PIN_RECORD, variables).PinJournalRecord ?: throw TeaNodeError.UnreadableAnswer
}

/** Takes a pin off its day, or hides one of the agent's choices, which the server then keeps hidden. */
suspend fun Connection.removeJournalLink(linkId: String) {
    if (execute<RemoveLinkAnswer>(JournalDocuments.REMOVE_LINK, mapOf("linkId" to JsonPrimitive(linkId))).RemoveJournalLink != true) {
        throw TeaNodeError.UnreadableAnswer
    }
}

/**
 * A page of the days and periods whose words, headline or story hold the query, newest first. The query is as the
 * server takes it: 1 to JournalSearch.QUERY_CHARACTER_LIMIT characters.
 */
suspend fun Connection.searchJournal(query: String, first: Int = JournalSearch.PAGE_SIZE, offset: Int = 0): JournalSearchPage =
    execute<SearchAnswer>(
        JournalDocuments.SEARCH, mapOf("query" to JsonPrimitive(query), "first" to JsonPrimitive(first), "offset" to JsonPrimitive(offset)),
    ).SearchJournal ?: throw TeaNodeError.UnreadableAnswer

/** Changes what the person decided about their journal; a setting left null keeps its value. */
suspend fun Connection.updateJournalSettings(isWeatherOn: Boolean? = null, isEveningQuestionOn: Boolean? = null, eveningQuestionTime: String? = null): JournalSettings {
    val variables = HashMap<String, JsonElement>()
    if (isWeatherOn != null) variables["isWeatherOn"] = JsonPrimitive(isWeatherOn)
    if (isEveningQuestionOn != null) variables["isEveningQuestionOn"] = JsonPrimitive(isEveningQuestionOn)
    if (eveningQuestionTime != null) variables["eveningQuestionTime"] = JsonPrimitive(eveningQuestionTime)
    return execute<UpdateSettingsAnswer>(JournalDocuments.UPDATE_SETTINGS, variables).UpdateJournalSettings ?: throw TeaNodeError.UnreadableAnswer
}

/** Erases the whole journal: every day, period, link, word, mood, narrative and correction. What it linked to stays. */
suspend fun Connection.eraseJournal(): JournalErased =
    execute<EraseAnswer>(JournalDocuments.ERASE).EraseJournal ?: throw TeaNodeError.UnreadableAnswer
