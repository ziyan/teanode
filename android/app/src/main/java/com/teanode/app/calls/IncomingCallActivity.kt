package com.teanode.app.calls

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.core.content.ContextCompat
import com.teanode.app.MainActivity
import com.teanode.app.TeaNodeApplication
import com.teanode.app.ui.theme.TeaNodeTheme
import com.teanode.app.ui.theme.AppearanceChoice

/**
 * IncomingCallActivity is what the full-screen notification opens over the lock screen: who is calling, with Answer
 * and Decline. Answering asks for the microphone first, then starts the call and brings the app forward.
 */
class IncomingCallActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setShowWhenLocked(true)
        setTurnScreenOn(true)
        val center = (application as TeaNodeApplication).push.calls
        val entryId = intent.getStringExtra(EXTRA_ENTRY_ID).orEmpty()
        val shouldAnswerAtOnce = intent.getBooleanExtra(EXTRA_SHOULD_ANSWER, false)
        setContent {
            TeaNodeTheme(AppearanceChoice.System) {
                val rings by center.ringing.collectAsState()
                val session by center.session.collectAsState()
                val ring = rings.firstOrNull { candidate -> candidate.entryId == entryId }
                // The ring ended (answered elsewhere, timed out, declined): nothing is left to answer.
                LaunchedEffect(ring) { if (ring == null && session == null) finish() }
                val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { isGranted ->
                    if (isGranted) {
                        answer(center, entryId)
                    } else {
                        center.declineRing(entryId)
                        finish()
                    }
                }
                fun answerOrAsk() {
                    if (hasMicrophone()) answer(center, entryId) else launcher.launch(Manifest.permission.RECORD_AUDIO)
                }
                LaunchedEffect(shouldAnswerAtOnce, ring != null) { if (shouldAnswerAtOnce && ring != null) answerOrAsk() }
                IncomingCallContent(
                    callerName = ring?.callerName.orEmpty(),
                    onAnswer = { answerOrAsk() },
                    onDecline = {
                        center.declineRing(entryId)
                        finish()
                    },
                )
            }
        }
    }

    private fun hasMicrophone(): Boolean = ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED

    private fun answer(center: CallCenter, entryId: String) {
        center.answerRing(entryId)
        startActivity(Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT))
        finish()
    }

    companion object {
        private const val EXTRA_ENTRY_ID = "entryId"
        private const val EXTRA_SHOULD_ANSWER = "shouldAnswer"

        fun intent(context: Context, entryId: String, shouldAnswer: Boolean): Intent =
            Intent(context, IncomingCallActivity::class.java)
                .putExtra(EXTRA_ENTRY_ID, entryId)
                .putExtra(EXTRA_SHOULD_ANSWER, shouldAnswer)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
    }
}
