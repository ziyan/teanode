package com.teanode.kit.health

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.sortedByKeys
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.doubleOrNull

// What a sample sends of its metadata. The app that wrote a sample decides what is in its metadata, and some write a
// lot; the server refuses a sample whose metadata text is over a limit, and a refused sample is dropped for good with
// the anchor moved past it. So the phone keeps the metadata within the limit by dropping keys, never the sample.

/** HealthJson turns the upload shapes into the JSON text the calls send, and back. */
object HealthJson {
    /** An object written as JSON text with its keys in order, which the server takes as a string (sampleMetadataJson, categorySecondsJson). */
    fun text(members: Map<String, JsonElement>): String = JsonObject(members).sortedByKeys().toString()

    /** The object in JSON text; empty when the text is empty or not an object. */
    fun objectOf(text: String?): Map<String, JsonElement> {
        if (text.isNullOrEmpty()) return emptyMap()
        return (runCatching { TeaNodeJson.parseToJsonElement(text) }.getOrNull() as? JsonObject) ?: emptyMap()
    }

    /** The seconds of a view, which the server sends as a JSON object (and which JSON text of one is read as too). */
    fun secondsOf(element: JsonElement?): Map<String, Double> {
        val members = when (element) {
            is JsonObject -> element
            is JsonPrimitive -> if (element.isString) (runCatching { TeaNodeJson.parseToJsonElement(element.content) }.getOrNull() as? JsonObject) else null
            else -> null
        } ?: return emptyMap()
        return members.mapNotNull { (key, value) -> (value as? JsonPrimitive)?.doubleOrNull?.let { number -> key to number } }.toMap()
    }

    fun number(element: JsonElement?): Double? = (element as? JsonPrimitive)?.takeIf { primitive -> !primitive.isString }?.doubleOrNull
}

/** HealthMetadataFit keeps the metadata of a sample within what the server takes. */
object HealthMetadataFit {
    /** The most bytes of metadata JSON text the server takes for one sample. */
    const val BYTE_COUNT_MOST = 8 * 1024

    /**
     * The keys the app writes itself from what it knows of a sample: the totals of a workout, the classification of an
     * electrocardiogram, the points of an audiogram, what a State of Mind entry says. The charts and the day summaries
     * read these, so they are dropped last, after every key the app that wrote the sample put there.
     */
    val ownKeys: Set<String> = setOf(
        HealthWorkoutMetadata.TOTAL_ENERGY_BURNED_KILOCALORIES, HealthWorkoutMetadata.TOTAL_DISTANCE_METERS, "workoutActivityTypeRawValue",
        "durationSeconds", "electrocardiogramClassification", "averageHeartRateBeatsPerMinute", "audiogramPoints", "stateOfMindKind",
        "stateOfMindValence", "stateOfMindLabels",
    )

    /**
     * The metadata within the most bytes. Over it, keys go one at a time, the one whose text is the longest first, those
     * the writing app put there before the app's own, until it fits. A text with a null character in it loses the
     * character, which the server would refuse.
     */
    fun fitting(metadata: Map<String, JsonElement>, byteCountMost: Int = BYTE_COUNT_MOST): Map<String, JsonElement> {
        val kept = LinkedHashMap<String, JsonElement>()
        for ((key, value) in metadata) kept[withoutNull(key)] = withoutNull(value)
        while (HealthJson.text(kept).toByteArray().size > byteCountMost && kept.isNotEmpty()) {
            val candidates = kept.keys.filter { key -> !ownKeys.contains(key) }
            val droppable = candidates.ifEmpty { kept.keys.toList() }
            val longestKey = droppable.sorted().maxWithOrNull(compareBy { key -> byteCount(key, kept) }) ?: break
            kept.remove(longestKey)
        }
        return kept
    }

    private fun byteCount(key: String, metadata: Map<String, JsonElement>): Int =
        HealthJson.text(mapOf(key to (metadata[key] ?: JsonNull))).toByteArray().size

    /** Text with its null characters taken out: JSON writes one as \u0000, which the server refuses. */
    fun withoutNull(text: String): String = if (text.contains('\u0000')) text.replace("\u0000", "") else text

    private fun withoutNull(value: JsonElement): JsonElement = when (value) {
        is JsonPrimitive -> if (value.isString) JsonPrimitive(withoutNull(value.content)) else value
        is JsonArray -> JsonArray(value.map { element -> withoutNull(element) })
        is JsonObject -> JsonObject(value.entries.associate { (key, element) -> withoutNull(key) to withoutNull(element) })
        JsonNull -> value
    }
}
