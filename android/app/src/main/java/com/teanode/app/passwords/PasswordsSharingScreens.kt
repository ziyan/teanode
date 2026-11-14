package com.teanode.app.passwords

import android.content.Context
import android.net.Uri
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.PersonAdd
import androidx.compose.material.icons.outlined.PersonRemove
import androidx.compose.material.icons.outlined.VerifiedUser
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.finance.ShareTargetNames
import com.teanode.app.finance.ShareTargetPicker
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shopping.ShoppingSheet
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.finance.ShareCircle
import com.teanode.kit.finance.SharePerson
import com.teanode.kit.finance.ShareTarget
import com.teanode.kit.finance.shareCircles
import com.teanode.kit.finance.sharePeople
import com.teanode.kit.passwords.PasswordItem
import com.teanode.kit.passwords.PasswordRecipient
import com.teanode.kit.passwords.PasswordShareGranteeInput
import com.teanode.kit.passwords.PasswordShareView
import com.teanode.kit.passwords.PasswordsCircleShareNotice
import com.teanode.kit.passwords.PasswordsError
import com.teanode.kit.passwords.PasswordsFinishPlan
import com.teanode.kit.passwords.PasswordsKeyChange
import com.teanode.kit.passwords.PasswordsSharingCheck

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

// Sharing one item with a circle or a person in one. The owner's phone seals the item's key for each person who has set up
// Passwords, so the server never sees it. A person who has not set up Passwords is listed as waiting; this phone gives them
// their key the next time it is open after they have. Nobody is asked to confirm a key: the first key this phone sees for a
// person is the one it uses, and a key that changes is used too, with a notice that stays until it is dismissed. "Compare
// codes" is there for anybody who wants to check a key with the person, and marks it "Codes compared"; it gates nothing.

/** The words of sharing that are built from an item or a plan. */
internal object PasswordsSharingWords {
    private fun joined(names: List<String>): String = android.icu.text.ListFormatter.getInstance(java.util.Locale.getDefault()).format(names)

    /** A share's person, or "Everyone in <circle>". */
    fun granteeName(context: Context, share: PasswordShareView): String =
        if (share.granteeCircleId != null) context.getString(R.string.passwords_share_everyone_in, share.granteeName) else share.granteeName

    /**
     * Whom an item the person owns is shared with, for the Share row: "Sam and Alex", "Home", or "Home, 1 waiting" when
     * somebody the share reaches has not set up Passwords or has not been given the item's key yet. Only the shares the
     * person's own record lists count; an item whose record could not be checked is shared with nobody as far as the app can tell.
     */
    fun shareSummary(context: Context, item: PasswordItem): String = when (item.sharingCheck) {
        PasswordsSharingCheck.Unverified, PasswordsSharingCheck.Replayed -> context.getString(R.string.passwords_sharing_unchecked)
        PasswordsSharingCheck.Verified, PasswordsSharingCheck.OwnedByAnotherPerson -> shareSummary(context, item.recordedShares)
    }

    private fun shareSummary(context: Context, shares: List<PasswordShareView>): String {
        if (shares.isEmpty()) return context.getString(R.string.passwords_share_not_shared)
        val names = joined(shares.map { share -> share.granteeName })
        val waitingUserIds = shares.flatMap { share -> share.recipients }.filter { recipient -> recipient.publicKey == null || !recipient.hasKey }.map { recipient -> recipient.userId }.toSet()
        if (waitingUserIds.isEmpty()) return names
        return context.getString(R.string.passwords_share_waiting_summary, names, waitingUserIds.size)
    }

    /** The sentence that names the people the server lists for an item whose record does not list them, or null when there are none. */
    fun unrecordedSharesNotice(context: Context, item: PasswordItem): String? {
        val names = item.unrecordedShares.map { share -> granteeName(context, share) }
        if (names.isEmpty()) return null
        return context.getString(R.string.passwords_share_unrecorded, joined(names))
    }

    /** The title of the question asked before sharing is finished for one item or for several. */
    fun finishConfirmationTitle(context: Context, itemCount: Int, itemName: String): String =
        if (itemCount == 1) context.getString(R.string.passwords_finish_title_one, itemName) else context.resources.getQuantityString(R.plurals.passwords_finish_title_many, itemCount, itemCount)

    /** What finishing the sharing would do, one sentence for each thing: who gets a key, the new keys and the agent's copy. The person reads it before anything is sealed. */
    fun finishConfirmationLines(context: Context, plan: PasswordsFinishPlan, isOneItem: Boolean): List<String> {
        val lines = mutableListOf<String>()
        if (isOneItem) {
            if (plan.people.isNotEmpty()) lines.add(context.getString(R.string.passwords_finish_gives_key, joined(plan.people.map { person -> person.name })))
            if (plan.newKeyItemCount > 0) lines.add(context.getString(R.string.passwords_finish_new_key_one))
            if (plan.agentItemCount > 0) lines.add(context.getString(R.string.passwords_finish_agent_one))
        } else {
            if (plan.people.isNotEmpty()) {
                lines.add(context.getString(R.string.passwords_finish_people))
                lines.addAll(plan.people.map { person -> "${person.name}: ${person.itemCount}" })
            }
            if (plan.newKeyItemCount > 0) lines.add(context.resources.getQuantityString(R.plurals.passwords_finish_new_key_many, plan.newKeyItemCount, plan.newKeyItemCount))
            if (plan.agentItemCount > 0) lines.add(context.resources.getQuantityString(R.plurals.passwords_finish_agent_many, plan.agentItemCount, plan.agentItemCount))
        }
        if (lines.isEmpty()) lines.add(context.getString(R.string.passwords_finish_nothing_else))
        return lines
    }

    /** The person's name, or a word for them when the copy does not hold it. */
    fun displayName(context: Context, change: PasswordsKeyChange): String = change.name.ifEmpty { context.getString(R.string.passwords_someone) }

    /** How a person is named in a list of recipients: the name and, in brackets, the username. */
    fun recipientLabel(recipient: PasswordRecipient): String {
        val trimmed = recipient.name.trim()
        return if (trimmed.isEmpty() || trimmed == recipient.username) recipient.username else "$trimmed (${recipient.username})"
    }
}

/** Somebody an item is shared with, and the people that reaches. */
private class ShareGrantee(val target: ShareTarget, val name: String, recipients: List<PasswordRecipient>) {
    var recipients by mutableStateOf(recipients)
    val id: String get() = when (target) {
        is ShareTarget.Person -> "person:${target.userId}"
        is ShareTarget.Circle -> "circle:${target.circleId}"
    }
    val input: PasswordShareGranteeInput get() = PasswordShareGranteeInput(target.granteeUserId, target.granteeCircleId)
}

/** A key whose safety codes the person can read out and compare. */
internal class PasswordsCompareTarget(
    val userId: String,
    val name: String,
    /** The key whose code is shown, standard base64: what "The codes match" marks as compared. */
    val publicKey: String,
    val safetyCode: String,
    /** The code of the key before it changed, when it did. */
    val previousSafetyCode: String?,
    val isCompared: Boolean,
) {
    companion object {
        fun of(recipient: PasswordRecipient) = PasswordsCompareTarget(
            recipient.userId, recipient.name, recipient.publicKey?.let { key -> java.util.Base64.getEncoder().encodeToString(key) }.orEmpty(), recipient.safetyCode.orEmpty(), recipient.knownSafetyCode, recipient.isCompared,
        )

        fun of(context: Context, change: PasswordsKeyChange) = PasswordsCompareTarget(
            change.userId, PasswordsSharingWords.displayName(context, change), change.newPublicKey, change.newSafetyCode, change.oldSafetyCode, false,
        )
    }
}

/** Changes who an item is shared with. The changes are kept when the person presses Save. */
@Composable
fun PasswordsShareScreen(itemId: String) {
    val model = rememberPasswordsModel()
    val navigator = LocalAppNavigator.current
    GoBackWhenClosed(model)
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val connection = com.teanode.app.session.LocalConnection.current
    val item = model.item(itemId)
    val grantees = remember { mutableStateListOf<ShareGrantee>() }
    var people by remember { mutableStateOf<List<SharePerson>>(emptyList()) }
    var circles by remember { mutableStateOf<List<ShareCircle>>(emptyList()) }
    var chosen by remember { mutableStateOf<ShareTarget?>(null) }
    var comparing by remember { mutableStateOf<PasswordsCompareTarget?>(null) }
    var isLoaded by remember { mutableStateOf(false) }
    var isAdding by remember { mutableStateOf(false) }
    var isSaving by remember { mutableStateOf(false) }
    var loadProblem by remember { mutableStateOf<String?>(null) }
    var loadCount by remember { mutableStateOf(0) }

    suspend fun recipientsReached(share: PasswordShareView): List<PasswordRecipient> =
        runCatching { model.recipientsOf(share) }.getOrElse {
            val circleIds = share.granteeCircleId?.let { circleId -> listOf(circleId) } ?: emptyList()
            share.recipients.map { view -> PasswordRecipient(view, circleIds = circleIds) }
        }

    LaunchedEffect(itemId, loadCount) {
        val current = model.item(itemId) ?: return@LaunchedEffect
        loadProblem = null
        try {
            people = connection.sharePeople()
            circles = connection.shareCircles()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            // Without a circle nobody can be added, but the shares there are can still be seen and ended.
            if (people.isEmpty() && circles.isEmpty() && grantees.isEmpty() && current.shares.isEmpty()) loadProblem = model.problemText(error, context.getString(R.string.passwords_check_connection))
        }
        if (!isLoaded && grantees.isEmpty()) {
            // The shares the owner's own checked record lists. A share only the server lists is not shown as one the person
            // made (the screen says so); saving ends it.
            for (share in current.recordedShares) {
                val target = share.granteeCircleId?.let { circleId -> ShareTarget.Circle(circleId) } ?: ShareTarget.Person(share.granteeUserId.orEmpty())
                grantees.add(ShareGrantee(target, PasswordsSharingWords.granteeName(context, share), recipientsReached(share)))
            }
        }
        if (loadProblem == null) isLoaded = true
    }

    suspend fun refreshRecipients() {
        for (grantee in grantees.toList()) runCatching { model.recipientsFor(grantee.target) }.getOrNull()?.let { fresh -> grantee.recipients = fresh }
    }

    fun add() {
        val target = chosen ?: return
        if (isAdding) return
        if (grantees.any { grantee -> grantee.target == target }) {
            chosen = null
            return
        }
        isAdding = true
        scope.launch {
            try {
                val recipients = model.recipientsFor(target)
                grantees.add(ShareGrantee(target, ShareTargetNames.nameOf(target, people, circles, context), recipients))
                chosen = null
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                model.report(error, context.getString(R.string.passwords_share_people_not_read))
            } finally {
                isAdding = false
            }
        }
    }

    fun save() {
        val current = item ?: return
        isSaving = true
        scope.launch {
            // Everyone each share reaches; the session seals once for a person who is reached twice.
            val recipients = grantees.flatMap { grantee -> grantee.recipients }
            val waitingCount = recipients.filter { recipient -> recipient.publicKey == null }.map { recipient -> recipient.userId }.toSet().size
            try {
                model.setShares(current, grantees.map { grantee -> grantee.input }, recipients)
                model.say(if (waitingCount > 0) context.resources.getQuantityString(R.plurals.passwords_share_saved_waiting, waitingCount, waitingCount) else context.getString(R.string.passwords_share_saved))
                navigator.back()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: PasswordsError.ItemNeedsNewKey) {
                // The removal went through; the item's screen says it has no new key yet and offers a retry.
                model.report(error, "")
                navigator.back()
            } catch (error: Throwable) {
                model.report(error, context.getString(R.string.passwords_share_not_saved))
            } finally {
                isSaving = false
            }
        }
    }

    TeaNodeScreen(
        title = if (item == null) stringResource(R.string.passwords_share) else stringResource(R.string.passwords_share_title, item.name),
        onBack = { navigator.back() },
        actions = {
            if (isSaving) CircularProgressIndicator(Modifier.padding(end = 16.dp).size(24.dp), strokeWidth = 2.dp)
            else TextButton(onClick = { save() }, enabled = isLoaded) { Text(stringResource(R.string.passwords_save)) }
        },
    ) { padding ->
        when {
            item == null -> Column(Modifier.padding(padding)) {
                EmptyState(Icons.Outlined.Key, stringResource(R.string.passwords_item_gone_title), description = stringResource(R.string.passwords_item_gone_description))
            }
            loadProblem != null && !isLoaded -> ErrorState(loadProblem.orEmpty(), onRetry = { loadCount++ }, modifier = Modifier.padding(padding))
            !isLoaded -> LoadingState(Modifier.padding(padding))
            else -> Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
                if (people.isEmpty() && circles.isEmpty()) {
                    Text(stringResource(R.string.passwords_share_no_circle), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                } else {
                    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        ShareTargetPicker(people, circles, chosen) { picked -> chosen = picked }
                        ActionRow(Icons.Outlined.Add, stringResource(R.string.passwords_share_add), enabled = chosen != null && !isAdding) { add() }
                    }
                }
                Footer(stringResource(R.string.passwords_share_footer))
                val userIdsHere = grantees.flatMap { grantee -> grantee.recipients }.map { recipient -> recipient.userId }.toSet()
                for (change in model.keyChanges.filter { change -> userIdsHere.contains(change.userId) }) {
                    HorizontalDivider()
                    PasswordsKeyChangeNotice(model, change, onCompare = { comparing = PasswordsCompareTarget.of(context, change) })
                }
                if (item.sharingCheck == PasswordsSharingCheck.Unverified || item.sharingCheck == PasswordsSharingCheck.Replayed) {
                    HorizontalDivider()
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(
                            stringResource(if (item.sharingCheck == PasswordsSharingCheck.Replayed) R.string.passwords_share_replayed else R.string.passwords_sharing_unchecked),
                            style = MaterialTheme.typography.titleSmall,
                        )
                        Text(stringResource(R.string.passwords_share_again_text), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                PasswordsSharingWords.unrecordedSharesNotice(context, item)?.let { unrecordedText ->
                    HorizontalDivider()
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(unrecordedText)
                        Text(stringResource(R.string.passwords_share_unrecorded_save), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                if (grantees.isEmpty()) {
                    HorizontalDivider()
                    Text(stringResource(R.string.passwords_share_nobody), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                for (grantee in grantees.toList()) {
                    HorizontalDivider()
                    Text(grantee.name, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(16.dp, 16.dp, 16.dp, 4.dp))
                    if (grantee.recipients.isEmpty()) Text(stringResource(R.string.passwords_share_circle_empty), modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    for (recipient in grantee.recipients) PasswordsRecipientRow(recipient) { comparing = PasswordsCompareTarget.of(recipient) }
                    ActionRow(Icons.Outlined.PersonRemove, stringResource(R.string.passwords_share_remove, grantee.name), isDestructive = true) { grantees.remove(grantee) }
                    Footer(stringResource(R.string.passwords_share_remove_footer))
                }
            }
        }
    }
    comparing?.let { target ->
        PasswordsCompareCodesSheet(model, target, onDismiss = {
            comparing = null
            scope.launch { refreshRecipients() }
        })
    }
}

/**
 * One person an item reaches, as rows of a list: their name with what is known of them (waiting until they set up
 * Passwords, too much shared with them already, or that the codes were compared), and "Compare codes" as a row of its own to
 * check the key with them (left out once compared). Nothing here has to be done before sharing.
 */
@Composable
internal fun PasswordsRecipientRow(recipient: PasswordRecipient, compare: () -> Unit = {}) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(PasswordsSharingWords.recipientLabel(recipient), style = MaterialTheme.typography.bodyLarge)
        if (recipient.safetyCode == null) {
            Text(stringResource(R.string.passwords_recipient_waiting), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else {
            if (recipient.hasTooMuchShared) {
                Text(stringResource(R.string.passwords_recipient_too_much, recipient.name), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (recipient.isCompared) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    Icon(Icons.Outlined.CheckCircle, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(stringResource(R.string.passwords_codes_compared), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
    if (recipient.safetyCode != null && !recipient.isCompared) ActionRow(Icons.Outlined.VerifiedUser, stringResource(R.string.passwords_compare_codes), onClick = compare)
}

@Composable
private fun SafetyCodeLines(caption: String, code: String) {
    Column {
        Text(caption, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(code, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/**
 * The safety codes of a person's key (and the code it had before when it changed) next to the person's own, to read out and
 * compare. "The codes match" marks the key as compared. Nothing waits on it.
 */
@Composable
internal fun PasswordsCompareCodesSheet(model: PasswordsModel, target: PasswordsCompareTarget, onDismiss: () -> Unit) {
    ShoppingSheet(stringResource(R.string.passwords_compare_codes), onDismiss) {
        val previous = target.previousSafetyCode
        Text(
            if (previous == null) stringResource(R.string.passwords_compare_text, target.name) else stringResource(R.string.passwords_compare_text_changed, target.name),
        )
        SafetyCodeLines(stringResource(R.string.passwords_safety_code_theirs), target.safetyCode)
        if (previous != null) SafetyCodeLines(stringResource(R.string.passwords_safety_code_before), previous)
        model.ownSafetyCode?.let { ownCode -> SafetyCodeLines(stringResource(R.string.passwords_safety_code_yours), ownCode) }
        Text(stringResource(R.string.passwords_compare_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (target.isCompared) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Outlined.CheckCircle, null)
                Text(stringResource(R.string.passwords_codes_compared))
            }
        } else {
            ActionRow(Icons.Outlined.Check, stringResource(R.string.passwords_codes_match)) {
                model.markCompared(target.userId, target.publicKey)
                onDismiss()
            }
        }
    }
}

/**
 * The notice that a person's key changed: it was accepted, nothing waits on it, and it stays until it is dismissed. The words
 * come first, then "Compare codes" (which shows both codes) and "Dismiss".
 */
@Composable
internal fun PasswordsKeyChangeNotice(model: PasswordsModel, change: PasswordsKeyChange, onCompare: () -> Unit) {
    val context = LocalContext.current
    Column(Modifier.fillMaxWidth()) {
        Text(
            stringResource(R.string.passwords_key_changed, PasswordsSharingWords.displayName(context, change)),
            modifier = Modifier.padding(16.dp, 12.dp, 16.dp, 4.dp), style = MaterialTheme.typography.bodyMedium,
        )
        ActionRow(Icons.Outlined.VerifiedUser, stringResource(R.string.passwords_compare_codes), onClick = onCompare)
        ActionRow(Icons.Outlined.Close, stringResource(R.string.passwords_dismiss)) { model.dismissKeyChange(change) }
    }
}

/**
 * The notice that this phone sealed an item's key for somebody the item reaches through a circle and had never sealed it for
 * before. Who is in a circle is the server's word, so the person is told. Nothing waits on it, and it stays until it is dismissed.
 */
@Composable
internal fun PasswordsCircleShareNoticeRow(model: PasswordsModel, notice: PasswordsCircleShareNotice) {
    Column(Modifier.fillMaxWidth()) {
        Text(
            stringResource(R.string.passwords_circle_notice, notice.recipientName, notice.itemName, notice.circleName),
            modifier = Modifier.padding(16.dp, 12.dp, 16.dp, 4.dp), style = MaterialTheme.typography.bodyMedium,
        )
        ActionRow(Icons.Outlined.Close, stringResource(R.string.passwords_dismiss)) { model.dismissCircleShareNotice(notice) }
    }
}

/** What finishing the sharing would do, one sentence for each thing, as the question shows it. */
@Composable
internal fun PasswordsFinishSharingBody(ask: PasswordsFinishAsk) {
    val context = LocalContext.current
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) { for (line in PasswordsSharingWords.finishConfirmationLines(context, ask.plan, ask.isOneItem)) Text(line) }
}

/** The question asked before sharing is finished: what would happen, then "Finish sharing". What is sealed is the plan as it was shown. */
@Composable
internal fun PasswordsFinishSharingDialog(ask: PasswordsFinishAsk?, onDismiss: () -> Unit, onConfirm: (PasswordsFinishAsk) -> Unit) {
    if (ask == null) return
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(ask.title) },
        text = { PasswordsFinishSharingBody(ask) },
        confirmButton = { TextButton(onClick = { onConfirm(ask) }) { Text(stringResource(R.string.passwords_finish_button)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.passwords_cancel)) } },
    )
}

/**
 * "Finish sharing N items" and one line for each item this phone left for the person to finish sharing (a person who joined a
 * circle later needs one tap for each item, so one row does them all). It does not block anything: each item opens as usual,
 * and its screen has the button.
 */
@Composable
internal fun PasswordsNeedsYouRows(model: PasswordsModel, onOpenItem: (PasswordItem) -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var ask by remember { mutableStateOf<PasswordsFinishAsk?>(null) }
    val waitingItems = model.items.filter { item -> model.itemIdsNeedingYou.contains(item.id) }
    if (waitingItems.isEmpty()) return
    val progress = model.sharingProgress
    Column(Modifier.fillMaxWidth()) {
        if (progress != null) {
            Row(Modifier.fillMaxWidth().padding(16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.passwords_finish_progress, progress.doneCount, progress.totalCount))
            }
        } else {
            ActionRow(Icons.Outlined.PersonAdd, pluralStringResource(R.plurals.passwords_finish_every_item, waitingItems.size, waitingItems.size)) {
                scope.launch {
                    val itemIds = waitingItems.map { item -> item.id }
                    val plan = model.finishPlan(itemIds)
                    ask = PasswordsFinishAsk(itemIds, plan, PasswordsSharingWords.finishConfirmationTitle(context, itemIds.size, waitingItems.firstOrNull()?.name.orEmpty()))
                }
            }
        }
        for (item in waitingItems) {
            Column(Modifier.fillMaxWidth().clickable { onOpenItem(item) }.padding(horizontal = 16.dp, vertical = 10.dp)) {
                Text(item.name, style = MaterialTheme.typography.bodyLarge)
                Text(stringResource(R.string.passwords_finish_waiting_note), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (model.hasMoreSharesWaiting) Footer(stringResource(R.string.passwords_finish_more_waiting))
        for (name in model.namesWithTooMuchShared) Footer(stringResource(R.string.passwords_recipient_too_much, name))
        HorizontalDivider()
    }
    PasswordsFinishSharingDialog(ask, onDismiss = { ask = null }, onConfirm = { confirmed ->
        ask = null
        scope.launch { model.finishSharingEveryItem(confirmed) }
    })
}

/** The route of an item's sharing. */
internal fun passwordsShareRoute(itemId: String): String = "passwords/share/${Uri.encode(itemId)}"

/** The notices at the top of the list: keys that changed, and people an item was shared with through a circle. */
@Composable
internal fun PasswordsListNotices(model: PasswordsModel) {
    val context = LocalContext.current
    var comparing by remember { mutableStateOf<PasswordsCompareTarget?>(null) }
    Column(Modifier.fillMaxWidth()) {
        for (change in model.keyChanges) {
            Surface(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), shape = RoundedCornerShape(16.dp), color = MaterialTheme.colorScheme.secondaryContainer) {
                PasswordsKeyChangeNotice(model, change, onCompare = { comparing = PasswordsCompareTarget.of(context, change) })
            }
        }
        for (notice in model.circleShareNotices) {
            Surface(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), shape = RoundedCornerShape(16.dp), color = MaterialTheme.colorScheme.secondaryContainer) {
                PasswordsCircleShareNoticeRow(model, notice)
            }
        }
    }
    comparing?.let { target -> PasswordsCompareCodesSheet(model, target, onDismiss = { comparing = null }) }
}
