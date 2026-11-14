package com.teanode.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

/**
 * The app's colors are TeaNode green (#729D39, the logo's), worked out into Material 3's tonal palette by hand rather
 * than from the phone's wallpaper: dynamic color is off so the brand shows. Light mode takes the green a little darker
 * so white text on it reads; dark mode takes the iPhone app's lighter green (#8FBF52).
 */
private val LightColors = lightColorScheme(
    primary = Color(0xFF577A28),
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFCAEF96),
    onPrimaryContainer = Color(0xFF161F00),
    inversePrimary = Color(0xFFAED27B),
    secondary = Color(0xFF586248),
    onSecondary = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFFDCE7C9),
    onSecondaryContainer = Color(0xFF171E04),
    tertiary = Color(0xFF216777),
    onTertiary = Color(0xFFFFFFFF),
    tertiaryContainer = Color(0xFFACEDFF),
    onTertiaryContainer = Color(0xFF001F26),
    background = Color(0xFFFBFDF7),
    onBackground = Color(0xFF1B1C18),
    surface = Color(0xFFFBFDF7),
    onSurface = Color(0xFF1B1C18),
    surfaceVariant = Color(0xFFE0E4D7),
    onSurfaceVariant = Color(0xFF44483E),
    surfaceTint = Color(0xFF577A28),
    inverseSurface = Color(0xFF2F312D),
    inverseOnSurface = Color(0xFFF2F4EF),
    error = Color(0xFFBA1A1A),
    onError = Color(0xFFFFFFFF),
    errorContainer = Color(0xFFFFDAD6),
    onErrorContainer = Color(0xFF410002),
    outline = Color(0xFF74796D),
    outlineVariant = Color(0xFFC4C8BB),
    scrim = Color(0xFF000000),
    surfaceBright = Color(0xFFFBFDF7),
    surfaceDim = Color(0xFFD9DBD5),
    surfaceContainerLowest = Color(0xFFFFFFFF),
    surfaceContainerLow = Color(0xFFF2F4EF),
    surfaceContainer = Color(0xFFEDEFE9),
    surfaceContainerHigh = Color(0xFFE7E9E3),
    surfaceContainerHighest = Color(0xFFE1E3DE),
)

private val DarkColors = darkColorScheme(
    primary = Color(0xFF8FBF52),
    onPrimary = Color(0xFF203700),
    primaryContainer = Color(0xFF2D5000),
    onPrimaryContainer = Color(0xFFCAEF96),
    inversePrimary = Color(0xFF456816),
    secondary = Color(0xFFC0CBAD),
    onSecondary = Color(0xFF2A331C),
    secondaryContainer = Color(0xFF404A32),
    onSecondaryContainer = Color(0xFFDCE7C9),
    tertiary = Color(0xFF90D0E2),
    onTertiary = Color(0xFF003641),
    tertiaryContainer = Color(0xFF034E5C),
    onTertiaryContainer = Color(0xFFACEDFF),
    background = Color(0xFF131411),
    onBackground = Color(0xFFE1E3DE),
    surface = Color(0xFF131411),
    onSurface = Color(0xFFE1E3DE),
    surfaceVariant = Color(0xFF44483E),
    onSurfaceVariant = Color(0xFFC4C8BB),
    surfaceTint = Color(0xFF8FBF52),
    inverseSurface = Color(0xFFE1E3DE),
    inverseOnSurface = Color(0xFF2F312D),
    error = Color(0xFFFFB4AB),
    onError = Color(0xFF690005),
    errorContainer = Color(0xFF93000A),
    onErrorContainer = Color(0xFFFFDAD6),
    outline = Color(0xFF8E9286),
    outlineVariant = Color(0xFF44483E),
    scrim = Color(0xFF000000),
    surfaceBright = Color(0xFF393B35),
    surfaceDim = Color(0xFF131411),
    surfaceContainerLowest = Color(0xFF0D0F0B),
    surfaceContainerLow = Color(0xFF1B1C18),
    surfaceContainer = Color(0xFF1F201C),
    surfaceContainerHigh = Color(0xFF292B26),
    surfaceContainerHighest = Color(0xFF343630),
)

/** Which scheme the person chose in Settings: the system's, always light, or always dark (iOS `Appearance`). */
enum class AppearanceChoice(val storedName: String) {
    System("system"),
    Light("light"),
    Dark("dark"),
    ;

    companion object {
        fun fromStoredName(storedName: String?): AppearanceChoice = entries.firstOrNull { choice -> choice.storedName == storedName } ?: System
    }
}

/** The app's Material 3 theme. Every screen is drawn inside it. */
@Composable
fun TeaNodeTheme(appearance: AppearanceChoice = AppearanceChoice.System, content: @Composable () -> Unit) {
    val isDark = when (appearance) {
        AppearanceChoice.System -> isSystemInDarkTheme()
        AppearanceChoice.Light -> false
        AppearanceChoice.Dark -> true
    }
    val colorScheme: ColorScheme = if (isDark) DarkColors else LightColors
    MaterialTheme(colorScheme = colorScheme, content = content)
}

/** The twelve colors a person without a picture is drawn on, the same as the dashboard's `--avatar-0` to `--avatar-11`. */
object AvatarPalette {
    private val lightColors = longArrayOf(
        0xB23A2E, 0xB0521A, 0x8C6600, 0x5C7A1C, 0x1D7A45, 0x0F7A73, 0x0E6E94, 0x2F5FB3, 0x5B4FC2, 0x8A44B3, 0xA63A82, 0x5F6873,
    )
    private val darkColors = longArrayOf(
        0xA8392E, 0xA4501C, 0x7D5C00, 0x55711C, 0x1B6D3F, 0x0E6D67, 0x0E6485, 0x2C56A3, 0x5348B0, 0x7C3FA3, 0x953676, 0x565E69,
    )

    /** Each holds white text at 4.5 to 1 or better; the dark set is the same hues one step down. */
    fun color(index: Int, isDark: Boolean): Color {
        val position = if (index in lightColors.indices) index else 0
        return Color(0xFF000000L or (if (isDark) darkColors[position] else lightColors[position]))
    }
}
