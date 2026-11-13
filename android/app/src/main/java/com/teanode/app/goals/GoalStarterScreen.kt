package com.teanode.app.goals

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.goals.startGoal
import kotlinx.coroutines.launch

/** GoalStarterScreen starts a goal: what it is, and what done looks like (ios GoalsView.swift, GoalStarter). */
@Composable
fun GoalStarterScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var title by remember { mutableStateOf("") }
    var details by remember { mutableStateOf("") }
    var isStarting by remember { mutableStateOf(false) }
    TeaNodeScreen(
        title = stringResource(R.string.goals_new_goal),
        onBack = { navigator.back() },
        actions = {
            TextButton(
                enabled = !isStarting && title.isNotBlank() && details.isNotBlank(),
                onClick = {
                    val goalTitle = title.trim()
                    val goalDetails = details.trim()
                    isStarting = true
                    scope.launch {
                        try {
                            connection.startGoal(goalTitle, goalDetails)
                            navigator.back()
                        } catch (failure: Exception) {
                            snackbar.showError(failure)
                        }
                        isStarting = false
                    }
                },
            ) { Text(stringResource(R.string.goals_start)) }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).padding(16.dp).verticalScroll(rememberScrollState())) {
            OutlinedTextField(
                value = title,
                onValueChange = { text -> title = text },
                label = { Text(stringResource(R.string.goals_starter_title_hint)) },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
            )
            OutlinedTextField(
                value = details,
                onValueChange = { text -> details = text },
                label = { Text(stringResource(R.string.goals_starter_details_hint)) },
                modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                minLines = 4,
            )
        }
    }
}
