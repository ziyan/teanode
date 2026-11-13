package com.teanode.app.widgets

import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.runtime.Composable
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.GlanceTheme
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.action.actionStartActivity
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.cornerRadius
import androidx.glance.background
import androidx.glance.layout.Alignment
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider
import com.teanode.app.MainActivity
import com.teanode.app.R
import com.teanode.app.TeaNodeApplication
import com.teanode.app.notifications.NotificationBuilder
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.notifications.Glance
import com.teanode.kit.notifications.GlanceEvent
import com.teanode.kit.notifications.NotificationInfo
import com.teanode.kit.notifications.glance
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.text.DateFormat
import java.util.Date

// TeaNode on the home screen: asking or calling the agent, what waits for the person, and what is next. They read with
// the sign-in the app keeps, every half hour (the system's smallest), as the iPhone app's widgets do every fifteen
// minutes. A widget's buttons open the app at a place; the same places the notifications and the icon's menu open.

/** GlanceEntry is one moment of a widget: what it shows, or that nobody is signed in. */
class GlanceEntry(val glance: Glance?, val isConnected: Boolean, val accountKey: String)

private suspend fun readGlance(context: Context): GlanceEntry = withContext(Dispatchers.IO) {
    val push = (context.applicationContext as TeaNodeApplication).push
    val account = runCatching { push.accountStore.load() }.getOrNull() ?: return@withContext GlanceEntry(null, isConnected = false, accountKey = "")
    val connection = push.connection(account.accountKey)
    val read = connection?.let { signedIn -> runCatching { signedIn.glance() }.getOrNull() }
    GlanceEntry(read, isConnected = true, accountKey = account.accountKey)
}

private fun openingIntent(context: Context, route: String, isCall: Boolean = false): Intent =
    Intent(context, MainActivity::class.java)
        .setAction(Intent.ACTION_VIEW)
        .setData(Uri.parse("teanode://app/$route"))
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        .apply { if (isCall) putExtra(EXTRA_CALL_AGENT, true) }

/** What the icon's menu and the widget's call button add to the intent: the call with the agent starts when the chats open. */
const val EXTRA_CALL_AGENT = "com.teanode.app.extra.CALL_AGENT"

@Composable
private fun Panel(content: @Composable () -> Unit) {
    GlanceTheme {
        androidx.glance.layout.Box(
            GlanceModifier.fillMaxSize().background(GlanceTheme.colors.widgetBackground).cornerRadius(20.dp).padding(12.dp),
        ) { content() }
    }
}

// MARK: The agent

/** AgentWidget opens the conversation with the agent, or a call with it. */
class AgentWidget : GlanceAppWidget() {
    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val entry = readGlance(context)
        val name = entry.glance?.agentName?.ifEmpty { null } ?: context.getString(R.string.widgets_agent_name)
        provideContent {
            Panel {
                Column(GlanceModifier.fillMaxSize()) {
                    Text(name, style = TextStyle(fontSize = 16.sp, fontWeight = FontWeight.Bold, color = GlanceTheme.colors.onSurface), maxLines = 1)
                    Text(context.getString(R.string.widgets_ask_anything), style = TextStyle(fontSize = 12.sp, color = GlanceTheme.colors.onSurfaceVariant))
                    Spacer(GlanceModifier.defaultWeight())
                    Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        WidgetButton(context.getString(R.string.widgets_ask_your_agent), "A", actionStartActivity(openingIntent(context, "agent/home")))
                        Spacer(GlanceModifier.width(8.dp))
                        WidgetButton(context.getString(R.string.widgets_call_your_agent), "C", actionStartActivity(openingIntent(context, "agent/home", isCall = true)))
                        Spacer(GlanceModifier.width(8.dp))
                        WidgetButton(context.getString(R.string.widgets_start_recording), "R", actionStartActivity(openingIntent(context, "recordings/home")))
                    }
                }
            }
        }
    }
}

@Composable
private fun WidgetButton(description: String, letter: String, action: androidx.glance.action.Action) {
    androidx.glance.layout.Box(
        GlanceModifier.height(40.dp).width(40.dp).cornerRadius(20.dp).background(GlanceTheme.colors.primaryContainer).clickable(action),
        contentAlignment = Alignment.Center,
    ) {
        Text(letter, style = TextStyle(fontSize = 14.sp, fontWeight = FontWeight.Bold, color = GlanceTheme.colors.onPrimaryContainer))
    }
}

class AgentWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = AgentWidget()
}

// MARK: What waits

/** WaitingWidget is how many questions and approvals wait for the person, and the newest; a tap opens its conversation. */
class WaitingWidget : GlanceAppWidget() {
    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val entry = readGlance(context)
        val count = entry.glance?.waitingCount ?: 0
        val newest = entry.glance?.newestWaiting
        val open = NotificationInfo(accountKey = entry.accountKey, notificationKind = "message", conversationId = newest?.conversationId.orEmpty())
        val intent = Intent(context, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
            .putExtra(NotificationBuilder.EXTRA_INFO, TeaNodeJson.encodeToString(NotificationInfo.serializer(), open))
            .putExtra(NotificationBuilder.EXTRA_ACTION, "open")
        provideContent {
            Panel {
                Column(GlanceModifier.fillMaxSize().clickable(actionStartActivity(intent))) {
                    if (!entry.isConnected) {
                        Text(context.getString(R.string.widgets_not_connected), style = TextStyle(fontSize = 13.sp, color = GlanceTheme.colors.onSurfaceVariant))
                        return@Column
                    }
                    Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            context.getString(R.string.widgets_waiting_name),
                            GlanceModifier.defaultWeight(),
                            style = TextStyle(fontSize = 12.sp, color = if (count > 0) GlanceTheme.colors.error else GlanceTheme.colors.onSurfaceVariant),
                        )
                        Text(count.toString(), style = TextStyle(fontSize = 24.sp, fontWeight = FontWeight.Bold, color = GlanceTheme.colors.onSurface))
                    }
                    Spacer(GlanceModifier.height(4.dp))
                    if (newest != null) {
                        Text(
                            context.getString(if (newest.interactionKind == "approval") R.string.widgets_approve else R.string.widgets_asks_you),
                            style = TextStyle(fontSize = 12.sp, fontWeight = FontWeight.Bold, color = GlanceTheme.colors.error),
                        )
                        Text(newest.interactionText, style = TextStyle(fontSize = 13.sp, color = GlanceTheme.colors.onSurface), maxLines = 3)
                    } else {
                        Text(context.getString(R.string.widgets_nothing_waits), style = TextStyle(fontSize = 13.sp, color = GlanceTheme.colors.onSurfaceVariant))
                    }
                }
            }
        }
    }
}

class WaitingWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = WaitingWidget()
}

// MARK: What is next

/** NextWidget is what is next: the coming events and the unread mail. */
class NextWidget : GlanceAppWidget() {
    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val entry = readGlance(context)
        provideContent {
            Panel {
                if (!entry.isConnected) {
                    Text(context.getString(R.string.widgets_not_connected), style = TextStyle(fontSize = 13.sp, color = GlanceTheme.colors.onSurfaceVariant))
                    return@Panel
                }
                val events = entry.glance?.events.orEmpty().take(3)
                val unread = entry.glance?.unreadCount ?: 0
                Column(GlanceModifier.fillMaxSize()) {
                    Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            context.getString(R.string.widgets_next_name), GlanceModifier.defaultWeight(),
                            style = TextStyle(fontSize = 12.sp, fontWeight = FontWeight.Bold, color = GlanceTheme.colors.primary),
                        )
                        if (unread > 0) {
                            Text(
                                context.getString(R.string.widgets_unread, unread),
                                GlanceModifier.clickable(actionStartActivity(openingIntent(context, "mail/home"))),
                                style = TextStyle(fontSize = 12.sp, color = GlanceTheme.colors.onSurfaceVariant),
                            )
                        }
                    }
                    Spacer(GlanceModifier.height(6.dp))
                    if (events.isEmpty()) {
                        Text(context.getString(R.string.widgets_nothing_coming_up), style = TextStyle(fontSize = 14.sp, color = GlanceTheme.colors.onSurfaceVariant))
                    } else {
                        Column(GlanceModifier.fillMaxWidth().clickable(actionStartActivity(openingIntent(context, "calendar/home")))) {
                            events.forEach { event ->
                                EventRow(context, event)
                                Spacer(GlanceModifier.height(6.dp))
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun EventRow(context: Context, event: GlanceEvent) {
    Column(GlanceModifier.fillMaxWidth()) {
        Text(
            event.title.ifEmpty { context.getString(R.string.widgets_no_title) },
            style = TextStyle(fontSize = 14.sp, fontWeight = FontWeight.Medium, color = GlanceTheme.colors.onSurface), maxLines = 1,
        )
        Text(eventTime(context, event, System.currentTimeMillis()), style = TextStyle(fontSize = 12.sp, color = GlanceTheme.colors.onSurfaceVariant))
    }
}

/** When an event is: its day when not today, and its time or that it is all day. */
fun eventTime(context: Context, event: GlanceEvent, nowEpochMS: Long): String {
    val zone = java.time.ZoneId.systemDefault()
    val today = java.time.Instant.ofEpochMilli(nowEpochMS).atZone(zone).toLocalDate()
    val start = java.time.Instant.ofEpochMilli(event.startsAtEpochMS).atZone(zone).toLocalDate()
    val isToday = start == today || (event.startsAtEpochMS < nowEpochMS && event.endsAtEpochMS > nowEpochMS)
    val date = Date(event.startsAtEpochMS)
    return when {
        event.isAllDay && isToday -> context.getString(R.string.widgets_today_all_day)
        event.isAllDay -> android.text.format.DateUtils.formatDateTime(context, event.startsAtEpochMS, android.text.format.DateUtils.FORMAT_SHOW_WEEKDAY or android.text.format.DateUtils.FORMAT_ABBREV_ALL or android.text.format.DateUtils.FORMAT_SHOW_DATE)
        isToday -> DateFormat.getTimeInstance(DateFormat.SHORT).format(date)
        else -> android.text.format.DateUtils.formatDateTime(context, event.startsAtEpochMS, android.text.format.DateUtils.FORMAT_SHOW_WEEKDAY or android.text.format.DateUtils.FORMAT_ABBREV_ALL or android.text.format.DateUtils.FORMAT_SHOW_TIME)
    }
}

class NextWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = NextWidget()
}
