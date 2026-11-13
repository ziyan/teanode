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
            // The recording is in Recordings, which opens it there; one shared with the person is shown there as well.
            onOpenRecording = { navigator.openSection(AppSection.Recordings) },
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
 * What the app's icon menu and the widgets ask for that the chats part carries out: the call with the agent starts when
 * the chats open. The chats part clears the request with [consume] once it has started the call.
 */
object OpeningRequests {
    private val callRequested = kotlinx.coroutines.flow.MutableStateFlow(false)

    /** Whether the person asked, from the icon or a widget, for a call with the agent. */
    val isCallRequested: kotlinx.coroutines.flow.StateFlow<Boolean> = callRequested

    fun requestCall() {
        callRequested.value = true
    }

    fun consume() {
        callRequested.value = false
    }
}
