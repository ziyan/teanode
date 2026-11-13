package com.teanode.app.photos

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.ui.theme.AppearanceChoice
import com.teanode.kit.photos.PhotoBackupFailure
import com.teanode.kit.photos.PhotoBackupPhase
import com.teanode.kit.photos.PhotoBackupStatus
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Pictures of every Photos screen and state, in the invented demo library: light and dark, the dense ones at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class PhotosScreenshotTest {
    @get:Rule val composeRule = createComposeRule()

    private fun shoot(name: String, appearance: AppearanceChoice = AppearanceChoice.Light, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = appearance) { WithFontScale(fontScale) { content() } } }
        composeRule.mainClock.advanceTimeBy(500)
        composeRule.waitForIdle()
        composeRule.photograph(name)
    }

    @Test fun `library all light`() = shoot("photos-library-all-light") { PhotosSection() }

    @Test fun `library all dark`() = shoot("photos-library-all-dark", AppearanceChoice.Dark) { PhotosSection() }

    @Test fun `library all largest text`() = shoot("photos-library-all-largest-text", fontScale = 2f) { PhotosSection() }

    @Test fun `library months light`() = shoot("photos-library-months-light") { PhotosSection(initialScale = PhotoScale.Months) }

    @Test fun `library years dark`() = shoot("photos-library-years-dark", AppearanceChoice.Dark) { PhotosSection(initialScale = PhotoScale.Years) }

    @Test fun `library selecting light`() = shoot("photos-library-selecting-light") { PhotosSection(initialIsSelecting = true, initialSelectedIds = setOf("p01", "p04")) }

    @Test fun `favorites light`() = shoot("photos-favorites-light") { PhotosSection(PhotoSectionChoice.Favorites) }

    @Test fun `trash light`() = shoot("photos-trash-light") { PhotosSection(PhotoSectionChoice.Trash) }

    @Test fun `trash selecting dark`() = shoot("photos-trash-selecting-dark", AppearanceChoice.Dark) { PhotosSection(PhotoSectionChoice.Trash, initialIsSelecting = true, initialSelectedIds = setOf("t01")) }

    @Test fun `albums light`() = shoot("photos-albums-light") { PhotosSection(PhotoSectionChoice.Albums) }

    @Test fun `albums dark`() = shoot("photos-albums-dark", AppearanceChoice.Dark) { PhotosSection(PhotoSectionChoice.Albums) }

    @Test fun `albums largest text`() = shoot("photos-albums-largest-text", fontScale = 2f) { PhotosSection(PhotoSectionChoice.Albums) }

    @Test fun `people light`() = shoot("photos-people-light") { PhotosSection(PhotoSectionChoice.People) }

    @Test fun `people dark`() = shoot("photos-people-dark", AppearanceChoice.Dark) { PhotosSection(PhotoSectionChoice.People) }

    @Test fun `viewer picture light`() = shoot("photos-viewer-picture-light") { PhotosSection(initialOpenedPhotoId = "p01") }

    @Test fun `viewer video dark`() = shoot("photos-viewer-video-dark", AppearanceChoice.Dark) { PhotosSection(initialOpenedPhotoId = "p02") }

    @Test fun `viewer failed light`() = shoot("photos-viewer-failed-light") { PhotosSection(initialOpenedPhotoId = "p09") }

    @Test fun `viewer waiting dark`() = shoot("photos-viewer-waiting-dark", AppearanceChoice.Dark) { PhotosSection(initialOpenedPhotoId = "p07") }

    @Test fun `viewer info light`() = shoot("photos-viewer-info-light") {
        Surface(Modifier.fillMaxSize()) { PhotoInfo(com.teanode.app.photos.PhotosDemoData.photos.let { list -> demoPhoto(list.first().id) }, onPhotoChanged = {}, albumIdOpenedFrom = null) }
    }

    @Test fun `viewer info largest text`() = shoot("photos-viewer-info-largest-text", fontScale = 2f) {
        Surface(Modifier.fillMaxSize()) { PhotoInfo(demoPhoto("p01"), onPhotoChanged = {}, albumIdOpenedFrom = null) }
    }

    @Test fun `album light`() = shoot("photos-album-light") { AlbumScreen("a1", onBack = {}) }

    @Test fun `album shared dark`() = shoot("photos-album-shared-dark", AppearanceChoice.Dark) { AlbumScreen("a3", onBack = {}) }

    @Test fun `album shares light`() = shoot("photos-album-shares-light") { AlbumSharesScreen("a1", onBack = {}) }

    @Test fun `album shares largest text`() = shoot("photos-album-shares-largest-text", fontScale = 2f) { AlbumSharesScreen("a1", onBack = {}) }

    @Test fun `person light`() = shoot("photos-person-light") { PersonScreen("g1", onBack = {}) }

    @Test fun `person unnamed dark`() = shoot("photos-person-unnamed-dark", AppearanceChoice.Dark) { PersonScreen("g2", onBack = {}) }

    @Test fun `backup off light`() = shoot("photos-backup-off-light") { PhotoBackupScreen(onBack = {}, initialIsEnabled = false) }

    @Test fun `backup uploading light`() = shoot("photos-backup-uploading-light") {
        PhotoBackupScreen(onBack = {}, initialIsEnabled = true, statusOverride = PhotoBackupStatus(PhotoBackupPhase.Uploading, 420, 1280, 860, 300, 2, 0, null, System.currentTimeMillis() - 3_600_000, 3_900))
    }

    @Test fun `backup problems dark`() = shoot("photos-backup-problems-dark", AppearanceChoice.Dark) {
        PhotoBackupScreen(onBack = {}, initialIsEnabled = true, statusOverride = PhotoBackupStatus(PhotoBackupPhase.WaitingForServer, 420, 1280, 860, 300, 3, 1, PhotoBackupFailure.ServerOutOfSpace))
    }

    @Test fun `backup needs access light`() = shoot("photos-backup-needs-access-light") {
        PhotoBackupScreen(onBack = {}, initialIsEnabled = true, statusOverride = PhotoBackupStatus(PhotoBackupPhase.NeedsAccess(isLimited = true)))
    }

    @Test fun `backup largest text`() = shoot("photos-backup-largest-text", fontScale = 2f) {
        PhotoBackupScreen(onBack = {}, initialIsEnabled = true, statusOverride = PhotoBackupStatus(PhotoBackupPhase.Uploading, 420, 1280, 860, 300, 2, 0, null, null, 3_900))
    }
}

private fun demoPhoto(id: String): com.teanode.kit.photos.Photo =
    com.teanode.kit.TeaNodeJson.decodeFromString(com.teanode.kit.photos.Photo.serializer(), PhotosDemoData.photoJson(PhotosDemoData.photos.first { photo -> photo.id == id }, withDescription = true))
