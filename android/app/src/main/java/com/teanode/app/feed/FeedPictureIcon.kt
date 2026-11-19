package com.teanode.app.feed

import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountBalance
import androidx.compose.material.icons.outlined.Cloud
import androidx.compose.material.icons.outlined.Air
import androidx.compose.material.icons.outlined.NightsStay
import androidx.compose.material.icons.outlined.Thunderstorm
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material.icons.outlined.Umbrella
import androidx.compose.material.icons.outlined.WbSunny
import androidx.compose.material.icons.outlined.FilterDrama
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.Flight
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.Insights
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material.icons.outlined.LocationOn
import androidx.compose.material.icons.outlined.Mail
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.Newspaper
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Videocam
import androidx.compose.material.icons.outlined.Psychology
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.ShoppingCart
import androidx.compose.material.icons.outlined.VerifiedUser
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import com.teanode.app.R
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.FeedPicture
import com.teanode.kit.feed.FeedPictureKind

/** The Material icon for a kind of picture, the counterpart of the SF Symbol the agent named; the newspaper stands for any it does not know. */
fun FeedPictureKind.icon(): ImageVector = when (this) {
    FeedPictureKind.Photo -> Icons.Outlined.PhotoLibrary
    FeedPictureKind.Video -> Icons.Outlined.Videocam
    FeedPictureKind.Weather -> Icons.Outlined.FilterDrama
    FeedPictureKind.Sun -> Icons.Outlined.WbSunny
    FeedPictureKind.Cloud -> Icons.Outlined.Cloud
    FeedPictureKind.Rain -> Icons.Outlined.Umbrella
    FeedPictureKind.Snow -> Icons.Outlined.AcUnit
    FeedPictureKind.Storm -> Icons.Outlined.Thunderstorm
    FeedPictureKind.Night -> Icons.Outlined.NightsStay
    FeedPictureKind.Wind -> Icons.Outlined.Air
    FeedPictureKind.Security -> Icons.Outlined.VerifiedUser
    FeedPictureKind.Chart -> Icons.Outlined.Insights
    FeedPictureKind.News, FeedPictureKind.Other -> Icons.Outlined.Newspaper
    FeedPictureKind.Tray -> Icons.Outlined.Inbox
    FeedPictureKind.Mail -> Icons.Outlined.Mail
    FeedPictureKind.Calendar -> Icons.Outlined.CalendarMonth
    FeedPictureKind.Money -> Icons.Outlined.AccountBalance
    FeedPictureKind.Shopping -> Icons.Outlined.ShoppingCart
    FeedPictureKind.Travel -> Icons.Outlined.Flight
    FeedPictureKind.Home -> Icons.Outlined.Home
    FeedPictureKind.Health -> Icons.Outlined.MonitorHeart
    FeedPictureKind.Idea -> Icons.Outlined.Lightbulb
    FeedPictureKind.Alert -> Icons.Outlined.NotificationsActive
    FeedPictureKind.Person -> Icons.Outlined.Person
    FeedPictureKind.Place -> Icons.Outlined.LocationOn
    FeedPictureKind.Key -> Icons.Outlined.Key
    FeedPictureKind.Code -> Icons.Outlined.Code
    FeedPictureKind.Memory -> Icons.Outlined.Psychology
    FeedPictureKind.Done -> Icons.Outlined.Check
    FeedPictureKind.Time -> Icons.Outlined.Schedule
}

/** The side of a card's badge; its corners are rounded by 30 percent of it. */
private val BADGE_SIDE = 26.dp

/**
 * A card's picture, small, as a phone draws an app's icon in a notification (ios FeedBadge): the icon of the symbol the
 * agent chose in white on a rounded square of the color of the card's kind, or its emoji on a plain fill; the newspaper
 * for any name it does not know.
 */
@Composable
fun FeedKindBadge(item: AgentFeedItemView, modifier: Modifier = Modifier) {
    val shape = RoundedCornerShape(percent = 30)
    when (val picture = FeedPicture.of(item.pictureSymbol)) {
        is FeedPicture.Symbol -> {
            val kindColor = feedKindColor(item.feedItemKind)
            // Lighter at the top, as an app icon's gradient is.
            val fill = Brush.verticalGradient(listOf(lerp(kindColor, Color.White, 0.18f), kindColor))
            Box(modifier.size(BADGE_SIDE).clip(shape).background(fill), contentAlignment = Alignment.Center) {
                Icon(picture.kind.icon(), contentDescription = null, tint = Color.White, modifier = Modifier.size(16.dp))
            }
        }
        is FeedPicture.Emoji -> Box(modifier.size(BADGE_SIDE).clip(shape).background(MaterialTheme.colorScheme.surfaceContainerHighest), contentAlignment = Alignment.Center) {
            Text(picture.text, style = MaterialTheme.typography.labelLarge)
        }
    }
}

/**
 * The color of a card's badge, one for each kind of card, so cards of a kind are told apart at a glance (the iPhone's
 * system colors). Never green, which is kept for what can be pressed.
 */
fun feedKindColor(feedItemKind: String): Color = when (feedItemKind) {
    "alert" -> Color(0xFFFF9500)
    "decision" -> Color(0xFF5856D6)
    "news" -> Color(0xFFFF2D55)
    "report" -> Color(0xFF30B0C7)
    "idea" -> Color(0xFFAF52DE)
    else -> Color(0xFF007AFF)
}

/** What kind of card it is, for the header of a card from no named source. */
@StringRes
fun feedKindWord(feedItemKind: String): Int = when (feedItemKind) {
    "alert" -> R.string.feed_kind_alert
    "decision" -> R.string.feed_kind_decision
    "news" -> R.string.feed_kind_news
    "report" -> R.string.feed_kind_report
    "idea" -> R.string.feed_kind_idea
    else -> R.string.feed_kind_update
}
