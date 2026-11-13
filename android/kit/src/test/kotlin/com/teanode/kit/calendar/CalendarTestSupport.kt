package com.teanode.kit.calendar

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject

/** A connection that sends everything to a fake server, which answers every request the same way. */
fun fakeConnection(answer: (String) -> String): Pair<Connection, FakeServer> {
    val fake = FakeServer { request -> 200 to answer(request.body?.toString(Charsets.UTF_8).orEmpty()) }
    val account = Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", Long.MAX_VALUE / 4, "robin")
    return Connection(account, fake) to fake
}

fun sentBody(fake: FakeServer, index: Int = 0): JsonObject = TeaNodeJson.parseToJsonElement(fake.sent[index].body!!.toString(Charsets.UTF_8)) as JsonObject

fun sentVariables(fake: FakeServer, index: Int = 0): Map<String, JsonElement> = sentBody(fake, index)["variables"] as JsonObject
