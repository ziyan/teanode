package com.teanode.app.settings

import androidx.compose.foundation.layout.Column
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

/** The calls made through one share, with Approve and Deny for the ones waiting when the person owns the skill. */
@Composable
private fun SkillShareCallsDialog(share: SkillShare, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad(share.id) { connection.skillShareCalls(share.id) }
    val approvedText = stringResource(R.string.settings_skills_approved)
    val deniedText = stringResource(R.string.settings_skills_denied)
    var answerNote by remember { mutableStateOf("") }
    FormDialog(title = share.skillName, confirmLabel = stringResource(R.string.common_done), isConfirmEnabled = true, onConfirm = onDismiss, onDismiss = onDismiss) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            val calls = loaded.value
            if (calls == null) Text(stringResource(R.string.common_loading), style = MaterialTheme.typography.bodyMedium)
            else if (calls.isEmpty()) Text(stringResource(R.string.settings_skills_no_calls), style = MaterialTheme.typography.bodyMedium)
            if (share.isOwnedByCaller && calls?.any { call -> call.isWaiting } == true) {
                FormField(answerNote, { answerNote = it }, stringResource(R.string.settings_skills_answer_note), isSingleLine = false, minLines = 2)
            }
            calls?.forEach { call ->
                val lines = listOfNotNull(call.toolName, call.amountText.ifEmpty { null }, call.requestReason.ifEmpty { null }, call.callState.replace('_', ' '), call.answerNote.ifEmpty { null })
                TeaNodeRow(headline = call.shownGrantee, supporting = lines.joinToString("\n"), supportingMaxLines = 5)
                if (call.isWaiting && share.isOwnedByCaller) {
                    androidx.compose.foundation.layout.Row {
                        TextButton(onClick = { actions.run(approvedText, onSuccess = { loaded.refresh() }) { connection.answerSkillShareCall(call.id, true, answerNote) } }) { Text(stringResource(R.string.settings_skills_approve)) }
                        TextButton(onClick = { actions.run(deniedText, onSuccess = { loaded.refresh() }) { connection.answerSkillShareCall(call.id, false, answerNote) } }) { Text(stringResource(R.string.settings_skills_deny)) }
                    }
                }
            }
        }
    }
}

/** Lend one of the person's skills: which, which tools, to whom, and the limits. */
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
    var currency by remember { mutableStateOf("usd") }
    var isApprovalRequired by remember { mutableStateOf(false) }
    val lentText = stringResource(R.string.settings_skills_lent)
    val paymentMinor = SkillShareMoney.parse(paymentLimit, currency)
    val monthlyMinor = SkillShareMoney.parse(monthlyLimit, currency)
    val isLimitValid = (paymentLimit.isEmpty() || paymentMinor != null) && (monthlyLimit.isEmpty() || monthlyMinor != null)
    FormDialog(
        title = stringResource(R.string.settings_skills_lend), confirmLabel = stringResource(R.string.settings_skills_lend_confirm),
        isConfirmEnabled = skill != null && toolNames.isNotEmpty() && granteeId != null && isLimitValid && !(isApprovalRequired && false), isWorking = actions.isWorking,
        onConfirm = {
            val chosen = skill ?: return@FormDialog
            val hasMoney = paymentMinor != null || monthlyMinor != null
            actions.run(lentText, onSuccess = onLent) {
                connection.shareSkill(SkillShareTerms(chosen.name, chosen.tools.map { tool -> tool.name }.filter { name -> toolNames.contains(name) }, granteeId!!, paymentMinor, monthlyMinor, if (hasMoney) currency.lowercase() else null, isApprovalRequired))
            }
        },
        onDismiss = onDismiss,
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            val availableSkills = skills.value.orEmpty()
            ChoiceRow(
                stringResource(R.string.settings_skills_skill), listOf<LendableSkill?>(null) + availableSkills, skill,
                label = { choice -> choice?.name ?: stringResource(R.string.settings_skills_choose) }, onSelect = { skill = it; toolNames = it?.tools?.map { tool -> tool.name }?.toSet() ?: emptySet() },
            )
            skill?.tools?.forEach { tool ->
                SwitchRow(tool.name, toolNames.contains(tool.name), { isOn -> toolNames = if (isOn) toolNames + tool.name else toolNames - tool.name }, supporting = tool.description.ifEmpty { null })
            }
            val members = people.value.orEmpty()
            ChoiceRow(
                stringResource(R.string.settings_skills_lend_to), listOf<String?>(null) + members.map { member -> member.userId }, granteeId,
                label = { id -> members.firstOrNull { member -> member.userId == id }?.displayName ?: stringResource(R.string.settings_skills_choose) }, onSelect = { granteeId = it },
            )
            FormField(currency, { currency = it.take(3) }, stringResource(R.string.settings_skills_currency))
            FormField(paymentLimit, { paymentLimit = it }, stringResource(R.string.settings_skills_payment_limit_field), placeholder = SkillShareMoney.typedExample(currency), keyboardType = KeyboardType.Decimal, isError = paymentLimit.isNotEmpty() && paymentMinor == null)
            FormField(monthlyLimit, { monthlyLimit = it }, stringResource(R.string.settings_skills_monthly_limit_field), placeholder = SkillShareMoney.typedExample(currency), keyboardType = KeyboardType.Decimal, isError = monthlyLimit.isNotEmpty() && monthlyMinor == null)
            SwitchRow(stringResource(R.string.settings_skills_approval_required), isApprovalRequired, { isApprovalRequired = it })
        }
    }
}
