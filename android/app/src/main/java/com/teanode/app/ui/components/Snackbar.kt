package com.teanode.app.ui.components

import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.Spacer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.TextButton
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.SnackbarVisuals
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.teanode.app.session.AppSession
import com.teanode.app.session.ErrorWords
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/** What a snackbar says, and whether it is a problem. */
class TeaNodeSnackbarVisuals(
    override val message: String,
    val isProblem: Boolean,
    override val actionLabel: String? = null,
) : SnackbarVisuals {
    // A problem stays for ten seconds and a success for four, as the dashboard's toasts do.
    override val duration: SnackbarDuration = if (isProblem) SnackbarDuration.Long else SnackbarDuration.Short
    override val withDismissAction: Boolean = isProblem
}

/**
 * SnackbarController is how any screen tells the person something done or something wrong: errors and successes show
 * as a snackbar over the bottom of the screen, never as text stuck in a card. One lives in the shell; screens reach it
 * through [LocalSnackbar].
 */
class SnackbarController(val hostState: SnackbarHostState, private val scope: CoroutineScope, private val session: AppSession?) {
    /** Says something was done. */
    fun show(message: String, actionLabel: String? = null, onAction: (() -> Unit)? = null) = post(message, false, actionLabel, onAction)

    /** Says something went wrong. */
    fun showProblem(message: String) = post(message, true, null, null)

    /** Says why a call failed, in words that tell the person what to do next. A request called off says nothing. */
    fun showError(error: Throwable) {
        if (ErrorWords.isCancellation(error)) return
        showProblem(session?.describe(error) ?: error.message.orEmpty())
    }

    private fun post(message: String, isProblem: Boolean, actionLabel: String?, onAction: (() -> Unit)?) {
        if (message.isBlank()) return
        scope.launch {
            // A new message replaces the one on screen rather than queueing behind it.
            hostState.currentSnackbarData?.dismiss()
            val result = hostState.showSnackbar(TeaNodeSnackbarVisuals(message, isProblem, actionLabel))
            if (result == SnackbarResult.ActionPerformed) onAction?.invoke()
        }
    }
}

val LocalSnackbar = staticCompositionLocalOf<SnackbarController> { error("no SnackbarController: draw the screen inside AppRoot or DemoHost") }

@Composable
fun rememberSnackbarController(session: AppSession?): SnackbarController {
    val hostState = remember { SnackbarHostState() }
    val scope = androidx.compose.runtime.rememberCoroutineScope()
    return remember(session) { SnackbarController(hostState, scope, session) }
}

/** The snackbars of [LocalSnackbar], drawn in a scaffold's `snackbarHost` slot. */
@Composable
fun TeaNodeSnackbarHost(controller: SnackbarController = LocalSnackbar.current, modifier: Modifier = Modifier) {
    SnackbarHost(controller.hostState, modifier) { data ->
        val visuals = data.visuals as? TeaNodeSnackbarVisuals
        val isProblem = visuals?.isProblem == true
        val contentColor = if (isProblem) MaterialTheme.colorScheme.onErrorContainer else MaterialTheme.colorScheme.inverseOnSurface
        val actionLabel = data.visuals.actionLabel
        Snackbar(
            modifier = Modifier.padding(12.dp),
            action = actionLabel?.let { label ->
                {
                    TextButton(onClick = { data.performAction() }) {
                        Text(label, color = if (isProblem) contentColor else MaterialTheme.colorScheme.inversePrimary)
                    }
                }
            },
            dismissAction = if (data.visuals.withDismissAction) {
                {
                    IconButton(onClick = { data.dismiss() }) {
                        Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.common_dismiss))
                    }
                }
            } else {
                null
            },
            containerColor = if (isProblem) MaterialTheme.colorScheme.errorContainer else MaterialTheme.colorScheme.inverseSurface,
            contentColor = contentColor,
            dismissActionContentColor = contentColor,
        ) {
            // The symbol beside the words: red alone never says "failed".
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(if (isProblem) Icons.Outlined.WarningAmber else Icons.Outlined.CheckCircle, contentDescription = null)
                Spacer(Modifier.width(12.dp))
                Text(data.visuals.message)
            }
        }
    }
}
