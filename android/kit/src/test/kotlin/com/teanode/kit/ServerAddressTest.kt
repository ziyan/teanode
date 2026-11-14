package com.teanode.kit

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test

class ServerAddressTest {
    @Test
    fun whatPeopleTypeBecomesTheServersOrigin() {
        val written = listOf(
            "mail.example.com" to "https://mail.example.com",
            "  Mail.Example.COM \n" to "https://mail.example.com",
            "https://mail.example.com/agent?tab=main#top" to "https://mail.example.com",
            "mail.example.com:10443" to "https://mail.example.com:10443",
            "https://mail.example.com:443" to "https://mail.example.com",
            "http://localhost:10081" to "http://localhost:10081",
            "http://127.0.0.1:10081/" to "http://127.0.0.1:10081",
            "http://teanode.localhost" to "http://teanode.localhost",
        )
        for ((typed, origin) in written) {
            assertEquals(origin, ServerAddress.parse(typed).toString(), "for $typed")
        }
    }

    @Test
    fun whatIsNotAServerIsRefusedWithWhy() {
        val refused = listOf(
            "" to ServerAddressError.IsEmpty,
            "   " to ServerAddressError.IsEmpty,
            "http://mail.example.com" to ServerAddressError.IsNotEncrypted,
            "https://someone:secret@mail.example.com" to ServerAddressError.HasCredentials,
            "ftp://mail.example.com" to ServerAddressError.IsNotAnAddress,
            "javascript:alert(1)" to ServerAddressError.IsNotAnAddress,
            "https://" to ServerAddressError.IsNotAnAddress,
        )
        for ((typed, reason) in refused) {
            val failure = assertThrows(ServerAddressException::class.java, { ServerAddress.parse(typed) }, "for $typed")
            assertEquals(reason, failure.reason, "for $typed")
        }
    }

    @Test
    fun endpointsAreOnTheServer() {
        val server = ServerAddress.parse("mail.example.com:10443")
        assertEquals("https://mail.example.com:10443/api/v1/graphql", server.endpoint("/api/v1/graphql").toString())
        assertEquals("https://mail.example.com:10443/oauth/token", server.endpoint("oauth/token").toString())
        assertEquals(
            "https://mail.example.com:10443/api/v1/mail/m1/remote?url=https%3A%2F%2Fpictures.example%2Fa.png",
            server.endpoint("/api/v1/mail/m1/remote?url=https%3A%2F%2Fpictures.example%2Fa.png").toString(),
            "a query stays a query, not an escaped part of the path",
        )
        assertEquals("wss://mail.example.com:10443/api/v1/graphql", server.websocketEndpoint("/api/v1/graphql"))
        assertEquals("ws://localhost:10081/api/v1/agent/voice", ServerAddress.parse("http://localhost:10081").websocketEndpoint("/api/v1/agent/voice"))
    }

    @Test
    fun aServerAddressIsKeptAsItsText() {
        val account = Account(ServerAddress.parse("mail.example.com"), "a-client", "a", "r", 1L, "robin")
        val written = TeaNodeJson.encodeToString(Account.serializer(), account)
        assertEquals(true, written.contains("\"server\":\"https://mail.example.com\""))
        assertEquals(account, TeaNodeJson.decodeFromString(Account.serializer(), written))
    }

    @Test
    fun aConnectLinkCarriesItsServerAndCode() {
        val server = ServerAddress.parse("mail.example.com:10443")
        val link = ConnectLink(server, "abc123")
        assertEquals("teanode.com", link.url.substringAfter("https://").substringBefore("/"))
        assertEquals(false, link.url.contains("?"), "the server and code belong after the #, not in the query")
        assertEquals(link, ConnectLink.parse(link.url))
    }

    @Test
    fun theServersLinkIsRead() {
        // As the server writes it: Go's url.Values, sorted, percent-encoded.
        val link = ConnectLink.parse("https://teanode.com/app/connect#code=abc123&server=https%3A%2F%2Fmail.example.com%3A10443")
        assertNotNull(link)
        assertEquals("https://mail.example.com:10443", link!!.server.toString())
        assertEquals("abc123", link.connectCode)
    }

    @Test
    fun aLinkWithOnlyAServerLeavesSigningInToThePerson() {
        val link = ConnectLink.parse("https://teanode.com/app/connect#server=mail.example.com")
        assertEquals("https://mail.example.com", link!!.server.toString())
        assertNull(link.connectCode)
    }

    @Test
    fun otherLinksAreNotConnectLinks() {
        for (written in listOf(
            "https://example.com/app/connect#server=mail.example.com",
            "https://teanode.com/app/other#server=mail.example.com",
            "http://teanode.com/app/connect#server=mail.example.com",
            "https://teanode.com/app/connect",
            "https://teanode.com/app/connect?server=mail.example.com",
            "https://teanode.com/app/connect#server=http://mail.example.com",
            "not a link",
        )) {
            assertNull(ConnectLink.parse(written), "for $written")
        }
    }
}
