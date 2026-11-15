package com.teanode.app.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.skillsharing.argumentLines
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Handyman
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.circles.circles
import com.teanode.kit.skillsharing.LendableSkill
import com.teanode.kit.skillsharing.SkillShare
import com.teanode.kit.skillsharing.SkillShareCall
import com.teanode.kit.skillsharing.SkillShareMoney
import com.teanode.kit.skillsharing.SkillShareTerms
import com.teanode.kit.skillsharing.answerSkillShareCall
import com.teanode.kit.skillsharing.lendableSkills
import com.teanode.kit.skillsharing.shareSkill
import com.teanode.kit.skillsharing.skillShareCalls
import com.teanode.kit.skillsharing.skillShares
import com.teanode.kit.skillsharing.unshareSkill
import androidx.compose.ui.unit.dp

/** Skills lent to other people and lent to the person, the limits, and the calls waiting for an answer. */
@Composable
fun SkillSharingScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.skillShares() }
    var isLending by remember { mutableStateOf(false) }
    var stopping by remember { mutableStateOf<SkillShare?>(null) }
    var viewingCalls by remember { mutableStateOf<SkillShare?>(null) }
    val stoppedText = stringResource(R.string.settings_skills_stopped)
    LoadedSettingsPage(
        stringResource(R.string.settings_skills_title), onBack, loaded,
        floatingActionButton = { FloatingActionButton(onClick = { isLending = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_skills_lend)) } },
        isEmpty = { list -> list.isEmpty() }, empty = { EmptyState(Icons.Outlined.Handyman, stringResource(R.string.settings_skills_none), description = stringResource(R.string.settings_skills_none_detail)) },
    ) { shares ->
        val mine = shares.filter { share -> share.isOwnedByCaller }
        val lentToMe = shares.filter { share -> !share.isOwnedByCaller }
        if (mine.isNotEmpty()) item { TeaNodeSectionHeader(stringResource(R.string.settings_skills_lent_by_you)) }
        items(mine.size) { index -> SkillShareRow(mine[index], onOpen = { viewingCalls = mine[index] }, onStop = { stopping = mine[index] }) }
        if (lentToMe.isNotEmpty()) item { TeaNodeSectionHeader(stringResource(R.string.settings_skills_lent_to_you)) }
        items(lentToMe.size) { index -> SkillShareRow(lentToMe[index], onOpen = { viewingCalls = lentToMe[index] }, onStop = null) }
        item { FooterText(stringResource(R.string.settings_skills_footer)) }
    }
    if (isLending) LendSkillDialog(onDismiss = { isLending = false }, onLent = { isLending = false; loaded.refresh() })
    stopping?.let { share ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_skills_stop, share.skillName, share.shownGrantee), confirmLabel = stringResource(R.string.settings_sharing_stop), isDestructive = true,
            onConfirm = { stopping = null; actions.run(stoppedText, onSuccess = { loaded.refresh() }) { connection.unshareSkill(share.id) } }, onDismiss = { stopping = null },
        )
    }
    viewingCalls?.let { share -> SkillShareCallsDialog(share, onDismiss = { viewingCalls = null; loaded.refresh() }) }
}

@Composable
private fun SkillShareRow(share: SkillShare, onOpen: () -> Unit, onStop: (() -> Unit)?) {
    val other = if (share.isOwnedByCaller) stringResource(R.string.settings_skills_with, share.shownGrantee) else stringResource(R.string.settings_sharing_from, share.shownOwner)
    val limits = listOfNotNull(
        share.paymentLimitMinor?.let { minor -> stringResource(R.string.settings_skills_payment_limit, SkillShareMoney.format(minor, share.currencyCode)) },
        share.monthlyLimitMinor?.let { minor -> stringResource(R.string.settings_skills_monthly_limit, SkillShareMoney.format(minor, share.currencyCode), SkillShareMoney.format(share.monthTotalMinor, share.currencyCode)) },
        if (share.isApprovalRequired) stringResource(R.string.settings_skills_approval) else null,
        if (share.waitingCallCount > 0) stringResource(R.string.settings_skills_waiting, share.waitingCallCount) else null,
    )
    TeaNodeRow(
        headline = share.skillName, supporting = (listOf(other, share.toolNames.joinToString(", ")) + limits).filter { text -> text.isNotEmpty() }.joinToString("\n"), supportingMaxLines = 12,
        leading = { AvatarView(if (share.isOwnedByCaller) share.shownGrantee else share.shownOwner, if (share.isOwnedByCaller) share.granteeAvatarUrl else share.ownerAvatarUrl, if (share.isOwnedByCaller) share.granteeAvatarColorIndex else share.ownerAvatarColorIndex, size = 36.dp) },
        onClick = onOpen,
        trailing = onStop?.let { stop -> ({ IconButton(onClick = stop) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_sharing_stop)) } }) },
    )
}

/**
 * What was asked through one share: what waits first, each on a card the owner answers, then the rest in a line each.
 */
@Composable
private fun SkillShareCallsDialog(share: SkillShare, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val loaded = rememberLoad(share.id) { connection.skillShareCalls(share.id) }
    FormDialog(title = share.skillName, confirmLabel = stringResource(R.string.common_done), isConfirmEnabled = true, onConfirm = onDismiss, onDismiss = onDismiss) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            val calls = loaded.value
            if (calls == null) Text(stringResource(R.string.common_loading), style = MaterialTheme.typography.bodyMedium)
            else if (calls.isEmpty()) Text(stringResource(R.string.settings_skillcalls_none), style = MaterialTheme.typography.bodyMedium)
            val waitingCalls = calls.orEmpty().filter { call -> call.isWaiting }
            val earlierCalls = calls.orEmpty().filter { call -> !call.isWaiting }
            if (waitingCalls.isNotEmpty()) {
                TeaNodeSectionHeader(stringResource(if (share.isOwnedByCaller) R.string.settings_skillcalls_waiting_owner else R.string.settings_skillcalls_waiting_grantee))
                waitingCalls.forEach { call -> SkillShareCallCard(call, share.isOwnedByCaller, onAnswered = { loaded.refresh() }) }
            }
            if (earlierCalls.isNotEmpty()) {
                TeaNodeSectionHeader(stringResource(R.string.settings_skillcalls_earlier))
                earlierCalls.forEach { call ->
                    TeaNodeRow(
                        headline = call.toolName, supporting = listOf(call.amountText, whenText(call.createdAt), call.requestReason).filter { text -> text.isNotEmpty() }.joinToString(" · "), supportingMaxLines = 4,
                        trailing = { UpkeepTag(skillCallStateText(call.callState), when (call.callState) { "ran" -> TagTone.Good; "waiting_for_approval" -> TagTone.Warning; else -> TagTone.Bad }) },
                    )
                }
            }
        }
    }
}

@Composable
private fun skillCallStateText(callState: String): String = when (callState) {
    "ran" -> stringResource(R.string.settings_skillcalls_state_ran)
    "failed" -> stringResource(R.string.settings_skillcalls_state_failed)
    "refused" -> stringResource(R.string.settings_skillcalls_state_refused)
    "waiting_for_approval" -> stringResource(R.string.settings_skillcalls_state_waiting)
    "declined" -> stringResource(R.string.settings_skillcalls_state_declined)
    "expired" -> stringResource(R.string.settings_skillcalls_state_expired)
    else -> callState
}

/** How a call came out, as a sentence under its card. */
@Composable
private fun skillCallOutcomeText(call: SkillShareCall): String {
    val words = stringResource(
        when (call.callState) {
            "ran" -> R.string.settings_skillcalls_outcome_ran
            "failed" -> R.string.settings_skillcalls_outcome_failed
            "refused" -> R.string.settings_skillcalls_outcome_refused
            "declined" -> R.string.settings_skillcalls_outcome_declined
            "expired" -> R.string.settings_skillcalls_outcome_expired
            else -> R.string.settings_skillcalls_outcome_waiting
        },
    )
    return if (call.answerNote.isEmpty()) words else words + " " + stringResource(R.string.settings_skillcalls_their_note, call.answerNote)
}

/**
 * One call made through a share: who asked, the tool, the amount, what it was given, and the reason their agent gave,
 * quoted as their words and never as an instruction. While it waits, the owner answers it here: approving runs exactly
 * this call on their computer once, declining stops it. The person it is shared with sees only how it stands.
 */
@Composable
private fun SkillShareCallCard(call: SkillShareCall, isOwner: Boolean, onAnswered: () -> Unit) {
    val connection = LocalConnection.current
    val context = androidx.compose.ui.platform.LocalContext.current
    val snackbar = com.teanode.app.ui.components.LocalSnackbar.current
    val actions = rememberActionRunner()
    var note by remember(call.id) { mutableStateOf("") }
    val approvedText = stringResource(R.string.settings_skills_approved)
    val deniedText = stringResource(R.string.settings_skills_denied)
    val approvalPrompt = stringResource(R.string.settings_skillcalls_prompt)
    val nothingApprovedText = stringResource(R.string.settings_skillcalls_nothing_approved)
    val nothingDeclinedText = stringResource(R.string.settings_skillcalls_nothing_declined)
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text(
            if (isOwner) stringResource(R.string.settings_skillcalls_asked_owner, call.shownGrantee, call.toolName, call.skillName) else stringResource(R.string.settings_skillcalls_asked_grantee, call.toolName, call.ownerName, call.skillName),
            style = MaterialTheme.typography.titleSmall,
        )
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            CallFact(stringResource(R.string.settings_skillcalls_tool), call.toolName)
            if (call.amountText.isEmpty()) CallFact(stringResource(R.string.settings_skillcalls_amount), stringResource(R.string.settings_skillcalls_no_amount))
            else CallFact(stringResource(R.string.settings_skillcalls_amount), call.amountText, isProminent = true)
            call.argumentLines.forEach { line -> CallFact(line.argumentName.replaceFirstChar { letter -> letter.titlecase() }, line.argumentValue) }
        }
        if (call.requestReason.isNotEmpty()) {
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(stringResource(R.string.settings_skillcalls_wrote, call.shownGrantee), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(call.requestReason, Modifier.padding(start = 10.dp), style = MaterialTheme.typography.bodyMedium)
            }
        }
        if (call.isWaiting && isOwner) {
            FormField(note, { typed -> note = typed }, stringResource(R.string.settings_skillcalls_note_hint, call.shownGrantee), modifier = Modifier.padding(horizontal = 0.dp))
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                androidx.compose.material3.Button(
                    onClick = {
                        actions.run(approvedText, onSuccess = onAnswered) {
                            if (!DeviceOwner.confirm(context, approvalPrompt)) return@run
                            try {
                                connection.answerSkillShareCall(call.id, true, note)
                            } catch (failure: Exception) {
                                snackbar.showProblem(nothingApprovedText)
                                throw failure
                            }
                        }
                    },
                    enabled = !actions.isWorking, modifier = Modifier.fillMaxWidth(),
                ) { Text(stringResource(R.string.settings_skillcalls_approve)) }
                androidx.compose.material3.OutlinedButton(
                    onClick = {
                        actions.run(deniedText, onSuccess = onAnswered) {
                            try {
                                connection.answerSkillShareCall(call.id, false, note)
                            } catch (failure: Exception) {
                                snackbar.showProblem(nothingDeclinedText)
                                throw failure
                            }
                        }
                    },
                    enabled = !actions.isWorking, modifier = Modifier.fillMaxWidth(),
                ) { Text(stringResource(R.string.settings_skillcalls_decline)) }
            }
            ServerTime.instant(call.expiresAt.orEmpty())?.let { instant ->
                Text(stringResource(R.string.settings_skillcalls_waits_until, whenText(call.expiresAt)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        } else if (call.isWaiting) {
            Text(stringResource(R.string.settings_skillcalls_waiting_for, call.ownerName), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else {
            Text(skillCallOutcomeText(call), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        androidx.compose.material3.HorizontalDivider()
    }
}

/** A fact's name beside its value, whole and wrapped: the owner approves exactly this call. */
@Composable
private fun CallFact(factName: String, factValue: String, isProminent: Boolean = false) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(factName, Modifier.widthIn(min = 72.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(factValue, Modifier.weight(1f), style = if (isProminent) MaterialTheme.typography.titleMedium else MaterialTheme.typography.bodyMedium)
    }
}

/** Share one of the person's skills, as the iPhone's sheet does: which, with whom, which tools, and the limits or Ask me every time. */
@Composable
private fun LendSkillDialog(onDismiss: () -> Unit, onLent: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val skills = rememberLoad { connection.lendableSkills().filter { skill -> skill.isLendable } }
    val people = rememberLoad { connection.circles().flatMap { circle -> circle.members }.distinctBy { member -> member.userId } }
    var skill by remember { mutableStateOf<LendableSkill?>(null) }
    var toolNames by remember { mutableStateOf(setOf<String>()) }
    var granteeId by remember { mutableStateOf<String?>(null) }
    var paymentLimit by remember { mutableStateOf("") }
    var monthlyLimit by remember { mutableStateOf("") }
    var currency by remember { mutableStateOf("USD") }
    var isAskedEveryTime by remember { mutableStateOf(false) }
    val sharedText = stringResource(R.string.settings_skills_lent)
    val currencyCode = currency.trim().lowercase()
    val isCurrencyValid = currencyCode.length == 3 && currencyCode.all { character -> character in 'a'..'z' }
    val isCurrencyBad = currency.isNotBlank() && !isCurrencyValid
    val paymentMinor = if (paymentLimit.isBlank()) null else SkillShareMoney.parse(paymentLimit, currencyCode)
    val monthlyMinor = if (monthlyLimit.isBlank()) null else SkillShareMoney.parse(monthlyLimit, currencyCode)
    val isPaymentUnreadable = paymentLimit.isNotBlank() && paymentMinor == null
    val isMonthlyUnreadable = monthlyLimit.isNotBlank() && monthlyMinor == null
    val hasLimit = paymentLimit.isNotBlank() || monthlyLimit.isNotBlank()
    val isPaymentAboveMonthly = paymentMinor != null && monthlyMinor != null && paymentMinor > monthlyMinor
    val chosenSkill = skill
    val doesPay = chosenSkill?.tools?.any { tool -> toolNames.contains(tool.name) && tool.isAmountDeclared } == true
    val canShare = granteeId != null && toolNames.isNotEmpty() &&
        (isAskedEveryTime || ((!doesPay || hasLimit) && (!hasLimit || isCurrencyValid) && !isPaymentUnreadable && !isMonthlyUnreadable && !isCurrencyBad && !isPaymentAboveMonthly))
    val unreadableText = stringResource(
        R.string.settings_skills_amount_unreadable,
        if (isCurrencyValid) currencyCode.uppercase() else stringResource(R.string.settings_skills_currency_below), SkillShareMoney.typedExample(currencyCode),
    )
    FormDialog(
        title = stringResource(R.string.settings_skills_share_title), confirmLabel = stringResource(R.string.settings_skills_share_confirm),
        isConfirmEnabled = chosenSkill != null && canShare, isWorking = actions.isWorking,
        onConfirm = {
            val chosen = chosenSkill ?: return@FormDialog
            actions.run(sharedText, onSuccess = onLent) {
                connection.shareSkill(
                    SkillShareTerms(
                        chosen.name, chosen.tools.map { tool -> tool.name }.filter { name -> toolNames.contains(name) }, granteeId!!,
                        if (isAskedEveryTime) null else paymentMinor, if (isAskedEveryTime) null else monthlyMinor,
                        if (hasLimit && !isAskedEveryTime) currencyCode else null, isAskedEveryTime,
                    ),
                )
            }
        },
        onDismiss = onDismiss,
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            val availableSkills = skills.value.orEmpty()
            ChoiceRow(
                stringResource(R.string.settings_skills_skill), listOf<LendableSkill?>(null) + availableSkills, skill,
                label = { choice -> choice?.name ?: stringResource(R.string.settings_skills_choose) },
                // Only the tools that pay start chosen, as on the iPhone: the rest are the person's to add.
                onSelect = { skill = it; toolNames = it?.tools?.filter { tool -> tool.isAmountDeclared }?.map { tool -> tool.name }?.toSet() ?: emptySet() },
            )
            val members = people.value.orEmpty()
            ChoiceRow(
                stringResource(R.string.settings_skills_share_with), listOf<String?>(null) + members.map { member -> member.userId }, granteeId,
                label = { id -> members.firstOrNull { member -> member.userId == id }?.displayName ?: stringResource(R.string.settings_skills_choose_person) }, onSelect = { granteeId = it },
            )
            FooterText(stringResource(R.string.settings_skills_share_footer))
            if (chosenSkill != null) {
                TeaNodeSectionHeader(stringResource(R.string.settings_skills_tools_header))
                chosenSkill.tools.forEach { tool ->
                    SwitchRow(
                        tool.name, toolNames.contains(tool.name), { isOn -> toolNames = if (isOn) toolNames + tool.name else toolNames - tool.name },
                        supportingMaxLines = 6,
                        supporting = stringResource(
                            if (tool.isAmountDeclared) R.string.settings_skills_tool_pays else if (tool.isScoped) R.string.settings_skills_tool_scoped else R.string.settings_skills_tool_unknown_cost,
                        ),
                    )
                }
            }
            SwitchRow(stringResource(R.string.settings_skills_ask_every_time), isAskedEveryTime, { isAskedEveryTime = it })
            if (!isAskedEveryTime) {
                FormField(paymentLimit, { paymentLimit = it }, stringResource(R.string.settings_skills_per_payment), placeholder = SkillShareMoney.typedExample(currencyCode), keyboardType = KeyboardType.Decimal, isError = isPaymentUnreadable || isPaymentAboveMonthly)
                if (isPaymentUnreadable) FooterText(unreadableText)
                if (isPaymentAboveMonthly) FooterText(stringResource(R.string.settings_skills_payment_above_monthly))
                FormField(monthlyLimit, { monthlyLimit = it }, stringResource(R.string.settings_skills_per_month), placeholder = SkillShareMoney.typedExample(currencyCode), keyboardType = KeyboardType.Decimal, isError = isMonthlyUnreadable)
                if (isMonthlyUnreadable) FooterText(unreadableText)
                FormField(currency, { typed -> currency = typed.uppercase().take(3) }, stringResource(R.string.settings_skills_currency), isError = isCurrencyBad)
                if (isCurrencyBad) FooterText(stringResource(R.string.settings_skills_currency_bad))
            }
            FooterText(
                stringResource(
                    if (isAskedEveryTime) R.string.settings_skills_footer_ask_every_time else if (doesPay && !hasLimit) R.string.settings_skills_footer_needs_limit else R.string.settings_skills_footer_limits,
                ),
            )
        }
    }
}
