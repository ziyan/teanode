package com.teanode.kit.skillsharing

import com.teanode.kit.Connection
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable

// Skills shared with other people, and the calls made through them. Whether a call goes ahead, waits for its owner or is
// refused is decided on the server; nothing here runs a skill or moves money.

/** A skill one person lends another: which tools, the limits, and what waits for the owner's word. */
@Serializable
data class SkillShare(
    val id: String,
    val skillName: String,
    val toolNames: List<String> = emptyList(),
    val ownerUserId: String = "",
    val ownerUsername: String = "",
    val ownerName: String = "",
    val ownerAvatarUrl: String? = null,
    val ownerAvatarColorIndex: Int? = null,
    val granteeUserId: String = "",
    val granteeUsername: String = "",
    val granteeName: String = "",
    val granteeAvatarUrl: String? = null,
    val granteeAvatarColorIndex: Int? = null,
    val paymentLimitMinor: Long? = null,
    val monthlyLimitMinor: Long? = null,
    val currencyCode: String = "",
    val isApprovalRequired: Boolean = false,
    val isOwnedByCaller: Boolean = false,
    val waitingCallCount: Int = 0,
    val monthTotalMinor: Long = 0,
) {
    val shownOwner: String get() = ownerName.ifEmpty { ownerUsername }
    val shownGrantee: String get() = granteeName.ifEmpty { granteeUsername }
}

/** One call made through a share. */
@Serializable
data class SkillShareCall(
    val id: String,
    val skillShareId: String = "",
    val skillName: String = "",
    val granteeUserId: String = "",
    val granteeUsername: String = "",
    val granteeName: String = "",
    val ownerName: String = "",
    val toolName: String = "",
    val toolArguments: String = "",
    val requestReason: String = "",
    val amountMinor: Long? = null,
    val currencyCode: String = "",
    val amountArgumentName: String? = null,
    val callState: String = "",
    val toolOutput: String = "",
    val answeredAt: String? = null,
    val answerNote: String = "",
    val expiresAt: String? = null,
    val createdAt: String = "",
) {
    val isWaiting: Boolean get() = callState == "waiting_for_approval"
    val shownGrantee: String get() = granteeName.ifEmpty { granteeUsername }
    val amountText: String get() = amountMinor?.let { minor -> SkillShareMoney.format(minor, currencyCode) } ?: ""
}

/** A tool of a skill the owner could lend. */
@Serializable
data class LendableTool(val name: String, val description: String = "", val needsComputer: Boolean = false, val isAmountDeclared: Boolean = false, val isScoped: Boolean = false)

/** A skill the person has, and whether it can be lent. */
@Serializable
data class LendableSkill(val name: String, val description: String = "", val enabled: Boolean = false, val readable: Boolean = false, val tools: List<LendableTool> = emptyList()) {
    val isLendable: Boolean get() = enabled && readable && tools.isNotEmpty()
}

/** Money as the share's limits and calls write it: in the currency's own minor units. */
object SkillShareMoney {
    private val noMinorUnit = setOf("bif", "clp", "djf", "gnf", "jpy", "kmf", "krw", "mga", "pyg", "rwf", "ugx", "vnd", "vuv", "xaf", "xof", "xpf")
    private val thousandths = setOf("bhd", "iqd", "jod", "kwd", "lyd", "omr", "tnd")

    fun exponent(currencyCode: String): Int {
        val code = currencyCode.lowercase()
        return when {
            noMinorUnit.contains(code) -> 0
            thousandths.contains(code) -> 3
            else -> 2
        }
    }

    fun format(minor: Long, currencyCode: String): String {
        val digits = exponent(currencyCode)
        var divisor = 1L
        repeat(digits) { divisor *= 10 }
        val sign = if (minor < 0) "-" else ""
        val absolute = if (minor < 0) -minor else minor
        var number = (absolute / divisor).toString()
        if (digits > 0) number += "." + (absolute % divisor).toString().padStart(digits, '0')
        return sign + number + if (currencyCode.isEmpty()) "" else " " + currencyCode.uppercase()
    }

    fun typedExample(currencyCode: String): String {
        val digits = exponent(currencyCode)
        return if (digits == 0) "1000" else "1000." + "0".repeat(digits)
    }

    /** The minor units a typed amount stands for; null for anything that is not a positive amount with no more digits than the currency has. */
    fun parse(typed: String, currencyCode: String): Long? {
        val text = typed.trim()
        val digits = exponent(currencyCode)
        val parts = text.split('.', limit = 2)
        val whole = parts[0]
        if (whole.isEmpty() || whole.length > 15 || !whole.all { character -> character in '0'..'9' }) return null
        val fraction = if (parts.size > 1) parts[1] else ""
        if (parts.size > 1 && fraction.isEmpty() && digits > 0) return null
        if (fraction.length > digits || !fraction.all { character -> character in '0'..'9' }) return null
        val minor = (whole + fraction + "0".repeat(digits - fraction.length)).toLongOrNull() ?: return null
        return if (minor > 0) minor else null
    }
}

/** What a new share lends and on what terms. */
data class SkillShareTerms(
    val skillName: String,
    val toolNames: List<String>,
    val granteeUserId: String,
    val paymentLimitMinor: Long? = null,
    val monthlyLimitMinor: Long? = null,
    val currencyCode: String? = null,
    val isApprovalRequired: Boolean = false,
)

@Serializable private class ListSkillSharesAnswer(val ListSkillShares: List<SkillShare>)

@Serializable private class ListAgentSkillsAnswer(val ListAgentSkills: List<LendableSkill>)

@Serializable private class ListSkillShareCallsAnswer(val ListSkillShareCalls: List<SkillShareCall>)

@Serializable private class AnswerSkillShareCallAnswer(val AnswerSkillShareCall: SkillShareCall)

suspend fun Connection.skillShares(): List<SkillShare> = execute<ListSkillSharesAnswer>(SkillSharingDocuments.SKILL_SHARES).ListSkillShares

suspend fun Connection.lendableSkills(): List<LendableSkill> = execute<ListAgentSkillsAnswer>(SkillSharingDocuments.LENDABLE_SKILLS).ListAgentSkills

suspend fun Connection.shareSkill(terms: SkillShareTerms) {
    execute<kotlinx.serialization.json.JsonObject>(
        SkillSharingDocuments.SHARE_SKILL,
        variablesOf(
            "skillName" to terms.skillName, "toolNames" to terms.toolNames, "granteeUserId" to terms.granteeUserId, "paymentLimitMinor" to terms.paymentLimitMinor,
            "monthlyLimitMinor" to terms.monthlyLimitMinor, "currencyCode" to terms.currencyCode, "isApprovalRequired" to terms.isApprovalRequired,
        ),
    )
}

suspend fun Connection.unshareSkill(shareId: String) {
    execute<kotlinx.serialization.json.JsonObject>(SkillSharingDocuments.UNSHARE_SKILL, variablesOf("shareId" to shareId))
}

suspend fun Connection.skillShareCalls(shareId: String, callState: String? = null): List<SkillShareCall> =
    execute<ListSkillShareCallsAnswer>(SkillSharingDocuments.SKILL_SHARE_CALLS, variablesOf("shareId" to shareId, "callState" to callState)).ListSkillShareCalls

suspend fun Connection.answerSkillShareCall(callId: String, isApproved: Boolean, answerNote: String?): SkillShareCall =
    execute<AnswerSkillShareCallAnswer>(
        SkillSharingDocuments.ANSWER_SKILL_SHARE_CALL, variablesOf("callId" to callId, "isApproved" to isApproved, "answerNote" to answerNote?.trim()?.ifEmpty { null }),
    ).AnswerSkillShareCall
