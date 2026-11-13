package com.teanode.app.settings

import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.sharing.SharingKind
import com.teanode.kit.sharing.SharingMade
import com.teanode.kit.sharing.SharingReceived
import com.teanode.kit.sharing.declineShare
import com.teanode.kit.sharing.sharing
import com.teanode.kit.sharing.stopSharing
import java.text.DateFormat
import java.util.Date

@Composable
private fun kindTitle(kind: SharingKind?, shareKind: String): String = when (kind) {
    SharingKind.Calendar -> stringResource(R.string.settings_sharing_kind_calendar)
    SharingKind.Location -> stringResource(R.string.settings_sharing_kind_location)
    SharingKind.Health -> stringResource(R.string.settings_sharing_kind_health)
    SharingKind.MemoryPage -> stringResource(R.string.settings_sharing_kind_memory_page)
    SharingKind.PhotoAlbum -> stringResource(R.string.settings_sharing_kind_photo_album)
    SharingKind.Recording -> stringResource(R.string.settings_sharing_kind_recording)
    SharingKind.Finance -> stringResource(R.string.settings_sharing_kind_finance)
    SharingKind.Skill -> stringResource(R.string.settings_sharing_kind_skill)
    SharingKind.Passwords -> stringResource(R.string.settings_sharing_kind_passwords)
    SharingKind.ShoppingList -> stringResource(R.string.settings_sharing_kind_shopping_list)
    null -> shareKind
}

/** What a share is of: the thing's name (a finance account with the last digits of its number), "3 items" for passwords, and the kind for location and Health, which are shared whole. */
@Composable
private fun whatText(kind: SharingKind?, shareKind: String, subjectName: String, subjectMask: String, passwordItemCount: Int): String = when {
    kind == SharingKind.Passwords -> if (passwordItemCount == 1) stringResource(R.string.settings_sharing_one_item) else stringResource(R.string.settings_sharing_items, passwordItemCount)
    subjectName.isEmpty() -> kindTitle(kind, shareKind)
    kind == SharingKind.Finance && subjectMask.isNotEmpty() -> "$subjectName ··$subjectMask"
    else -> subjectName
}

@Composable
private fun allFinanceText(shareLevel: String): String = when (shareLevel) {
    "spending" -> stringResource(R.string.settings_sharing_all_finance_spending)
    "transactions" -> stringResource(R.string.settings_sharing_all_finance_transactions)
    else -> stringResource(R.string.settings_sharing_all_finance)
}

@Composable
private fun madeWhat(share: SharingMade): String = when {
    share.isPageGone -> stringResource(R.string.settings_sharing_page_deleted)
    share.isAllFinance -> allFinanceText(share.shareLevel)
    else -> whatText(share.knownKind, share.shareKind, share.subjectName, share.shownSubjectMask, share.passwordItemCount)
}

@Composable
private fun receivedWhat(share: SharingReceived): String =
    if (share.isAllFinance) allFinanceText(share.shareLevel) else whatText(share.knownKind, share.shareKind, share.subjectName, share.shownSubjectMask, share.passwordItemCount)

/** A level in words. The words are the ones a note of a new share uses; a level the app does not know is shown as the server wrote it. */
@Composable
private fun levelText(shareLevel: String, isReceived: Boolean): String = when (shareLevel) {
    "busy" -> stringResource(R.string.settings_sharing_level_busy)
    "titles" -> stringResource(R.string.settings_sharing_level_titles)
    "details" -> stringResource(R.string.settings_sharing_level_details)
    "edit" -> stringResource(R.string.settings_sharing_level_edit)
    "current" -> stringResource(if (isReceived) R.string.settings_sharing_level_current_theirs else R.string.settings_sharing_level_current)
    "history" -> stringResource(if (isReceived) R.string.settings_sharing_level_history_theirs else R.string.settings_sharing_level_history)
    "view" -> stringResource(R.string.settings_sharing_level_view)
    "contribute" -> stringResource(R.string.settings_sharing_level_contribute)
    "summary" -> stringResource(R.string.settings_sharing_level_summary)
    "transcript" -> stringResource(R.string.settings_sharing_level_transcript)
    "balance" -> stringResource(R.string.settings_sharing_level_balance)
    "spending" -> stringResource(R.string.settings_sharing_level_spending)
    "transactions" -> stringResource(R.string.settings_sharing_level_transactions)
    else -> shareLevel
}

@Composable
private fun scopeTexts(kind: SharingKind?, shareScopes: List<String>): List<String> = shareScopes.map { scope ->
    when {
        kind == SharingKind.MemoryPage && scope == "quotes" -> stringResource(R.string.settings_sharing_scope_quotes)
        kind == SharingKind.PhotoAlbum && scope == "location" -> stringResource(R.string.settings_sharing_scope_places)
        kind == SharingKind.PhotoAlbum && scope == "faceNames" -> stringResource(R.string.settings_sharing_scope_face_names)
        else -> scope
    }
}

@Composable
private fun levelAndScopes(kind: SharingKind?, shareLevel: String, shareScopes: List<String>, isReceived: Boolean): String {
    // A share of all finance says its level in its title.
    if (com.teanode.kit.sharing.SharingScope.isAllFinance(kind?.serverName.orEmpty(), shareScopes)) return ""
    val parts = buildList {
        if (shareLevel.isNotEmpty()) add(levelText(shareLevel, isReceived))
        addAll(scopeTexts(kind, shareScopes))
    }
    return parts.joinToString(", ")
}

@Composable
private fun sinceText(since: java.time.Instant?): String? = since?.let { instant -> stringResource(R.string.settings_sharing_since, DateFormat.getDateInstance(DateFormat.MEDIUM).format(Date.from(instant))) }

@Composable
private fun madeDetails(share: SharingMade): String {
    val parts = buildList {
        if (share.subjectName.isNotEmpty() || share.knownKind == SharingKind.Passwords) add(kindTitle(share.knownKind, share.shareKind))
        add(if (share.isCircle) stringResource(R.string.settings_sharing_with_circle, share.shownGrantee) else stringResource(R.string.settings_sharing_with, share.shownGrantee))
        levelAndScopes(share.knownKind, share.shareLevel, share.shareScopes, false).takeIf { words -> words.isNotEmpty() }?.let { words -> add(words) }
        sinceText(share.since)?.let { text -> add(text) }
    }
    return parts.joinToString(" · ")
}

@Composable
private fun receivedDetails(share: SharingReceived): String {
    val parts = buildList {
        if (share.subjectName.isNotEmpty() || share.knownKind == SharingKind.Passwords) add(kindTitle(share.knownKind, share.shareKind))
        add(stringResource(R.string.settings_sharing_from, share.shownOwner))
        add(if (share.isDirect) stringResource(R.string.settings_sharing_by_name) else stringResource(R.string.settings_sharing_through_circle, share.granteeCircleName))
        levelAndScopes(share.knownKind, share.shareLevel, share.shareScopes, true).takeIf { words -> words.isNotEmpty() }?.let { words -> add(words) }
        sinceText(share.since)?.let { text -> add(text) }
        if (!share.canDecline) add(stringResource(if (share.isDirect) R.string.settings_sharing_removed_by_owner else R.string.settings_sharing_ends_with_circle))
    }
    return parts.joinToString(" · ")
}

/** What the person shares, then what is shared with them. */
@Composable
fun SharingScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.sharing() }
    var stopping by remember { mutableStateOf<SharingMade?>(null) }
    var removing by remember { mutableStateOf<SharingReceived?>(null) }
    LoadedSettingsPage(
        stringResource(R.string.settings_sharing_title), onBack, loaded, isEmpty = { list -> list.isEmpty },
        empty = { EmptyState(Icons.Outlined.Group, stringResource(R.string.settings_sharing_none), description = stringResource(R.string.settings_sharing_none_detail)) },
    ) { list ->
        if (list.sharesMade.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_sharing_by_you)) }
            items(list.sharesMade.size) { index ->
                val share = list.sharesMade[index]
                TeaNodeRow(
                    headline = madeWhat(share), supporting = madeDetails(share), supportingMaxLines = 4,
                    trailing = if (share.canStop) ({ IconButton(onClick = { stopping = share }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_sharing_stop)) } }) else null,
                )
            }
        }
        if (list.sharesReceived.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_sharing_with_you)) }
            items(list.sharesReceived.size) { index ->
                val share = list.sharesReceived[index]
                TeaNodeRow(
                    headline = receivedWhat(share), supporting = receivedDetails(share), supportingMaxLines = 4,
                    trailing = if (share.canDecline) ({ IconButton(onClick = { removing = share }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_remove)) } }) else null,
                )
            }
            item { FooterText(stringResource(R.string.settings_sharing_footer)) }
        }
    }
    stopping?.let { share ->
        val thing = if (share.isPageGone) stringResource(R.string.settings_sharing_this_page) else madeWhat(share)
        val stoppedText = stringResource(R.string.settings_sharing_stopped, share.shownGrantee)
        ConfirmationDialog(
            title = stringResource(R.string.settings_sharing_stop), confirmLabel = stringResource(R.string.settings_sharing_stop), isDestructive = true,
            text = stringResource(R.string.settings_sharing_stop_detail, share.shownGrantee, thing),
            onConfirm = { stopping = null; actions.run(stoppedText, onSuccess = { loaded.refresh() }) { connection.stopSharing(share) } },
            onDismiss = { stopping = null },
        )
    }
    removing?.let { share ->
        val removedText = stringResource(R.string.settings_sharing_removed, share.shownOwner)
        ConfirmationDialog(
            title = stringResource(R.string.settings_sharing_remove_title, share.shownOwner), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true,
            text = stringResource(R.string.settings_sharing_remove_detail, receivedWhat(share), share.shownOwner),
            onConfirm = { removing = null; actions.run(removedText, onSuccess = { loaded.refresh() }) { connection.declineShare(share) } },
            onDismiss = { removing = null },
        )
    }
}
