package com.teanode.app.widgets

import android.app.PendingIntent
import android.content.Intent
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService

/**
 * The quick settings tiles, the Android counterpart of the iPhone app's Control Center controls: one calls the agent,
 * one starts a recording. A tile only opens the app, as the widget's buttons do: the call waits for the person's answer
 * to the app's own question, and the recorder starts with a tap on its screen, so neither the call nor the microphone
 * begins from the tile alone.
 */
abstract class AppOpeningTileService : TileService() {
    /** The address inside the app that the tile opens. */
    abstract val route: String

    /** Whether the tile asks for a call with the agent. */
    open val isCall: Boolean = false

    override fun onStartListening() {
        qsTile?.let { tile ->
            tile.state = Tile.STATE_INACTIVE
            tile.updateTile()
        }
    }

    @Suppress("DEPRECATION")
    override fun onClick() {
        val opening: Intent = openingIntent(this, route, isCall)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            val pending = PendingIntent.getActivity(this, route.hashCode(), opening, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
            startActivityAndCollapse(pending)
        } else {
            startActivityAndCollapse(opening)
        }
    }
}

/** The tile that calls the agent. */
class CallAgentTileService : AppOpeningTileService() {
    override val route: String = "agent/home"
    override val isCall: Boolean = true
}

/** The tile that opens the recorder. */
class StartRecordingTileService : AppOpeningTileService() {
    override val route: String = "recordings/capture"
}
