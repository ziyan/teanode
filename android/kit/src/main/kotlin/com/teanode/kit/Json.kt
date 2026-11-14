package com.teanode.kit

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

/**
 * TeaNodeJson is how the app reads what the server says: a field the app does not know is ignored, as a newer server
 * may send more than this app reads.
 */
val TeaNodeJson: Json = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    encodeDefaults = true
}

/** The value under key, when this is an object that has it. */
operator fun JsonElement.get(key: String): JsonElement? = (this as? JsonObject)?.get(key)

/** The string this holds, when it is one. */
val JsonElement.stringValue: String?
    get() = (this as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull

/** The same JSON with every object's keys in alphabetical order, so what is sent is stable. */
fun JsonElement.sortedByKeys(): JsonElement = when (this) {
    is JsonObject -> JsonObject(entries.sortedBy { entry -> entry.key }.associate { entry -> entry.key to entry.value.sortedByKeys() })
    is JsonArray -> JsonArray(map { element -> element.sortedByKeys() })
    else -> this
}
