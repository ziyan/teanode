package com.teanode.kit.calendar

/** The calendar's GraphQL: the same text as the iPhone app's (ios/TeaNodeKit Calendar.swift, FindATime.swift). */
object CalendarDocuments {
    const val CALENDARS = """query {
  ListCalendars {
    id name description colour timezone weekStart calendarKind
    shareLevel ownerUserId ownerName ownerAvatarUrl ownerAvatarColorIndex circleId circleName isCircleCalendar colorIndex
  }
}"""
    const val CALENDAR_EVENTS = """query (${'$'}calendarId: String!, ${'$'}from: String!, ${'$'}until: String!) {
  ListCalendarEvents(calendarId: ${'$'}calendarId, from: ${'$'}from, until: ${'$'}until) {
    id calendarId summary location startsAt endsAt allDay recurring occurrence status isBusyOnly isPrivate
  }
}"""
    const val CALENDAR_EVENT = """query (${'$'}calendarId: String!, ${'$'}id: String!) {
  GetCalendarEvent(calendarId: ${'$'}calendarId, id: ${'$'}id) {
    id summary location description startsAt endsAt allDay
    recurring recurrence status timezone isPrivate organizer attendees { address name participation role }
  }
}"""
    const val SAVE_CALENDAR_EVENT = """mutation (${'$'}requestId: String!, ${'$'}calendarId: String!, ${'$'}id: String, ${'$'}summary: String, ${'$'}location: String,
  ${'$'}description: String, ${'$'}startsAt: String, ${'$'}endsAt: String, ${'$'}allDay: Boolean,
  ${'$'}timezone: String, ${'$'}recurrence: String, ${'$'}status: String, ${'$'}isPrivate: Boolean, ${'$'}attendees: [String!]) {
  SaveCalendarEvent(requestId: ${'$'}requestId, calendarId: ${'$'}calendarId, id: ${'$'}id, summary: ${'$'}summary, location: ${'$'}location,
    description: ${'$'}description, startsAt: ${'$'}startsAt, endsAt: ${'$'}endsAt, allDay: ${'$'}allDay,
    timezone: ${'$'}timezone, recurrence: ${'$'}recurrence, status: ${'$'}status, isPrivate: ${'$'}isPrivate, attendees: ${'$'}attendees) { id }
}"""
    const val DELETE_CALENDAR_EVENT = """mutation (${'$'}requestId: String!, ${'$'}calendarId: String!, ${'$'}id: String!) {
  DeleteCalendarEvent(requestId: ${'$'}requestId, calendarId: ${'$'}calendarId, id: ${'$'}id)
}"""
    const val CALENDAR_REQUEST = """query (${'$'}requestId: String!) { GetCalendarRequest(requestId: ${'$'}requestId) { requestId isMissing } }"""
    const val CANCEL_CALENDAR_REQUEST = """mutation (${'$'}requestId: String!) { CancelCalendarRequest(requestId: ${'$'}requestId) { requestId isMissing } }"""
    const val CALENDAR_SHARES = """query (${'$'}calendarId: String!) {
  ListCalendarShares(calendarId: ${'$'}calendarId) {
    id calendarId granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel
  }
}"""
    const val SHARE_CALENDAR = """mutation (${'$'}calendarId: String!, ${'$'}granteeUserId: String, ${'$'}granteeCircleId: String, ${'$'}shareLevel: String!) {
  ShareCalendar(calendarId: ${'$'}calendarId, granteeUserId: ${'$'}granteeUserId, granteeCircleId: ${'$'}granteeCircleId, shareLevel: ${'$'}shareLevel) { id }
}"""
    const val UNSHARE_CALENDAR = """mutation (${'$'}shareId: String!) { UnshareCalendar(shareId: ${'$'}shareId) }"""
    const val CALENDAR_SHARE_PEOPLE = """query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }"""
    /** The circles with the caller's role in each, to know which calendars they may rename and delete. Fields of ios Circles.swift circleFields. */
    const val CIRCLE_ROLES = """query { ListCircles { id name circleKind memberRole } }"""
    const val CALENDAR_SHARE_CIRCLES = """query { ListCircles { id name members { userId } } }"""
    const val SAVE_CALENDAR = """mutation (${'$'}id: String!, ${'$'}name: String, ${'$'}description: String, ${'$'}colour: String, ${'$'}timezone: String, ${'$'}weekStart: String) {
  SaveCalendar(id: ${'$'}id, name: ${'$'}name, description: ${'$'}description, colour: ${'$'}colour, timezone: ${'$'}timezone, weekStart: ${'$'}weekStart) { id name }
}"""
    const val CREATE_CIRCLE_CALENDAR = """mutation (${'$'}circleId: String!) { CreateCircleCalendar(circleId: ${'$'}circleId) { id } }"""
    const val DELETE_CIRCLE_CALENDAR = """mutation (${'$'}circleId: String!) { DeleteCircleCalendar(circleId: ${'$'}circleId) }"""
    const val REMINDERS = """query (${'$'}isDone: Boolean) { ListReminders(isDone: ${'$'}isDone) { id title notes dueAt isDueDate isDone doneAt priority isRepeating } }"""
    const val SAVE_REMINDER = """mutation (${'$'}reminderId: String, ${'$'}title: String, ${'$'}notes: String, ${'$'}dueAt: String, ${'$'}dueOn: String, ${'$'}isDueCleared: Boolean) {
  SaveReminder(reminderId: ${'$'}reminderId, title: ${'$'}title, notes: ${'$'}notes, dueAt: ${'$'}dueAt, dueOn: ${'$'}dueOn, isDueCleared: ${'$'}isDueCleared) { id }
}"""
    const val SET_REMINDER_DONE = """mutation (${'$'}reminderId: String!, ${'$'}isDone: Boolean!) { SetReminderDone(reminderId: ${'$'}reminderId, isDone: ${'$'}isDone) { id } }"""
    const val DELETE_REMINDER = """mutation (${'$'}reminderId: String!) { DeleteReminder(reminderId: ${'$'}reminderId) }"""
    const val CALENDAR_FREE_BUSY = """query (${'$'}userIds: [String!]!, ${'$'}from: String!, ${'$'}until: String!) {
  ListCalendarFreeBusy(userIds: ${'$'}userIds, from: ${'$'}from, until: ${'$'}until) {
    userId username name isShared periods { startsAt endsAt }
  }
}"""
}
