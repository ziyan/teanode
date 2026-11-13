package com.teanode.app.health

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.PersonAdd
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.health.connect.client.PermissionController
import com.teanode.app.R
import com.teanode.app.location.ShareCandidates
import com.teanode.app.location.ShareTargetPicker
import com.teanode.app.location.readShareCandidates
import com.teanode.app.location.shareTargetName
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.RelativeTime
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.Connection
import com.teanode.kit.accessSession
import com.teanode.kit.health.HealthCategory
import com.teanode.kit.health.HealthSettingsView
import com.teanode.kit.health.HealthShareView
import com.teanode.kit.health.HealthTypes
import com.teanode.kit.health.healthSettings
import com.teanode.kit.health.healthShares
import com.teanode.kit.health.setHealthShare
import com.teanode.kit.health.updateHealthSettings
import com.teanode.kit.location.ShareTarget
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** A fact about the person, shown in the settings. */
data class HealthAboutRow(val healthType: String, val labelText: String, val valueText: String)

/** The Health settings screen's state: what the server holds, the person's shares, and what has been said. */
class HealthSettingsState(private val connection: Connection, private val describeProblem: (Throwable) -> String, private val text: HealthText, private val say: (String, Boolean) -> Unit) {
    var settings by mutableStateOf<HealthSettingsView?>(null)
        private set
    var shares by mutableStateOf<List<HealthShareView>>(emptyList())
        private set
    var ownUserId by mutableStateOf<String?>(null)
        private set
    var hasLoaded by mutableStateOf(false)
        private set
    var hasReadShares by mutableStateOf(false)
        private set
    var loadFailedText by mutableStateOf<String?>(null)
        private set
    var isBusy by mutableStateOf(false)
        private set

    val ownShares: List<HealthShareView> get() = ownUserId?.let { own -> shares.filter { share -> share.ownerUserId == own } } ?: emptyList()
    val isOwnSharesKnown: Boolean get() = hasReadShares && ownUserId != null

    val aboutRows: List<HealthAboutRow>
        get() {
            val characteristics = settings?.characteristicList.orEmpty()
            return ABOUT_HEALTH_TYPES.mapNotNull { healthType ->
                val found = characteristics.firstOrNull { each -> each.healthType == healthType } ?: return@mapNotNull null
                HealthAboutRow(healthType, text.typeName(healthType, HealthTypes.typeName(healthType)), text.characteristicText(healthType, found.characteristicValue))
            }
        }

    suspend fun load() {
        if (ownUserId == null) runCatching { connection.accessSession() }.getOrNull()?.let { session -> ownUserId = session.userId }
        try {
            settings = connection.healthSettings()
            loadFailedText = null
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            if (settings == null) loadFailedText = describeProblem(error) else say(describeProblem(error), true)
        }
        runCatching { connection.healthShares() }.getOrNull()?.let { read -> shares = read; hasReadShares = true }
        hasLoaded = true
    }

    suspend fun setAgentCanRead(canRead: Boolean, saidOn: String, saidOff: String, couldNotSave: String) {
        isBusy = true
        try {
            settings = connection.updateHealthSettings(canRead)
            say(if (canRead) saidOn else saidOff, false)
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            say((error as? com.teanode.kit.TeaNodeError.Refused)?.reason ?: couldNotSave, true)
        } finally {
            isBusy = false
        }
    }

    /** Shares the categories with a person or a circle; an empty list takes the share back. Null when it worked, else the reason. */
    suspend fun setShare(target: ShareTarget, name: String, categories: List<HealthCategory>, savedText: (String) -> String, stoppedText: (String) -> String, couldNotSave: String): String? {
        isBusy = true
        try {
            connection.setHealthShare(target.granteeUserId, target.granteeCircleId, categories)
            say(if (categories.isEmpty()) stoppedText(name) else savedText(name), false)
            runCatching { connection.healthShares() }.getOrNull()?.let { read -> shares = read }
            return null
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            val refusal = (error as? com.teanode.kit.TeaNodeError.Refused)?.reason ?: couldNotSave
            say(refusal, true)
            return refusal
        } finally {
            isBusy = false
        }
    }

    companion object {
        private val ABOUT_HEALTH_TYPES = listOf("dateOfBirth", "biologicalSex", "bloodType", "fitzpatrickSkinType", "wheelchairUse", "activityMoveMode")
    }
}

/** Health in Settings: the switch that copies Health Connect data to the server, what has been sent, whether the agent may read it, who it is shared with, and erasing it. */
@Composable
fun HealthSettingsScreen(onBack: () -> Unit, onOpenShare: (HealthShareView) -> Unit = {}, shareToOpen: HealthShareView? = null) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val text = rememberHealthText()
    val syncing = LocalHealthSyncing.current
    val snapshot by syncing.snapshot.collectAsState()
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val state = remember(connection) { HealthSettingsState(connection, { error -> session.describeLoadProblem(error) }, text) { message, isProblem -> if (isProblem) snackbar.showProblem(message) else snackbar.show(message) } }
    var isSettingUp by remember { mutableStateOf(false) }
    var isSharing by remember { mutableStateOf(false) }
    var isConfirmingErase by remember { mutableStateOf(false) }
    var openedShare by remember { mutableStateOf(shareToOpen) }
    LaunchedEffect(Unit) { state.load() }
    // What the server holds changes as the copying sends data.
    LaunchedEffect(snapshot.isSyncing) { if (!snapshot.isSyncing) state.load() }
    val saidAgentOn = stringResource(R.string.health_agent_can_read_now)
    val saidAgentOff = stringResource(R.string.health_agent_can_no_longer_read)
    val couldNotSave = stringResource(R.string.health_could_not_save_setting)
    val erased = stringResource(R.string.health_data_erased)
    val couldNotErase = stringResource(R.string.health_could_not_erase)

    val share = openedShare
    if (share != null) {
        HealthShareScreen(state, share, onBack = { openedShare = null })
        return
    }
    TeaNodeScreen(title = text.title, onBack = onBack) { padding ->
        LazyColumn(Modifier.padding(padding).fillMaxSize()) {
            val failure = state.loadFailedText
            if (failure != null && state.settings == null) {
                item {
                    Row(Modifier.fillMaxWidth().padding(16.dp)) { Icon(Icons.Outlined.Warning, contentDescription = null); Text(failure, Modifier.padding(start = 12.dp)) }
                    TextButton(onClick = { scope.launch { state.load() } }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.common_try_again)) }
                }
            }
            item {
                Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(text.title, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                    Switch(checked = snapshot.isOn, enabled = snapshot.isAvailable, onCheckedChange = { isOn -> if (isOn) isSettingUp = true else syncing.turnOff() })
                }
                if (snapshot.isAvailable) {
                    // Why the switch is off, even though the person did not turn it off: the server was erased from somewhere else.
                    if (!snapshot.isOn && snapshot.isStoppedByErase) Footer(text.erasedOnServerText)
                    Footer(stringResource(R.string.health_copying_footer))
                } else {
                    Footer(stringResource(R.string.health_not_available_on_device))
                }
                HorizontalDivider()
            }
            if (snapshot.isOn) item { HealthStatusSection(snapshot, onSendNow = { scope.launch { syncing.syncNow() } }) }
            state.settings?.let { serverSettings ->
                item {
                    Text(
                        when (serverSettings.sampleCount) {
                            0 -> stringResource(R.string.health_no_readings_kept)
                            else -> pluralStringResource(R.plurals.health_readings_kept, serverSettings.sampleCount, "%,d".format(serverSettings.sampleCount))
                        },
                        Modifier.padding(horizontal = 16.dp, vertical = 12.dp),
                    )
                    serverSettings.lastReceivedInstant?.let { received ->
                        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
                            Text(stringResource(R.string.health_last_received), Modifier.weight(1f))
                            Text(RelativeTime.text(androidx.compose.ui.platform.LocalContext.current, received.toEpochMilli()), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    HorizontalDivider(Modifier.padding(top = 8.dp))
                }
            }
            item {
                TeaNodeSectionHeader(stringResource(R.string.health_the_agent))
                Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.health_let_agent_read), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                    Switch(
                        checked = state.settings?.canAgentReadHealth ?: false, enabled = state.settings != null && !state.isBusy,
                        onCheckedChange = { canRead -> scope.launch { state.setAgentCanRead(canRead, saidAgentOn, saidAgentOff, couldNotSave) } },
                    )
                }
                val provider = state.settings?.modelProviderName.orEmpty()
                val ownOnly = stringResource(R.string.health_agent_reads_only_your_own)
                Footer(if (provider.isEmpty()) stringResource(R.string.health_agent_goes_to_provider) + " " + ownOnly else stringResource(R.string.health_agent_goes_to, provider) + " " + ownOnly)
            }
            val aboutRows = state.aboutRows
            if (aboutRows.isNotEmpty()) {
                item { TeaNodeSectionHeader(stringResource(R.string.health_about_you)) }
                items(aboutRows.size) { index ->
                    val row = aboutRows[index]
                    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
                        Text(row.labelText, Modifier.weight(1f))
                        Text(row.valueText, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                item { Footer(stringResource(R.string.health_about_footer)) }
            }
            item {
                TeaNodeSectionHeader(stringResource(R.string.health_sharing))
                if (state.isOwnSharesKnown && state.ownShares.isEmpty()) Text(stringResource(R.string.health_you_do_not_share), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            items(state.ownShares.size) { index ->
                val ownShare = state.ownShares[index]
                Column(Modifier.fillMaxWidth().clickable { openedShare = ownShare }.padding(horizontal = 16.dp, vertical = 10.dp)) {
                    Text(HealthSharing.name(ownShare, text), style = MaterialTheme.typography.bodyLarge)
                    Text(HealthSharing.categoriesText(ownShare, text), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            item {
                TextButton(onClick = { isSharing = true }, modifier = Modifier.padding(horizontal = 8.dp)) {
                    Icon(Icons.Outlined.PersonAdd, contentDescription = null)
                    Text(stringResource(R.string.health_share), Modifier.padding(start = 8.dp))
                }
                Footer(stringResource(R.string.health_sharing_footer))
                HorizontalDivider()
            }
            item {
                TextButton(onClick = { isConfirmingErase = true }, enabled = !state.isBusy, modifier = Modifier.padding(horizontal = 8.dp, vertical = 8.dp)) {
                    Icon(Icons.Outlined.Delete, contentDescription = null, tint = MaterialTheme.colorScheme.error)
                    Text(stringResource(R.string.health_erase_all), Modifier.padding(start = 8.dp), color = MaterialTheme.colorScheme.error)
                }
            }
        }
    }
    if (isSettingUp) HealthSetupSheet(onDismiss = { isSettingUp = false })
    if (isSharing) HealthShareSheet(state, onDismiss = { isSharing = false })
    if (isConfirmingErase) {
        ConfirmationDialog(
            title = stringResource(R.string.health_erase_all_title), text = stringResource(R.string.health_erase_all_explanation), confirmLabel = stringResource(R.string.health_erase),
            isDestructive = true, onDismiss = { isConfirmingErase = false },
            onConfirm = {
                isConfirmingErase = false
                scope.launch {
                    try {
                        syncing.eraseEverything()
                        snackbar.show(erased)
                        state.load()
                    } catch (error: Exception) {
                        if (error is CancellationException) throw error
                        snackbar.showProblem((error as? com.teanode.kit.TeaNodeError.Refused)?.reason ?: couldNotErase)
                    }
                }
            },
        )
    }
}

@Composable
private fun Footer(message: String) {
    Text(message, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
private fun HealthStatusSection(snapshot: HealthSyncSnapshot, onSendNow: () -> Unit) {
    val context = androidx.compose.ui.platform.LocalContext.current
    Column(Modifier.fillMaxWidth()) {
        TeaNodeSectionHeader(stringResource(R.string.health_status))
        StatusRow(stringResource(R.string.health_last_sent), snapshot.lastSentAt?.let { sent -> RelativeTime.text(context, sent.toEpochMilli()) } ?: stringResource(R.string.health_not_yet))
        StatusRow(stringResource(R.string.health_readings_sent), "%,d".format(snapshot.sentSampleCount))
        val progress = snapshot.passProgress
        if (progress != null) {
            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(stringResource(R.string.health_reading_your_data))
                LinearProgressIndicator(progress = { progress.typesDone.toFloat() / maxOf(1, progress.typesTotal) }, modifier = Modifier.fillMaxWidth())
                Text(stringResource(R.string.health_kinds_done, progress.typesDone, progress.typesTotal), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        } else if (!snapshot.isHistoryDone) {
            StatusRow(stringResource(R.string.health_older_readings), stringResource(R.string.health_still_being_read))
        }
        if (snapshot.pendingDayCount > 0) StatusRow(stringResource(R.string.health_days_to_recalculate), snapshot.pendingDayCount.toString())
        if (snapshot.droppedEntryCount > 0) StatusRow(stringResource(R.string.health_left_out), snapshot.droppedEntryCount.toString())
        snapshot.problemText?.let { problem -> Row(Modifier.fillMaxWidth().padding(16.dp)) { Icon(Icons.Outlined.Warning, contentDescription = null); Text(problem, Modifier.padding(start = 12.dp)) } }
        TextButton(onClick = onSendNow, enabled = !snapshot.isSyncing, modifier = Modifier.padding(horizontal = 8.dp)) {
            Icon(Icons.Outlined.Sync, contentDescription = null)
            Text(stringResource(if (snapshot.isSyncing) R.string.health_sending else R.string.health_send_now), Modifier.padding(start = 8.dp))
        }
        if (snapshot.droppedEntryCount > 0) Footer(stringResource(R.string.health_left_out_footer))
        if (snapshot.isHistoryDone && snapshot.sentSampleCount == 0) Footer(stringResource(R.string.health_missing_readings_footer))
        HorizontalDivider()
    }
}

@Composable
private fun StatusRow(label: String, value: String) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Text(label, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Text(value, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The words for who a Health share is with and what it holds. */
object HealthSharing {
    fun name(share: HealthShareView, text: HealthText): String {
        if (share.granteeCircleId != null) return text.everyoneIn(share.granteeCircleName.orEmpty())
        if (!share.granteeName.isNullOrEmpty()) return share.granteeName.orEmpty()
        return share.granteeUsername.orEmpty()
    }

    fun categoriesText(share: HealthShareView, text: HealthText): String = share.sharedCategories.joinToString(", ") { category -> text.categoryName(category) }
}

/** The toggles of the Health app's groups. */
@Composable
fun HealthCategoryToggles(chosen: Set<HealthCategory>, isDisabled: Boolean = false, onChange: (Set<HealthCategory>, Set<HealthCategory>) -> Unit) {
    val text = rememberHealthText()
    Column {
        HealthCategory.entries.forEach { category ->
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(text.categoryName(category), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                Switch(
                    checked = category in chosen, enabled = !isDisabled,
                    onCheckedChange = { isOn -> onChange(chosen, if (isOn) chosen + category else chosen - category) },
                )
            }
        }
    }
}

/** One share: which groups it holds, changed where they are shown, and a way to stop it. */
@Composable
fun HealthShareScreen(state: HealthSettingsState, share: HealthShareView, onBack: () -> Unit) {
    val text = rememberHealthText()
    val scope = rememberCoroutineScope()
    val name = HealthSharing.name(share, text)
    val target = share.granteeCircleId?.let { id -> ShareTarget.Circle(id) } ?: ShareTarget.Person(share.granteeUserId.orEmpty())
    var chosen by remember(share.id) { mutableStateOf(share.sharedCategories.toSet()) }
    var isConfirmingStop by remember { mutableStateOf(false) }
    val saved = { who: String -> text.sharingSaved(who) }
    val stopped = { who: String -> text.stoppedSharing(who) }
    TeaNodeScreen(title = name, onBack = onBack) { padding ->
        Column(Modifier.padding(padding).fillMaxSize().verticalScroll(rememberScrollState())) {
            TeaNodeSectionHeader(stringResource(R.string.health_shared_with, name))
            HealthCategoryToggles(chosen, isDisabled = state.isBusy) { previous, updated ->
                chosen = updated
                scope.launch {
                    val refusal = state.setShare(target, name, HealthCategory.entries.filter { category -> category in updated }, saved, stopped, text.couldNotSaveSharing)
                    if (refusal != null) chosen = previous
                }
            }
            Text(stringResource(R.string.health_share_footer), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            TextButton(onClick = { isConfirmingStop = true }, modifier = Modifier.padding(horizontal = 8.dp)) {
                Text(stringResource(R.string.health_stop_sharing), color = MaterialTheme.colorScheme.error)
            }
        }
    }
    if (isConfirmingStop) {
        ConfirmationDialog(
            title = stringResource(R.string.health_stop_sharing), text = stringResource(R.string.health_will_no_longer_see, name), confirmLabel = stringResource(R.string.health_stop_sharing),
            isDestructive = true, onDismiss = { isConfirmingStop = false },
            onConfirm = { isConfirmingStop = false; scope.launch { state.setShare(target, name, emptyList(), saved, stopped, text.couldNotSaveSharing); onBack() } },
        )
    }
}

/** Shares Health with a person or a circle, by group. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HealthShareSheet(state: HealthSettingsState, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val text = rememberHealthText()
    val scope = rememberCoroutineScope()
    var candidates by remember { mutableStateOf<ShareCandidates?>(null) }
    var loadProblem by remember { mutableStateOf<String?>(null) }
    var chosenTarget by remember { mutableStateOf<ShareTarget?>(null) }
    var chosenCategories by remember { mutableStateOf<Set<HealthCategory>>(emptySet()) }
    var isWorking by remember { mutableStateOf(false) }
    suspend fun load() {
        loadProblem = null
        try {
            candidates = readShareCandidates(connection)
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            loadProblem = session.describeLoadProblem(error)
        }
    }
    LaunchedEffect(Unit) { load() }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(bottom = 24.dp)) {
            Text(stringResource(R.string.health_share_health), Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.titleLarge)
            val ready = candidates
            when {
                loadProblem != null && ready == null -> ErrorState(loadProblem.orEmpty(), onRetry = { scope.launch { load() } })
                ready == null -> Column(Modifier.fillMaxWidth().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally) { CircularProgressIndicator() }
                ready.isEmpty -> Text(stringResource(R.string.health_not_in_a_circle), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                else -> {
                    ShareTargetPicker(ready, chosenTarget, onChoose = { chosenTarget = it }, modifier = Modifier.padding(16.dp))
                    TeaNodeSectionHeader(stringResource(R.string.health_what_they_see))
                    HealthCategoryToggles(chosenCategories) { _, updated -> chosenCategories = updated }
                    Text(stringResource(R.string.health_share_footer), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), horizontalArrangement = Arrangement.End) {
                        TextButton(onClick = onDismiss, enabled = !isWorking) { Text(stringResource(R.string.common_cancel)) }
                        Button(
                            enabled = chosenTarget != null && chosenCategories.isNotEmpty() && !isWorking,
                            onClick = {
                                val target = chosenTarget ?: return@Button
                                isWorking = true
                                scope.launch {
                                    val name = shareTargetName(target, ready, text::everyoneIn)
                                    val refusal = state.setShare(target, name, HealthCategory.entries.filter { category -> category in chosenCategories }, { who -> text.sharingSaved(who) }, { who -> text.stoppedSharing(who) }, text.couldNotSaveSharing)
                                    isWorking = false
                                    if (refusal == null) onDismiss()
                                }
                            },
                        ) { Text(stringResource(R.string.health_share)) }
                    }
                }
            }
        }
    }
}

/** Turning Health on the first time: what is copied, where it goes and who reads it, before Health Connect asks which data TeaNode may read. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HealthSetupSheet(onDismiss: () -> Unit) {
    val syncing = LocalHealthSyncing.current
    val scope = rememberCoroutineScope()
    var isTurningOn by remember { mutableStateOf(false) }
    var problem by remember { mutableStateOf<String?>(null) }
    val couldNotTurnOn = stringResource(R.string.health_could_not_turn_on)
    val launcher = rememberLauncherForActivityResult(PermissionController.createRequestPermissionResultContract()) { granted ->
        scope.launch {
            val didTurnOn = syncing.turnOn(granted)
            isTurningOn = false
            if (didTurnOn) onDismiss() else problem = syncing.snapshot.value.problemText ?: couldNotTurnOn
        }
    }
    ModalBottomSheet(onDismissRequest = { if (!isTurningOn) onDismiss() }) {
        HealthSetupContent(
            isTurningOn = isTurningOn, problemText = problem,
            onContinue = {
                isTurningOn = true
                problem = null
                val permissions = syncing.requiredPermissions
                if (permissions.isEmpty()) {
                    scope.launch { syncing.turnOn(emptySet()); isTurningOn = false; onDismiss() }
                } else {
                    launcher.launch(permissions)
                }
            },
            onCancel = onDismiss,
        )
    }
}

@Composable
fun HealthSetupContent(isTurningOn: Boolean, problemText: String?, onContinue: () -> Unit, onCancel: () -> Unit) {
    Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(20.dp)) {
        Icon(Icons.Outlined.FavoriteBorder, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
        Text(stringResource(R.string.health_setup_title), style = MaterialTheme.typography.titleLarge)
        SetupPoint(Icons.Outlined.CloudUpload, stringResource(R.string.health_setup_what_title), stringResource(R.string.health_setup_what_text))
        SetupPoint(Icons.Outlined.Dns, stringResource(R.string.health_setup_where_title), stringResource(R.string.health_setup_where_text))
        SetupPoint(Icons.Outlined.AutoAwesome, stringResource(R.string.health_setup_who_title), stringResource(R.string.health_setup_who_text))
        Text(stringResource(R.string.health_setup_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        problemText?.let { problem -> Row { Icon(Icons.Outlined.Warning, contentDescription = null); Text(problem, Modifier.padding(start = 12.dp)) } }
        Button(onClick = onContinue, enabled = !isTurningOn, modifier = Modifier.fillMaxWidth()) {
            if (isTurningOn) CircularProgressIndicator(Modifier.width(20.dp), strokeWidth = 2.dp) else Text(stringResource(R.string.health_continue))
        }
        OutlinedButton(onClick = onCancel, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_cancel)) }
    }
}

@Composable
private fun SetupPoint(icon: ImageVector, title: String, text: String) {
    Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
        Icon(icon, contentDescription = null, modifier = Modifier.width(32.dp))
        Column {
            Text(title, style = MaterialTheme.typography.titleMedium)
            Text(text, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}
