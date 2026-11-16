package com.teanode.app.journal

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Checklist
import androidx.compose.material.icons.outlined.Circle
import androidx.compose.material.icons.outlined.CreditCard
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.GraphicEq
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material.icons.outlined.Notes
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.ShoppingCart
import androidx.compose.material.icons.outlined.TrackChanges
import androidx.compose.ui.graphics.vector.ImageVector

/** The symbols the journal's rows are marked with (ios JournalText.symbol(forRecordKind:)). */
object JournalIcons {
    fun forRecordKind(recordKind: String): ImageVector = when (recordKind) {
        "mail" -> Icons.Outlined.Email
        "photo" -> Icons.Outlined.Image
        "place_visit" -> Icons.Outlined.Place
        "agent_document" -> Icons.Outlined.Description
        "calendar_event", "calendar_occurrence" -> Icons.Outlined.CalendarMonth
        "recording", "recording_passage" -> Icons.Outlined.GraphicEq
        "chat_message", "conversation_message" -> Icons.Outlined.ChatBubbleOutline
        "finance_transaction", "finance_receipt" -> Icons.Outlined.CreditCard
        "health_day" -> Icons.Outlined.FavoriteBorder
        "shopping_trip" -> Icons.Outlined.ShoppingCart
        "note" -> Icons.Outlined.Notes
        "reminder" -> Icons.Outlined.Checklist
        "idea" -> Icons.Outlined.Lightbulb
        "goal" -> Icons.Outlined.TrackChanges
        "contact" -> Icons.Outlined.AccountCircle
        else -> Icons.Outlined.Circle
    }
}
