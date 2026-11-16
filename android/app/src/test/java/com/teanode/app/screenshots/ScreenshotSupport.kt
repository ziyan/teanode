package com.teanode.app.screenshots

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.Density
import com.github.takahirom.roborazzi.captureRoboImage
import com.teanode.app.ui.theme.AppearanceChoice
import java.io.File

/** Where the pictures go: one folder for the whole app under the module's build directory. */
internal val screenshotDirectory: File = File("build/outputs/roborazzi").also { directory -> directory.mkdirs() }

/** The phone every picture is taken on: a common 393 by 852 dp screen at 2.75x, in Robolectric's qualifier form. */
internal const val PHONE_QUALIFIERS = "w393dp-h852dp-xxhdpi"

/** Light or dark, the first axis of a picture. */
internal enum class Look(val fileName: String, val appearance: AppearanceChoice) {
    Light("light", AppearanceChoice.Light),
    Dark("dark", AppearanceChoice.Dark),
}

/** Draws [content] with the text scaled by [fontScale], 2.0 being the largest size a phone offers. */
@Composable
internal fun WithFontScale(fontScale: Float, content: @Composable () -> Unit) {
    val density = LocalDensity.current
    CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale)) { content() }
}

/** Writes the whole window as `<name>.png` and returns the file, so a test can say which picture it took. */
internal fun ComposeContentTestRule.photograph(name: String): File {
    val file = File(screenshotDirectory, "$name.png")
    onRoot().captureRoboImage(file.path)
    return file
}
