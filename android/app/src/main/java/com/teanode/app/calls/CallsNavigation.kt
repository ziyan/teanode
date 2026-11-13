package com.teanode.app.calls

import android.net.Uri
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.BuildConfig
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The places of calls outside any part: a call's transcript, opened from the line of a call in a chat. The call
 * screen itself is not a place of the navigation; it is drawn over the app by [CallPresenter].
 */
object CallsRoutes {
    const val TRANSCRIPT = "calls/transcript/{callId}?title={title}"

    /** The address of a call's transcript. */
    fun transcript(callId: String, title: String): String = "calls/transcript/${Uri.encode(callId)}?title=${Uri.encode(title)}"
}

fun NavGraphBuilder.callsRoutes(navigator: AppNavigator) {
    composable(
        CallsRoutes.TRANSCRIPT,
        arguments = listOf(
            navArgument("callId") { type = NavType.StringType },
            navArgument("title") { type = NavType.StringType; defaultValue = "" },
        ),
        deepLinks = Routes.deepLinks(CallsRoutes.TRANSCRIPT),
    ) { entry ->
        ChatCallTranscriptScreen(
            callId = entry.arguments?.getString("callId").orEmpty(),
            title = entry.arguments?.getString("title").orEmpty(),
            onBack = { navigator.back() },
            // The person's own recording opens in Recordings; one shared with them opens on its shared screen.
            onOpenRecording = { detail ->
                val recordingId = detail.recordingId.orEmpty()
                navigator.navigate(Routes.child(AppSection.Recordings, if (detail.isRecordingOwned) "detail/$recordingId" else "shared/$recordingId"))
            },
        )
    }
    // Demo mode (debug builds) reaches each screen and state of a call from `calls/demo/<name>`, with no server or other person.
    if (BuildConfig.DEBUG) {
        composable("calls/demo/{name}", deepLinks = Routes.deepLinks("calls/demo/{name}")) { entry ->
            CallsDemoScreen(entry.arguments?.getString("name").orEmpty())
        }
    }
}

/**
 * What the app's icon menu, the widgets and the call button ask for that the chats part carries out: the call with the
 * agent starts when the chats open. A call starts only from the person's own tap. The button inside the app is one
 * ([requestCall]); the icon menu, a widget and a link arrive as an intent that any app could also send, so they only
 * [offerCall], and the person answers a question before [acceptOffer] makes it a request. The chats part clears the
 * request with [consume] once it has started the call.
 */
object OpeningRequests {
    private val callRequested = kotlinx.coroutines.flow.MutableStateFlow(false)
    private val callOffered = kotlinx.coroutines.flow.MutableStateFlow(false)

    /** Whether the person asked, with a tap inside the app, for a call with the agent. */
    val isCallRequested: kotlinx.coroutines.flow.StateFlow<Boolean> = callRequested

    /** Whether an intent asked for a call and the person has not yet said yes. */
    val isCallOffered: kotlinx.coroutines.flow.StateFlow<Boolean> = callOffered

    fun requestCall() {
        callRequested.value = true
    }

    /** A call asked for by an intent: nothing starts until the person accepts. */
    fun offerCall() {
        callOffered.value = true
    }

    fun acceptOffer() {
        if (callOffered.value) callRequested.value = true
        callOffered.value = false
    }

    fun declineOffer() {
        callOffered.value = false
    }

    fun consume() {
        callRequested.value = false
    }
}
