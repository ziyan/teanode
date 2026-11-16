package com.teanode.app.settings.manage

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material3.Button
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.settings.BarTextAction
import com.teanode.app.settings.ChoiceRow
import com.teanode.app.settings.FooterText
import com.teanode.app.settings.FormField
import com.teanode.app.settings.LoadedSettingsPage
import com.teanode.app.settings.SwitchRow
import com.teanode.app.settings.rememberActionRunner
import com.teanode.app.settings.shortDateTime
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.openWebPage
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.asReasonSentence
import com.teanode.kit.manage.AgentBillingCheck
import com.teanode.kit.manage.AgentPlanUsage
import com.teanode.kit.manage.AgentProviderDraft
import com.teanode.kit.manage.AgentProviderSettings
import com.teanode.kit.manage.ModelPriceDraft
import com.teanode.kit.manage.ProviderSignIn
import com.teanode.kit.manage.SettingsList
import com.teanode.kit.manage.agentBillingChecks
import com.teanode.kit.manage.agentServerSettings
import com.teanode.kit.manage.beginProviderSignIn
import com.teanode.kit.manage.checkAgentProviderPrices
import com.teanode.kit.manage.finishProviderSignIn
import com.teanode.kit.manage.testAgentProvider
import com.teanode.kit.manage.updateAgentServerSettings
import com.teanode.kit.photos.ServerTime
import java.time.Instant
import kotlinx.coroutines.launch

@Composable
private fun planWindowName(windowMinutes: Int): String = when {
    windowMinutes == 7 * 24 * 60 -> stringResource(R.string.manage_plan_weekly)
    windowMinutes == 24 * 60 -> stringResource(R.string.manage_plan_daily)
    windowMinutes % (24 * 60) == 0 -> stringResource(R.string.manage_plan_days, windowMinutes / (24 * 60))
    else -> stringResource(R.string.manage_plan_hours, "%.1f".format(windowMinutes / 60.0).removeSuffix(".0"))
}

/** Draws each window of a plan's allowance as a bar of what is used, with what is left and when it starts over. */
@Composable
fun PlanUsageRows(usage: AgentPlanUsage, modifier: Modifier = Modifier, now: Instant = Instant.now()) {
    Column(modifier.fillMaxWidth().padding(top = 6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        usage.windows.forEach { window ->
            val usedPercent = window.shownUsedPercent(now)
            val resets = ServerTime.instant(window.resetsAt)
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    Text(stringResource(R.string.manage_plan_left, planWindowName(window.windowMinutes), 100 - usedPercent), style = MaterialTheme.typography.bodySmall)
                    if (resets != null) Text(
                        stringResource(if (window.hasReset(now)) R.string.manage_plan_reset_since else R.string.manage_plan_resets, shortDateTime(resets)), style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                LinearProgressIndicator(
                    progress = { usedPercent / 100f }, modifier = Modifier.fillMaxWidth(),
                    color = if (usedPercent >= 90) MaterialTheme.colorScheme.error else if (usedPercent >= 70) MaterialTheme.colorScheme.tertiary else MaterialTheme.colorScheme.outline,
                )
            }
        }
        val observed = ServerTime.instant(usage.observedAt)
        if (observed != null) Text(stringResource(R.string.manage_plan_observed, shortDateTime(observed)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun providerDetails(provider: AgentProviderSettings): String {
    val parts = mutableListOf<String>()
    if (provider.isSignedInKind) {
        parts += stringResource(if (provider.hasRefreshToken) R.string.manage_signed_in else R.string.manage_not_signed_in)
    } else {
        parts += provider.baseUrl.ifEmpty { stringResource(R.string.manage_its_own_endpoint) }
        parts += stringResource(if (provider.hasApiKey) R.string.manage_key_set else R.string.manage_no_key)
    }
    if (provider.allow.isNotEmpty()) parts += stringResource(R.string.manage_provider_offers, SettingsList.text(provider.allow))
    if (provider.deny.isNotEmpty()) parts += stringResource(R.string.manage_provider_hides, SettingsList.text(provider.deny))
    if (provider.pricingInput != 0.0 || provider.pricingOutput != 0.0) {
        parts += stringResource(R.string.manage_provider_prices, SettingsList.text(provider.pricingInput).ifEmpty { "0" }, SettingsList.text(provider.pricingOutput).ifEmpty { "0" })
    }
    // The prices above are only what is left when no model's own price matches, so the count of those is said too.
    val pricedCount = provider.modelPricing?.size ?: 0
    if (pricedCount > 0) parts += stringResource(R.string.manage_provider_priced_apart, pricedCount)
    return parts.joinToString(" · ")
}

/** The model services, a row each, edited one at a time. The list is sent whole whenever a row changes. */
@Composable
fun AgentProvidersScreen(onBack: () -> Unit, onOpenProvider: (Int?) -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val snackbar = LocalSnackbar.current
    val loaded = rememberLoad { connection.agentServerSettings() }
    var removingIndex by remember { mutableStateOf<Int?>(null) }
    var testingName by remember { mutableStateOf<String?>(null) }
    val removedText = stringResource(R.string.manage_removed)
    val resources = LocalContext.current.resources
    LoadedSettingsPage(
        stringResource(R.string.manage_providers), onBack, loaded,
        floatingActionButton = { FloatingActionButton(onClick = { onOpenProvider(null) }) { Icon(Icons.Outlined.Add, stringResource(R.string.manage_add_a_provider)) } },
        isEmpty = { settings -> settings.providers.isEmpty() }, empty = { EmptyState(Icons.Outlined.Memory, stringResource(R.string.manage_no_providers)) },
    ) { settings ->
        items(settings.providers.size) { index ->
            val provider = settings.providers[index]
            Column {
                TeaNodeRow(
                    headline = provider.name,
                    overline = listOfNotNull(
                        provider.providerKind, provider.planUsage?.planName?.takeIf { name -> name.isNotEmpty() }?.let { name -> stringResource(R.string.manage_plan_name, name.replaceFirstChar { first -> first.uppercase() }) },
                        if (!provider.isEnabled) stringResource(R.string.manage_off) else null,
                    ).joinToString(" · "),
                    supporting = providerDetails(provider), supportingMaxLines = 5, onClick = { if (testingName == null) onOpenProvider(index) },
                    trailing = {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            if (testingName == provider.name) androidx.compose.material3.CircularProgressIndicator(Modifier.padding(end = 8.dp), strokeWidth = 2.dp)
                            else if (provider.canBeTested) TextButton(onClick = {
                                testingName = provider.name
                                actions.run(onSuccess = {}) {
                                    try {
                                        val names = connection.testAgentProvider(provider.name)
                                        snackbar.show(resources.getString(R.string.manage_test_result, names.size, names.take(5).joinToString(", ")))
                                    } finally {
                                        testingName = null
                                    }
                                }
                            }) { Text(stringResource(R.string.manage_test)) }
                            IconButton(onClick = { removingIndex = index }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_remove)) }
                        }
                    },
                )
                provider.planUsage?.takeIf { usage -> usage.windows.isNotEmpty() }?.let { usage -> PlanUsageRows(usage, Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
            }
        }
        item { FooterText(stringResource(R.string.manage_providers_footer)) }
    }
    val settings = loaded.value
    removingIndex?.let { index ->
        val provider = settings?.providers?.getOrNull(index)
        if (provider != null) ConfirmationDialog(
            title = stringResource(R.string.manage_remove_provider), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.manage_remove_provider_detail, provider.name),
            onConfirm = { removingIndex = null; actions.run(removedText, onSuccess = { loaded.refresh() }) { connection.updateAgentServerSettings(AgentProviderDraft.agentParameters(index, settings)) } },
            onDismiss = { removingIndex = null },
        )
    }
}

/** Adds a provider or changes one. A provider that runs on a person's plan is signed in to rather than keyed, and the sign-in is what saves it. */
@Composable
fun AgentProviderFormScreen(providerIndex: Int?, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val snackbar = LocalSnackbar.current
    val loaded = rememberLoad { connection.agentServerSettings() }
    val settings = loaded.value
    var draft by remember(settings != null) { mutableStateOf(settings?.let { found -> providerIndex?.let { index -> found.providers.getOrNull(index) }?.let { provider -> AgentProviderDraft.of(provider) } ?: AgentProviderDraft() }) }
    val storedProvider = settings?.providers?.getOrNull(providerIndex ?: -1)
    var billingCheck by remember { mutableStateOf<AgentBillingCheck?>(null) }
    var isCheckingPrices by remember { mutableStateOf(false) }
    val savedText = stringResource(R.string.settings_saved)
    val resources = LocalContext.current.resources
    LaunchedEffect(storedProvider?.name, storedProvider?.hasBillingApiKey) {
        if (storedProvider != null && storedProvider.hasBillingApiKey == true) {
            try {
                billingCheck = connection.agentBillingChecks().firstOrNull { check -> check.providerName == storedProvider.name }
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
        }
    }
    TeaNodeScreen(
        title = stringResource(if (providerIndex == null) R.string.manage_add_a_provider else R.string.manage_edit_provider), onBack = onBack,
        actions = {
            val current = draft
            if (settings != null && current != null) BarTextAction(
                stringResource(R.string.settings_save), onClick = { actions.run(savedText, onSuccess = onBack) { connection.updateAgentServerSettings(current.agentParameters(providerIndex, settings)) } },
                isEnabled = current.canSave, isWorking = actions.isWorking,
            )
        },
    ) { contentPadding ->
        LoadedContent(loaded, Modifier.padding(contentPadding)) { found ->
            val current = draft
            if (current != null) Column(Modifier.verticalScroll(rememberScrollState())) {
                ProviderFormFields(
                    current, { changed -> draft = changed }, found.kinds.ifEmpty { listOf(current.providerKind) }, storedProvider?.name, billingCheck, isCheckingPrices,
                    onCheckPrices = {
                        val providerName = storedProvider?.name
                        if (providerName != null) {
                            isCheckingPrices = true
                            actions.run {
                                try {
                                    val check = connection.checkAgentProviderPrices().firstOrNull { candidate -> candidate.providerName == providerName }
                                    if (check != null) {
                                        billingCheck = check
                                        if (check.errorMessage.isNotEmpty()) {
                                            snackbar.showProblem(check.errorMessage)
                                        } else if (check.changed.isEmpty()) {
                                            snackbar.show(resources.getString(R.string.manage_prices_match))
                                        } else {
                                            snackbar.show(resources.getString(R.string.manage_prices_changed, check.changed.size))
                                            // A check that changed this provider's prices changed it on the server, so the prices in the form are taken from the server: saving with the old ones would put them back.
                                            val fresh = connection.agentServerSettings().providers.getOrNull(providerIndex ?: -1)
                                            if (fresh != null) {
                                                val refreshed = AgentProviderDraft.of(fresh)
                                                draft = draft?.copy(
                                                    pricingInputText = refreshed.pricingInputText, pricingOutputText = refreshed.pricingOutputText, pricingCacheReadText = refreshed.pricingCacheReadText,
                                                    pricingCacheWriteText = refreshed.pricingCacheWriteText, pricingAudioInputText = refreshed.pricingAudioInputText, modelPrices = refreshed.modelPrices,
                                                )
                                            }
                                        }
                                    }
                                } finally {
                                    isCheckingPrices = false
                                }
                            }
                        }
                    },
                    onSignedIn = { signIn ->
                        snackbar.show(if (signIn.plan.isEmpty()) resources.getString(R.string.manage_signed_in_snack) else resources.getString(R.string.manage_signed_in_plan, signIn.plan))
                        onBack()
                    },
                )
                androidx.compose.foundation.layout.Spacer(Modifier.padding(bottom = 24.dp))
            }
        }
    }
}

@Composable
private fun ProviderFormFields(
    draft: AgentProviderDraft,
    onChange: (AgentProviderDraft) -> Unit,
    kinds: List<String>,
    storedProviderName: String?,
    billingCheck: AgentBillingCheck?,
    isCheckingPrices: Boolean,
    onCheckPrices: () -> Unit,
    onSignedIn: (ProviderSignIn) -> Unit,
) {
    FormField(draft.name, { typed -> onChange(draft.copy(name = typed)) }, stringResource(R.string.manage_provider_name))
    ChoiceRow(stringResource(R.string.manage_provider_kind), kinds, draft.providerKind, label = { kind -> kind }, onSelect = { kind -> onChange(draft.copy(providerKind = kind)) })
    if (draft.isSignedInKind) {
        ProviderSignInSection(draft.name, draft.hasRefreshToken, onSignedIn)
    } else {
        FormField(draft.baseUrl, { typed -> onChange(draft.copy(baseUrl = typed)) }, stringResource(R.string.manage_base_url), placeholder = stringResource(R.string.manage_base_url_prompt), keyboardType = KeyboardType.Uri)
        SecretField(stringResource(R.string.manage_api_key), draft.hasApiKey, draft.apiKey, { typed -> onChange(draft.copy(apiKey = typed)) })
        if (draft.providerKind == "openai") {
            SecretField(stringResource(R.string.manage_billing_key), draft.hasBillingApiKey, draft.billingApiKey, { typed -> onChange(draft.copy(billingApiKey = typed)) }, hint = stringResource(R.string.manage_billing_key_hint))
        }
    }
    FormField(draft.allowText, { typed -> onChange(draft.copy(allowText = typed)) }, stringResource(R.string.manage_models_to_offer), placeholder = stringResource(R.string.manage_models_to_offer_prompt))
    FormField(draft.denyText, { typed -> onChange(draft.copy(denyText = typed)) }, stringResource(R.string.manage_models_to_hide), placeholder = stringResource(R.string.manage_models_to_hide_prompt))
    TeaNodeSectionHeader(stringResource(R.string.manage_price_per_million))
    PriceField(stringResource(R.string.manage_price_input), draft.pricingInputText) { typed -> onChange(draft.copy(pricingInputText = typed)) }
    PriceField(stringResource(R.string.manage_price_output), draft.pricingOutputText) { typed -> onChange(draft.copy(pricingOutputText = typed)) }
    PriceField(stringResource(R.string.manage_price_cache_read), draft.pricingCacheReadText) { typed -> onChange(draft.copy(pricingCacheReadText = typed)) }
    PriceField(stringResource(R.string.manage_price_cache_write), draft.pricingCacheWriteText) { typed -> onChange(draft.copy(pricingCacheWriteText = typed)) }
    PriceField(stringResource(R.string.manage_price_audio_input), draft.pricingAudioInputText) { typed -> onChange(draft.copy(pricingAudioInputText = typed)) }
    draft.modelPrices.forEach { row ->
        fun change(changed: ModelPriceDraft) = onChange(draft.copy(modelPrices = draft.modelPrices.map { price -> if (price.rowId == row.rowId) changed else price }))
        TeaNodeSectionHeader(stringResource(R.string.manage_prices_for_models))
        FormField(row.model, { typed -> change(row.copy(model = typed)) }, stringResource(R.string.manage_price_model), placeholder = "model-name*")
        PriceField(stringResource(R.string.manage_price_in), row.inputText) { typed -> change(row.copy(inputText = typed)) }
        PriceField(stringResource(R.string.manage_price_out), row.outputText) { typed -> change(row.copy(outputText = typed)) }
        PriceField(stringResource(R.string.manage_price_cached), row.cacheReadText) { typed -> change(row.copy(cacheReadText = typed)) }
        PriceField(stringResource(R.string.manage_price_caching), row.cacheWriteText) { typed -> change(row.copy(cacheWriteText = typed)) }
        PriceField(stringResource(R.string.manage_price_audio), row.audioInputText) { typed -> change(row.copy(audioInputText = typed)) }
        TeaNodeRow(headline = stringResource(R.string.settings_remove), leadingIcon = Icons.Outlined.Delete, onClick = { onChange(draft.copy(modelPrices = draft.modelPrices.filter { price -> price.rowId != row.rowId })) })
    }
    TeaNodeRow(
        headline = stringResource(R.string.manage_price_another_model), leadingIcon = Icons.Outlined.Add,
        onClick = { onChange(draft.copy(modelPrices = draft.modelPrices + ModelPriceDraft(rowId = (draft.modelPrices.maxOfOrNull { price -> price.rowId } ?: -1) + 1))) },
    )
    FooterText(stringResource(R.string.manage_prices_for_models_footer))
    if (draft.hasBillingApiKey && storedProviderName != null) BilledPricesSection(billingCheck, isCheckingPrices, onCheckPrices)
    SwitchRow(stringResource(R.string.manage_enabled), draft.isEnabled, { isOn -> onChange(draft.copy(isEnabled = isOn)) })
}

@Composable
private fun PriceField(label: String, text: String, onChange: (String) -> Unit) {
    FormField(text, { typed -> onChange(SettingsList.price(typed)) }, label, keyboardType = KeyboardType.Decimal)
}

@Composable
private fun priceText(price: Double): String = if (price == 0.0) stringResource(R.string.manage_price_not_set) else SettingsList.text(price)

@Composable
private fun tokenKindText(tokenKind: String): String = when (tokenKind) {
    "input" -> stringResource(R.string.manage_price_input)
    "cachedInput" -> stringResource(R.string.manage_price_cached_input)
    "cacheWrite" -> stringResource(R.string.manage_price_cache_writes)
    "output" -> stringResource(R.string.manage_price_output)
    "audioInput" -> stringResource(R.string.manage_price_audio_input)
    else -> tokenKind
}

/** How the prices stand against the provider's bill, for a stored provider with a billing key. */
@Composable
private fun BilledPricesSection(check: AgentBillingCheck?, isCheckingPrices: Boolean, onCheckPrices: () -> Unit) {
    TeaNodeSectionHeader(stringResource(R.string.manage_prices_from_bill))
    val checkedInstant = check?.checkedInstant
    if (check != null && checkedInstant != null) {
        if (check.errorMessage.isEmpty()) {
            FooterText(stringResource(R.string.manage_prices_checked, shortDateTime(checkedInstant), check.changed.size))
        } else {
            Text(check.errorMessage.asReasonSentence(), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
        }
    } else {
        FooterText(stringResource(R.string.manage_prices_not_checked))
    }
    TeaNodeRow(headline = stringResource(if (isCheckingPrices) R.string.manage_checking else R.string.manage_check_prices_now), onClick = if (isCheckingPrices) null else onCheckPrices)
    if (check != null && check.errorMessage.isEmpty()) {
        if (check.compared.isEmpty() && checkedInstant != null) FooterText(stringResource(R.string.manage_prices_nothing_to_compare))
        check.compared.forEach { price ->
            val change = check.changed.firstOrNull { candidate -> candidate.model == price.model && candidate.tokenKind == price.tokenKind }
            val valueText = when {
                change != null -> "${priceText(change.beforePerMillion)} → ${priceText(change.afterPerMillion)}"
                price.configuredPerMillion == price.billedPerMillion -> priceText(price.configuredPerMillion)
                else -> stringResource(R.string.manage_price_with_bill, priceText(price.configuredPerMillion), priceText(price.billedPerMillion))
            }
            TeaNodeRow(
                headline = price.model,
                supporting = listOfNotNull(tokenKindText(price.tokenKind), if (price.isPattern) stringResource(R.string.manage_price_pattern) else null).joinToString(" · "),
                trailing = { Text(valueText, style = MaterialTheme.typography.bodyMedium) },
            )
        }
    }
    FooterText(stringResource(R.string.manage_prices_from_bill_footer))
}

/**
 * Signs a provider in to a person's plan: the server asks the service for a one-time code, the operator types it on the service's own page
 * from any device, and this asks the server whether it has been. Leaving the screen stops the asking; the code simply runs out.
 */
@Composable
private fun ProviderSignInSection(providerName: String, isSignedIn: Boolean, onSignedIn: (ProviderSignIn) -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    var waiting by remember { mutableStateOf<ProviderSignIn?>(null) }
    var isBeginning by remember { mutableStateOf(false) }
    val scope = androidx.compose.runtime.rememberCoroutineScope()
    LaunchedEffect(waiting?.signInId) {
        val signInId = waiting?.signInId ?: return@LaunchedEffect
        // Each finish waits on the server for up to half a minute, so this asks again as soon as one answers that the code is still unused.
        while (true) {
            try {
                val finished = connection.finishProviderSignIn(signInId)
                if (finished.isSignedIn) {
                    waiting = null
                    onSignedIn(finished)
                    return@LaunchedEffect
                }
            } catch (cancelled: kotlinx.coroutines.CancellationException) {
                throw cancelled
            } catch (failure: Throwable) {
                snackbar.showError(failure)
                waiting = null
                return@LaunchedEffect
            }
        }
    }
    val current = waiting
    if (current != null) {
        FooterText(stringResource(R.string.manage_sign_in_open_page))
        TeaNodeRow(headline = current.verificationAddress, onClick = { openWebPage(context, current.verificationAddress) })
        TeaNodeRow(
            headline = current.userCode,
            trailing = { IconButton(onClick = { clipboard.setText(AnnotatedString(current.userCode)) }) { Icon(Icons.Outlined.ContentCopy, stringResource(R.string.manage_copy)) } },
        )
        Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            androidx.compose.material3.CircularProgressIndicator(Modifier.padding(0.dp), strokeWidth = 2.dp)
            Text(stringResource(R.string.manage_sign_in_waiting), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    } else {
        Button(
            onClick = {
                isBeginning = true
                scope.launch {
                    try {
                        waiting = connection.beginProviderSignIn(providerName)
                    } catch (cancelled: kotlinx.coroutines.CancellationException) {
                        throw cancelled
                    } catch (failure: Throwable) {
                        snackbar.showError(failure)
                    } finally {
                        isBeginning = false
                    }
                }
            },
            enabled = !isBeginning && providerName.trim().isNotEmpty(), modifier = Modifier.padding(16.dp).fillMaxWidth(),
        ) { Text(stringResource(if (isSignedIn) R.string.manage_sign_in_again else R.string.manage_sign_in_with_chatgpt)) }
        FooterText(stringResource(if (isSignedIn) R.string.manage_sign_in_signed_footer else R.string.manage_sign_in_footer))
    }
}
