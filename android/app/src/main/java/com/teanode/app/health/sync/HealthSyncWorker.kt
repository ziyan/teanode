package com.teanode.app.health.sync

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters

/** Runs one copying pass from the background, every hour while Health is on. */
class HealthSyncWorker(context: Context, parameters: WorkerParameters) : CoroutineWorker(context, parameters) {
    override suspend fun doWork(): Result {
        HealthSync.get(applicationContext).syncNow()
        return Result.success()
    }
}
