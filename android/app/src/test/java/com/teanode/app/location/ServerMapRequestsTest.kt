package com.teanode.app.location

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.HTTPResponse
import com.teanode.kit.HTTPTransport
import com.teanode.kit.ServerAddress
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ServerMapRequestsTest {
    private val sentRequests = mutableListOf<Request>()

    /** The map's client, with the network replaced by an answer of 200 to whatever reaches it. */
    private val client: OkHttpClient = ServerMapRequests.client().newBuilder()
        .addInterceptor { chain ->
            sentRequests.add(chain.request())
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(200).message("OK").body(ByteArray(0).toResponseBody(null)).build()
        }
        .build()

    private val noTransport = object : HTTPTransport {
        override suspend fun send(request: HTTPRequest): HTTPResponse = error("the map's token needs no renewal here")
    }

    private fun signIn() {
        val account = Account(ServerAddress.parse("https://mail.example.com"), "client", "tnt_map", "", System.currentTimeMillis() + 3_600_000, "alex")
        ServerMapRequests.connection = Connection(account, noTransport)
    }

    private fun fetch(url: String): Response = client.newCall(Request.Builder().url(url).build()).execute()

    @After fun signOut() {
        ServerMapRequests.connection = null
    }

    @Test fun `a file of the server's map is asked for with the account's token`() {
        signIn()
        val response = fetch("https://mail.example.com/api/v1/map/planet/1/2/3.pbf")
        assertEquals(200, response.code)
        assertEquals(1, sentRequests.size)
        assertEquals("Bearer tnt_map", sentRequests.single().header("Authorization"))
    }

    @Test fun `any other address is refused without being sent`() {
        signIn()
        for (url in listOf("https://tiles.example.org/planet/1/2/3.pbf", "http://mail.example.com/api/v1/map/style", "https://mail.example.com:8443/api/v1/map/style")) {
            assertEquals(url, 404, fetch(url).code)
        }
        assertTrue(sentRequests.isEmpty())
    }

    @Test fun `nothing is sent while signed out`() {
        assertEquals(404, fetch("https://mail.example.com/api/v1/map/style").code)
        assertTrue(sentRequests.isEmpty())
    }
}
