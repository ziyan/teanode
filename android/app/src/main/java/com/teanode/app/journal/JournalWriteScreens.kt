package com.teanode.app.journal

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoFixHigh
import androidx.compose.material.icons.outlined.HighlightOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.journal.JournalPeriodKind
import com.teanode.kit.journal.JournalTarget
import com.teanode.kit.journal.JournalWords
import com.teanode.kit.journal.correctJournalNarrative
import com.teanode.kit.journal.journalDay
import com.teanode.kit.journal.journalPeriod
import com.teanode.kit.journal.setJournalWords
import com.teanode.kit.journal.stopJournalTouchUp
import com.teanode.kit.journal.touchUpJournalWords
import kotlinx.coroutines.launch

/** The words of a day or of a period's looking back, as the server holds them now (null words are no words yet), or why they could not be read. */
@Composable
fun JournalWriteScreen(periodKind: JournalPeriodKind?, day: String) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val navigator = LocalAppNavigator.current
    val target: JournalTarget = if (periodKind == null) JournalTarget.Day(day) else JournalTarget.Period(periodKind, day)
    var words by remember(target) { mutableStateOf<JournalWords?>(null) }
    var isRead by remember(target) { mutableStateOf(false) }
    var failure by remember(target) { mutableStateOf<Throwable?>(null) }
    var attempt by remember { mutableStateOf(0) }
    LaunchedEffect(target, attempt) {
        failure = null
        try {
            words = if (periodKind == null) connection.journalDay(day).words else connection.journalPeriod(periodKind, day).lookingBack
            isRead = true
        } catch (error: kotlinx.coroutines.CancellationException) {
            throw error
        } catch (error: Throwable) {
            failure = error
        }
    }
    val problem = failure
    when {
        isRead -> JournalWriteEditor(target, words)
        problem != null -> TeaNodeScreen(title = stringResource(R.string.journal_your_words), onBack = { navigator.back() }) { contentPadding ->
            ErrorState(session.describeLoadProblem(problem), onRetry = { attempt += 1 }, modifier = Modifier.padding(contentPadding))
        }
        else -> TeaNodeScreen(title = stringResource(R.string.journal_your_words), onBack = { navigator.back() }) { contentPadding -> LoadingState(Modifier.padding(contentPadding)) }
    }
}

/**
 * Where the person writes the original of a day's words or a period's looking back, asks TeaNode to touch it up, and
 * saves. Saving sends the time the words were read at, so that a day saved somewhere else in between is refused rather
 * than overwritten (ios JournalWriteViews.swift, JournalWriteSheet).
 */
@Composable
fun JournalWriteEditor(target: JournalTarget, request: JournalWords?) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val session = LocalAppSession.current
    val host = rememberJournalModel()
    val scope = rememberCoroutineScope()
    var original by remember { mutableStateOf(request?.original.orEmpty()) }
    // The original as last saved, and the time it was saved at, which a save sends so that a day changed somewhere else is not overwritten.
    var savedOriginal by remember { mutableStateOf(request?.original.orEmpty()) }
    var baseModifiedAt by remember { mutableStateOf(request?.modifiedAt) }
    var how by remember { mutableStateOf(request?.touchUpHow.orEmpty()) }
    var isTouchUpOn by remember { mutableStateOf(request?.isTouchUpOn ?: false) }
    var isWorking by remember { mutableStateOf(false) }
    var isConfirmingDiscard by remember { mutableStateOf(false) }
    val hasChanges = original != savedOriginal
    val isForDay = target is JournalTarget.Day
    val couldNotSave = stringResource(R.string.journal_could_not_save_words)
    val couldNotTouchUp = stringResource(R.string.journal_could_not_ask_touch_up)
    val couldNotStop = stringResource(R.string.journal_could_not_stop_touch_up)

    fun close() {
        if (hasChanges) isConfirmingDiscard = true else navigator.back()
    }
    BackHandler(enabled = hasChanges || isWorking) { if (!isWorking) close() }

    /** Saves the original when it differs from the one that was read; whether it is saved now (or needed no saving). */
    suspend fun saveOriginal(): Boolean {
        if (!hasChanges) return true
        isWorking = true
        return try {
            val savedAt = connection.setJournalWords(target, original, baseModifiedAt)
            savedOriginal = original
            if (savedAt != null) baseModifiedAt = savedAt
            true
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            host.complain(reasonOrSay(failure, couldNotSave))
            false
        } finally {
            isWorking = false
        }
    }

    TeaNodeScreen(
        title = stringResource(if (isForDay) R.string.journal_your_words else R.string.journal_looking_back),
        onBack = { if (!isWorking) close() },
        actions = {
            if (isWorking) {
                CircularProgressIndicator(Modifier.padding(end = 16.dp).heightIn(max = 24.dp), strokeWidth = 2.dp)
            } else {
                TextButton(enabled = hasChanges, onClick = { scope.launch { if (saveOriginal()) { host.refreshToday(); navigator.back() } } }) { Text(stringResource(R.string.journal_save)) }
            }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Heading(stringResource(R.string.journal_your_original))
            OutlinedTextField(
                value = original, onValueChange = { text -> original = text }, enabled = !isWorking,
                textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace),
                modifier = Modifier.fillMaxWidth().heightIn(min = 220.dp).padding(horizontal = 16.dp),
            )
            Footnote(stringResource(if (isForDay) R.string.journal_original_footer_day else R.string.journal_your_own_words_about_stretch))
            Heading(stringResource(R.string.journal_touched_up))
            OutlinedTextField(
                value = how, onValueChange = { text -> how = text }, enabled = !isWorking, label = { Text(stringResource(R.string.journal_how_hint)) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp), singleLine = true,
            )
            JournalAction(stringResource(R.string.journal_touch_up), Icons.Outlined.AutoFixHigh, isEnabled = !isWorking && original.isNotBlank()) {
                scope.launch {
                    // Saves the original and asks for it to be touched up, then closes: the touched-up version arrives while the day is on screen.
                    if (!saveOriginal()) return@launch
                    isWorking = true
                    try {
                        connection.touchUpJournalWords(target, how.trim().ifEmpty { null })
                        host.refreshToday()
                        navigator.back()
                    } catch (failure: kotlinx.coroutines.CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        host.complain(reasonOrSay(failure, couldNotTouchUp))
                    }
                    isWorking = false
                }
            }
            if (isTouchUpOn) {
                JournalAction(stringResource(R.string.journal_stop_touching_up), Icons.Outlined.HighlightOff, isEnabled = !isWorking) {
                    scope.launch {
                        isWorking = true
                        try {
                            connection.stopJournalTouchUp(target)
                            isTouchUpOn = false
                        } catch (failure: kotlinx.coroutines.CancellationException) {
                            throw failure
                        } catch (failure: Throwable) {
                            host.complain(reasonOrSay(failure, couldNotStop))
                        }
                        isWorking = false
                    }
                }
            }
            Footnote(stringResource(R.string.journal_touch_up_footer))
        }
    }
    if (isConfirmingDiscard) {
        ConfirmationDialog(
            title = stringResource(R.string.journal_discard_what_you_wrote),
            confirmLabel = stringResource(R.string.journal_discard),
            isDestructive = true,
            onConfirm = {
                isConfirmingDiscard = false
                navigator.back()
            },
            onDismiss = { isConfirmingDiscard = false },
        )
    }
}

/**
 * JournalCorrectionScreen takes what the person says a story got wrong. It is kept with the day or the period and given
 * to every later writing of the story, which is written again at once (ios JournalWriteViews.swift, JournalCorrectionSheet).
 */
@Composable
fun JournalCorrectionScreen(periodKind: JournalPeriodKind?, day: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val host = rememberJournalModel()
    val scope = rememberCoroutineScope()
    val target: JournalTarget = if (periodKind == null) JournalTarget.Day(day) else JournalTarget.Period(periodKind, day)
    var correctionText by remember { mutableStateOf("") }
    var isWorking by remember { mutableStateOf(false) }
    val couldNotSend = stringResource(R.string.journal_could_not_send_correction)
    val trimmed = correctionText.trim()
    val isSendable = trimmed.isNotEmpty() && trimmed.length <= CORRECTION_LIMIT
    TeaNodeScreen(
        title = stringResource(R.string.journal_something_wrong), onBack = { if (!isWorking) navigator.back() },
        actions = {
            if (isWorking) CircularProgressIndicator(Modifier.padding(end = 16.dp).heightIn(max = 24.dp), strokeWidth = 2.dp)
            else TextButton(enabled = isSendable, onClick = {
                scope.launch {
                    isWorking = true
                    try {
                        connection.correctJournalNarrative(target, trimmed)
                        navigator.back()
                    } catch (failure: kotlinx.coroutines.CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        host.complain(reasonOrSay(failure, couldNotSend))
                    }
                    isWorking = false
                }
            }) { Text(stringResource(R.string.journal_send)) }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState())) {
            Heading(stringResource(R.string.journal_what_is_wrong))
            OutlinedTextField(
                value = correctionText, onValueChange = { text -> correctionText = text }, enabled = !isWorking,
                modifier = Modifier.fillMaxWidth().heightIn(min = 140.dp).padding(horizontal = 16.dp),
                supportingText = { Text("${trimmed.length}/$CORRECTION_LIMIT") }, isError = trimmed.length > CORRECTION_LIMIT,
            )
            Footnote(stringResource(R.string.journal_correction_footer))
        }
    }
}

/** The longest a correction can be, as the server takes it. */
private const val CORRECTION_LIMIT = 500
