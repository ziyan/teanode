package com.teanode.app.settings

import com.teanode.app.calendar.DateWords
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.photos.ServerTime
import java.text.NumberFormat
import java.time.ZoneId
import java.util.Currency
import java.util.Locale

/** How a stamp from the server reads: the day and the time, as the locale writes them; empty for none. */
internal fun whenText(stamp: String?): String =
    ServerTime.instant(stamp.orEmpty())?.let { instant -> DateWords.abbreviatedDateTime(instant, ZoneId.systemDefault()) }.orEmpty()

/** How long ago a stamp from the server was: "3 hours ago". */
@androidx.compose.runtime.Composable
internal fun relativeText(stamp: String): String = ServerTime.instant(stamp)?.let { instant -> relativeTimeText(instant.toEpochMilli()) }.orEmpty()

/** A number in short form: 12K. */
internal fun compactNumber(number: Int): String = when {
    number >= 1_000_000 -> "%.1fM".format(Locale.getDefault(), number / 1_000_000.0)
    number >= 10_000 -> "%dK".format(Locale.getDefault(), number / 1_000)
    number >= 1_000 -> "%.1fK".format(Locale.getDefault(), number / 1_000.0)
    else -> number.toString()
}

/** An amount of money in the currency given, dollars where none is. */
internal fun moneyText(amount: Double, currencyCode: String?): String {
    val format = NumberFormat.getCurrencyInstance(Locale.getDefault())
    format.currency = runCatching { Currency.getInstance(currencyCode.orEmpty().ifEmpty { "USD" }) }.getOrDefault(Currency.getInstance("USD"))
    if (amount < 1.0 && amount > 0.0) format.maximumFractionDigits = 4
    return format.format(amount)
}
