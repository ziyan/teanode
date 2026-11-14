package com.teanode.app.passwords

import android.content.Context
import android.hardware.biometrics.BiometricManager
import android.hardware.biometrics.BiometricPrompt
import android.os.Build
import android.os.CancellationSignal
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyPermanentlyInvalidatedException
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import java.security.MessageDigest
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.suspendCancellableCoroutine

// Where the account key waits so the passphrase is typed once: sealed with a key of the Android Keystore that opens only
// for a fingerprint or face the phone knows, the counterpart of the iPhone's keychain item behind Face ID.

/** Why the kept account key could not be read or written. */
class PasswordsKeyStoreException(val failure: PasswordsKeyStoreFailure, cause: Throwable? = null) : Exception(failure.name, cause)

enum class PasswordsKeyStoreFailure {
    /** The person dismissed the prompt. */
    Canceled,

    /** The phone cannot keep a key behind biometrics: no sensor, or none enrolled. */
    Unavailable,

    /** The key is gone for good (the fingers or faces the phone knows changed) and a passphrase is needed again. */
    KeyLost,

    /** Anything else; the passphrase still works. */
    Failed,
}

/** PasswordsKeyStore keeps the account key between unlocks. The app's is behind the biometric prompt; demo mode and the tests keep it in memory. */
interface PasswordsKeyStore {
    /** Whether the phone can keep a key behind biometrics. */
    val isBiometryAvailable: Boolean

    suspend fun save(accountKey: ByteArray)

    /** The kept account key, after the person proved it is them; null when none is kept. */
    suspend fun load(promptReason: String): ByteArray?

    fun delete()
}

/** A [PasswordsKeyStore] that keeps the key in memory, for demo mode and tests. */
class MemoryPasswordsKeyStore(override val isBiometryAvailable: Boolean = true) : PasswordsKeyStore {
    private var kept: ByteArray? = null

    override suspend fun save(accountKey: ByteArray) {
        kept = accountKey.copyOf()
    }

    override suspend fun load(promptReason: String): ByteArray? = kept?.copyOf()

    override fun delete() {
        kept = null
    }
}

/**
 * The Android Keystore's own: an AES-GCM key that needs the person's biometrics for every use seals the account key, and
 * the sealed bytes are kept in the app's private preferences. The prompt is the system's (framework BiometricPrompt, no
 * extra library). Not verified on a phone: no emulator with a sensor was available to the port.
 */
class AndroidPasswordsKeyStore(private val context: Context, private val signInAccountKey: String) : PasswordsKeyStore {
    private val preferences = context.getSharedPreferences("teanode-passwords-key", Context.MODE_PRIVATE)
    private val alias = "teanode.passwords." + Base64.encodeToString(MessageDigest.getInstance("SHA-256").digest(signInAccountKey.toByteArray()), Base64.NO_WRAP or Base64.URL_SAFE).take(24)
    private val preferenceName get() = "kept." + alias

    override val isBiometryAvailable: Boolean
        get() = runCatching { context.getSystemService(BiometricManager::class.java)?.canAuthenticate() == BiometricManager.BIOMETRIC_SUCCESS }.getOrDefault(false)

    private fun keyStore(): KeyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

    private fun makeKey(): SecretKey {
        val builder = KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setUserAuthenticationRequired(true)
            .setInvalidatedByBiometricEnrollment(true)
        if (Build.VERSION.SDK_INT >= 30) builder.setUserAuthenticationParameters(0, KeyProperties.AUTH_BIOMETRIC_STRONG)
        else @Suppress("DEPRECATION") builder.setUserAuthenticationValidityDurationSeconds(-1)
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(builder.build())
        return generator.generateKey()
    }

    /** Asks for the person's biometrics with the cipher, and gives the cipher back once they passed. */
    private suspend fun authenticate(cipher: Cipher, reason: String): Cipher = suspendCancellableCoroutine { continuation ->
        val signal = CancellationSignal()
        continuation.invokeOnCancellation { signal.cancel() }
        val prompt = BiometricPrompt.Builder(context)
            .setTitle(reason)
            .setNegativeButton(context.getString(android.R.string.cancel), context.mainExecutor) { _, _ ->
                if (continuation.isActive) continuation.resumeWithException(PasswordsKeyStoreException(PasswordsKeyStoreFailure.Canceled))
            }
            .build()
        prompt.authenticate(
            BiometricPrompt.CryptoObject(cipher), signal, context.mainExecutor,
            object : BiometricPrompt.AuthenticationCallback() {
                override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                    val authenticated = result.cryptoObject?.cipher
                    if (continuation.isActive) {
                        if (authenticated != null) continuation.resume(authenticated)
                        else continuation.resumeWithException(PasswordsKeyStoreException(PasswordsKeyStoreFailure.Failed))
                    }
                }

                override fun onAuthenticationError(errorCode: Int, errorString: CharSequence?) {
                    if (!continuation.isActive) return
                    val failure = if (errorCode == android.hardware.biometrics.BiometricPrompt.BIOMETRIC_ERROR_USER_CANCELED || errorCode == android.hardware.biometrics.BiometricPrompt.BIOMETRIC_ERROR_CANCELED) {
                        PasswordsKeyStoreFailure.Canceled
                    } else {
                        PasswordsKeyStoreFailure.Failed
                    }
                    continuation.resumeWithException(PasswordsKeyStoreException(failure))
                }
            },
        )
    }

    override suspend fun save(accountKey: ByteArray) {
        if (!isBiometryAvailable) throw PasswordsKeyStoreException(PasswordsKeyStoreFailure.Unavailable)
        try {
            delete()
            val key = makeKey()
            val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key) }
            val authenticated = authenticate(cipher, context.getString(com.teanode.app.R.string.passwords_open_your_passwords))
            val sealed = authenticated.doFinal(accountKey)
            preferences.edit().putString(preferenceName, Base64.encodeToString(authenticated.iv, Base64.NO_WRAP) + ":" + Base64.encodeToString(sealed, Base64.NO_WRAP)).apply()
        } catch (error: PasswordsKeyStoreException) {
            delete()
            throw error
        } catch (error: Exception) {
            delete()
            throw PasswordsKeyStoreException(PasswordsKeyStoreFailure.Failed, error)
        }
    }

    override suspend fun load(promptReason: String): ByteArray? {
        val text = preferences.getString(preferenceName, null) ?: return null
        try {
            val key = keyStore().getKey(alias, null) as? SecretKey ?: return null.also { delete() }
            val (ivText, sealedText) = text.split(":")
            val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(128, Base64.decode(ivText, Base64.NO_WRAP))) }
            return authenticate(cipher, promptReason).doFinal(Base64.decode(sealedText, Base64.NO_WRAP))
        } catch (error: PasswordsKeyStoreException) {
            throw error
        } catch (error: KeyPermanentlyInvalidatedException) {
            delete()
            throw PasswordsKeyStoreException(PasswordsKeyStoreFailure.KeyLost, error)
        } catch (error: Exception) {
            throw PasswordsKeyStoreException(PasswordsKeyStoreFailure.Failed, error)
        }
    }

    override fun delete() {
        preferences.edit().remove(preferenceName).apply()
        runCatching { keyStore().deleteEntry(alias) }
    }
}
