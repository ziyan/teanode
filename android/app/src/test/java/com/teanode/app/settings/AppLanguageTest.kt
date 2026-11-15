package com.teanode.app.settings

import androidx.core.os.LocaleListCompat
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/** The language the app is shown in is read back from the locales AppCompat keeps, whatever way the tag was written. */
@RunWith(RobolectricTestRunner::class)
class AppLanguageTest {
    @Test
    fun noLocaleMeansTheSystemLanguage() {
        assertEquals(AppLanguage.System, AppLanguage.fromLocales(LocaleListCompat.getEmptyLocaleList()))
    }

    @Test
    fun eachOfTheAppsLanguagesIsRecognized() {
        assertEquals(AppLanguage.English, AppLanguage.fromLocales(LocaleListCompat.forLanguageTags("en")))
        assertEquals(AppLanguage.English, AppLanguage.fromLocales(LocaleListCompat.forLanguageTags("en-GB")))
        assertEquals(AppLanguage.Chinese, AppLanguage.fromLocales(LocaleListCompat.forLanguageTags("zh-CN")))
        assertEquals(AppLanguage.Japanese, AppLanguage.fromLocales(LocaleListCompat.forLanguageTags("ja-JP")))
    }

    @Test
    fun aLanguageTheAppDoesNotHaveIsTheSystemLanguage() {
        assertEquals(AppLanguage.System, AppLanguage.fromLocales(LocaleListCompat.forLanguageTags("fr")))
    }

    @Test
    fun theChoicesCarryTheTagsTheManifestListsInItsLocaleConfig() {
        assertEquals(listOf(null, "en", "zh-CN", "ja"), AppLanguage.entries.map { language -> language.languageTag })
    }
}
