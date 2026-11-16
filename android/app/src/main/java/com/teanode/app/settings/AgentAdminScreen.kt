package com.teanode.app.settings

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.PersonOff
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.manage.AgentAdminSummary
import com.teanode.kit.manage.AgentLimitDraft
import com.teanode.kit.manage.managedAgents
import com.teanode.kit.manage.setAgentDisabled
import com.teanode.kit.manage.setAgentLimit

/** Everybody's agents with what each used today: set a daily limit, and switch one off for good measure. */
@Composable
fun AgentAdminScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.managedAgents() }
    var editing by remember { mutableStateOf<AgentAdminSummary?>(null) }
    val savedText = stringResource(R.string.settings_saved)
    LoadedSettingsPage(
        stringResource(R.string.settings_agents_title), onBack, loaded, isEmpty = { list -> list.isEmpty() },
        empty = { EmptyState(Icons.Outlined.PersonOff, stringResource(R.string.settings_agents_none)) },
    ) { agents ->
        items(agents.size) { index ->
            val agent = agents[index]
            val today = agent.today
            val lines = listOfNotNull(
                if (agent.isSwitchedOff) stringResource(R.string.settings_agents_switched_off) else if (!agent.enabled) stringResource(R.string.settings_agents_off) else null,
                today?.let { budget -> if (budget.limit > 0) stringResource(R.string.settings_agent_tokens_of, budget.used.toLong(), budget.limit.toLong()) else stringResource(R.string.settings_agent_tokens_unlimited, budget.used.toLong()) },
                today?.takeIf { budget -> budget.cost > 0 || budget.costLimit > 0 }?.let { budget -> "%.2f / %.2f %s".format(budget.cost, budget.costLimit, budget.currency.orEmpty()).trim() },
                if (agent.dead > 0) stringResource(R.string.settings_agents_dead, agent.dead) else null,
                if (agent.queued > 0) stringResource(R.string.settings_agents_queued, agent.queued) else null,
            )
            TeaNodeRow(
                headline = agent.shownName, overline = agent.username.takeIf { name -> name != agent.shownName }, supporting = lines.joinToString("\n").ifEmpty { null }, supportingMaxLines = 5,
                leading = { AvatarView(agent.shownName, null, null, size = 36.dp) },
                trailing = {
                    androidx.compose.foundation.layout.Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                        IconButton(onClick = { editing = agent }) { Icon(Icons.Outlined.Edit, stringResource(R.string.settings_agents_limits)) }
                        androidx.compose.material3.Switch(
                            checked = !agent.isSwitchedOff, onCheckedChange = { isOn -> actions.run(savedText, onSuccess = { loaded.refresh() }) { connection.setAgentDisabled(agent.agentId, !isOn) } },
                        )
                    }
                },
            )
        }
        item { FooterText(stringResource(R.string.settings_agents_footer)) }
    }
    editing?.let { agent ->
        var draft by remember(agent.agentId) { mutableStateOf(AgentLimitDraft.of(agent)) }
        FormDialog(
            title = stringResource(R.string.settings_agents_limits_for, agent.shownName), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = draft.isValid, isWorking = actions.isWorking,
            onConfirm = { actions.run(savedText, onSuccess = { editing = null; loaded.refresh() }) { connection.setAgentLimit(agent.agentId, draft) } },
            onDismiss = { editing = null },
        ) {
            FormField(draft.dailyTokensText, { draft = draft.copy(dailyTokensText = it) }, stringResource(R.string.settings_agents_daily_tokens), keyboardType = KeyboardType.Number, supporting = stringResource(R.string.settings_agents_empty_no_limit))
            FormField(draft.dailyCostText, { draft = draft.copy(dailyCostText = it) }, stringResource(R.string.settings_agents_daily_cost), keyboardType = KeyboardType.Decimal, supporting = stringResource(R.string.settings_agents_empty_no_limit))
        }
    }
}
