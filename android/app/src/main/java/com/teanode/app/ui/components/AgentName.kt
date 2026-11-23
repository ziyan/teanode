package com.teanode.app.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import com.teanode.app.session.LocalConnection
import com.teanode.kit.chats.agentName

/** What the agent is called, read once when the screen opens; empty until read, or when it has no name. */
@Composable
fun rememberAgentName(): String {
    val connection = LocalConnection.current
    val agentName by produceState("", connection) { value = runCatching { connection.agentName().trim() }.getOrDefault("") }
    return agentName
}
