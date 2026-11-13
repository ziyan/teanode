package com.teanode.app.shell

import com.teanode.app.calendar.calendarArea
import com.teanode.app.chats.chatsArea
import com.teanode.app.coding.codingArea
import com.teanode.app.contacts.contactsArea
import com.teanode.app.finance.financeArea
import com.teanode.app.goals.goalsArea
import com.teanode.app.health.healthArea
import com.teanode.app.ideas.ideasArea
import com.teanode.app.journal.journalArea
import com.teanode.app.knowledge.knowledgeArea
import com.teanode.app.location.locationArea
import com.teanode.app.mail.mailArea
import com.teanode.app.passwords.passwordsArea
import com.teanode.app.photos.photosArea
import com.teanode.app.recordings.recordingsArea
import com.teanode.app.search.searchArea
import com.teanode.app.settings.settingsArea
import com.teanode.app.shopping.shoppingArea

/**
 * Areas lists the parts of the app, once. Every part is already here, each in its own package with its own
 * `<Domain>Navigation.kt`, `<Domain>Section.kt` and `<Domain>Demo.kt`; building a part means filling its package, and
 * this file is not edited.
 */
object Areas {
    val all: List<Area> = listOf(
        chatsArea, journalArea, goalsArea, ideasArea, mailArea, calendarArea, contactsArea, photosArea, locationArea,
        healthArea, passwordsArea, shoppingArea, knowledgeArea, financeArea, recordingsArea, codingArea, settingsArea,
        searchArea,
    )
}
