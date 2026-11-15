package com.teanode.app.settings

import androidx.appcompat.app.AppCompatDelegate
import androidx.core.os.LocaleListCompat

/**
 * The language the app is shown in, chosen in the app so that every Android version can pick one: AppCompat keeps the
 * choice itself below Android 13 and hands it to the system's per-app language setting from Android 13 on.
 */
enum class AppLanguage(val languageTag: String?) {
    System(null),
    English("en"),
    Chinese("zh-CN"),
    Japanese("ja"),
    ;

    fun apply() {
        AppCompatDelegate.setApplicationLocales(if (languageTag == null) LocaleListCompat.getEmptyLocaleList() else LocaleListCompat.forLanguageTags(languageTag))
    }

    companion object {
        /** The language chosen now: the first of the app's locales, or [System] when none is set or it is not one the app has. */
        fun fromLocales(locales: LocaleListCompat): AppLanguage {
            val language = locales.get(0)?.language ?: return System
            return entries.firstOrNull { candidate -> candidate.languageTag?.substringBefore('-') == language } ?: System
        }

        fun current(): AppLanguage = fromLocales(AppCompatDelegate.getApplicationLocales())
    }
}
