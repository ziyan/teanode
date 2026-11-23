package com.teanode.app.widgets

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.view.View
import android.widget.FrameLayout
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.runComposition
import androidx.test.core.app.ApplicationProvider
import com.teanode.kit.notifications.Glance
import com.teanode.kit.notifications.GlanceEvent
import com.teanode.kit.notifications.WaitingInteraction
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** Photographs of the three widgets as the home screen draws them, from an invented reading of a server. */
@OptIn(androidx.glance.ExperimentalGlanceApi::class, androidx.glance.appwidget.ExperimentalGlanceRemoteViewsApi::class)
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = "w393dp-h852dp-xxhdpi")
open class WidgetScreenshotTest {
    /** Added to every picture's name, so the dark ones sit beside the light. */
    protected open val lookName: String = "light"

    private val context: Application = ApplicationProvider.getApplicationContext()
    private val directory = File("build/outputs/roborazzi").also { folder -> folder.mkdirs() }
    private val now = System.currentTimeMillis()

    private val realReader = glanceReader

    @After
    fun putTheRealReadingBack() {
        glanceReader = realReader
    }

    private fun connectedWith(glance: Glance?) = GlanceEntry(glance, isConnected = true, accountKey = "account-1")

    private fun glance(waitingCount: Int, unreadCount: Int, events: List<GlanceEvent>, newest: WaitingInteraction?) =
        Glance(events = events, unreadCount = unreadCount, agentName = "Juno", waitingCount = waitingCount, newestWaiting = newest)

    private fun photograph(name: String, widget: GlanceAppWidget, entry: GlanceEntry, widthDp: Int, heightDp: Int) {
        glanceReader = { entry }
        val remoteViews = runBlocking { widget.runComposition(context).first() }
        val density = context.resources.displayMetrics.density
        val parent = FrameLayout(context)
        val view = remoteViews.apply(context, parent)
        parent.addView(view)
        parent.measure(
            View.MeasureSpec.makeMeasureSpec((widthDp * density).toInt(), View.MeasureSpec.EXACTLY),
            View.MeasureSpec.makeMeasureSpec((heightDp * density).toInt(), View.MeasureSpec.EXACTLY),
        )
        parent.layout(0, 0, parent.measuredWidth, parent.measuredHeight)
        // A home screen's wallpaper behind it, so the widget's own edge can be seen.
        val bitmap = Bitmap.createBitmap(parent.width, parent.height, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(bitmap)
        canvas.drawColor(if (lookName == "dark") Color.rgb(32, 40, 48) else Color.rgb(176, 190, 197))
        parent.draw(canvas)
        val file = File(directory, "widget-$name-$lookName.png")
        file.outputStream().use { output -> bitmap.compress(Bitmap.CompressFormat.PNG, 100, output) }
        assertTrue(file.length() > 0)
    }

    private val events = listOf(
        GlanceEvent("e1", "Design review", "Room 4", now + 3_600_000L, now + 7_200_000L, false),
        GlanceEvent("e2", "Lunch with Sam", "", now + 10_800_000L, now + 14_400_000L, false),
        GlanceEvent("e3", "Sprint planning", "", now + 90_000_000L, now + 93_600_000L, false),
    )
    private val approval = WaitingInteraction("w1", "conversation-1", "approval", "Send the draft reply to Alex?", "")

    @Test
    fun theAgentWidget() = photograph("agent", AgentWidget(), connectedWith(glance(0, 0, emptyList(), null)), 180, 180)

    @Test
    fun theWaitingWidgetWithAnApproval() = photograph("waiting-approval", WaitingWidget(), connectedWith(glance(2, 0, events, approval)), 180, 180)

    @Test
    fun theWaitingWidgetWithNothingWaiting() = photograph("waiting-empty", WaitingWidget(), connectedWith(glance(0, 0, events, null)), 180, 180)

    @Test
    fun theNextWidget() = photograph("next", NextWidget(), connectedWith(glance(0, 3, events, null)), 250, 180)

    @Test
    fun theNextWidgetWithNothingComingUp() = photograph("next-empty", NextWidget(), connectedWith(glance(0, 0, emptyList(), null)), 250, 180)

    @Test
    fun aWidgetBeforeSignIn() = photograph("not-connected", NextWidget(), GlanceEntry(null, isConnected = false, accountKey = ""), 250, 180)
}

/** The same pictures with the phone in dark mode. */
@OptIn(androidx.glance.ExperimentalGlanceApi::class, androidx.glance.appwidget.ExperimentalGlanceRemoteViewsApi::class)
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = "w393dp-h852dp-night-xxhdpi")
class WidgetDarkScreenshotTest : WidgetScreenshotTest() {
    override val lookName: String = "dark"
}
