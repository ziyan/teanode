package com.teanode.app.shell

import androidx.annotation.StringRes
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.runtime.Composable
import androidx.compose.foundation.layout.padding
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.AppSection

/** What a part shows until its screens are built: its name, and that it is not built yet. */
@Composable
fun SectionPlaceholder(section: AppSection) {
    SectionPlaceholder(titleResource = section.titleResource, icon = section.icon)
}

@Composable
fun SectionPlaceholder(@StringRes titleResource: Int, icon: ImageVector) {
    TeaNodeScreen(title = stringResource(titleResource)) { contentPadding ->
        EmptyState(
            icon = icon,
            title = stringResource(titleResource),
            description = stringResource(R.string.placeholder_not_built),
            modifier = Modifier.padding(contentPadding),
        )
    }
}
