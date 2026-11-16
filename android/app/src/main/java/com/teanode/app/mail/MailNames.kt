package com.teanode.app.mail

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.NoteAlt
import androidx.compose.material.icons.outlined.Report
import androidx.compose.material.icons.outlined.Send
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.runtime.Composable
import com.teanode.app.R
import com.teanode.kit.mail.MailFolder

/** MailNames are the words and symbols of the folders: a system folder in the person's language, their own as named. */
object MailNames {
    @Composable
    fun folder(folder: MailFolder): String = when (folder.kind) {
        "inbox" -> stringResource(R.string.mail_inbox)
        "drafts" -> stringResource(R.string.mail_drafts)
        "sent" -> stringResource(R.string.mail_sent)
        "archive" -> stringResource(R.string.mail_archive)
        "junk" -> stringResource(R.string.mail_junk)
        "trash" -> stringResource(R.string.mail_trash)
        else -> folder.name
    }

    /** The same words where there is no composition to read them in. */
    fun folder(folder: MailFolder, context: android.content.Context): String = when (folder.kind) {
        "inbox" -> context.getString(R.string.mail_inbox)
        "drafts" -> context.getString(R.string.mail_drafts)
        "sent" -> context.getString(R.string.mail_sent)
        "archive" -> context.getString(R.string.mail_archive)
        "junk" -> context.getString(R.string.mail_junk)
        "trash" -> context.getString(R.string.mail_trash)
        else -> folder.name
    }

    fun icon(folder: MailFolder): ImageVector = when (folder.kind) {
        "inbox" -> Icons.Outlined.Inbox
        "drafts" -> Icons.Outlined.Description
        "sent" -> Icons.Outlined.Send
        "archive" -> Icons.Outlined.Archive
        "junk" -> Icons.Outlined.Report
        "trash" -> Icons.Outlined.Delete
        else -> if (folder.isNotes) Icons.Outlined.NoteAlt else Icons.Outlined.Folder
    }
}
