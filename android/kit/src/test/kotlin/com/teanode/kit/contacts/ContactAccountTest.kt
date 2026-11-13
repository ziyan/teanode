package com.teanode.kit.contacts

import com.teanode.kit.FakeServer
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.connectionFor
import com.teanode.kit.sentVariables
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ContactAccountTest {
    private val personJson = """{"userId":"user-2","username":"person-b","name":"Person B","avatarUrl":"/api/v1/avatar/user/user-2?v=1","avatarColorIndex":3}"""

    private fun text(variables: Map<String, kotlinx.serialization.json.JsonElement>, key: String) = (variables[key] as? JsonPrimitive)?.content

    @Test
    fun aLinkedContactCarriesItsAccountAndAnOlderServerSaysNone() {
        val linked = TeaNodeJson.decodeFromString(ContactDetail.serializer(), """{"id":"c1","name":"Person B","accountUserId":"user-2","account":$personJson}""")
        assertTrue(linked.isLinkedToAccount)
        assertEquals("person-b", linked.account?.username)
        assertEquals("Person B", linked.account?.displayName)
        val plain = TeaNodeJson.decodeFromString(ContactDetail.serializer(), """{"id":"c2","name":"Person C"}""")
        assertFalse(plain.isLinkedToAccount)
        assertNull(plain.account)
        assertFalse(TeaNodeJson.decodeFromString(ContactDetail.serializer(), """{"id":"c3","accountUserId":null,"account":null}""").isLinkedToAccount)
    }

    @Test
    fun linkingSendsTheBookTheContactAndTheAccountAndClearingSendsNull() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"SetContactAccount":{"id":"c1","accountUserId":"user-2","account":$personJson}}}""" }
        val link = connectionFor(server).setContactAccount("book-1", "c1", "user-2")
        assertEquals("user-2", link.accountUserId)
        val variables = sentVariables(server.sent.first())
        assertEquals("book-1", text(variables, "addressBookId"))
        assertEquals("c1", text(variables, "contactId"))
        assertEquals("user-2", text(variables, "accountUserId"))

        val clearing = FakeServer { 200 to """{"data":{"SetContactAccount":{"id":"c1","accountUserId":null,"account":null}}}""" }
        assertNull(connectionFor(clearing).setContactAccount("book-1", "c1", null).account)
        assertEquals(JsonNull, sentVariables(clearing.sent.first())["accountUserId"])
    }

    @Test
    fun suggestionsPairAContactWithAnAccountAndAreIdentifiedByTheContact() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"ListContactAccountSuggestions":[{"addressBookId":"book-1","contact":{"id":"c1","name":"Person B","emails":["b@example.org"]},"account":$personJson}]}}"""
        }
        val suggestions = connectionFor(server).contactAccountSuggestions()
        assertEquals(listOf("c1"), suggestions.map { suggestion -> suggestion.id })
        assertEquals("book-1", suggestions.first().addressBookId)
        assertEquals("user-2", suggestions.first().account.userId)
    }

    @Test
    fun aBookSaysWhoseItIsAndAnOlderServerSaysItIsTheirOwn() {
        val circle = TeaNodeJson.decodeFromString(
            AddressBook.serializer(),
            """{"id":"b2","name":"Everyone","description":"","contacts":3,"circleId":"c1","circleName":"Home","isCircleBook":true,"canManage":false}""",
        )
        assertTrue(circle.isOfACircle)
        assertEquals(false, circle.canManage)
        assertEquals("Everyone (Home)", circle.shownName)
        val sameName = TeaNodeJson.decodeFromString(
            AddressBook.serializer(), """{"id":"b3","name":"Home","contacts":0,"circleId":"c1","circleName":"Home","isCircleBook":true}""",
        )
        assertEquals("Home", sameName.shownName)
        val own = TeaNodeJson.decodeFromString(AddressBook.serializer(), """{"id":"b1","name":"Contacts","contacts":5}""")
        assertFalse(own.isOfACircle)
        assertEquals("Contacts", own.shownName)
    }

    @Test
    fun thePicturesOfACircleBooksContactsAreAskedForWithTheBook() {
        val contact = ContactSummary(id = "c1", name = "Person B", hasPhoto = true)
        assertEquals("/api/v1/contacts/c1/photo", contact.pictureAddress(null))
        assertEquals("/api/v1/contacts/c1/photo?addressBookId=b2", contact.pictureAddress("b2"))
        assertEquals("/api/v1/contacts/c1/photo", ContactSummary.pictureAddress("c1", ""))
    }

    @Test
    fun aContactIsReadDeletedAndHasItsPictureRemovedInTheBookNamed() = runBlocking {
        val server = FakeServer { request ->
            val body = request.body!!.toString(Charsets.UTF_8)
            when {
                body.contains("DeleteContact") -> 200 to """{"data":{"DeleteContact":true}}"""
                body.contains("RemoveContactPicture") -> 200 to """{"data":{"RemoveContactPicture":{"id":"c1","hasPhoto":false}}}"""
                else -> 200 to """{"data":{"GetContact":{"id":"c1","name":"Person B"}}}"""
            }
        }
        val connection = connectionFor(server)
        connection.contact("c1", "b2")
        connection.deleteContact("c1", "b2")
        connection.removeContactPicture("c1", "b2")
        for (request in server.sent) {
            assertEquals("c1", text(sentVariables(request), "id"))
            assertEquals("b2", text(sentVariables(request), "addressBookId"))
        }
        connection.contact("c1")
        connection.deleteContact("c1", "")
        connection.removeContactPicture("c1")
        for (request in server.sent.drop(3)) assertNull(sentVariables(request)["addressBookId"])
        for (document in listOf(ContactDocuments.CONTACT, ContactDocuments.DELETE_CONTACT, ContactDocuments.REMOVE_CONTACT_PICTURE)) {
            assertTrue(document.contains("\$addressBookId: String"))
        }
    }

    @Test
    fun theReachableListIsTheOneThatHasTheCircles() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"ListReachableAddressBooks":[{"id":"b1","name":"Contacts","contacts":5,"circleId":null,"circleName":"","isCircleBook":false,"canManage":true},{"id":"b2","name":"Home","contacts":2,"circleId":"c1","circleName":"Home","isCircleBook":true,"canManage":true}]}}"""
        }
        val books = connectionFor(server).reachableAddressBooks()
        assertEquals(listOf("b1", "b2"), books.map { book -> book.id })
        assertEquals(listOf(false, true), books.map { book -> book.isOfACircle })
        assertTrue(server.sent.first().body!!.toString(Charsets.UTF_8).contains("ListReachableAddressBooks"))
    }

    @Test
    fun makingARenamingAndDeletingACircleBookSendTheirArguments() = runBlocking {
        val making = FakeServer {
            200 to """{"data":{"CreateCircleAddressBook":{"id":"b2","name":"Home","contacts":0,"circleId":"c1","circleName":"Home","isCircleBook":true,"canManage":false}}}"""
        }
        assertEquals("b2", connectionFor(making).createCircleAddressBook("c1").id)
        assertEquals("c1", text(sentVariables(making.sent.first()), "circleId"))
        assertNull(sentVariables(making.sent.first())["name"])

        val renaming = FakeServer {
            200 to """{"data":{"SaveAddressBook":{"id":"b2","name":"Everyone","description":"Kept","contacts":2,"circleId":"c1","circleName":"Home","isCircleBook":true,"canManage":true}}}"""
        }
        val renamed = connectionFor(renaming).renameAddressBook(AddressBook("b2", "Home", description = "Kept", circleId = "c1", isCircleBook = true), "Everyone")
        assertEquals("Everyone", renamed.name)
        val renamedVariables = sentVariables(renaming.sent.first())
        assertEquals("b2", text(renamedVariables, "id"))
        assertEquals("Everyone", text(renamedVariables, "name"))
        assertEquals("Kept", text(renamedVariables, "description"))

        val deleting = FakeServer { 200 to """{"data":{"DeleteCircleAddressBook":true}}""" }
        connectionFor(deleting).deleteCircleAddressBook("b2")
        assertEquals("b2", text(sentVariables(deleting.sent.first()), "addressBookId"))
    }
}
