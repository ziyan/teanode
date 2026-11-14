package com.teanode.app.screenshots

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * The app's icon as launchers cut it: a circle and a rounded square of the adaptive icon's layers, and its themed
 * (monochrome) layer on a tinted circle, so a change to the logo's paths is seen before it ships.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class LauncherIconScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun theIconAsLaunchersCutIt() {
        composeRule.setContent {
            Row(
                modifier = Modifier.background(Color(0xFF3C3C3C)).padding(24.dp),
                horizontalArrangement = Arrangement.spacedBy(24.dp),
            ) {
                for (iconShape in listOf(CircleShape, RoundedCornerShape(28.dp))) {
                    Box(modifier = Modifier.size(108.dp).clip(iconShape)) {
                        Image(painterResource(R.drawable.ic_launcher_background), contentDescription = null, modifier = Modifier.size(108.dp))
                        Image(painterResource(R.drawable.ic_launcher_foreground), contentDescription = null, modifier = Modifier.size(108.dp))
                    }
                }
                Box(modifier = Modifier.size(108.dp).clip(CircleShape).background(Color(0xFFDCE8C8))) {
                    Image(painterResource(R.drawable.ic_launcher_monochrome), contentDescription = null, modifier = Modifier.size(108.dp))
                }
            }
        }
        composeRule.photograph("launcher-icon")
    }
}
