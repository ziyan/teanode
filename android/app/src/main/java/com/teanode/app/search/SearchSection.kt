package com.teanode.app.search

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Search
import androidx.compose.runtime.Composable
import com.teanode.app.R
import com.teanode.app.shell.SectionPlaceholder

/** The search screen: the bottom bar's last item. Replace the placeholder with the real screen. */
@Composable
fun SearchSection() {
    SectionPlaceholder(titleResource = R.string.common_search, icon = Icons.Outlined.Search)
}
