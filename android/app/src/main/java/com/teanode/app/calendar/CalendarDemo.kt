package com.teanode.app.calendar

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoRequest
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField
import com.teanode.kit.calendar.CalendarDates
import com.teanode.kit.calendar.CalendarDay
import com.teanode.kit.calendar.ServerTime
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneId

private val demoZone: ZoneId get() = ZoneId.systemDefault()

private fun at(day: LocalDate, hour: Int, minute: Int = 0): String = CalendarDates.text(LocalDateTime.of(day, java.time.LocalTime.of(hour, minute)).atZone(demoZone).toInstant())

private fun allDay(day: LocalDate): String = CalendarDates.allDayText(CalendarDay.of(day))

private fun quoted(text: String?): String = if (text == null) "null" else "\"" + text.replace("\"", "'") + "\""

/** One event of the demo calendars, as `ListCalendarEvents` answers it. */
private fun eventJson(
    calendarId: String,
    kind: String,
    day: LocalDate,
    summary: String?,
    startsAt: String,
    endsAt: String,
    isAllDay: Boolean = false,
    location: String? = null,
    isRecurring: Boolean = false,
    status: String = "confirmed",
    isBusyOnly: Boolean = false,
    isPrivate: Boolean = false,
): String =
    """{"id":"${if (isBusyOnly) "" else "$calendarId.$kind.${day.toEpochDay()}"}","calendarId":"$calendarId","summary":${quoted(summary)},"location":${quoted(location)},
        "startsAt":"$startsAt","endsAt":"$endsAt","allDay":$isAllDay,"recurring":$isRecurring,"occurrence":$isRecurring,"status":"$status",
        "isBusyOnly":$isBusyOnly,"isPrivate":$isPrivate}"""

/** The events of a demo calendar on one day: the same every time, so a photograph and the screen agree. */
private fun eventsOn(calendarId: String, day: LocalDate): List<String> {
    val weekday = day.dayOfWeek.value
    val number = day.toEpochDay()
    val list = ArrayList<String>()
    when (calendarId) {
        "home" -> {
            if (weekday in 1..5) list += eventJson("home", "standup", day, "Team standup", at(day, 9, 30), at(day, 9, 45), isRecurring = true, location = "Meeting room 2")
            if (weekday == 2 || weekday == 4) list += eventJson("home", "gym", day, "Gym", at(day, 18), at(day, 19), isRecurring = true)
            if (number % 3L == 0L && weekday <= 5) list += eventJson("home", "lunch", day, "Lunch with Sam", at(day, 12, 30), at(day, 13, 30), location = "Corner Cafe")
            if (weekday == 3) {
                list += eventJson("home", "review", day, "Design review", at(day, 14), at(day, 15, 30), location = "Room 4")
                list += eventJson("home", "one", day, "1:1 with Alex", at(day, 14, 30), at(day, 15))
            }
            if (number % 11L == 0L) list += eventJson("home", "checkup", day, "Checkup", at(day, 8), at(day, 9), isPrivate = true)
            if (number % 17L == 0L) list += eventJson("home", "cancelled", day, "Yoga class", at(day, 17), at(day, 18), status = "CANCELLED")
            if (day.dayOfMonth == 20) list += eventJson("home", "birthday", day, "Alex's birthday", allDay(day), allDay(day.plusDays(1)), isAllDay = true)
            if (number % 29L == 0L) list += eventJson("home", "trip", day, "Trip to the coast", allDay(day), allDay(day.plusDays(3)), isAllDay = true)
            if (weekday == 6 && number % 2L == 0L) list += eventJson("home", "market", day, "Farmers market", at(day, 10), at(day, 11, 30), location = "Town square")
            if (weekday == 1) list += eventJson("home", "weekly", day, "Weekly planning", at(day, 8, 30), at(day, 9, 15), isRecurring = true)
        }
        "ben" -> {
            if (weekday in 1..5 && number % 2L == 0L) list += eventJson("ben", "plan", day, "Roadmap planning", at(day, 11), at(day, 12))
            if (weekday == 4) list += eventJson("ben", "run", day, "Evening run", at(day, 18, 30), at(day, 19, 15))
        }
        "cora" -> {
            if (weekday in 1..5) list += eventJson("cora", "busy", day, null, at(day, 13), at(day, 14, 30), isBusyOnly = true)
            if (weekday == 3) list += eventJson("cora", "busy-late", day, null, at(day, 16), at(day, 17), isBusyOnly = true)
        }
        "household" -> {
            if (weekday == 7) list += eventJson("household", "dinner", day, "Family dinner", at(day, 18), at(day, 20), location = "Home", isRecurring = true)
            if (number % 13L == 0L) list += eventJson("household", "chores", day, "Garden day", at(day, 10), at(day, 13))
        }
    }
    return list
}

private fun eventsAnswer(request: DemoRequest): String {
    val calendarId = (request.variables["calendarId"] as? JsonPrimitive)?.content.orEmpty()
    val from = ServerTime.instant((request.variables["from"] as? JsonPrimitive)?.content) ?: Instant.now()
    val until = ServerTime.instant((request.variables["until"] as? JsonPrimitive)?.content) ?: from.plusSeconds(31L * 86400)
    var day = from.atZone(demoZone).toLocalDate().minusDays(1)
    val last = until.atZone(demoZone).toLocalDate()
    val rows = ArrayList<String>()
    while (!day.isAfter(last)) {
        rows += eventsOn(calendarId, day)
        day = day.plusDays(1)
    }
    return "[" + rows.joinToString(",") + "]"
}

/** The event whole, as `GetCalendarEvent` answers it, from the id the list gave it: calendar.kind.day. */
private fun eventDetail(request: DemoRequest): String {
    val calendarId = (request.variables["calendarId"] as? JsonPrimitive)?.content.orEmpty()
    val id = (request.variables["id"] as? JsonPrimitive)?.content.orEmpty()
    val kind = id.split('.').getOrNull(1).orEmpty()
    val epochDay = id.split('.').getOrNull(2)?.toLongOrNull() ?: LocalDate.now().toEpochDay()
    val day = LocalDate.ofEpochDay(epochDay)
    val found = eventsOn(calendarId, day).firstOrNull { row -> row.contains("\"id\":\"$id\"") }
    val summary = Regex("\"summary\":(\"[^\"]*\"|null)").find(found.orEmpty())?.groupValues?.get(1) ?: "\"Event\""
    val location = Regex("\"location\":(\"[^\"]*\"|null)").find(found.orEmpty())?.groupValues?.get(1) ?: "null"
    val startsAt = Regex("\"startsAt\":\"([^\"]*)\"").find(found.orEmpty())?.groupValues?.get(1) ?: at(day, 9)
    val endsAt = Regex("\"endsAt\":\"([^\"]*)\"").find(found.orEmpty())?.groupValues?.get(1) ?: at(day, 10)
    val isAllDay = found?.contains("\"allDay\":true") == true
    val recurrence = when (kind) {
        "standup" -> "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"
        "gym" -> "FREQ=WEEKLY;INTERVAL=1;BYDAY=TU,TH"
        "weekly" -> "FREQ=WEEKLY"
        "dinner" -> "FREQ=WEEKLY;BYDAY=SU"
        "review" -> "FREQ=MONTHLY;BYDAY=1WE"
        else -> ""
    }
    val attendees = when (kind) {
        "review" -> """[{"address":"alex@example.com","name":"Alex Example","participation":"ACCEPTED","role":"REQ-PARTICIPANT"},{"address":"sam@example.com","name":null,"participation":"NEEDS-ACTION","role":"REQ-PARTICIPANT"}]"""
        "plan" -> """[{"address":"me@example.com","name":"Ben Example","participation":"ACCEPTED","role":"CHAIR"}]"""
        else -> "[]"
    }
    return """{"id":"$id","summary":$summary,"location":$location,"description":"${if (kind == "review" || kind == "plan") "Bring the notes from last time." else ""}",
        "startsAt":"$startsAt","endsAt":"$endsAt","allDay":$isAllDay,"recurring":${recurrence.isNotEmpty()},"recurrence":${quoted(recurrence.ifEmpty { null })},
        "status":"confirmed","timezone":"${demoZone.id}","isPrivate":${kind == "checkup"},"organizer":"me@example.com","attendees":$attendees}"""
}

private fun hoursFromNow(hours: Long): String = Instant.now().plusSeconds(hours * 3600).toString()

private fun reminderJson(id: String, title: String, notes: String?, dueHours: Long?, isDueDate: Boolean = false, isDone: Boolean = false, priority: Int? = null, isRepeating: Boolean = false): String {
    val dueAt = when {
        dueHours == null -> "null"
        isDueDate -> "\"" + allDay(LocalDate.now(demoZone).plusDays(dueHours / 24)) + "\""
        else -> "\"" + hoursFromNow(dueHours) + "\""
    }
    return """{"id":"$id","title":"$title","notes":${quoted(notes)},"dueAt":$dueAt,"isDueDate":$isDueDate,"isDone":$isDone,
        "doneAt":${if (isDone) "\"${hoursFromNow(-5)}\"" else "null"},"priority":${priority ?: "null"},"isRepeating":$isRepeating}"""
}

/** Busy periods for the free-and-busy question: everybody is busy at lunch, and Ben also in the mornings of even days. */
private fun freeBusyAnswer(request: DemoRequest): String {
    val userIds = (request.variables["userIds"] as? JsonArray).orEmpty().map { element -> (element as JsonPrimitive).content }
    val from = ServerTime.instant((request.variables["from"] as? JsonPrimitive)?.content) ?: Instant.now()
    val until = ServerTime.instant((request.variables["until"] as? JsonPrimitive)?.content) ?: from.plusSeconds(86400)
    var day = from.atZone(demoZone).toLocalDate()
    val last = until.atZone(demoZone).toLocalDate()
    return "[" + userIds.joinToString(",") { userId ->
        val periods = ArrayList<String>()
        var each = day
        while (each.isBefore(last)) {
            periods += """{"startsAt":"${at(each, 12)}","endsAt":"${at(each, 13)}"}"""
            if (userId == "ben-id" && each.toEpochDay() % 2L == 0L) periods += """{"startsAt":"${at(each, 9)}","endsAt":"${at(each, 11)}"}"""
            if (userId == "demo-user" && each.dayOfWeek.value == 2) periods += """{"startsAt":"${at(each, 14)}","endsAt":"${at(each, 16, 30)}"}"""
            each = each.plusDays(1)
        }
        val isShared = userId != "dana-id"
        """{"userId":"$userId","username":"${userId.removeSuffix("-id")}","name":"","isShared":$isShared,"periods":[${if (isShared) periods.joinToString(",") else ""}]}"""
    } + "]"
}

private val sharePeopleJson = """[
    {"userId":"ben-id","username":"ben","name":"Ben Example","avatarUrl":null,"avatarColorIndex":3},
    {"userId":"cora-id","username":"cora","name":"Cora Example","avatarUrl":null,"avatarColorIndex":5},
    {"userId":"dana-id","username":"dana","name":null,"avatarUrl":null,"avatarColorIndex":1}]"""

/** What demo mode answers for Calendar: four calendars (their own, a circle's and two shared with them), their events, the reminders, shares and free-and-busy times. */
val calendarDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("ListCalendars") {
        """[
            {"id":"home","name":"Calendar","description":"My own","colour":"#2F7FE0","timezone":"${demoZone.id}","weekStart":"monday","calendarKind":"events","shareLevel":"owner","ownerUserId":"demo-user","ownerName":"Alex Demo","colorIndex":0},
            {"id":"household","name":"The household","description":null,"colour":null,"timezone":null,"weekStart":"monday","calendarKind":"events","shareLevel":"edit","circleId":"circle-home","circleName":"Home","isCircleCalendar":true,"colorIndex":1},
            {"id":"ben","name":"Calendar","description":null,"colour":null,"timezone":null,"weekStart":"monday","calendarKind":"events","shareLevel":"titles","ownerUserId":"ben-id","ownerName":"Ben Example","ownerAvatarColorIndex":3,"colorIndex":2},
            {"id":"cora","name":"Calendar","description":null,"colour":null,"timezone":null,"weekStart":"monday","calendarKind":"events","shareLevel":"busy","ownerUserId":"cora-id","ownerName":"Cora Example","ownerAvatarColorIndex":5,"colorIndex":3},
            {"id":"reminders","name":"Reminders","description":null,"colour":null,"timezone":null,"weekStart":null,"calendarKind":"reminders","shareLevel":"owner","colorIndex":4}]"""
    },
    DemoAnswerer { request -> if (request.document.contains("ListCalendarEvents")) DemoResponse("""{"data":{"ListCalendarEvents":${eventsAnswer(request)}}}""") else null },
    DemoAnswerer { request -> if (request.document.contains("GetCalendarEvent")) DemoResponse("""{"data":{"GetCalendarEvent":${eventDetail(request)}}}""") else null },
    graphQLField("SaveCalendarEvent", """{"id":"saved-event"}"""),
    graphQLField("DeleteCalendarEvent", "true"),
    graphQLField("GetCalendarRequest", "null"),
    graphQLField("CancelCalendarRequest", """{"requestId":"r","isMissing":false}"""),
    graphQLField("ListCalendarShares") {
        """[
            {"id":"share-1","calendarId":"home","granteeUserId":"ben-id","granteeUsername":"ben","granteeName":"Ben Example","granteeCircleId":null,"granteeCircleName":null,"shareLevel":"details"},
            {"id":"share-2","calendarId":"home","granteeUserId":null,"granteeUsername":null,"granteeName":null,"granteeCircleId":"circle-home","granteeCircleName":"Home","shareLevel":"busy"}]"""
    },
    graphQLField("ShareCalendar", """{"id":"share-3"}"""),
    graphQLField("UnshareCalendar", "true"),
    graphQLField("ListCirclePeople", sharePeopleJson),
    DemoAnswerer { request ->
        if (!request.document.contains("ListCircles") || request.document.contains("pendingInvitations")) return@DemoAnswerer null
        val isRoles = request.document.contains("memberRole")
        DemoResponse(
            if (isRoles) {
                """{"data":{"ListCircles":[{"id":"circle-home","name":"Home","circleKind":"family","memberRole":"admin"},{"id":"circle-work","name":"Work friends","circleKind":"colleagues","memberRole":"admin"},{"id":"circle-club","name":"Book club","circleKind":"friends","memberRole":"member"}]}}"""
            } else {
                """{"data":{"ListCircles":[{"id":"circle-home","name":"Home","members":[{"userId":"demo-user"},{"userId":"ben-id"},{"userId":"cora-id"},{"userId":"dana-id"}]},{"id":"circle-club","name":"Book club","members":[{"userId":"demo-user"},{"userId":"ben-id"}]}]}}"""
            },
        )
    },
    graphQLField("SaveCalendar", """{"id":"home","name":"Calendar"}"""),
    graphQLField("CreateCircleCalendar", """{"id":"circle-calendar"}"""),
    graphQLField("DeleteCircleCalendar", "true"),
    graphQLField("ListCalendarFreeBusy") { variables -> freeBusyAnswer(DemoRequest("", "POST", "", "", variables, null)) },
    graphQLField("ListReminders") { variables ->
        val isDone = (variables["isDone"] as? JsonPrimitive)?.content == "true"
        if (isDone) {
            "[" + listOf(
                reminderJson("r4", "Renew the library card", null, null, isDone = true),
                reminderJson("r5", "Send the form to school", "Signed by both parents", null, isDone = true),
            ).joinToString(",") + "]"
        } else {
            "[" + listOf(
                reminderJson("r1", "Call the plumber", "About the kitchen tap", -3, priority = 1),
                reminderJson("r2", "Water the plants", null, 20, isRepeating = true),
                reminderJson("r3", "Pay the water bill", "Due at the end of the month", 72, isDueDate = true),
                reminderJson("r6", "Book a table for Sam's birthday", null, null),
            ).joinToString(",") + "]"
        }
    },
    graphQLField("SaveReminder", """{"id":"saved-reminder"}"""),
    graphQLField("SetReminderDone", """{"id":"r1"}"""),
    graphQLField("DeleteReminder", "true"),
)
