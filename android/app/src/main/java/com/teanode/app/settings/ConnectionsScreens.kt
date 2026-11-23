package com.teanode.app.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.chats.BackgroundCommandView
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.agent.AgentChatChannel
import com.teanode.kit.agent.AgentConnectedServer
import com.teanode.kit.agent.AgentReach
import com.teanode.kit.agent.AgentSkillSecret
import com.teanode.kit.agent.ServerCondition
import com.teanode.kit.agent.agentReaches
import com.teanode.kit.agent.canConnectWithCredential
import com.teanode.kit.agent.chatChannels
import com.teanode.kit.agent.clearSkillSecret
import com.teanode.kit.agent.condition
import com.teanode.kit.agent.connectServer
import com.teanode.kit.agent.connectedServers
import com.teanode.kit.agent.disconnectServer
import com.teanode.kit.agent.removeChatChannel
import com.teanode.kit.agent.setAgentReach
import com.teanode.kit.agent.setChatChannel
import com.teanode.kit.agent.setSkillSecret
import com.teanode.kit.agent.skillSecrets
import com.teanode.kit.agent.unlinkChatChannel
import com.teanode.kit.chats.AgentBackgroundCommand
import com.teanode.kit.chats.AgentStatus
import com.teanode.kit.chats.backgroundCommands

/** How a small tag reads: plain, good, bad or in between. */
enum class TagTone { Plain, Good, Bad, Warning }

/** A short word beside a row's name that says where it stands. */
@Composable
fun UpkeepTag(text: String, tone: TagTone = TagTone.Plain) {
    val color = when (tone) {
        TagTone.Plain -> MaterialTheme.colorScheme.onSurfaceVariant
        TagTone.Good -> MaterialTheme.colorScheme.primary
        TagTone.Bad -> MaterialTheme.colorScheme.error
        TagTone.Warning -> MaterialTheme.colorScheme.tertiary
    }
    Surface(shape = MaterialTheme.shapes.small, color = color.copy(alpha = 0.12f), contentColor = color) {
        Text(text, Modifier.padding(horizontal = 8.dp, vertical = 2.dp), style = MaterialTheme.typography.labelMedium)
    }
}

@Composable
private fun chatAppName(kind: String): String = stringResource(if (kind == "discord") R.string.settings_connections_chat_discord else R.string.settings_connections_chat_telegram)

@Composable
private fun chatAppHow(kind: String): String = stringResource(if (kind == "discord") R.string.settings_connections_chat_how_discord else R.string.settings_connections_chat_how_telegram)

/** Everything Connections reads at once; a part that fails to read stays empty, as on the iPhone. */
private class ConnectionsRead(
    val servers: List<AgentConnectedServer>,
    val skillSecrets: List<AgentSkillSecret>,
    val reaches: List<AgentReach>,
    val computers: List<AgentStatus.Computer>,
    val commands: List<AgentBackgroundCommand>,
    val chatChannels: List<AgentChatChannel>,
)

/** A row's overflow menu, shown for the actions the iPhone puts on a swipe and a long press. */
@Composable
internal fun RowMenu(actions: List<Pair<String, () -> Unit>>) {
    if (actions.isEmpty()) return
    var isOpen by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { isOpen = true }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.common_more)) }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            actions.forEach { (label, action) -> DropdownMenuItem(text = { Text(label) }, onClick = { isOpen = false; action() }) }
        }
    }
}

/** The servers the agent is connected to, the keys of its skills, where each reaches through, and its chat app bots. */
@Composable
fun ConnectionsScreen(onBack: () -> Unit, onOpenSkillSharing: () -> Unit = {}) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad {
        val reaches = runCatching { connection.agentReaches() }.getOrNull()
        ConnectionsRead(
            servers = runCatching { connection.connectedServers() }.getOrDefault(emptyList()),
            skillSecrets = runCatching { connection.skillSecrets() }.getOrDefault(emptyList()),
            reaches = reaches?.reaches.orEmpty(),
            computers = reaches?.computers.orEmpty(),
            commands = runCatching { connection.backgroundCommands(null) }.getOrDefault(emptyList()),
            chatChannels = connection.chatChannels(),
        )
    }
    var connecting by remember { mutableStateOf<AgentConnectedServer?>(null) }
    var disconnecting by remember { mutableStateOf<AgentConnectedServer?>(null) }
    var fillingSecret by remember { mutableStateOf<AgentSkillSecret?>(null) }
    var forgettingSecret by remember { mutableStateOf<AgentSkillSecret?>(null) }
    var editingChatKind by remember { mutableStateOf<String?>(null) }
    var unlinking by remember { mutableStateOf<AgentChatChannel?>(null) }
    var removingChannel by remember { mutableStateOf<AgentChatChannel?>(null) }
    var openCommand by remember { mutableStateOf<AgentBackgroundCommand?>(null) }
    val clipboardText = stringResource(R.string.settings_connections_chat_copied)
    val context = androidx.compose.ui.platform.LocalContext.current

    openCommand?.let { command ->
        BackgroundCommandView(command, connection, onBack = { openCommand = null; loaded.refresh() })
        return
    }

    LoadedSettingsPage(stringResource(R.string.settings_connections_title), onBack, loaded) { read ->
        if (read.servers.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_connections_servers_header)) }
            items(read.servers.size) { index ->
                val server = read.servers[index]
                val serverActions = buildList {
                    if (server.isPersonal && server.isConnected) add(stringResource(R.string.settings_apps_disconnect) to { disconnecting = server })
                    else if (server.canConnectWithCredential) add(stringResource(R.string.settings_connect) to { connecting = server })
                }
                TeaNodeRow(
                    headline = server.name,
                    supporting = serverDetails(server),
                    supportingMaxLines = 4,
                    overline = null,
                    onClick = serverActions.firstOrNull()?.second,
                    trailing = {
                        androidx.compose.foundation.layout.Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                            ServerTag(server)
                            RowMenu(serverActions)
                        }
                    },
                )
            }
            item { FooterText(stringResource(R.string.settings_connections_servers_footer)) }
        }
        if (read.skillSecrets.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_connections_secrets_header)) }
            items(read.skillSecrets.size) { index ->
                val secret = read.skillSecrets[index]
                val secretActions = buildList {
                    add(stringResource(if (secret.set) R.string.settings_connections_secret_replace else R.string.settings_connections_secret_fill) to { fillingSecret = secret })
                    if (secret.set) add(stringResource(R.string.settings_connections_secret_forget) to { forgettingSecret = secret })
                }
                TeaNodeRow(
                    headline = secret.key, supporting = secret.description.ifEmpty { null }, supportingMaxLines = 4,
                    onClick = { fillingSecret = secret },
                    trailing = {
                        androidx.compose.foundation.layout.Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                            UpkeepTag(secret.skill)
                            if (secret.set) UpkeepTag(stringResource(R.string.settings_connections_secret_set), TagTone.Good)
                            RowMenu(secretActions)
                        }
                    },
                )
            }
            item { FooterText(stringResource(R.string.settings_connections_secrets_footer)) }
        }
        item { TeaNodeRow(headline = stringResource(R.string.settings_connections_skills_link), onClick = onOpenSkillSharing) }
        item { FooterText(stringResource(R.string.settings_connections_skills_footer)) }
        if (read.reaches.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_connections_reach_header)) }
            items(read.reaches.size) { index ->
                val reach = read.reaches[index]
                val options = reach.options(read.computers)
                val chosen = options.firstOrNull { option -> option.computerName == reach.computerName } ?: options.first()
                Column {
                    ChoiceRow(
                        reach.name, options, chosen,
                        label = { option -> reachOptionText(option) },
                        onSelect = { option ->
                            if (option.computerName != reach.computerName) {
                                val through = if (option.computerName.isEmpty()) context.getString(R.string.settings_connections_reach_this_server) else context.getString(R.string.settings_connections_reach_through, option.computerName)
                                actions.run(context.getString(R.string.settings_connections_reach_now, reach.name, through), onSuccess = { loaded.refresh() }) {
                                    connection.setAgentReach(reach.kind, reach.name, option.computerName)
                                }
                            }
                        },
                    )
                    FlowRow(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        UpkeepTag(stringResource(if (reach.isSkill) R.string.settings_connections_reach_skill else R.string.settings_connections_reach_server))
                    }
                    if (reach.needsChoice(read.computers.size)) {
                        Text(
                            stringResource(R.string.settings_connections_reach_choose), Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.tertiary,
                        )
                    }
                }
            }
            item { FooterText(stringResource(R.string.settings_connections_reach_footer)) }
        }
        if (read.commands.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_connections_commands_header)) }
            items(read.commands.size) { index ->
                val command = read.commands[index]
                TeaNodeRow(
                    headline = command.command,
                    supporting = "${command.computer} · ${stringResource(if (command.isRunning) R.string.settings_connections_command_running else R.string.settings_connections_command_stopped)}",
                    onClick = { openCommand = command },
                )
            }
            item { FooterText(stringResource(R.string.settings_connections_commands_footer)) }
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_connections_chat_header)) }
        items(AgentChatChannel.kinds.size) { index ->
            val kind = AgentChatChannel.kinds[index]
            val channel = read.chatChannels.firstOrNull { candidate -> candidate.kind == kind }
            val savedBotText = stringResource(R.string.settings_connections_chat_saved, chatAppName(kind))
            val channelActions = buildList {
                add(stringResource(if (channel == null) R.string.settings_connections_chat_set_token else R.string.settings_connections_chat_replace_token) to { editingChatKind = kind })
                if (channel != null) {
                    add(stringResource(if (channel.enabled) R.string.settings_connections_chat_turn_off else R.string.settings_connections_chat_turn_on) to {
                        actions.run(savedBotText, onSuccess = { loaded.refresh() }) { connection.setChatChannel(kind, isEnabled = !channel.enabled) }
                    })
                    channel.linkCommand?.let { command ->
                        add(stringResource(R.string.settings_connections_chat_copy) to {
                            val clipboard = context.getSystemService(android.content.ClipboardManager::class.java)
                            clipboard?.setPrimaryClip(android.content.ClipData.newPlainText(command, command))
                            snackbar.show(clipboardText)
                        })
                    }
                    if (channel.linked) add(stringResource(R.string.settings_connections_chat_unlink) to { unlinking = channel })
                    add(stringResource(R.string.settings_connections_chat_remove) to { removingChannel = channel })
                }
            }
            TeaNodeRow(
                headline = chatAppName(kind), supporting = chatDetails(channel, kind), supportingMaxLines = 6,
                onClick = { editingChatKind = kind },
                trailing = {
                    androidx.compose.foundation.layout.Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                        ChatTag(channel)
                        RowMenu(channelActions)
                    }
                },
            )
        }
        item { FooterText(stringResource(R.string.settings_connections_chat_footer)) }
    }

    connecting?.let { server ->
        SecretValueDialog(
            title = "${stringResource(R.string.settings_connect)} · ${server.name}", hint = stringResource(R.string.settings_connections_credential_hint), confirmLabel = stringResource(R.string.settings_connect),
            onConfirm = { given ->
                connecting = null
                actions.run(onSuccess = { loaded.refresh() }) {
                    val refusal = connection.connectServer(server.name, given)
                    if (refusal != null) snackbar.showProblem(refusal.ifEmpty { context.getString(R.string.settings_connections_refused) })
                    else snackbar.show(context.getString(R.string.settings_connections_connected_named, server.name))
                }
            },
            onDismiss = { connecting = null },
        )
    }
    disconnecting?.let { server ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_connections_disconnect, server.name), confirmLabel = stringResource(R.string.settings_apps_disconnect), isDestructive = true,
            onConfirm = { disconnecting = null; actions.run(onSuccess = { loaded.refresh() }) { connection.disconnectServer(server.name) } }, onDismiss = { disconnecting = null },
        )
    }
    fillingSecret?.let { secret ->
        SecretValueDialog(
            title = "${secret.key} · ${secret.skill}", hint = secret.description.ifEmpty { secret.key }, confirmLabel = stringResource(R.string.settings_connections_secret_fill),
            onConfirm = { given ->
                fillingSecret = null
                actions.run(context.getString(R.string.settings_connections_secret_kept, secret.key, secret.skill), onSuccess = { loaded.refresh() }) { connection.setSkillSecret(secret.skill, secret.key, given) }
            },
            onDismiss = { fillingSecret = null },
        )
    }
    forgettingSecret?.let { secret ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_connections_secret_forget), text = "${secret.key} · ${secret.skill}", confirmLabel = stringResource(R.string.settings_connections_secret_forget), isDestructive = true,
            onConfirm = {
                forgettingSecret = null
                actions.run(context.getString(R.string.settings_connections_secret_forgot, secret.key), onSuccess = { loaded.refresh() }) { connection.clearSkillSecret(secret.skill, secret.key) }
            },
            onDismiss = { forgettingSecret = null },
        )
    }
    unlinking?.let { channel ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_connections_chat_unlink), text = stringResource(R.string.settings_connections_chat_linked, channel.linkedName.orEmpty()), confirmLabel = stringResource(R.string.settings_connections_chat_unlink), isDestructive = true,
            onConfirm = { unlinking = null; actions.run(onSuccess = { loaded.refresh() }) { connection.unlinkChatChannel(channel.kind) } }, onDismiss = { unlinking = null },
        )
    }
    removingChannel?.let { channel ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_connections_chat_remove), text = chatAppName(channel.kind), confirmLabel = stringResource(R.string.settings_connections_chat_remove), isDestructive = true,
            onConfirm = { removingChannel = null; actions.run(onSuccess = { loaded.refresh() }) { connection.removeChatChannel(channel.kind) } }, onDismiss = { removingChannel = null },
        )
    }
    editingChatKind?.let { kind ->
        val savedBotText = stringResource(R.string.settings_connections_chat_saved, chatAppName(kind))
        SecretValueDialog(
            title = stringResource(R.string.settings_connections_chat_editor_title, chatAppName(kind)), hint = stringResource(R.string.settings_connections_chat_token_hint), explanation = chatAppHow(kind),
            confirmLabel = stringResource(R.string.settings_connections_save),
            onConfirm = { given ->
                editingChatKind = null
                actions.run(savedBotText, onSuccess = { loaded.refresh() }) { connection.setChatChannel(kind, token = given) }
            },
            onDismiss = { editingChatKind = null },
        )
    }
}

/** A dialog that takes one sealed value, which is never shown again. */
@Composable
private fun SecretValueDialog(title: String, hint: String, confirmLabel: String, onConfirm: (String) -> Unit, onDismiss: () -> Unit, explanation: String? = null) {
    var text by remember { mutableStateOf("") }
    FormDialog(title = title, confirmLabel = confirmLabel, isConfirmEnabled = text.isNotBlank(), onConfirm = { onConfirm(text.trim()) }, onDismiss = onDismiss) {
        FormField(text, { text = it }, hint, isPassword = true, supporting = explanation)
    }
}

@Composable
private fun ServerTag(server: AgentConnectedServer) {
    when (server.condition) {
        ServerCondition.Off -> UpkeepTag(stringResource(R.string.settings_connections_tag_off))
        ServerCondition.Shared -> UpkeepTag(stringResource(R.string.settings_connections_tag_shared), TagTone.Good)
        ServerCondition.Connected -> UpkeepTag(stringResource(R.string.settings_connections_tag_connected), TagTone.Good)
        ServerCondition.NotAnswering -> UpkeepTag(stringResource(R.string.settings_connections_tag_not_answering), TagTone.Bad)
        ServerCondition.AuthorizationUnderway -> UpkeepTag(stringResource(R.string.settings_connections_tag_authorizing), TagTone.Warning)
        ServerCondition.NotConnected -> UpkeepTag(stringResource(R.string.settings_connections_tag_not_connected))
    }
}

@Composable
private fun ChatTag(channel: AgentChatChannel?) {
    if (channel == null) {
        UpkeepTag(stringResource(R.string.settings_connections_chat_not_set_up))
        return
    }
    when (channel.condition) {
        AgentChatChannel.Condition.Off -> UpkeepTag(stringResource(R.string.settings_connections_tag_off))
        AgentChatChannel.Condition.NotAnswering -> UpkeepTag(stringResource(R.string.settings_connections_tag_not_answering), TagTone.Bad)
        AgentChatChannel.Condition.Running -> UpkeepTag(stringResource(R.string.settings_connections_chat_running), TagTone.Good)
        AgentChatChannel.Condition.Starting -> UpkeepTag(stringResource(R.string.settings_connections_chat_starting), TagTone.Warning)
    }
}

@Composable
private fun serverDetails(server: AgentConnectedServer): String {
    val details = mutableListOf(server.transport)
    if (!server.isPersonal) details += stringResource(R.string.settings_connections_detail_shared)
    if (server.headless) details += stringResource(R.string.settings_connections_detail_headless)
    // An authorization leaves the app for the service's page and comes back to the dashboard, so it is done there.
    if (server.doesAuthorizeInBrowser && !server.isConnected) details += stringResource(R.string.settings_connections_detail_authorize)
    if (server.status == "error" && !server.lastError.isNullOrEmpty()) details += server.lastError.orEmpty()
    return details.filter { text -> text.isNotEmpty() }.joinToString(" · ")
}

@Composable
private fun chatDetails(channel: AgentChatChannel?, kind: String): String {
    if (channel == null) return chatAppHow(kind)
    val details = mutableListOf<String>()
    channel.botName?.takeIf { name -> name.isNotEmpty() }?.let { name -> details += name }
    if (channel.linked) details += stringResource(R.string.settings_connections_chat_linked, channel.linkedName.orEmpty())
    else channel.linkCode?.takeIf { code -> code.isNotEmpty() }?.let { code -> details += stringResource(R.string.settings_connections_chat_send_bot, code) }
    channel.lastError?.takeIf { text -> text.isNotEmpty() }?.let { text -> details += text }
    return if (details.isEmpty()) chatAppHow(kind) else details.joinToString(" · ")
}

@Composable
private fun reachOptionText(option: AgentReach.Option): String = when (option.optionKind) {
    AgentReach.OptionKind.ThroughThisServer -> stringResource(R.string.settings_connections_reach_this_server)
    AgentReach.OptionKind.NoComputerChosen -> stringResource(R.string.settings_connections_reach_no_choice)
    AgentReach.OptionKind.ThroughOnlyComputer -> stringResource(R.string.settings_connections_reach_only)
    AgentReach.OptionKind.ThroughComputer -> stringResource(R.string.settings_connections_reach_through, option.computerName)
    AgentReach.OptionKind.ThroughComputerNotAttached -> stringResource(R.string.settings_connections_reach_not_attached, option.computerName)
}
