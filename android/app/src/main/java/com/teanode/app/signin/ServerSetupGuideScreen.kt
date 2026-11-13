package com.teanode.app.signin

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.automirrored.outlined.MenuBook
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.openWebPage

private const val INSTALL_COMMAND = "curl -fsSL https://teanode.com/get.sh | sh"
private const val GUIDE_ADDRESS = "https://teanode.com/doc/getting-started"
private const val INSTALL_SCRIPT_ADDRESS = "https://github.com/ziyan/teanode/blob/main/deploy/get.sh"
private const val SOURCE_ADDRESS = "https://github.com/ziyan/teanode"

/** ServerSetupGuideScreen tells somebody without a server how to run one: the one command, and what to do after it. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServerSetupGuideScreen(onBack: () -> Unit) {
    val context = LocalContext.current
    @Suppress("DEPRECATION") // LocalClipboard only has suspend functions; a button's click wants the plain ones.
    val clipboard = LocalClipboardManager.current
    var hasCopiedCommand by remember { mutableStateOf(false) }
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.guide_title)) },
                navigationIcon = {
                    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.common_back)) }
                },
            )
        },
    ) { contentPadding ->
        LazyColumn(Modifier.padding(contentPadding)) {
            item { Paragraph(R.string.guide_intro) }
            item { TeaNodeSectionHeader(stringResource(R.string.guide_step_computer_title)) }
            item { Paragraph(R.string.guide_step_computer_body) }
            item { TeaNodeSectionHeader(stringResource(R.string.guide_step_install_title)) }
            item {
                Column {
                    Surface(
                        color = MaterialTheme.colorScheme.surfaceContainerHigh, shape = MaterialTheme.shapes.small,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                    ) {
                        Text(INSTALL_COMMAND, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(12.dp))
                    }
                    TeaNodeRow(
                        headline = stringResource(if (hasCopiedCommand) R.string.guide_copied else R.string.guide_copy_command),
                        leadingIcon = if (hasCopiedCommand) Icons.Outlined.Check else Icons.Outlined.ContentCopy,
                        onClick = {
                            clipboard.setText(AnnotatedString(INSTALL_COMMAND))
                            hasCopiedCommand = true
                        },
                    )
                    TeaNodeRow(
                        headline = stringResource(R.string.guide_read_script),
                        leadingIcon = Icons.Outlined.Description,
                        onClick = { openWebPage(context, INSTALL_SCRIPT_ADDRESS) },
                    )
                    Paragraph(R.string.guide_step_install_note, isSecondary = true)
                }
            }
            item { TeaNodeSectionHeader(stringResource(R.string.guide_step_claim_title)) }
            item { Paragraph(R.string.guide_step_claim_body) }
            item { TeaNodeSectionHeader(stringResource(R.string.guide_step_https_title)) }
            item { Paragraph(R.string.guide_step_https_body) }
            item { TeaNodeSectionHeader(stringResource(R.string.guide_step_signin_title)) }
            item { Paragraph(R.string.guide_step_signin_body) }
            item {
                Column(Modifier.padding(top = 16.dp)) {
                    TeaNodeRow(stringResource(R.string.guide_full_guide), leadingIcon = Icons.AutoMirrored.Outlined.MenuBook, onClick = { openWebPage(context, GUIDE_ADDRESS) })
                    TeaNodeRow(stringResource(R.string.guide_source_code), leadingIcon = Icons.Outlined.Code, onClick = { openWebPage(context, SOURCE_ADDRESS) })
                    Paragraph(R.string.guide_full_guide_note, isSecondary = true)
                }
            }
        }
    }
}

@Composable
private fun Paragraph(textResource: Int, isSecondary: Boolean = false) {
    Text(
        stringResource(textResource),
        style = if (isSecondary) MaterialTheme.typography.bodySmall else MaterialTheme.typography.bodyMedium,
        color = if (isSecondary) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
    )
}
