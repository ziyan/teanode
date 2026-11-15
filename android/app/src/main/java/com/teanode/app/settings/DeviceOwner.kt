package com.teanode.app.settings

import android.content.Context
import android.hardware.biometrics.BiometricManager
import android.hardware.biometrics.BiometricPrompt
import android.os.Build
import android.os.CancellationSignal
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlin.coroutines.resume

/**
 * DeviceOwner asks the phone's owner to confirm, with a fingerprint, a face or the screen lock. A phone with none of
 * them set cannot tell its owner from anyone else, so confirming is not refused there: the server's approval is still
 * the person's own, signed in. Approving spends money, so it takes the phone's owner and not just whoever holds it
 * unlocked; declining spends nothing and needs no one.
 */
object DeviceOwner {
    suspend fun confirm(context: Context, approvalPrompt: String): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) return true
        val authenticators = BiometricManager.Authenticators.BIOMETRIC_WEAK or BiometricManager.Authenticators.DEVICE_CREDENTIAL
        val canAuthenticate = runCatching { context.getSystemService(BiometricManager::class.java)?.canAuthenticate(authenticators) }.getOrNull()
        if (canAuthenticate != BiometricManager.BIOMETRIC_SUCCESS) return true
        return suspendCancellableCoroutine { continuation ->
            val signal = CancellationSignal()
            continuation.invokeOnCancellation { signal.cancel() }
            val prompt = BiometricPrompt.Builder(context).setTitle(approvalPrompt).setAllowedAuthenticators(authenticators).build()
            prompt.authenticate(
                signal, context.mainExecutor,
                object : BiometricPrompt.AuthenticationCallback() {
                    override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                        if (continuation.isActive) continuation.resume(true)
                    }

                    override fun onAuthenticationError(errorCode: Int, errorMessage: CharSequence?) {
                        if (continuation.isActive) continuation.resume(false)
                    }
                },
            )
        }
    }
}
