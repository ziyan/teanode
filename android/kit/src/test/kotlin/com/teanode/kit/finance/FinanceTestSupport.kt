package com.teanode.kit.finance

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.HTTPRequest
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

internal fun connectionAnswering(body: String): Pair<Connection, FakeServer> {
    val fake = FakeServer { _ -> 200 to body }
    val account = Account(ServerAddress.parse("assistant.example.com"), "test-client", "tnt_test", "", Long.MAX_VALUE / 4, "test-owner")
    return Connection(account, fake) to fake
}

/** The variables of the last request the fake server was sent. */
internal fun variablesOf(request: HTTPRequest?): JsonObject =
    TeaNodeJson.parseToJsonElement(request!!.body!!.toString(Charsets.UTF_8)).jsonObject["variables"]!!.jsonObject

internal fun queryOf(request: HTTPRequest?): String =
    TeaNodeJson.parseToJsonElement(request!!.body!!.toString(Charsets.UTF_8)).jsonObject["query"]!!.jsonPrimitive.content

internal fun JsonObject.text(key: String): String? = (this[key] as? JsonPrimitive)?.takeIf { primitive -> primitive.isString }?.contentOrNull

internal fun JsonObject.number(key: String): Int? = (this[key] as? JsonPrimitive)?.let { primitive -> if (primitive.isString) null else primitive.int }

internal fun JsonObject.flag(key: String): Boolean? = (this[key] as? JsonPrimitive)?.contentOrNull?.toBooleanStrictOrNull()

internal fun JsonObject.objects(key: String): List<JsonObject> = this[key]!!.jsonArray.map { element -> element.jsonObject }
