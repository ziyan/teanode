package com.teanode.kit.settings

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive

/**
 * The variables of a GraphQL call, written as pairs: a text, number or boolean becomes its JSON, null becomes null, a
 * list becomes an array, and a JSON element stays as it is.
 */
fun variablesOf(vararg pairs: Pair<String, Any?>): Map<String, JsonElement> = pairs.associate { pair -> pair.first to jsonOf(pair.second) }

fun jsonOf(content: Any?): JsonElement = when (content) {
    null -> JsonNull
    is JsonElement -> content
    is String -> JsonPrimitive(content)
    is Boolean -> JsonPrimitive(content)
    is Number -> JsonPrimitive(content)
    is Enum<*> -> JsonPrimitive(content.name)
    is Iterable<*> -> JsonArray(content.map { element -> jsonOf(element) })
    else -> throw IllegalArgumentException("not a JSON value: ${content::class}")
}

/** The words a person typed, without the spaces around them. */
fun trimmedText(text: String): String = text.trim()
