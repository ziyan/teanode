package com.teanode.app.settings

import android.content.res.Resources
import com.teanode.app.R

/** How the agent's upkeep pages say the memory check in a person's words. */
object UpkeepWords {
    /** A question's state in the reader's words, or as the server names it when a newer server added one. */
    fun questionState(resources: Resources, questionState: String): String = when (questionState) {
        "asked" -> resources.getString(R.string.settings_memory_state_asked)
        "confirmed" -> resources.getString(R.string.settings_memory_state_confirmed)
        "corrected" -> resources.getString(R.string.settings_memory_state_corrected)
        "dropped" -> resources.getString(R.string.settings_memory_state_dropped)
        "unsure" -> resources.getString(R.string.settings_memory_state_unsure)
        else -> questionState
    }

    fun questionKind(resources: Resources, questionKind: String): String = when (questionKind) {
        "direct" -> resources.getString(R.string.settings_memory_kind_direct)
        "paraphrase" -> resources.getString(R.string.settings_memory_kind_paraphrase)
        "changed" -> resources.getString(R.string.settings_memory_kind_changed)
        "multihop" -> resources.getString(R.string.settings_memory_kind_multihop)
        "abstain" -> resources.getString(R.string.settings_memory_kind_abstain)
        else -> questionKind
    }

    fun verdict(resources: Resources, answerVerdict: String): String = when (answerVerdict) {
        "correct" -> resources.getString(R.string.settings_memory_verdict_correct)
        "partial" -> resources.getString(R.string.settings_memory_verdict_partial)
        "not_known" -> resources.getString(R.string.settings_memory_verdict_not_known)
        "missed" -> resources.getString(R.string.settings_memory_verdict_missed)
        "stale" -> resources.getString(R.string.settings_memory_verdict_stale)
        "invented" -> resources.getString(R.string.settings_memory_verdict_invented)
        "wrong" -> resources.getString(R.string.settings_memory_verdict_wrong)
        "ungraded" -> resources.getString(R.string.settings_memory_verdict_ungraded)
        else -> answerVerdict
    }
}
