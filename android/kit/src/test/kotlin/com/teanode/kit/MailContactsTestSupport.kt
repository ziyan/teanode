package com.teanode.kit

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.jsonObject

/** A connection signed in to an invented server whose answers the test gives. */
fun connectionFor(server: FakeServer): Connection =
    Connection(Account(ServerAddress.parse("assistant.example.com"), "test-client", "tnt_test", "", Long.MAX_VALUE / 4, "test-owner"), server)

/** The variables a request sent. */
fun sentVariables(request: HTTPRequest): Map<String, JsonElement> =
    TeaNodeJson.parseToJsonElement(request.body!!.toString(Charsets.UTF_8)).jsonObject["variables"]!!.jsonObject
