package com.teanode.app.signin

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.ContentPaste
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Eco
import androidx.compose.material.icons.outlined.QrCodeScanner
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.TeaNodeSnackbarHost

/**
 * SignInScreen connects the phone to a TeaNode server: its address typed here and signed in to on the server's own
 * page, or a code from the server's dashboard, scanned from a computer's screen or its link pasted.
 *
 * It holds no logic: [SignInRoute] gives it the session's state and carries out what it asks.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SignInScreen(
    serverText: String,
    onServerTextChange: (String) -> Unit,
    problemText: String?,
    isConnecting: Boolean,
    onSignIn: () -> Unit,
    onScan: () -> Unit,
    onPasteLink: () -> Unit,
    onOpenSetupGuide: () -> Unit,
    modifier: Modifier = Modifier,
    isAdding: Boolean = false,
    onCancel: (() -> Unit)? = null,
) {
    val isServerEmpty = serverText.isBlank()
    Scaffold(
        modifier = modifier,
        topBar = {
            TopAppBar(
                title = { Text(stringResource(if (isAdding) R.string.signin_add_account else R.string.app_name)) },
                navigationIcon = {
                    if (isAdding && onCancel != null) {
                        IconButton(onClick = onCancel) {
                            Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.common_cancel))
                        }
                    }
                },
            )
        },
        snackbarHost = { TeaNodeSnackbarHost() },
    ) { contentPadding ->
        Box(Modifier.fillMaxSize().padding(contentPadding), contentAlignment = Alignment.TopCenter) {
            Column(
                Modifier.widthIn(max = 480.dp).fillMaxWidth().verticalScroll(rememberScrollState()).padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(28.dp),
            ) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Icon(Icons.Outlined.Eco, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(40.dp))
                    Text(stringResource(R.string.signin_headline), style = MaterialTheme.typography.headlineSmall)
                    Text(stringResource(R.string.signin_subheadline), color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                // The address first: it is all a password sign-in needs, and the server's own page asks for the rest.
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    OutlinedTextField(
                        value = serverText,
                        onValueChange = onServerTextChange,
                        modifier = Modifier.fillMaxWidth(),
                        label = { Text(stringResource(R.string.signin_server_address)) },
                        placeholder = { Text(stringResource(R.string.signin_server_placeholder)) },
                        singleLine = true,
                        enabled = !isConnecting,
                        isError = problemText != null,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
                        keyboardActions = KeyboardActions(onGo = { if (!isServerEmpty) onSignIn() }),
                    )
                    Button(onClick = onSignIn, enabled = !isServerEmpty && !isConnecting, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                        Text(stringResource(R.string.signin_sign_in))
                    }
                    if (problemText != null) {
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.Top) {
                            Icon(Icons.Outlined.WarningAmber, contentDescription = null, tint = MaterialTheme.colorScheme.error, modifier = Modifier.size(18.dp))
                            Text(problemText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                        }
                    } else {
                        Text(stringResource(R.string.signin_address_hint), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                // A code from the dashboard, for a phone set up from a computer, or a link copied on this phone.
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text(stringResource(R.string.signin_or_code), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    // Side by side, or one above the other at the largest text sizes, where both would not fit.
                    val isLargeText = LocalDensity.current.fontScale >= 1.5f
                    if (isLargeText) {
                        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                            OutlinedButton(onClick = onScan, enabled = !isConnecting, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                                Icon(Icons.Outlined.QrCodeScanner, contentDescription = null, modifier = Modifier.size(18.dp))
                                Spacer(Modifier.size(8.dp))
                                Text(stringResource(R.string.signin_scan_code))
                            }
                            OutlinedButton(onClick = onPasteLink, enabled = !isConnecting, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                                Icon(Icons.Outlined.ContentPaste, contentDescription = null, modifier = Modifier.size(18.dp))
                                Spacer(Modifier.size(8.dp))
                                Text(stringResource(R.string.signin_paste_link))
                            }
                        }
                    } else {
                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            OutlinedButton(onClick = onScan, enabled = !isConnecting, modifier = Modifier.weight(1f).heightIn(min = 48.dp)) {
                                Icon(Icons.Outlined.QrCodeScanner, contentDescription = null, modifier = Modifier.size(18.dp))
                                Spacer(Modifier.size(8.dp))
                                Text(stringResource(R.string.signin_scan_code))
                            }
                            OutlinedButton(onClick = onPasteLink, enabled = !isConnecting, modifier = Modifier.weight(1f).heightIn(min = 48.dp)) {
                                Icon(Icons.Outlined.ContentPaste, contentDescription = null, modifier = Modifier.size(18.dp))
                                Spacer(Modifier.size(8.dp))
                                Text(stringResource(R.string.signin_paste_link))
                            }
                        }
                    }
                    Text(stringResource(R.string.signin_code_hint), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                // For somebody who came without a server: how to run one.
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text(stringResource(R.string.signin_no_server_yet), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    OutlinedButton(onClick = onOpenSetupGuide, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                        Icon(Icons.Outlined.Dns, contentDescription = null, modifier = Modifier.size(18.dp))
                        Spacer(Modifier.size(8.dp))
                        Text(stringResource(R.string.signin_set_up_server))
                    }
                    Text(stringResource(R.string.signin_open_source_hint), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            if (isConnecting) {
                // Dims the form and takes its taps while the sign-in finishes.
                Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.scrim.copy(alpha = 0.32f)).pointerInput(Unit) { detectTapGestures { } })
                Surface(
                    modifier = Modifier.align(Alignment.Center),
                    shape = MaterialTheme.shapes.large,
                    tonalElevation = 6.dp,
                    shadowElevation = 6.dp,
                ) {
                    Row(Modifier.padding(horizontal = 24.dp, vertical = 16.dp), horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 3.dp)
                        Text(stringResource(R.string.signin_connecting))
                    }
                }
            }
        }
    }
}

/** The sign-in screen wired to the session: the address, the browser tab, the scanner, the pasted link and the guide. */
@Composable
fun SignInRoute(isAdding: Boolean = false, onCancel: (() -> Unit)? = null) {
    val session = LocalAppSession.current
    val problemText by session.problemText.collectAsState()
    val signedOutText by session.signedOutText.collectAsState()
    val isConnecting by session.isConnecting.collectAsState()
    val openedLink by session.openedLink.collectAsState()
    var serverText by remember { mutableStateOf("") }
    var isScanning by remember { mutableStateOf(false) }
    var isShowingGuide by remember { mutableStateOf(false) }
    @Suppress("DEPRECATION") // LocalClipboard only has suspend functions; a button's click wants the plain ones.
    val clipboard = LocalClipboardManager.current

    // A link opened from a message, a page or the camera app. Anyone can send one that names their own server, so nothing is
    // asked of that server until the person has seen its name and said to go on. The connection then runs in the session.
    openedLink?.let { link ->
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { session.linkWasUsed() },
            title = { androidx.compose.material3.Text(stringResource(R.string.signin_link_ask_title, link.server.host)) },
            text = { androidx.compose.material3.Text(stringResource(R.string.signin_link_ask_body, link.server.host)) },
            confirmButton = {
                androidx.compose.material3.TextButton(onClick = { session.linkWasUsed(); session.connect(link.url) }) { androidx.compose.material3.Text(stringResource(R.string.signin_connect)) }
            },
            dismissButton = { androidx.compose.material3.TextButton(onClick = { session.linkWasUsed() }) { androidx.compose.material3.Text(stringResource(android.R.string.cancel)) } },
        )
    }

    when {
        isScanning -> ScanScreen(
            onFound = { text ->
                isScanning = false
                session.connect(text)
            },
            onCancel = { isScanning = false },
        )
        isShowingGuide -> ServerSetupGuideScreen(onBack = { isShowingGuide = false })
        else -> SignInScreen(
            serverText = serverText,
            onServerTextChange = { text ->
                serverText = text
                session.clearProblem()
            },
            problemText = problemText ?: if (isAdding) null else signedOutText,
            isConnecting = isConnecting,
            onSignIn = { session.startPasswordSignIn(serverText) },
            onScan = {
                session.clearProblem()
                isScanning = true
            },
            onPasteLink = {
                session.clearProblem()
                val pasted = clipboard.getText()?.text
                if (pasted != null) session.connect(pasted) else session.showProblem(session.notALinkText())
            },
            onOpenSetupGuide = { isShowingGuide = true },
            isAdding = isAdding,
            onCancel = onCancel,
        )
    }
}
