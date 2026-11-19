package com.teanode.app.feed

import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountBalance
import androidx.compose.material.icons.outlined.Cloud
import androidx.compose.material.icons.outlined.Air
import androidx.compose.material.icons.outlined.NightsStay
import androidx.compose.material.icons.outlined.Thunderstorm
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material.icons.outlined.Umbrella
import androidx.compose.material.icons.outlined.WbSunny
import androidx.compose.material.icons.outlined.WbCloudy
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
import androidx.compose.material.icons.outlined.Psychology
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.ShoppingCart
import androidx.compose.material.icons.outlined.VerifiedUser
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import com.teanode.kit.feed.FeedPicture
import com.teanode.kit.feed.FeedPictureKind

/** The Material icon for a kind of picture, the counterpart of the SF Symbol the agent named; the newspaper stands for any it does not know. */
fun FeedPictureKind.icon(): ImageVector = when (this) {
    FeedPictureKind.Weather -> Icons.Outlined.WbCloudy
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

/** A card's small picture at its left: a round tile holding the icon of the symbol the agent chose, or its emoji. */
@Composable
fun FeedPictureTile(pictureSymbol: String, modifier: Modifier = Modifier) {
    Surface(modifier.size(44.dp), shape = CircleShape, color = MaterialTheme.colorScheme.primaryContainer) {
        Box(contentAlignment = Alignment.Center) {
            when (val picture = FeedPicture.of(pictureSymbol)) {
                is FeedPicture.Symbol -> Icon(picture.kind.icon(), contentDescription = null, tint = MaterialTheme.colorScheme.onPrimaryContainer, modifier = Modifier.size(24.dp))
                is FeedPicture.Emoji -> Text(picture.text, style = MaterialTheme.typography.titleLarge)
            }
        }
    }
}
