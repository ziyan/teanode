package com.teanode.kit.skillsharing

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import com.teanode.kit.TeaNodeJson

/** One argument of a call, on a line of its own: its name as words, and its value whole. */
data class SkillShareArgumentLine(val argumentName: String, val argumentValue: String)

/**
 * The arguments one to a line, less the amount, the currency and the reason, which are shown apart. A value is kept
 * whole: the owner approves exactly this call, and a cut value could hide the part that matters.
 */
val SkillShareCall.argumentLines: List<SkillShareArgumentLine>
    get() {
        val arguments = runCatching { TeaNodeJson.parseToJsonElement(toolArguments) as? JsonObject }.getOrNull() ?: return emptyList()
        val apart = mutableSetOf("amount_minor", "amountMinor", "currency", "request_reason")
        // The amount is on the card in the currency's own digits, so the argument the skill declares it is read from is not
        // said again. Any other argument, even one with "amount" in its name and the same number, is part of what the owner
        // is approving and stays; and so does that one when no amount could be read, since the card shows none.
        if (amountMinor != null && !amountArgumentName.isNullOrEmpty()) apart += amountArgumentName.orEmpty()
        val lines = mutableListOf<SkillShareArgumentLine>()
        arguments.keys.sorted().filter { name -> name !in apart }.forEach { name ->
            val text = when (val argument: JsonElement? = arguments[name]) {
                null, is JsonNull -> return@forEach
                is JsonPrimitive -> if (argument.isString) argument.content else argument.content.removeSuffix(".0")
                is JsonObject, is JsonArray -> argument.toString()
            }
            if (text.isNotEmpty()) lines += SkillShareArgumentLine(name.replace('_', ' '), text)
        }
        return lines
    }
