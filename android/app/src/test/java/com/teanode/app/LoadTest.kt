package com.teanode.app

import androidx.compose.material3.Text
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.teanode.app.demo.DemoHost
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.TeaNodeError
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** rememberLoad and LoadedContent: the loading, error, retry and empty states every screen shares. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class LoadTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun aReadShowsItsValue() {
        composeRule.setContent {
            DemoHost {
                val load = rememberLoad { listOf("one", "two") }
                LoadedContent(load) { values -> Text(values.joinToString()) }
            }
        }
        composeRule.onNodeWithText("one, two").assertIsDisplayed()
    }

    @Test
    fun aFailedReadSaysWhyAndTriesAgain() {
        var attempts = 0
        composeRule.setContent {
            DemoHost {
                val load = rememberLoad {
                    attempts += 1
                    if (attempts == 1) throw TeaNodeError.Refused("photo storage is unavailable")
                    "back again"
                }
                LoadedContent(load) { value -> Text(value) }
            }
        }
        composeRule.onNodeWithText("This could not be loaded").assertIsDisplayed()
        composeRule.onNodeWithText("Photo storage is unavailable.").assertIsDisplayed()
        composeRule.onNodeWithText("Try again").performClick()
        composeRule.onNodeWithText("back again").assertIsDisplayed()
        assertEquals(2, attempts)
    }

    @Test
    fun anEmptyAnswerShowsTheEmptyState() {
        composeRule.setContent {
            DemoHost {
                val load = rememberLoad { emptyList<String>() }
                LoadedContent(load, isEmpty = { values -> values.isEmpty() }, empty = { Text("Nothing here") }) { values -> Text(values.joinToString()) }
            }
        }
        composeRule.onNodeWithText("Nothing here").assertIsDisplayed()
    }
}
