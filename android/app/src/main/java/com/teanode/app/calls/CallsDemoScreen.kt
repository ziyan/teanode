package com.teanode.app.calls

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.calls.ChatCallAudioRoute
import com.teanode.kit.calls.ChatCallDetailView
import com.teanode.kit.calls.ChatCallTranscriptSegment

/**
 * The places of calls that demo mode reaches (debug builds): `calls/demo/<name>` draws one screen or state of a call from
 * the invented calls of [CallsDemo], so that each can be seen on a phone without a server or another person:
 * `screen`, `featured`, `chosen`, `ringing`, `reconnecting`, `incoming`, `bar`, `banner`, `lines`, `who`, `transcript`.
 */
@Composable
fun CallsDemoScreen(name: String) {
    val routes = listOf(ChatCallAudioRoute.Phone, ChatCallAudioRoute.Speaker, ChatCallAudioRoute.Bluetooth("Example Buds"))
    when (name) {
        "screen" -> ChatCallScreen(CallsDemo.callScreenSession, routes, CallActions())
        "featured" -> ChatCallScreen(CallsDemo.featuredSession, routes, CallActions())
        "chosen" -> ChatCallScreen(CallsDemo.chosenSession, routes, CallActions())
        "ringing" -> ChatCallScreen(CallsDemo.ringingOutSession, routes, CallActions())
        "reconnecting" -> ChatCallScreen(CallsDemo.reconnectingSession, routes, CallActions())
        "incoming" -> IncomingCallContent("Sam", onAnswer = {}, onDecline = {})
        "bar" -> Column(Modifier.fillMaxSize()) {
            CallBar(CallsDemo.minimizedSession, CallActions())
            Surface(Modifier.fillMaxSize()) { Text("The rest of the app", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(16.dp)) }
        }
        "banner" -> TeaNodeScreen(title = "Home") { padding -> Column(Modifier.padding(padding)) { ChatCallBannerContent(onCount = 2, onJoin = {}) } }
        "lines" -> TeaNodeScreen(title = "Sam") { padding ->
            Column(Modifier.padding(padding)) {
                (CallsDemo.directChatRecords + CallsDemo.otherRecords).forEach { record ->
                    val line = CallsDemo.lineOf(record)
                    val detail = when {
                        line.isGoing || line.isTranscriptDone -> "You and Sam"
                        record.id == "call-direct-missed" -> "Sam called"
                        else -> null
                    }
                    ChatCallLineCard(line, detail, isOnThisCall = false, onJoin = {}, onOpenTranscript = {})
                }
            }
        }
        "who" -> Surface(Modifier.fillMaxSize()) {
            ChatCallPeopleContent(
                people = listOf(ChatCallPerson("sam", "Sam", null, 5), ChatCallPerson("alex", "Alex", null, 8), ChatCallPerson("casey", "Casey", null, 1)),
                chosenUserIds = listOf("sam", "alex"), onToggle = {}, onCall = {},
            )
        }
        else -> TeaNodeScreen(title = "Sam") { padding ->
            ChatCallTranscriptList(
                ChatCallDetailView(
                    call = CallsDemo.callScreenCall,
                    transcript = listOf(
                        ChatCallTranscriptSegment("segment-1", "call-1", "robin", "Robin", 4_000, 9_000, "Are we still having the tart on Saturday?"),
                        ChatCallTranscriptSegment("segment-2", "call-1", "sam", "Sam", 10_000, 15_000, "Yes, bring two. Mom is making soup."),
                    ),
                    recordingId = "recording-1", isRecordingOwned = true,
                ),
                onOpenRecording = {}, modifier = Modifier.padding(padding),
            )
        }
    }
}
