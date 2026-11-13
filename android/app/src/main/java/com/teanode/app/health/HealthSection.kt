package com.teanode.app.health

import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.AppSection
import com.teanode.kit.health.HealthTypeView
import kotlinx.coroutines.launch

/** The routes Health can open, besides its first screen. */
object HealthRoutes {
    val home: String = Routes.home(AppSection.Health)
    val settings: String = Routes.child(AppSection.Health, "settings")
    const val TYPE_ARGUMENT = "healthType"
    const val SAMPLE_ARGUMENT = "healthSampleId"
    val type: String = Routes.child(AppSection.Health, "type/{$TYPE_ARGUMENT}")
    val route: String = Routes.child(AppSection.Health, "route/{$SAMPLE_ARGUMENT}")

    fun type(healthType: String): String = Routes.child(AppSection.Health, "type/$healthType")
    fun route(healthSampleId: String): String = Routes.child(AppSection.Health, "route/$healthSampleId")
}

/** The model of the signed-in account, kept across the screens of the part. */
@Composable
fun rememberHealthModel(): HealthModel {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val context = LocalContext.current
    val text = rememberHealthText()
    val words = remember(context, text, session) {
        HealthModelWords(
            describeProblem = { error -> session.describeLoadProblem(error) },
            notSharedWithYou = context.getString(R.string.health_that_person_does_not_share),
            erasedText = { count -> context.getString(R.string.health_readings_erased, count) },
            couldNotErase = context.getString(R.string.health_could_not_erase),
            typeName = { type -> text.typeName(type.healthType, type.typeName) },
        )
    }
    val factory = remember(connection, words) { HealthModelFactory(connection, words) }
    val owner = LocalContext.current as? ComponentActivity
    return if (owner != null) viewModel(viewModelStoreOwner = owner, key = "health-" + connection.accountKey, factory = factory)
    else viewModel(key = "health-" + connection.accountKey, factory = factory)
}

/** Shows what the model wants said, in the snackbar. */
@Composable
fun HealthNoticeEffect(model: HealthModel) {
    val snackbar = LocalSnackbar.current
    val notice = model.notice
    LaunchedEffect(notice) {
        when (notice) {
            is HealthNotice.Done -> snackbar.show(notice.text)
            is HealthNotice.Problem -> snackbar.showProblem(notice.text)
            null -> Unit
        }
        if (notice != null) model.notice = null
    }
}

/** The first screen of Health: the kinds of data the person has copied to their server, grouped as the Health app groups them. */
@Composable
fun HealthSection() {
    val model = rememberHealthModel()
    val navigator = LocalAppNavigator.current
    HealthScreen(model, onOpenType = { type -> navigator.navigate(HealthRoutes.type(type.healthType)) }, onOpenSettings = { navigator.navigate(HealthRoutes.settings) })
}

@Composable
fun HealthScreen(model: HealthModel, onOpenType: (HealthTypeView) -> Unit, onOpenSettings: () -> Unit, onSetup: () -> Unit = {}) {
    val text = rememberHealthText()
    val syncing = LocalHealthSyncing.current
    val snapshot by syncing.snapshot.collectAsState()
    val scope = rememberCoroutineScope()
    var isSettingUp by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { model.load() }
    // Health turned on from this screen, a pass ending and, while nothing is shown yet, the first data reaching the server each read again, so that a new person sees their data arrive.
    LaunchedEffect(snapshot.isSyncing) { if (!snapshot.isSyncing) model.load() }
    LaunchedEffect(snapshot.sentSampleCount) { if (model.types.isEmpty()) model.load() }
    HealthNoticeEffect(model)
    TeaNodeScreen(
        title = text.title,
        actions = {
            if (model.sharers.isNotEmpty()) HealthPersonMenu(model)
            IconButton(onClick = onOpenSettings) { Icon(Icons.Outlined.Settings, contentDescription = stringResource(R.string.health_settings_title)) }
        },
    ) { padding ->
        val failure = model.loadFailedText
        when {
            failure != null -> ErrorState(failure, onRetry = { scope.launch { model.load() } }, modifier = Modifier.padding(padding))
            !model.hasLoaded -> LoadingState(Modifier.padding(padding))
            model.types.isEmpty() -> HealthEmptyView(model, snapshot, onTurnOn = { isSettingUp = true }, modifier = Modifier.padding(padding))
            else -> LazyColumn(Modifier.padding(padding).fillMaxSize()) {
                model.groups.forEach { group ->
                    item(key = "header-" + group.id) { TeaNodeSectionHeader(text.categoryName(group.healthCategory)) }
                    items(group.types, key = { type -> type.healthType }) { type -> HealthTypeRow(model, type, onClick = { onOpenType(type) }) }
                }
            }
        }
    }
    if (isSettingUp) HealthSetupSheet(onDismiss = { isSettingUp = false })
    onSetup.hashCode()
}

@Composable
fun HealthTypeRow(model: HealthModel, type: HealthTypeView, onClick: () -> Unit) {
    val text = rememberHealthText()
    TeaNodeRow(
        headline = model.name(type), supporting = type.latestDaySummary?.let { latest -> text.shortDayText(latest.summaryDay) }, onClick = onClick,
        trailing = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(text.latestAmountText(type).orEmpty(), style = MaterialTheme.typography.bodyLarge)
                Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
    )
}

/** The choice of whose Health is shown, in the bar: the person's own, or somebody who shares. */
@Composable
fun HealthPersonMenu(model: HealthModel) {
    var isOpen by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    Box {
        IconButton(onClick = { isOpen = true }) { Icon(Icons.Outlined.People, contentDescription = stringResource(R.string.health_showing)) }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.health_you)) }, leadingIcon = { if (model.selectedUserId == null) Icon(Icons.Outlined.CheckCircle, contentDescription = null) },
                onClick = { isOpen = false; scope.launch { model.select(null) } },
            )
            model.sharers.forEach { sharer ->
                DropdownMenuItem(
                    text = { Text(sharer.displayName) }, leadingIcon = { if (model.selectedUserId == sharer.userId) Icon(Icons.Outlined.CheckCircle, contentDescription = null) },
                    onClick = { isOpen = false; scope.launch { model.select(sharer.userId) } },
                )
            }
        }
    }
}

/** What the screen says when there is nothing to show: nobody shares, or Health is off, or the first data is on its way. */
@Composable
fun HealthEmptyView(model: HealthModel, snapshot: HealthSyncSnapshot, onTurnOn: () -> Unit, modifier: Modifier = Modifier) {
    val text = rememberHealthText()
    val sharer = model.selectedSharer
    if (sharer != null) {
        EmptyState(Icons.Outlined.FavoriteBorder, text.title, modifier, description = stringResource(R.string.health_shares_no_data_yet, sharer.displayName))
        return
    }
    val problem = if (snapshot.isOn) snapshot.problemText else if (snapshot.isStoppedByErase) text.erasedOnServerText else null
    val description = problem ?: stringResource(
        when {
            !snapshot.isAvailable -> R.string.health_not_available_on_device
            snapshot.isOn -> R.string.health_being_copied
            else -> R.string.health_copies_description
        },
    )
    EmptyState(
        Icons.Outlined.FavoriteBorder, stringResource(R.string.health_no_data_yet), modifier, description = description,
        actionLabel = if (snapshot.isAvailable && !snapshot.isOn) stringResource(R.string.health_turn_on) else null, onAction = onTurnOn,
    )
}

/** What the screen says to somebody whose account may not use Health. */
@Composable
fun HealthNotAllowedView(modifier: Modifier = Modifier) {
    EmptyState(Icons.Outlined.FavoriteBorder, stringResource(R.string.health_not_available_to_you), modifier, description = stringResource(R.string.health_not_allowed_description))
}

