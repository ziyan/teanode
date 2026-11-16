package com.teanode.kit.location

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import java.time.Instant

internal val testServer = ServerAddress.parse("mail.example.com")

/** A connection to a fake server that answers every call with [body]. */
internal fun connectionAnswering(body: String): Pair<Connection, FakeServer> {
    val fake = FakeServer { _ -> 200 to body }
    val account = Account(testServer, "a-client", "tnt_a", "", Long.MAX_VALUE / 4, "robin")
    return Connection(account, fake) to fake
}

/** The variables a request carried. */
internal fun sentVariables(fake: FakeServer, index: Int = 0): Map<String, JsonElement> {
    val body = TeaNodeJson.parseToJsonElement(fake.sent[index].body!!.toString(Charsets.UTF_8)) as JsonObject
    return body["variables"] as JsonObject
}

internal fun instantOf(text: String): Instant = ServerTime.instant(text)!!
