package com.teanode.app.journal

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import com.teanode.app.demo.text
import java.time.DayOfWeek
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.temporal.TemporalAdjusters

private val today: LocalDate get() = LocalDate.now()

private fun at(day: LocalDate, hour: Int, minute: Int = 0): String = day.atTime(hour, minute).toInstant(ZoneOffset.UTC).toString()

private fun quoted(text: String?): String = if (text == null) "null" else "\"" + text.replace("\\", "\\\\").replace("\"", "\\\"").replace("\n", "\\n") + "\""

private fun facts(photoCount: Int, placeCount: Int, spentMinorUnits: Int = 0, codingMinutes: Int = 0): String =
    """{"photoCount":$photoCount,"placeCount":$placeCount,"walkedMeters":${placeCount * 900.0},"stepCount":${placeCount * 2300},"sleepMinutes":${if (placeCount > 0) 440 else 0},"workoutMinutes":${if (placeCount > 1) 35 else 0},
    "codingSessionCount":${if (codingMinutes > 0) 2 else 0},"workProjectCount":${if (codingMinutes > 0) 2 else 0},"codingMinutes":$codingMinutes,"meetingCount":${if (codingMinutes > 0) 2 else 0},"meetingMinutes":${if (codingMinutes > 0) 60 else 0},
    "recordingCount":0,"conversationCount":1,"chatMessageCount":${if (codingMinutes > 0) 14 else 0},"mailReceivedCount":${if (codingMinutes > 0) 9 else 2},"mailSentCount":${if (codingMinutes > 0) 3 else 0},
    "documentCount":0,"transactionCount":${if (spentMinorUnits > 0) 2 else 0},"spentMinorUnits":$spentMinorUnits,"spentCurrency":"USD","shoppingTripCount":${if (spentMinorUnits > 0) 1 else 0},"memoryPageCount":0,"mainPlaceName":"${if (placeCount > 0) "Home" else ""}"}"""

private fun mood(level: Int): String = """{"moodLevel":$level,"moodWords":"","moodSource":"person"}"""

private val weather = """{"weatherCode":2,"weatherKind":"partlyCloudy","highCelsius":18.0,"lowCelsius":9.0,"precipitationMM":0.0,"placeName":"Home"}"""

private fun summaryJson(day: LocalDate, index: Int): String {
    val isToday = day == today
    val level = if (index % 7 == 3) 0 else 2 + (index % 4)
    val hasPhotos = index % 3 != 1
    return """{"day":"$day","isToday":$isToday,"mood":${if (level == 0) "null" else mood(level)},"weather":${if (index % 2 == 0) weather else "null"},
        "headline":${quoted(if (index % 4 == 1) "" else HEADLINES[index % HEADLINES.size])},"wordsExcerpt":${quoted(if (index % 2 == 0) "Slow morning, then a long walk by the river." else "")},"hasWords":${index % 2 == 0},
        "facts":${facts(if (hasPhotos) 4 + index else 0, 1 + index % 3, if (index % 5 == 0) 4650 else 0, if (index % 3 == 0) 190 else 0)},
        "coverPhotoId":${quoted(if (hasPhotos && index % 3 == 0) "photo-$index" else "")},"photoIds":[${if (hasPhotos) "\"photo-$index\"" else ""}],"hasNarrative":${index % 4 != 1}}"""
}

private val HEADLINES = listOf("A walk by the river", "Planning day", "Dinner with Sam", "Quiet Sunday", "Garden beds ordered")

private val weekStart: LocalDate get() = today.with(TemporalAdjusters.previousOrSame(DayOfWeek.MONDAY))

private fun daysJson(): String {
    val summaries = (0L until 24L).map { offset -> summaryJson(today.minusDays(offset), offset.toInt()) }
    return """{"days":[${summaries.joinToString(",")}],"totalCount":24,"today":"$today","firstDay":"${today.minusDays(23)}"}"""
}

private fun narrativeJson(headline: String, isPending: Boolean = false, hasCorrection: Boolean = false): String =
    """{"headline":${quoted(headline)},"narrative":${quoted("A quiet morning at home, then [[mail:m1]] from Sam about dinner. The afternoon went to the planning call, and the evening to the garden.\n\nBefore bed there was a short walk, about a mile.")},
      "references":[{"recordKind":"mail","recordId":"m1","reference":"mail:m1","title":"Dinner on Friday?","happenedAt":"${at(today, 9)}","dashboardPath":"/mailbox/starred/m1","isUnavailable":false}],
      "writtenAt":"${Instant.now()}","isStale":false,"narrativePending":$isPending,"isBeyondDepth":false,"failure":"",
      "corrections":[${if (hasCorrection) """{"correctionText":"It was a short call, not a long one.","createdAt":"${Instant.now()}"}""" else ""}],"sourceCount":9}"""

private fun wordsJson(original: String, isTouchUpOn: Boolean = false): String =
    """{"original":${quoted(original)},"modifiedAt":"${Instant.now()}","isTouchUpOn":$isTouchUpOn,"touchedUp":${quoted(if (isTouchUpOn) "A slow morning, then a long walk by the river." else "")},"touchUpHow":"","isTouchedUpStale":false,"touchUpPending":false,"journalNotes":[]}"""

private fun linkJson(id: String, kind: String, recordId: String, title: String, source: String, reason: String, isHidden: Boolean = false): String =
    """{"linkId":"$id","linkSource":"$source","linkReason":${quoted(reason)},"isHidden":$isHidden,"record":{"recordKind":"$kind","recordId":"$recordId","reference":"$kind:$recordId","title":${quoted(title)},"happenedAt":"${at(today, 10)}","dashboardPath":"","isUnavailable":false}}"""

private fun dayJson(day: LocalDate, kind: String): String {
    val isFull = kind == "full"
    val isEmpty = kind == "empty"
    val isToday = day == today
    return """{"day":"$day","timeZone":"UTC","isToday":$isToday,"weekday":"${day.dayOfWeek.name.lowercase()}","weekStart":"${day.with(TemporalAdjusters.previousOrSame(DayOfWeek.MONDAY))}",
      "words":${if (isEmpty) "null" else wordsJson("Slow morning, then a long walk by the river. Sam cooked in the evening.", isTouchUpOn = isFull)},
      "mood":${if (isEmpty) "null" else mood(4)},"weather":${if (isEmpty) "null" else weather},"facts":${facts(if (isEmpty) 0 else 6, if (isEmpty) 0 else 3, if (isFull) 4650 else 0, if (isFull) 190 else 0)},
      "narrative":${if (isFull) narrativeJson("A walk by the river and dinner with Sam", hasCorrection = true) else "null"},
      "choices":[${if (isFull) linkJson("l1", "calendar_event", "e1", "Planning call", "agent", "The longest meeting of the day") else ""}],
      "pins":[${if (isFull) linkJson("l2", "photo", "photo-1", "River at noon", "person", "") else ""}],
      "coverPhotoId":${quoted(if (isFull) "photo-0" else "")},
      "workProjects":[${if (isFull) """{"name":"Garden planner","firstAt":"${at(day, 13)}","lastAt":"${at(day, 16)}","activeMinutes":120,"stretchCount":2,"topics":["Bed layout","Seed order"],"shipped":[],"summary":"Laid out the beds and ordered seeds.","documentId":"doc-1"}""" else ""}],
      "codingSessions":[],"documents":[],"documentTotal":0,"conversations":[],"chats":[],
      "mail":[${if (isFull) """{"itemId":"m1","fromName":"Sam Example","subject":"Dinner on Friday?","receivedAt":"${at(day, 9)}","isSent":false,"isImportant":true,"isChosen":true,"linkReason":"","repeatCount":null}""" else ""}],
      "mailTotal":${if (isFull) 9 else 0},"recordings":[],
      "photos":[${if (isEmpty) "" else (1..3).joinToString(",") { number -> """{"photoId":"photo-$number","takenAt":"${at(day, 11 + number)}","placeName":"River park","isVideo":${number == 3}}""" }}],
      "photoTotal":${if (isEmpty) 0 else 3},
      "visits":[${if (isFull) """{"visitId":"v1","placeId":"p1","placeName":"River park","arrivedAt":"${at(day, 11)}","departedAt":"${at(day, 13)}","latitude":51.5,"longitude":-0.1}""" else ""}],
      "events":[${if (isFull) """{"eventId":"e1","title":"Planning call","calendarName":"Work","startsAt":"${at(day, 14)}","endsAt":"${at(day, 15)}","isAllDay":false,"location":"","reference":"calendar_event:e1"}""" else ""}],
      "reminders":[${if (isFull) """{"reminderId":"r1","title":"Order seeds","dueAt":"${at(day, 17)}","doneAt":"${at(day, 16)}"}""" else ""}],
      "transactions":[${if (isFull) """{"transactionId":"t1","postedOn":"$day","transactedAt":"${at(day, 17)}","description":"Garden centre","accountName":"Everyday","amountMinorUnits":-4650,"currency":"USD"}""" else ""}],
      "health":${if (isEmpty) "null" else """{"stepCount":7400,"stepsByHour":[0,0,0,0,0,0,120,300,500,200,100,900,1200,300,200,400,900,1200,600,300,100,0,0,0],"sleepMinutes":440,
        "sleepStages":[{"sleepStage":"deep","minutes":90},{"sleepStage":"rem","minutes":110},{"sleepStage":"core","minutes":240}],"restingHeartRate":58.0,"workouts":[{"sampleId":"w1","activityType":"walking","startedAt":"${at(day, 18)}","minutes":35,"meters":2600.0}]}"""},
      "shoppingTrips":[${if (isFull) """{"tripId":"s1","storeName":"Garden centre","listNames":["Garden"],"startedAt":"${at(day, 17)}","finishedAt":"${at(day, 18)}","dashboardPath":""}""" else ""}],
      "notes":[],"memoryPages":[],"unavailableSections":[],
      "moments":[${if (isFull) """{"at":"${at(day, 9)}","recordKind":"mail","recordId":"m1","title":"Dinner on Friday?","detail":"Sam Example","dashboardPath":""},{"at":"${at(day, 14)}","recordKind":"calendar_event","recordId":"e1","title":"Planning call","detail":"1 hr","dashboardPath":""}""" else ""}]}"""
}

private fun periodJson(periodKind: String, anchor: LocalDate): String {
    val start = when (periodKind) {
        "week" -> anchor.with(TemporalAdjusters.previousOrSame(DayOfWeek.MONDAY))
        "month" -> anchor.withDayOfMonth(1)
        else -> anchor.withDayOfYear(1)
    }
    val end = when (periodKind) {
        "week" -> start.plusDays(6)
        "month" -> start.plusMonths(1).minusDays(1)
        else -> start.plusYears(1).minusDays(1)
    }
    val dayCount = if (periodKind == "year") 0 else minOf(end.toEpochDay(), today.toEpochDay()) - start.toEpochDay() + 1
    val days = (0 until maxOf(dayCount, 0)).map { offset -> summaryJson(start.plusDays(offset.toLong()), offset.toInt()) }
    val months = if (periodKind == "year") (1..12).filter { month -> start.withMonth(month) <= today }.map { month ->
        """{"periodStart":"${start.withMonth(month)}","headline":${quoted(if (month % 2 == 0) "Seeds, soil and a lot of walking" else "")},"dayMoods":[${(1..28).joinToString(",") { number -> ((number + month) % 5 + 1).toString() }}],"placeCount":${month + 3},"photoCount":${month * 11}}"""
    } else emptyList()
    return """{"periodKind":"$periodKind","periodStart":"$start","periodEnd":"$end","isCurrent":${today in start..end},
      "lookingBack":${wordsJson("A good stretch. The garden took shape and the evenings got longer.")},"narrative":${narrativeJson("A stretch of garden plans and long walks")},
      "choices":[${linkJson("pl1", "calendar_event", "e1", "Planning call", "agent", "The week's anchor")}],
      "days":[${days.joinToString(",")}],"months":[${months.joinToString(",")}],"totals":${facts(48, 12, 18400, 960)},"moodCounts":[0,1,2,3,2],"dayCount":$dayCount,
      "chapters":[{"fromDay":"$start","untilDay":"${start.plusDays(3)}","title":"Planning the beds","detail":"Layouts, seed lists and one trip to the garden centre."}]}"""
}

private val searchJson = """{"hits":[{"day":"${today.minusDays(3)}","periodKind":"","headline":"Dinner with Sam","snippet":"Sam cooked, and we talked about the garden."},
    {"day":"${weekStart.minusDays(7)}","periodKind":"week","headline":"A week of planning","snippet":"The week the garden beds were drawn."}],"totalCount":2}"""

private val settingsJson = """{"isWeatherOn":true,"isEveningQuestionOn":true,"eveningQuestionTime":"21:00","weekStartsOn":"monday","narrativeDepthDays":30,"modelProviderName":"Example Models"}"""

/**
 * What demo mode answers for Journal: every day of the last weeks (with and without photos, moods, words and a story),
 * a full day, a day with only words, an empty day, the week, month and year, search, and the settings. The day of the
 * kind asked for is the one the variables name: today is a full day, the day before only words, and the one before empty.
 */
val journalDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("ListJournalDays", daysJson()),
    graphQLField("ReadJournalDay") { variables ->
        val day = LocalDate.parse(variables.text("day") ?: today.toString())
        dayJson(day, when (today.toEpochDay() - day.toEpochDay()) { 0L, 2L -> "full"; 1L -> "words"; 4L -> "empty"; else -> "words" })
    },
    graphQLField("ReadJournalPeriod") { variables -> periodJson(variables.text("periodKind") ?: "week", LocalDate.parse(variables.text("day") ?: today.toString())) },
    graphQLField("SearchJournal", searchJson),
    graphQLField("ReadJournalSettings", settingsJson),
    graphQLField("UpdateJournalSettings", settingsJson),
    graphQLField("SetJournalWords") { variables -> """{"day":"${variables.text("day")}","words":{"modifiedAt":"${Instant.now()}"}}""" },
    graphQLField("SetJournalLookingBack") { _ -> """{"periodStart":"$weekStart","lookingBack":{"modifiedAt":"${Instant.now()}"}}""" },
    graphQLField("SetJournalMood") { variables -> """{"day":"${variables.text("day")}"}""" },
    graphQLField("TouchUpJournalWords", """{"day":"$today","periodKind":null}"""),
    graphQLField("StopJournalTouchUp", """{"day":"$today","periodKind":null}"""),
    graphQLField("CorrectJournalNarrative", """{"narrativePending":true}"""),
    graphQLField("RemoveJournalCorrection", """{"narrativePending":true}"""),
    graphQLField("WriteJournalNarrative", """{"narrativePending":true}"""),
    graphQLField("DeleteJournalNarrative", """{"narrativePending":false}"""),
    graphQLField("PinJournalRecord", linkJson("l9", "mail", "m1", "Dinner on Friday?", "person", "")),
    graphQLField("RemoveJournalLink", "true"),
    graphQLField("EraseJournal", """{"dayCount":24,"periodCount":6,"linkCount":31}"""),
)
