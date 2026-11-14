package com.teanode.app.calendar

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.kit.calendar.CalendarRepeat

/**
 * CalendarRepeatWords puts a repeat into the app's own localized sentences. The kit hands over the pieces with the
 * locale's day names, their list and the date already in them, so the order of the words is all that is left to each
 * language (ios CalendarRepeatViews.swift).
 */
object CalendarRepeatWords {
    /** The whole sentence: "Repeats every 2 weeks on Monday and Thursday, until December 31, 2026". */
    @Composable
    fun sentenceText(sentence: CalendarRepeat.Sentence): String {
        val cadence = cadenceText(sentence.cadence)
        return when (val ending = sentence.ending) {
            CalendarRepeat.Sentence.Ending.Never -> cadence
            is CalendarRepeat.Sentence.Ending.OnDate -> stringResource(R.string.calendar_repeat_until, cadence, ending.dateText)
            is CalendarRepeat.Sentence.Ending.AfterCount ->
                stringResource(R.string.calendar_repeat_times_sentence, cadence, stringResource(R.string.calendar_times, ending.occurrenceCount))
        }
    }

    @Composable
    private fun cadenceText(cadence: CalendarRepeat.Sentence.Cadence): String = when (cadence) {
        is CalendarRepeat.Sentence.Cadence.Daily ->
            if (cadence.intervalCount > 1) stringResource(R.string.calendar_repeats_every_days, cadence.intervalCount) else stringResource(R.string.calendar_repeats_every_day)
        is CalendarRepeat.Sentence.Cadence.Yearly ->
            if (cadence.intervalCount > 1) stringResource(R.string.calendar_repeats_every_years, cadence.intervalCount) else stringResource(R.string.calendar_repeats_every_year)
        is CalendarRepeat.Sentence.Cadence.Weekly ->
            if (cadence.intervalCount > 1) stringResource(R.string.calendar_repeats_every_weeks_on, cadence.intervalCount, cadence.weekdayNamesText)
            else stringResource(R.string.calendar_repeats_every_week_on, cadence.weekdayNamesText)
        is CalendarRepeat.Sentence.Cadence.MonthlyDay ->
            if (cadence.intervalCount > 1) stringResource(R.string.calendar_repeats_every_months_on_day, cadence.intervalCount, cadence.dayOfMonth)
            else stringResource(R.string.calendar_repeats_every_month_on_day, cadence.dayOfMonth)
        is CalendarRepeat.Sentence.Cadence.MonthlyWeekday -> {
            val ordinalText = ordinalName(cadence.ordinal)
            if (cadence.intervalCount > 1) stringResource(R.string.calendar_repeats_every_months_on_the, cadence.intervalCount, ordinalText, cadence.weekdayName)
            else stringResource(R.string.calendar_repeats_every_month_on_the, ordinalText, cadence.weekdayName)
        }
    }

    /** The name of a place in the month, 1 to 5 or -1 for the last, as it stands in "the second Tuesday". */
    @Composable
    fun ordinalName(ordinal: Int): String = stringResource(
        when (ordinal) {
            1 -> R.string.calendar_ordinal_first
            2 -> R.string.calendar_ordinal_second
            3 -> R.string.calendar_ordinal_third
            4 -> R.string.calendar_ordinal_fourth
            5 -> R.string.calendar_ordinal_fifth
            else -> R.string.calendar_ordinal_last
        },
    )

    /** How often, in a few words, for the Repeat row of a custom repeat: "Every 2 weeks". */
    @Composable
    fun shortText(parts: CalendarRepeat.Parts): String {
        val count = parts.intervalCount
        return when (parts.frequency) {
            CalendarRepeat.Frequency.Daily -> if (count > 1) stringResource(R.string.calendar_every_days, count) else stringResource(R.string.calendar_every_day)
            CalendarRepeat.Frequency.Weekly -> if (count > 1) stringResource(R.string.calendar_every_weeks, count) else stringResource(R.string.calendar_every_week)
            CalendarRepeat.Frequency.Monthly -> if (count > 1) stringResource(R.string.calendar_every_months, count) else stringResource(R.string.calendar_every_month)
            CalendarRepeat.Frequency.Yearly -> if (count > 1) stringResource(R.string.calendar_every_years, count) else stringResource(R.string.calendar_every_year)
        }
    }
}
