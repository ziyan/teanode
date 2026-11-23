package com.teanode.kit

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement

/**
 * Runs a document and reads one field of its answer as [Value]; null when the server answers null for it. The iPhone
 * app declares a small Answer struct for every call: this is the same thing said once.
 */
suspend inline fun <reified Value> Connection.graphQLField(
    document: String,
    fieldName: String,
    variables: Map<String, JsonElement> = emptyMap(),
    requestTimeoutMS: Long? = null,
): Value? {
    val data: JsonObject = execute(document, variables, requestTimeoutMS)
    val element = data[fieldName] ?: return null
    if (element is JsonNull) return null
    return TeaNodeJson.decodeFromJsonElement<Value>(element)
}

/** Runs a document whose answer is not read: a mutation that only has to succeed. */
suspend fun Connection.graphQLCall(document: String, variables: Map<String, JsonElement> = emptyMap()) {
    execute<JsonObject>(document, variables)
}
