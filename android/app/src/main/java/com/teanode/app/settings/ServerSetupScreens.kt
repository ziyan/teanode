package com.teanode.app.settings

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.manage.MailProgramAccess
import com.teanode.kit.manage.MailProgramAccessDraft
import com.teanode.kit.manage.OutgoingIdentity
import com.teanode.kit.manage.ServerAddresses
import com.teanode.kit.manage.ServerSetup
import com.teanode.kit.manage.ServerSetupAdvice
import com.teanode.kit.manage.SsoProviderDraft
import com.teanode.kit.manage.SsoProviders
import com.teanode.kit.manage.SubmissionAccess
import com.teanode.kit.manage.SubmissionAccessDraft
import com.teanode.kit.manage.serverSetup
import com.teanode.kit.manage.serverSettings
import com.teanode.kit.manage.updateMailProgramAccess
import com.teanode.kit.manage.updateServerSettingsSection
import com.teanode.kit.manage.updateSubmissionAccess
import kotlinx.serialization.json.JsonObject

/** The Setup tab: the address seen from outside, how outgoing mail identifies itself, and what mail clients are told. */
@Composable
fun ServerSetupTab() {
    val connection = LocalConnection.current
    val loaded = rememberLoad { connection.serverSetup() }
    var editing by remember { mutableStateOf<SetupForm?>(null) }
    androidx.compose.foundation.layout.Box(Modifier.fillMaxSize()) {
    LoadedContent(loaded) { setup ->
        LazyColumn(contentPadding = PaddingValues(bottom = 24.dp)) {
            item { FooterText(stringResource(R.string.settings_setup_intro)) }
            setup.addresses?.let { addresses -> item { AddressesSection(addresses) } }
            setup.outgoingIdentity?.let { identity -> item { OutgoingSection(identity) } }
            item { ClientsSection(setup, onEdit = { form -> editing = form }) }
        }
    }
    editing?.let { form ->
        val setup = loaded.value
        when (form) {
            SetupForm.Submission -> setup?.submission?.let { submission ->
                SubmissionDialog(submission, onDismiss = { editing = null }, onSaved = { editing = null; loaded.refresh() })
            }
            SetupForm.MailProgram -> setup?.mailProgram?.let { mailProgram ->
                MailProgramDialog(mailProgram, onDismiss = { editing = null }, onSaved = { editing = null; loaded.refresh() })
            }
        }
    }
    }
}

enum class SetupForm { Submission, MailProgram }

@Composable
private fun AddressesSection(addresses: ServerAddresses) {
    TeaNodeSectionHeader(stringResource(R.string.settings_setup_address_header))
    if (addresses.isKnown) {
        addresses.ipv4?.takeIf { text -> text.isNotEmpty() }?.let { text -> TeaNodeRow(headline = "A", supporting = text) }
        addresses.ipv6?.takeIf { text -> text.isNotEmpty() }?.let { text -> TeaNodeRow(headline = "AAAA", supporting = text) }
        FooterText(stringResource(R.string.settings_setup_address_seen))
        if (addresses.ipv6.isNullOrEmpty()) FooterText(stringResource(R.string.settings_setup_no_ipv6))
    } else {
        TeaNodeRow(headline = addresses.errorMessage?.takeIf { text -> text.isNotEmpty() } ?: stringResource(R.string.settings_setup_address_unknown), supportingMaxLines = 3)
        FooterText(stringResource(R.string.settings_setup_address_advice))
    }
}

@Composable
private fun OutgoingSection(identity: OutgoingIdentity) {
    val resources = LocalContext.current.resources
    TeaNodeSectionHeader(stringResource(R.string.settings_setup_outgoing_header))
    if (!identity.uncheckableReason.isNullOrEmpty()) {
        TeaNodeRow(headline = identity.uncheckableReason.orEmpty(), supportingMaxLines = 4)
        return
    }
    val address = identity.address.orEmpty()
    val none = resources.getString(R.string.settings_setup_none)
    val nothing = resources.getString(R.string.settings_setup_nothing)
    TeaNodeRow(headline = stringResource(R.string.settings_setup_sends_from), supporting = address)
    val reverseAdvice = when (ServerSetupAdvice.reverseProblem(identity)) {
        null -> null
        ServerSetupAdvice.ReverseProblem.NoReverseName -> resources.getString(R.string.settings_setup_reverse_none, address)
        ServerSetupAdvice.ReverseProblem.DoesNotResolveBack -> resources.getString(R.string.settings_setup_reverse_mismatch, identity.reverseName.orEmpty(), ServerSetupAdvice.resolves(identity.forwardAddresses) ?: nothing, address)
    }
    IdentityRow(stringResource(R.string.settings_setup_reverse_name), identity.reverseName?.ifEmpty { null } ?: none, identity.isConfirmed, reverseAdvice)
    val helloAdvice = if (identity.doesHelloMatch) null else resources.getString(R.string.settings_setup_hello_mismatch, identity.helloName.orEmpty(), ServerSetupAdvice.resolves(identity.helloAddresses) ?: nothing, address)
    IdentityRow(stringResource(R.string.settings_setup_hello_name), identity.helloName?.ifEmpty { null } ?: none, identity.doesHelloMatch, helloAdvice)
    FooterText(stringResource(if (identity.via == "proxy") R.string.settings_setup_outgoing_proxy else R.string.settings_setup_outgoing_direct))
}

@Composable
private fun IdentityRow(label: String, shownValue: String, isPassing: Boolean, advice: String?) {
    TeaNodeRow(
        headline = label,
        supporting = listOfNotNull(shownValue, advice).joinToString("\n"), supportingMaxLines = 8,
        trailing = { Text(stringResource(if (isPassing) R.string.settings_setup_ok else R.string.settings_setup_change), style = MaterialTheme.typography.labelLarge, color = if (isPassing) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error) },
    )
}

@Composable
private fun ClientsSection(setup: ServerSetup, onEdit: (SetupForm) -> Unit) {
    TeaNodeSectionHeader(stringResource(R.string.settings_setup_clients_header))
    setup.submission?.let { submission ->
        TeaNodeRow(headline = stringResource(R.string.settings_setup_tell_client), supporting = "${submission.effectiveHost} : ${submission.effectivePort}", onClick = { onEdit(SetupForm.Submission) })
    }
    setup.mailProgram?.takeIf { program -> program.isListening }?.let { program ->
        val shownPort = program.effectiveTlsPort.ifEmpty { program.effectivePort }
        TeaNodeRow(headline = stringResource(R.string.settings_setup_read_in_program), supporting = "${program.effectiveHost} : $shownPort", onClick = { onEdit(SetupForm.MailProgram) })
    }
}

@Composable
private fun SubmissionDialog(submission: SubmissionAccess, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val savedText = stringResource(R.string.settings_setup_submission_saved)
    val original = remember(submission) { SubmissionAccessDraft(submission) }
    var draft by remember(submission) { mutableStateOf(original) }
    FormDialog(
        title = stringResource(R.string.settings_setup_tell_client), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = draft != original, isWorking = actions.isWorking,
        onConfirm = { actions.run(savedText, onSuccess = onSaved) { connection.updateSubmissionAccess(draft) } }, onDismiss = onDismiss,
    ) {
        FooterText("${submission.effectiveHost} : ${submission.effectivePort}")
        FooterText(stringResource(R.string.settings_setup_submission_about))
        FormField(draft.host, { draft = draft.copy(host = it) }, stringResource(R.string.settings_setup_host), placeholder = submission.effectiveHost)
        FormField(draft.portText, { draft = draft.copy(portText = com.teanode.kit.manage.digits(it)) }, stringResource(R.string.settings_setup_port), placeholder = submission.effectivePort, keyboardType = KeyboardType.Number)
        FooterText(stringResource(R.string.settings_setup_submission_follow))
    }
}

@Composable
private fun MailProgramDialog(mailProgram: MailProgramAccess, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val savedText = stringResource(R.string.settings_setup_program_saved)
    val original = remember(mailProgram) { MailProgramAccessDraft(mailProgram) }
    var draft by remember(mailProgram) { mutableStateOf(original) }
    val shownPort = mailProgram.effectiveTlsPort.ifEmpty { mailProgram.effectivePort }
    FormDialog(
        title = stringResource(R.string.settings_setup_read_in_program), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = draft != original, isWorking = actions.isWorking,
        onConfirm = { actions.run(savedText, onSuccess = onSaved) { connection.updateMailProgramAccess(draft) } }, onDismiss = onDismiss,
    ) {
        FooterText("${mailProgram.effectiveHost} : $shownPort")
        FooterText(stringResource(if (mailProgram.effectiveTlsPort.isEmpty()) R.string.settings_setup_program_starttls else R.string.settings_setup_program_tls))
        FormField(draft.host, { draft = draft.copy(host = it) }, stringResource(R.string.settings_setup_host), placeholder = mailProgram.effectiveHost)
        FormField(draft.tlsPortText, { draft = draft.copy(tlsPortText = com.teanode.kit.manage.digits(it)) }, stringResource(R.string.settings_setup_tls_port), placeholder = mailProgram.effectiveTlsPort.ifEmpty { "993" }, keyboardType = KeyboardType.Number)
        FormField(draft.portText, { draft = draft.copy(portText = com.teanode.kit.manage.digits(it)) }, stringResource(R.string.settings_setup_starttls_port), placeholder = mailProgram.effectivePort.ifEmpty { "143" }, keyboardType = KeyboardType.Number)
        FooterText(stringResource(R.string.settings_setup_program_follow))
    }
}

/** The identity providers people may sign in through. The client secret is never shown: a stored one is kept unless a new one is typed. */
@Composable
fun SsoProvidersTab() {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.serverSettings() }
    val serverOrigin = LocalAppSession.current.accounts.collectAsState().value.active?.server?.origin?.toString()?.trimEnd('/').orEmpty()
    val savedText = stringResource(R.string.settings_saved)
    LoadedContent(loaded) { settings ->
        val stored = remember(settings) { SsoProviders.drafts(settings["sso"] as? JsonObject) }
        var providers by remember(settings) { mutableStateOf(stored) }
        var nextKey by remember(settings) { mutableStateOf(stored.size) }
        LazyColumn(contentPadding = PaddingValues(bottom = 24.dp)) {
            item {
                if (providers.isEmpty()) TeaNodeRow(headline = stringResource(R.string.settings_sso_none), supportingMaxLines = 3)
                FooterText(stringResource(R.string.settings_sso_about))
            }
            providers.forEach { provider ->
                item(key = provider.key) {
                    SsoProviderForm(provider, serverOrigin, onChange = { changed -> providers = providers.map { candidate -> if (candidate.key == provider.key) changed else candidate } }, onRemove = { providers = providers.filter { candidate -> candidate.key != provider.key } })
                }
            }
            item {
                TeaNodeRow(headline = stringResource(R.string.settings_sso_add), leadingIcon = Icons.Outlined.Add, onClick = { providers = providers + SsoProviderDraft(key = nextKey++) })
                Button(
                    onClick = { actions.run(savedText, onSuccess = { loaded.refresh() }) { connection.updateServerSettingsSection("sso", SsoProviders.changes(providers)) } },
                    enabled = providers != stored && SsoProviders.canSave(providers) && !actions.isWorking, modifier = Modifier.padding(16.dp).fillMaxWidth(),
                ) { Text(stringResource(R.string.settings_save)) }
            }
        }
    }
}

@Composable
private fun SsoProviderForm(provider: SsoProviderDraft, serverOrigin: String, onChange: (SsoProviderDraft) -> Unit, onRemove: () -> Unit) {
    TeaNodeSectionHeader(provider.name.ifEmpty { provider.providerId })
    FormField(provider.providerId, { onChange(provider.copy(providerId = it)) }, stringResource(R.string.settings_sso_id), placeholder = "okta")
    FormField(provider.name, { onChange(provider.copy(name = it)) }, stringResource(R.string.settings_sso_button_says))
    FormField(provider.issuer, { onChange(provider.copy(issuer = it)) }, stringResource(R.string.settings_sso_issuer), placeholder = "https://login.example.net/", keyboardType = KeyboardType.Uri)
    FormField(provider.clientId, { onChange(provider.copy(clientId = it)) }, stringResource(R.string.settings_sso_client_id))
    FormField(
        provider.clientSecret, { onChange(provider.copy(clientSecret = it)) }, stringResource(R.string.settings_sso_client_secret), isPassword = true,
        supporting = if (provider.hasClientSecret) stringResource(R.string.settings_sso_secret_kept) else null,
    )
    FormField(provider.groupsClaim, { onChange(provider.copy(groupsClaim = it)) }, stringResource(R.string.settings_sso_groups_claim), placeholder = "groups")
    SwitchRow(stringResource(R.string.settings_sso_create_users), provider.shouldCreateUsers, { isOn -> onChange(provider.copy(shouldCreateUsers = isOn)) })
    TeaNodeRow(headline = stringResource(R.string.settings_sso_remove), leadingIcon = Icons.Outlined.Delete, onClick = onRemove)
    FooterText(stringResource(R.string.settings_sso_redirect, provider.redirectAddress(serverOrigin)))
}
