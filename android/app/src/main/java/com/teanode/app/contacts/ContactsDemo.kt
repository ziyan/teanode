package com.teanode.app.contacts

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import com.teanode.app.demo.text
import com.teanode.kit.mail.ServerTime
import kotlinx.serialization.json.JsonObject
import java.time.Instant
import java.time.LocalDate
import java.time.temporal.ChronoUnit

/**
 * The invented address books of demo mode: the person's own book, a circle's book, contacts to tell apart, one the server
 * thinks is somebody in the circles, and the cards of an account in the circles, an account with no contact, and a
 * contact with no account. Everything is made up.
 */
object ContactsDemoData {
    private val now: Instant = Instant.now()
    private fun hoursAgo(hours: Long): String = ServerTime.text(now.minus(hours, ChronoUnit.HOURS))
    private fun daysAgo(days: Long): String = ServerTime.text(now.minus(days, ChronoUnit.DAYS))
    private fun quoted(text: String?): String = if (text == null) "null" else "\"$text\""

    val books: String = """
        [{"id":"book-own","name":"Contacts","description":"","contacts":6,"circleId":null,"circleName":"","isCircleBook":false,"canManage":true},
         {"id":"book-home","name":"Home","description":"","contacts":2,"circleId":"circle-home","circleName":"Home","isCircleBook":true,"canManage":true}]
    """.trimIndent()

    private fun summary(id: String, name: String?, organization: String?, emails: String, phones: String, accountUserId: String? = null): String =
        """{"id":"$id","name":${quoted(name)},"organization":${quoted(organization)},"emails":$emails,"phones":$phones,"hasPhoto":false,"addresses":[],"accountUserId":${quoted(accountUserId)}}"""

    val ownContacts: List<String> = listOf(
        summary("contact-alex", "Alex Example", "Example Co", """["alex@example.com"]""", """["+1 555 0100"]"""),
        summary("contact-sam", "Sam Fixture", null, """["sam@example.org","sam.work@example.org"]""", """["+1 (555) 0142"]""", accountUserId = "user-sam"),
        summary("contact-pat", "Pat Sample", "Sample Trains", """["pat@example.net"]""", """[]"""),
        summary("contact-books", "Example Books", null, """["orders@example.org"]""", """[]"""),
        summary("contact-robin", "Robin Demo", null, """[]""", """["+1 555 0177"]"""),
        summary("contact-noname", null, null, """["zed@example.org"]""", """[]"""),
    )

    private val circleContacts: List<String> = listOf(
        summary("contact-plumber", "Home Plumbing", "Example Plumbing", """[]""", """["+1 555 0188"]"""),
        summary("contact-school", "Sample School Office", null, """["office@example.edu"]""", """[]"""),
    )

    private const val PERSON_SAM = """{"userId":"user-sam","username":"sam","name":"Sam Fixture","avatarUrl":null,"avatarColorIndex":2}"""
    private const val PERSON_ALEXA = """{"userId":"user-alex","username":"alexa","name":"Alexa Demo","avatarUrl":null,"avatarColorIndex":5}"""
    private const val PERSON_PAT = """{"userId":"user-pat","username":"pat","name":"Patricia Sample","avatarUrl":null,"avatarColorIndex":7}"""

    val suggestions: String = """
        [{"addressBookId":"book-own","contact":${summary("contact-pat", "Pat Sample", "Sample Trains", """["pat@example.net"]""", """[]""")},"account":$PERSON_PAT}]
    """.trimIndent()

    private fun detail(id: String, bookId: String, name: String, organization: String?, emails: String, phones: String, note: String?, accountUserId: String?, account: String?): String =
        """{"id":"$id","addressBookId":"$bookId","name":"$name","organization":${quoted(organization)},"emails":$emails,"phones":$phones,"note":${quoted(note)},"hasPhoto":false,"addresses":[{"street":"1 Example Road","locality":"Sampleton","region":"","postalCode":"00000","country":"Exampleland"}],"accountUserId":${quoted(accountUserId)},"account":${account ?: "null"}}"""

    fun contact(contactId: String): String = when (contactId) {
        "contact-sam" -> detail("contact-sam", "book-own", "Sam Fixture", null, """["sam@example.org","sam.work@example.org"]""", """["+1 (555) 0142"]""", "Likes tea", "user-sam", PERSON_SAM)
        "contact-plumber" -> detail("contact-plumber", "book-home", "Home Plumbing", "Example Plumbing", """[]""", """["+1 555 0188"]""", null, null, null)
        "contact-books" -> detail("contact-books", "book-own", "Example Books", null, """["orders@example.org"]""", """[]""", null, null, null)
        else -> detail(contactId, "book-own", "Alex Example", "Example Co", """["alex@example.com"]""", """["+1 555 0100"]""", null, null, null)
    }

    private const val SAM_MEMBER = """{"userId":"user-sam","username":"sam","name":"Sam Fixture","avatarUrl":null,"avatarColorIndex":2,"memberRole":"member","joinedAt":"2030-01-02T00:00:00Z","relationshipKind":"parent","relationshipTermGender":"feminine","relationshipWord":"mother","linkedContactId":"contact-sam"}"""
    private const val ALEXA_MEMBER = """{"userId":"user-alex","username":"alexa","name":"Alexa Demo","avatarUrl":null,"avatarColorIndex":5,"memberRole":"member","joinedAt":"2030-01-02T00:00:00Z","relationshipKind":"friend","relationshipTermGender":"neutral","relationshipWord":"friend","linkedContactId":""}"""

    private fun section(state: String, body: String): String = """{"sectionState":"$state"$body}"""

    private fun samCard(): String = """
        {"person":$SAM_MEMBER,"contact":${contact("contact-sam")},"sharedCircles":[{"circleId":"circle-home","circleName":"Home","circleKind":"family"},{"circleId":"circle-book","circleName":"Book club","circleKind":"friends"}],"canMessage":true,
         "location":${section("shown", ""","location":{"userId":"user-sam","shareLevel":"history","latestPoint":{"latitudeDegrees":10.5,"longitudeDegrees":20.5,"measuredAt":"${hoursAgo(1)}"},"openVisit":{"id":"visit-1","place":{"id":"place-1","placeName":"Fixture Park"},"arrivedAt":"${hoursAgo(3)}","departedAt":null},"pause":null},"todayVisits":[{"id":"visit-1","place":{"id":"place-1","placeName":"Fixture Park"},"arrivedAt":"${hoursAgo(3)}","departedAt":null},{"id":"visit-0","place":{"id":"place-2","placeName":"Corner Cafe"},"arrivedAt":"${hoursAgo(8)}","departedAt":"${hoursAgo(5)}"}],"hasMoreTodayVisits":false""")},
         "health":${section("shown", ""","healthTypes":[{"healthType":"stepCount","typeName":"Steps","quantityUnit":"count","summaryKind":"sum","latestDaySummary":{"summaryDay":"${LocalDate.now()}","sumValue":8214,"averageValue":null,"quantityUnit":"count"}}],"hasMore":false""")},
         "finance":${section("shown", ""","allFinanceShareLevel":"spending","financeAccounts":[{"financeAccountId":"account-1","ownerName":"Sam Fixture","financeShareLevel":"balance","accountName":"Everyday checking","accountMask":"4242","accountKind":"checking","currencyCode":"USD","currentBalance":1280.5,"balanceAt":"${hoursAgo(2)}"}],"hasMore":false""")},
         "photos":{"photosState":"shown","photos":[{"id":"photo-1","smallPreviewUrl":null,"placeName":"","takenAt":"${daysAgo(1)}"},{"id":"photo-2","smallPreviewUrl":null,"placeName":"","takenAt":"${daysAgo(2)}"},{"id":"photo-3","smallPreviewUrl":null,"placeName":"","takenAt":"${daysAgo(3)}"}],"hasMorePhotos":true,"albumsState":"shown","albums":[{"id":"album-1","albumTitle":"Picnic","photoCount":12,"coverPreviewUrl":null,"isOwnedByCaller":false,"ownerName":"Sam Fixture"}],"hasMoreAlbums":false},
         "mail":${section("shown", ""","mails":[{"mailboxItemId":"item-lunch","mailboxId":"mailbox-1","folderPath":"Inbox","subject":"Lunch on Friday?","fromAddress":"sam@example.org","fromName":"Sam Fixture","isFromPerson":true,"isUnread":true,"receivedAt":"${hoursAgo(1)}","snippet":"Sam suggests the noodle place."},{"mailboxItemId":"item-lunch-2","mailboxId":"mailbox-1","folderPath":"Sent","subject":"Re: Lunch on Friday?","fromAddress":"alex@example.com","fromName":"","isFromPerson":false,"isUnread":false,"receivedAt":"${hoursAgo(20)}","snippet":""}],"hasMore":true""")},
         "recordings":${section("shown", ""","recordings":[{"recordingId":"recording-1","title":"Planning the picnic","recordedAt":"${daysAgo(2)}","durationMS":600000,"spokenPassageCount":12}],"hasMore":false""")},
         "memory":${section("shown", ""","pages":[{"nodePath":"people/sam","nodeName":"Sam","nodeKind":"person","summary":"Likes tea.","updatedAt":"${daysAgo(1)}"},{"nodePath":"people/sam-park","nodeName":"Park picnics","nodeKind":"topic","summary":"Plans for the next picnic.","updatedAt":"${daysAgo(4)}","isMatchedByName":true}],"hasMore":false""")}}
    """.trimIndent()

    private fun alexaCard(): String = """
        {"person":$ALEXA_MEMBER,"contact":null,"sharedCircles":[{"circleId":"circle-friends","circleName":"Book club","circleKind":"friends"}],"canMessage":true,
         "location":${section("shown", ""","location":{"userId":"user-alex","shareLevel":"current","latestPoint":{"latitudeDegrees":10.5,"longitudeDegrees":20.5,"measuredAt":"${hoursAgo(1)}"},"openVisit":null,"pause":null},"todayVisits":[],"hasMoreTodayVisits":false""")},
         "health":${section("notShared", ""","healthTypes":[],"hasMore":false""")},
         "photos":{"photosState":"needsContact","photos":[],"hasMorePhotos":false,"albumsState":"shown","albums":[{"id":"album-1","albumTitle":"Spring walk","photoCount":14,"coverPreviewUrl":null,"isOwnedByCaller":false,"ownerName":"Alexa Demo"}],"hasMoreAlbums":false},
         "mail":${section("needsContact", ""","mails":[],"hasMore":false""")},
         "recordings":${section("needsContact", ""","recordings":[],"hasMore":false""")},
         "memory":${section("needsContact", ""","pages":[],"hasMore":false""")}}
    """.trimIndent()

    private fun booksCard(): String = """
        {"person":null,"contact":${contact("contact-books")},"sharedCircles":[],"canMessage":false,
         "location":${section("unavailable", ""","location":null,"todayVisits":[]""")},"health":${section("unavailable", ""","healthTypes":[],"hasMore":false""")},
         "photos":{"photosState":"shown","photos":[],"hasMorePhotos":false,"albumsState":"unavailable","albums":[],"hasMoreAlbums":false},
         "mail":${section("shown", ""","mails":[{"mailboxItemId":"item-invoice","mailboxId":"mailbox-1","folderPath":"Inbox","subject":"Your receipt for order 1042","fromAddress":"orders@example.org","fromName":"Example Books","isFromPerson":true,"isUnread":true,"receivedAt":"${hoursAgo(3)}","snippet":"Receipt for two books."}],"hasMore":false""")},
         "recordings":${section("shown", ""","recordings":[],"hasMore":false""")},"memory":${section("shown", ""","pages":[],"hasMore":false""")}}
    """.trimIndent()

    fun personCard(variables: JsonObject): String = when {
        variables.text("userId") == "user-alex" -> alexaCard()
        variables.text("contactId") == "contact-books" -> booksCard()
        else -> samCard()
    }

    val visits: String
        get() = """
            {"placeVisits":[
              {"id":"visit-1","place":{"id":"place-1","placeName":"Fixture Park","isNamedByPerson":false,"placeLabel":"","addressText":"","visitCount":3,"lastVisitAt":"${hoursAgo(3)}"},"arrivedAt":"${hoursAgo(3)}","departedAt":null,"durationSeconds":null,"photoCount":2},
              {"id":"visit-0","place":{"id":"place-2","placeName":"Corner Cafe","isNamedByPerson":true,"placeLabel":"","addressText":"2 Example Road","visitCount":9,"lastVisitAt":"${hoursAgo(5)}"},"arrivedAt":"${hoursAgo(8)}","departedAt":"${hoursAgo(5)}","durationSeconds":10800,"photoCount":0}],"nextOffset":null}
        """.trimIndent()

    val places: String = """
        {"places":[{"id":"place-1","placeName":"Fixture Park","isNamedByPerson":false,"placeLabel":"","addressText":"","visitCount":3,"lastVisitAt":null},
                   {"id":"place-2","placeName":"Corner Cafe","isNamedByPerson":true,"placeLabel":"","addressText":"2 Example Road","visitCount":9,"lastVisitAt":null},
                   {"id":"place-3","placeName":"Sample Library","isNamedByPerson":false,"placeLabel":"","addressText":"5 Example Street","visitCount":2,"lastVisitAt":null}],"nextOffset":null}
    """.trimIndent()

    val answerers: List<DemoAnswerer> = listOf(
        graphQLField("ListReachableAddressBooks") { _ -> books },
        graphQLField("ListAddressBooks") { _ -> """[{"id":"book-own","name":"Contacts","contacts":6}]""" },
        graphQLField("ListContacts") { variables ->
            val all = if (variables.text("addressBookId") == "book-home") circleContacts else ownContacts
            val words = variables.text("query")?.lowercase().orEmpty()
            val shown = if (words.isEmpty()) all else all.filter { row -> row.lowercase().contains(words) }
            "[" + shown.joinToString(",") + "]"
        },
        graphQLField("GetContact") { variables -> contact(variables.text("id").orEmpty()) },
        // The person card asks only for the contact of a memory page; the Knowledge screens ask for the whole page (its facts), which Knowledge answers.
        graphQLField("AgentGraphPage") { _ -> """{"contact":{"id":"contact-alex"}}""" }.let { contactOnly -> DemoAnswerer { request -> if (request.document.contains("facts")) null else contactOnly.answer(request) } },
        graphQLField("ListContactAccountSuggestions") { _ -> suggestions },
        graphQLField("ListCircles") { _ -> """[{"id":"circle-home","name":"Home","circleKind":"family"},{"id":"circle-book","name":"Book club","circleKind":"friends"}]""" },
        graphQLField("ListCirclePeople") { _ -> "[$PERSON_SAM,$PERSON_ALEXA,$PERSON_PAT]" },
        graphQLField("ReadPersonCard") { variables -> personCard(variables) },
        graphQLField("ListLocationVisits") { _ -> visits },
        graphQLField("ListPlaces") { _ -> places },
        graphQLField("SaveContact") { _ -> """{"id":"contact-new","name":"New Contact"}""" },
        graphQLField("DeleteContact", "true"),
        graphQLField("RemoveContactPicture", """{"id":"contact-sam","hasPhoto":false}"""),
        graphQLField("SetMyContact", "true"),
        graphQLField("SetContactAccount", """{"id":"contact-pat","accountUserId":"user-pat","account":$PERSON_PAT}"""),
        graphQLField("CreateCircleAddressBook", """{"id":"book-book","name":"Book club","description":"","contacts":0,"circleId":"circle-book","circleName":"Book club","isCircleBook":true,"canManage":true}"""),
        graphQLField("SaveAddressBook", """{"id":"book-home","name":"Home","description":"","contacts":2,"circleId":"circle-home","circleName":"Home","isCircleBook":true,"canManage":true}"""),
        graphQLField("DeleteCircleAddressBook", "true"),
        graphQLField("OpenDirectChat", """{"id":"conversation-sam"}"""),
    )
}

/**
 * What demo mode answers for Contacts: one `graphQLField("<Field>", """<json>""")` per document the screens send, so every
 * screen and state can be reached and photographed without a server. Most specific first.
 */
val contactsDemoAnswerers: List<DemoAnswerer> = ContactsDemoData.answerers
