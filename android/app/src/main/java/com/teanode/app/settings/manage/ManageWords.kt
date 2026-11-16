package com.teanode.app.settings.manage

import com.teanode.app.R

/** The kinds a run can be, for a filter over runs: a page holds one page of runs, so the kinds on it are not the kinds there are. */
val RUN_JOB_KINDS: List<String> = listOf(
    "triage", "reply", "research", "summarize", "remember", "ingest", "recording", "dream", "schedule", "extract", "draft", "describe", "compact", "evaluate", "survey", "subagent", "mcp", "goal", "background", "speak_first", "alert", "categorize", "statement_import", "read_receipt", "describe_photo", "statement_photo", "tax_document", "tax_mail", "watch", "follow_up_sweep", "nearby", "journal", "match_shopping", "shopping_picture", "shopping_import",
)

/** What a kind of run is called, or null for a kind a newer server added. */
fun runKindNameResource(jobKind: String): Int? = when (jobKind) {
    "triage" -> R.string.manage_runkind_triage
    "reply" -> R.string.manage_runkind_reply
    "research" -> R.string.manage_runkind_research
    "summarize" -> R.string.manage_runkind_summarize
    "remember" -> R.string.manage_runkind_remember
    "ingest" -> R.string.manage_runkind_ingest
    "recording" -> R.string.manage_runkind_recording
    "dream" -> R.string.manage_runkind_dream
    "schedule" -> R.string.manage_runkind_schedule
    "extract" -> R.string.manage_runkind_extract
    "draft" -> R.string.manage_runkind_draft
    "describe" -> R.string.manage_runkind_describe
    "react" -> R.string.manage_runkind_react
    "compact" -> R.string.manage_runkind_compact
    "evaluate" -> R.string.manage_runkind_evaluate
    "survey" -> R.string.manage_runkind_survey
    "subagent" -> R.string.manage_runkind_subagent
    "mcp" -> R.string.manage_runkind_mcp
    "goal" -> R.string.manage_runkind_goal
    "background" -> R.string.manage_runkind_background
    "speak_first" -> R.string.manage_runkind_speak_first
    "alert" -> R.string.manage_runkind_alert
    "categorize" -> R.string.manage_runkind_categorize
    "statement_import" -> R.string.manage_runkind_statement_import
    "read_receipt" -> R.string.manage_runkind_read_receipt
    "describe_photo" -> R.string.manage_runkind_describe_photo
    "statement_photo" -> R.string.manage_runkind_statement_photo
    "tax_document" -> R.string.manage_runkind_tax_document
    "tax_mail" -> R.string.manage_runkind_tax_mail
    "watch" -> R.string.manage_runkind_watch
    "follow_up_sweep" -> R.string.manage_runkind_follow_up_sweep
    "nearby" -> R.string.manage_runkind_nearby
    "match_shopping" -> R.string.manage_runkind_match_shopping
    "shopping_picture" -> R.string.manage_runkind_shopping_picture
    "shopping_import" -> R.string.manage_runkind_shopping_import
    "journal" -> R.string.manage_runkind_journal
    else -> null
}
