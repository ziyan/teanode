package com.teanode.app.journal

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.SwitchRow
import com.teanode.app.calendar.TimeField
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.journal.JournalEveningTime
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.LocalTime

/** What the person decides about their journal: the weather, the evening question, who reads it, and erasing it (ios JournalSettingsViews.swift). */
@Composable
fun JournalSettingsScreen() {
    val host = rememberJournalModel()
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val session = LocalAppSession.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val model = remember(connection) { JournalSettingsModel(connection, host, context) }
    var isConfirmingErase by remember { mutableStateOf(false) }
    LaunchedEffect(model) { model.load() }
    val settings = model.settings
    val loadFailure = model.loadFailure
    val eveningTime = settings?.let { found -> JournalEveningTime.parts(found.eveningQuestionTime) }?.let { parts -> LocalTime.of(parts.hour, parts.minute) } ?: LocalTime.of(21, 0)
    // A moment after the time stops changing, so a turn of the wheel is one change.
    var pickedTime by remember(eveningTime) { mutableStateOf(eveningTime) }
    LaunchedEffect(pickedTime) {
        if (pickedTime == eveningTime) return@LaunchedEffect
        delay(800)
        model.setEveningQuestionTime(JournalEveningTime.text(pickedTime.hour, pickedTime.minute))
    }
    TeaNodeScreen(title = stringResource(R.string.journal_title), onBack = { navigator.back() }) { contentPadding ->
        when {
            settings == null && loadFailure != null -> ErrorState(session.describeLoadProblem(loadFailure), onRetry = { scope.launch { model.load() } }, modifier = Modifier.padding(contentPadding))
            settings == null -> LoadingState(Modifier.padding(contentPadding))
            else -> Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState())) {
                SwitchRow(stringResource(R.string.journal_weather), settings.isWeatherOn, { isOn -> scope.launch { model.setWeather(isOn) } })
                Footnote(stringResource(R.string.journal_weather_footer))
                Heading(stringResource(R.string.journal_the_evening_question))
                SwitchRow(stringResource(R.string.journal_ask_how_the_day_was), settings.isEveningQuestionOn, { isOn -> scope.launch { model.setEveningQuestion(isOn) } })
                if (settings.isEveningQuestionOn) TimeField(stringResource(R.string.journal_time), pickedTime, { time -> pickedTime = time })
                Footnote(stringResource(R.string.journal_evening_footer))
                Heading(stringResource(R.string.journal_who_reads_it))
                Text(
                    if (settings.modelProviderName.isEmpty()) stringResource(R.string.journal_reading_text) else stringResource(R.string.journal_reading_text_provider, settings.modelProviderName),
                    style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
                JournalAction(stringResource(R.string.journal_erase_the_journal), Icons.Outlined.DeleteOutline, isDestructive = true, isEnabled = !model.isBusy) { isConfirmingErase = true }
            }
        }
    }
    if (isConfirmingErase) {
        ConfirmationDialog(
            title = stringResource(R.string.journal_erase_the_journal_question),
            text = stringResource(R.string.journal_erase_message),
            confirmLabel = stringResource(R.string.journal_erase),
            isDestructive = true,
            onConfirm = {
                isConfirmingErase = false
                scope.launch { model.erase() }
            },
            onDismiss = { isConfirmingErase = false },
        )
    }
}
