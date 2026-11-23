package com.teanode.app.calls

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Call
import androidx.compose.material.icons.filled.CallEnd
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.AvatarView

/** IncomingCallContent is the call ringing over the lock screen: who is calling, with Answer and Decline. */
@Composable
fun IncomingCallContent(callerName: String, onAnswer: () -> Unit, onDecline: () -> Unit, modifier: Modifier = Modifier) {
    Surface(modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            Modifier.fillMaxSize().safeDrawingPadding().padding(horizontal = 32.dp, vertical = 48.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.SpaceBetween,
        ) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text(stringResource(R.string.calls_incoming_call), style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.height(24.dp))
                AvatarView(name = callerName, avatarUrl = null, colorIndex = null, size = 120.dp)
                Spacer(Modifier.height(16.dp))
                Text(callerName, style = MaterialTheme.typography.headlineLarge, textAlign = TextAlign.Center)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(72.dp)) {
                RoundAnswerButton(Icons.Filled.CallEnd, MaterialTheme.colorScheme.error, MaterialTheme.colorScheme.onError, stringResource(R.string.calls_decline), onDecline)
                RoundAnswerButton(Icons.Filled.Call, MaterialTheme.colorScheme.primary, MaterialTheme.colorScheme.onPrimary, stringResource(R.string.calls_answer), onAnswer)
            }
        }
    }
}

@Composable
private fun RoundAnswerButton(icon: ImageVector, container: Color, content: Color, label: String, onClick: () -> Unit) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Surface(onClick = onClick, shape = CircleShape, color = container, contentColor = content, modifier = Modifier.size(72.dp).semantics { contentDescription = label }) {
            Box(Modifier.size(72.dp), contentAlignment = Alignment.Center) { Icon(icon, contentDescription = null, modifier = Modifier.size(32.dp)) }
        }
        Spacer(Modifier.height(8.dp))
        Text(label, style = MaterialTheme.typography.labelLarge)
    }
}
