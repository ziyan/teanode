package com.teanode.kit.mail


/** The GraphQL documents, the same text as the iPhone app's (ios/TeaNodeKit): the server's tests check that text against its schema. */
object MailProposalDocuments {
    private const val D = "$"

    const val CALENDARS = """query {
  ListCalendars {
    id name description colour timezone weekStart calendarKind
    shareLevel ownerUserId ownerName ownerAvatarUrl ownerAvatarColorIndex circleId circleName isCircleCalendar colorIndex
  }
}"""

    const val SAVE_CALENDAR_EVENT = """mutation (${D}requestId: String!, ${D}calendarId: String!, ${D}id: String, ${D}summary: String, ${D}location: String,
  ${D}description: String, ${D}startsAt: String, ${D}endsAt: String, ${D}allDay: Boolean,
  ${D}timezone: String, ${D}recurrence: String, ${D}status: String, ${D}isPrivate: Boolean, ${D}attendees: [String!]) {
  SaveCalendarEvent(requestId: ${D}requestId, calendarId: ${D}calendarId, id: ${D}id, summary: ${D}summary, location: ${D}location,
    description: ${D}description, startsAt: ${D}startsAt, endsAt: ${D}endsAt, allDay: ${D}allDay,
    timezone: ${D}timezone, recurrence: ${D}recurrence, status: ${D}status, isPrivate: ${D}isPrivate, attendees: ${D}attendees) { id }
}"""

    const val READ_RECEIPT = """mutation (${D}agentAttachmentId: String, ${D}mailboxItemId: String, ${D}financeTransactionId: String) {
  ReadReceipt(agentAttachmentId: ${D}agentAttachmentId, mailboxItemId: ${D}mailboxItemId,
    financeTransactionId: ${D}financeTransactionId) { agentJobId }
}"""

    const val READ_TAX_DOCUMENT = """mutation (${D}agentAttachmentId: String, ${D}mailboxItemId: String) {
  ReadTaxDocument(agentAttachmentId: ${D}agentAttachmentId, mailboxItemId: ${D}mailboxItemId) { agentAttachmentIds agentJobIds }
}"""

}