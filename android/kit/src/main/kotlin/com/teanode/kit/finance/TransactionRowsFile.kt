package com.teanode.kit.finance

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.math.BigDecimal

/**
 * TransactionRowsFile reads the file the command line's import-transactions reads: one JSON object of the
 * ImportTransactions arguments, with transactionRows in it.
 */
object TransactionRowsFile {
    /** The amounts a file may write as numbers, which the API takes as text. */
    private val amountKeys = setOf("amount", "runningBalanceAmount", "totalAmount", "ledgerBalanceAmount")

    /**
     * The arguments in the file, or null when it is not an object holding transactionRows. Amounts written as numbers
     * become decimal text, and null fields are left out.
     */
    fun variables(data: ByteArray): Map<String, JsonElement>? {
        val decoded = try {
            TeaNodeJson.parseToJsonElement(data.toString(Charsets.UTF_8))
        } catch (_: Exception) {
            return null
        }
        if (decoded !is JsonObject || decoded["transactionRows"] !is JsonArray) return null
        return (normalized(decoded, "") as? JsonObject)?.toMap()
    }

    private fun normalized(element: JsonElement, key: String): JsonElement = when {
        element is JsonPrimitive && !element.isString && key in amountKeys && element !is JsonNull -> {
            val number = element.content.toDoubleOrNull()
            if (number == null) element else JsonPrimitive(decimalText(number))
        }
        element is JsonArray -> JsonArray(element.map { each -> normalized(each, key) })
        element is JsonObject -> JsonObject(
            element.filterValues { each -> each !is JsonNull }.mapValues { (fieldKey, each) -> normalized(each, fieldKey) },
        )
        else -> element
    }

    /** A number as the shortest decimal text: "12.5", "-3", not "1.25e1". */
    internal fun decimalText(number: Double): String {
        if (number == Math.rint(number) && Math.abs(number) < 1e15) return number.toLong().toString()
        var text = BigDecimal(String.format(java.util.Locale.ROOT, "%.8f", number)).toPlainString()
        while (text.endsWith("0")) text = text.dropLast(1)
        if (text.endsWith(".")) text = text.dropLast(1)
        return text
    }
}
