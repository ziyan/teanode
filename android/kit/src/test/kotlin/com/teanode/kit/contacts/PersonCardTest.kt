package com.teanode.kit.contacts

import com.teanode.kit.FakeServer
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.connectionFor
import com.teanode.kit.sentVariables
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.LocalDate

class PersonCardTest {
    private fun decode(json: String): PersonCard = TeaNodeJson.decodeFromString(PersonCard.serializer(), json)

    private val personJson =
        """{"userId":"user-2","username":"sam","name":"Sam Fixture","avatarUrl":"/api/v1/avatar/user/user-2?v=1","avatarColorIndex":2,"memberRole":"member","joinedAt":"2030-01-02T00:00:00Z","relationshipKind":"parent","relationshipTermGender":"feminine","relationshipWord":"mother","linkedContactId":"contact-2"}"""

    private val unlinkedPersonJson = personJson.replace("\"linkedContactId\":\"contact-2\"", "\"linkedContactId\":\"\"")

    private val contactJson =
        """{"id":"contact-2","name":"Sam Fixture","organization":null,"emails":["sam@example.org","sam.work@example.org"],"phones":["+1 (555) 0142"],"note":"Likes tea","hasPhoto":false,"addresses":[{"street":"1 Example Road","locality":"Sampleton","region":"","postalCode":"00000","country":"Exampleland"}],"accountUserId":"user-2","account":{"userId":"user-2","username":"sam","name":"Sam Fixture","avatarUrl":null,"avatarColorIndex":2}}"""

    private val placeJson =
        """{"id":"place-1","userId":"user-2","placeName":"Fixture Park","isNamedByPerson":false,"placeLabel":"","latitudeDegrees":10.5,"longitudeDegrees":20.5,"radiusMeters":75,"osmType":"way","osmId":1,"osmName":"Fixture Park","osmCategory":"leisure=park","addressText":"","isAddressTyped":false,"visitCount":3,"lastVisitAt":"2030-01-02T10:00:00Z","namingErrorMessage":"","createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-02T10:00:00Z"}"""

    private val fullCardJson = """
        {"person":$personJson,"contact":$contactJson,
         "sharedCircles":[{"circleId":"circle-1","circleName":"Home","circleKind":"family"}],"canMessage":true,
         "location":{"sectionState":"shown","hasMoreTodayVisits":true,
           "location":{"userId":"user-2","username":"sam","name":"Sam Fixture","avatarUrl":null,"avatarColorIndex":2,"shareLevel":"history",
             "latestPoint":{"latitudeDegrees":10.5,"longitudeDegrees":20.5,"accuracyMeters":12,"altitudeMeters":null,"speedMetersPerSecond":null,"courseDegrees":null,"measuredAt":"2030-01-02T10:30:00Z","locationSource":"phone"},
             "openVisit":{"id":"visit-1","arrivedAt":"2030-01-02T09:00:00Z","departedAt":null,"durationSeconds":null,"latitudeDegrees":10.5,"longitudeDegrees":20.5,"accuracyMeters":30,"visitSource":"visit","place":$placeJson}},
           "todayVisits":[{"id":"visit-1","arrivedAt":"2030-01-02T09:00:00Z","departedAt":null,"durationSeconds":null,"latitudeDegrees":10.5,"longitudeDegrees":20.5,"accuracyMeters":30,"visitSource":"visit","place":$placeJson}]},
         "health":{"sectionState":"shown","hasMore":false,"healthTypes":[
           {"healthType":"stepCount","healthCategory":"activity","typeName":"Steps","valueKind":"quantity","quantityUnit":"count","summaryKind":"sum","firstDay":"2029-12-01","lastDay":"2030-01-02","sampleCount":0,
            "latestDaySummary":{"summaryDay":"2030-01-02","quantityUnit":"count","sumValue":8214,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":40,"durationSeconds":null,"categorySeconds":null}}]},
         "photos":{"photosState":"shown","hasMorePhotos":true,"albumsState":"shown","hasMoreAlbums":false,
           "photos":[{"id":"photo-1","createdAt":"2030-01-01T00:00:00Z","photoKind":"picture","takenAt":"2030-01-01T10:00:00Z","placeName":"","smallPreviewUrl":"/p/1/small","largePreviewUrl":"/p/1/large"}],
           "albums":[{"id":"album-1","albumTitle":"Picnic","createdAt":"2030-01-01T00:00:00Z","photoCount":12,"coverPreviewUrl":"/p/1/small","ownerUserId":"user-2","ownerName":"Sam Fixture","isOwnedByCaller":false,"shareLevel":"view"}]},
         "mail":{"sectionState":"shown","hasMore":true,"mails":[
           {"mailboxItemId":"item-1","mailboxId":"box-1","folderPath":"Inbox","subject":"Lunch on Friday","fromAddress":"sam@example.org","fromName":"Sam Fixture","isFromPerson":true,"isUnread":true,"receivedAt":"2030-01-02T08:00:00.123456789Z","snippet":"Sam suggests the noodle place."},
           {"mailboxItemId":"item-2","mailboxId":"box-1","folderPath":"Sent","subject":"Re: Lunch on Friday","fromAddress":"me@example.com","fromName":"","isFromPerson":false,"isUnread":false,"receivedAt":"2030-01-02T08:30:00Z","snippet":""}]},
         "recordings":{"sectionState":"shown","hasMore":false,"recordings":[
           {"recordingId":"recording-1","title":"Planning the picnic","recordedAt":"2030-01-01T12:00:00Z","durationMS":600000,"spokenPassageCount":12}]},
         "memory":{"sectionState":"shown","hasMore":false,"pages":[
           {"nodePath":"people/sam","nodeName":"Sam","nodeKind":"person","summary":"Likes tea.","updatedAt":"2030-01-02T00:00:00Z"}]}}
    """

    @Test
    fun aLinkedPersonsCardDecodesEverySection() {
        val card = decode(fullCardJson)
        assertEquals("user-2", card.person?.userId)
        assertEquals("mother", card.person?.relationshipWord)
        assertTrue(card.hasContact)
        assertTrue(card.contact?.isLinkedToAccount == true)
        assertEquals(listOf("Home"), card.sharedCircles?.map { circle -> circle.circleName })
        assertEquals("family", card.sharedCircles?.first()?.circleKind)
        assertTrue(card.canMessage)
        val location = card.location!!
        assertEquals(PersonCardSectionState.Shown, location.sectionState)
        assertEquals("history", location.location?.shareLevel)
        assertEquals("Fixture Park", location.location?.openVisit?.place?.placeName)
        assertEquals(listOf("visit-1"), location.todayVisits?.map { visit -> visit.id })
        assertTrue(location.hasMoreTodayVisits)
        assertEquals(listOf("stepCount"), card.health?.healthTypes?.map { type -> type.healthType })
        assertEquals(8214.0, card.health?.healthTypes?.first()?.latestDaySummary?.sumValue)
        assertEquals(listOf("photo-1"), card.photos?.photos?.map { photo -> photo.id })
        assertTrue(card.photos!!.hasMorePhotos)
        assertEquals(listOf("Picnic"), card.photos?.albums?.map { album -> album.albumTitle })
        val mail = card.mail!!
        assertEquals(listOf("item-1", "item-2"), mail.mails?.map { item -> item.mailboxItemId })
        assertEquals("Sam Fixture", mail.mails!![0].senderText)
        assertEquals("me@example.com", mail.mails!![1].senderText)
        assertTrue(mail.hasMore)
        assertEquals(12, card.recordings?.recordings?.first()?.spokenPassageCount)
        assertEquals(listOf("people/sam"), card.memory?.pages?.map { page -> page.nodePath })
    }

    @Test
    fun aPersonWithNoContactAsksForOneAndKeepsTheAccountsSections() {
        val card = decode(
            """
            {"person":$unlinkedPersonJson,"contact":null,
             "sharedCircles":[],"canMessage":true,
             "location":{"sectionState":"shown","location":null,"todayVisits":null},
             "health":{"sectionState":"notShared","healthTypes":null,"hasMore":false},
             "photos":{"photosState":"needsContact","photos":null,"hasMorePhotos":false,"albumsState":"shown","albums":null,"hasMoreAlbums":false},
             "mail":{"sectionState":"needsContact","mails":null,"hasMore":false},
             "recordings":{"sectionState":"needsContact","recordings":null,"hasMore":false},
             "memory":{"sectionState":"needsContact","pages":null,"hasMore":false}}
            """,
        )
        assertFalse(card.hasContact)
        assertTrue(card.needsContact)
        assertNull(card.contact)
        assertFalse(card.person?.isLinkedToContact ?: true)
        assertEquals(PersonCardSectionState.NotShared, card.health?.sectionState)
        assertEquals(PersonCardSectionState.NeedsContact, card.photos?.photosState)
        assertEquals(PersonCardSectionState.Shown, card.photos?.albumsState)
        assertNull(card.mailAddress)
        assertNull(card.callAddress)
        assertNull(card.directionsAddress)
        assertEquals("Sam Fixture", card.displayName)
    }

    @Test
    fun aContactWithoutAnAccountHasNoPersonAndLeavesTheAccountsSectionsOut() {
        val card = decode(
            """
            {"person":null,"contact":{"id":"contact-9","name":null,"organization":"Fixture Books","emails":["orders@example.org"],"phones":[],"hasPhoto":false,"addresses":[],"accountUserId":null,"account":null},
             "sharedCircles":null,"canMessage":false,
             "location":{"sectionState":"unavailable","location":null,"todayVisits":[]},
             "health":{"sectionState":"unavailable","healthTypes":[],"hasMore":false},
             "photos":{"photosState":"shown","photos":[],"hasMorePhotos":false,"albumsState":"unavailable","albums":[],"hasMoreAlbums":false},
             "mail":{"sectionState":"shown","mails":[],"hasMore":false},
             "recordings":{"sectionState":"shown","recordings":[],"hasMore":false},
             "memory":{"sectionState":"shown","pages":[],"hasMore":false}}
            """,
        )
        assertNull(card.person)
        assertFalse(card.needsContact)
        assertEquals("orders@example.org", card.displayName)
        assertEquals("orders@example.org", card.mailAddress)
        assertFalse(card.canMessage)
        assertEquals(PersonCardSectionState.Unavailable, card.location?.sectionState)
        assertEquals(PersonCardSectionState.Unavailable, card.photos?.albumsState)
        assertTrue(card.sharedCircles.isNullOrEmpty())
    }

    @Test
    fun aSectionStateThisAppDoesNotKnowIsUnavailableAndAMissingSectionIsNull() {
        val card = decode("""{"person":null,"contact":null,"canMessage":false,"health":{"sectionState":"snoozed","healthTypes":[],"hasMore":false}}""")
        assertEquals(PersonCardSectionState.Unavailable, card.health?.sectionState)
        assertNull(card.location)
        assertNull(card.photos)
        assertNull(card.mail)
        assertNull(card.recordings)
        assertNull(card.memory)
        assertEquals("", card.displayName)
    }

    @Test
    fun aSectionIsListedUnlessItIsLeftOutOrWaitsForAContact() {
        assertTrue(PersonCardSectionState.Shown.isListed)
        assertTrue(PersonCardSectionState.NotShared.isListed)
        assertTrue(PersonCardSectionState.Failed.isListed)
        assertFalse(PersonCardSectionState.Unavailable.isListed)
        assertFalse(PersonCardSectionState.NeedsContact.isListed)
    }

    @Test
    fun theButtonsAreMadeFromTheContactAndTheLatestPoint() {
        val card = decode(fullCardJson)
        assertEquals("sam@example.org", card.mailAddress)
        assertEquals("tel:+15550142", card.callAddress)
        assertEquals("geo:10.5,20.5?q=10.5,20.5", card.directionsAddress)
    }

    @Test
    fun aKindIsTodayOnlyWhenItsLatestDayIsTheDayGiven() {
        val steps = decode(fullCardJson).health!!.healthTypes!!.first()
        assertTrue(PersonCardHealthSection.isToday(steps, LocalDate.of(2030, 1, 2)))
        assertFalse(PersonCardHealthSection.isToday(steps, LocalDate.of(2030, 1, 3)))
    }

    @Test
    fun anAccountAndAContactAreNamedAsTheServerTakesThem() {
        assertEquals(mapOf("userId" to JsonPrimitive("user-2")), PersonCardTarget.Account("user-2").variables)
        assertEquals(
            mapOf("addressBookId" to JsonPrimitive("book-1"), "contactId" to JsonPrimitive("contact-9")),
            PersonCardTarget.Contact("book-1", "contact-9").variables,
        )
        assertNotEquals(PersonCardTarget.Account("user-2").id, PersonCardTarget.Contact("user-2", "user-2").id)
    }

    @Test
    fun readingACardSendsTheAccountAndTheLimitAndNothingElse() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"ReadPersonCard":$fullCardJson}}""" }
        val card = connectionFor(server).readPersonCard(PersonCardTarget.Account("user-2"))
        assertEquals("sam", card.person?.username)
        val variables = sentVariables(server.sent.first())
        assertEquals("user-2", (variables["userId"] as JsonPrimitive).content)
        assertEquals(PersonCard.ITEM_LIMIT.toString(), (variables["itemLimit"] as JsonPrimitive).content)
        assertNull(variables["addressBookId"])
        assertNull(variables["contactId"])
        assertTrue(server.sent.first().body!!.toString(Charsets.UTF_8).contains("ReadPersonCard"))
    }

    @Test
    fun readingAContactsCardSendsTheBookAndTheContactAndNoAccount() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"ReadPersonCard":{"person":null,"contact":null,"sharedCircles":[],"canMessage":false}}}""" }
        val card = connectionFor(server).readPersonCard(PersonCardTarget.Contact("book-1", "contact-9"), itemLimit = 6)
        assertNull(card.person)
        val variables = sentVariables(server.sent.first())
        assertEquals("book-1", (variables["addressBookId"] as JsonPrimitive).content)
        assertEquals("contact-9", (variables["contactId"] as JsonPrimitive).content)
        assertEquals("6", (variables["itemLimit"] as JsonPrimitive).content)
        assertNull(variables["userId"])
    }

    @Test
    fun aRefusedCardIsAnErrorNotAnEmptyCard() {
        val server = FakeServer { 200 to """{"data":null,"errors":[{"message":"not found"}]}""" }
        val failure = assertThrows(TeaNodeError.Refused::class.java) { runBlocking { connectionFor(server).readPersonCard(PersonCardTarget.Account("user-9")) } }
        assertEquals("not found", failure.reason)
    }

    @Test
    fun theDocumentAsksForEverySectionTheCardShows() {
        val document = PersonCardDocuments.PERSON_CARD
        for (name in listOf("person {", "contact {", "sharedCircles", "canMessage", "location {", "health {", "photos {", "mail {", "recordings {", "memory {")) {
            assertTrue(document.contains(name), name)
        }
        assertTrue(document.contains("ReadPersonCard(userId: \$userId, addressBookId: \$addressBookId, contactId: \$contactId, itemLimit: \$itemLimit)"))
    }
}
