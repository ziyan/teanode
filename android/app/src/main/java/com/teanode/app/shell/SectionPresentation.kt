package com.teanode.app.shell

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AdsClick
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.CreditCard
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.FiberManualRecord
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material.icons.outlined.LocationOn
import androidx.compose.material.icons.automirrored.outlined.MenuBook
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Psychology
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.ShoppingCart
import androidx.compose.ui.graphics.vector.ImageVector
import com.teanode.app.R
import com.teanode.kit.AppSection
import com.teanode.kit.AppSectionGroup

/**
 * The part's name in the bottom bar and the drawer. The agent's part is Chats: it holds the chats with people and
 * groups as well as the agent's own.
 */
val AppSection.titleResource: Int
    @StringRes get() = when (this) {
        AppSection.Agent -> R.string.section_agent
        AppSection.Journal -> R.string.section_journal
        AppSection.Goals -> R.string.section_goals
        AppSection.Ideas -> R.string.section_ideas
        AppSection.Mail -> R.string.section_mail
        AppSection.Calendar -> R.string.section_calendar
        AppSection.Contacts -> R.string.section_contacts
        AppSection.Photos -> R.string.section_photos
        AppSection.Location -> R.string.section_location
        AppSection.Health -> R.string.section_health
        AppSection.Passwords -> R.string.section_passwords
        AppSection.Shopping -> R.string.section_shopping
        AppSection.Knowledge -> R.string.section_knowledge
        AppSection.Finance -> R.string.section_finance
        AppSection.Recordings -> R.string.section_recordings
        AppSection.Coding -> R.string.section_coding
        AppSection.Settings -> R.string.section_settings
    }

/** The part's symbol, the Material counterpart of the iPhone app's SF Symbol. */
val AppSection.icon: ImageVector
    get() = when (this) {
        AppSection.Agent -> Icons.Outlined.ChatBubbleOutline
        AppSection.Journal -> Icons.AutoMirrored.Outlined.MenuBook
        AppSection.Goals -> Icons.Outlined.AdsClick
        AppSection.Ideas -> Icons.Outlined.Lightbulb
        AppSection.Mail -> Icons.Outlined.Email
        AppSection.Calendar -> Icons.Outlined.CalendarMonth
        AppSection.Contacts -> Icons.Outlined.Person
        AppSection.Photos -> Icons.Outlined.PhotoLibrary
        AppSection.Location -> Icons.Outlined.LocationOn
        AppSection.Health -> Icons.Outlined.MonitorHeart
        AppSection.Passwords -> Icons.Outlined.Key
        AppSection.Shopping -> Icons.Outlined.ShoppingCart
        AppSection.Knowledge -> Icons.Outlined.Psychology
        AppSection.Finance -> Icons.Outlined.CreditCard
        AppSection.Recordings -> Icons.Outlined.FiberManualRecord
        AppSection.Coding -> Icons.Outlined.Code
        AppSection.Settings -> Icons.Outlined.Settings
    }

/** The heading of a group in the drawer. */
val AppSectionGroup.titleResource: Int
    @StringRes get() = when (this) {
        AppSectionGroup.EveryDay -> R.string.group_every_day
        AppSectionGroup.Work -> R.string.group_work
        AppSectionGroup.Plans -> R.string.group_plans
        AppSectionGroup.Money -> R.string.group_money
    }
