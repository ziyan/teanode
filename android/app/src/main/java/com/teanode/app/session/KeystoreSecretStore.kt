package com.teanode.app.session

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import com.teanode.kit.SecretStore
import java.io.File
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * KeystoreSecretStore keeps the sign-ins on this phone, each secret in a file of its own, sealed with an AES-GCM key
 * that lives in the Android Keystore and never leaves it. Nothing is written in plaintext, and a file copied off the
 * phone cannot be read.
 */
class KeystoreSecretStore(context: Context, private val keyAlias: String = "teanode.secrets") : SecretStore {
    private val directory = File(context.filesDir, "secrets").apply { mkdirs() }

    @Synchronized
    override fun read(name: String): ByteArray? {
        val file = fileFor(name)
        if (!file.isFile) return null
        val sealed = file.readBytes()
        if (sealed.size <= IV_LENGTH_BYTES) return null
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(TAG_LENGTH_BITS, sealed, 0, IV_LENGTH_BYTES))
        return cipher.doFinal(sealed, IV_LENGTH_BYTES, sealed.size - IV_LENGTH_BYTES)
    }

    @Synchronized
    override fun write(name: String, secret: ByteArray) {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val sealed = cipher.iv + cipher.doFinal(secret)
        // Written beside the file and moved over it, so a crash never leaves half a sign-in.
        val temporary = File(directory, "$name.tmp")
        temporary.writeBytes(sealed)
        if (!temporary.renameTo(fileFor(name))) {
            fileFor(name).writeBytes(sealed)
            temporary.delete()
        }
    }

    @Synchronized
    override fun delete(name: String) {
        fileFor(name).delete()
    }

    private fun fileFor(name: String) = File(directory, "$name.bin")

    private fun key(): SecretKey {
        val keyStore = KeyStore.getInstance(ANDROID_KEY_STORE).apply { load(null) }
        (keyStore.getKey(keyAlias, null) as? SecretKey)?.let { existing -> return existing }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEY_STORE)
        generator.init(
            KeyGenParameterSpec.Builder(keyAlias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    private companion object {
        const val ANDROID_KEY_STORE = "AndroidKeyStore"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val IV_LENGTH_BYTES = 12
        const val TAG_LENGTH_BITS = 128
    }
}

/** MemorySecretStore keeps secrets in memory only: demo mode and screenshot tests, where nothing is a real sign-in. */
class MemorySecretStore : SecretStore {
    private val secrets = HashMap<String, ByteArray>()

    @Synchronized
    override fun read(name: String): ByteArray? = secrets[name]

    @Synchronized
    override fun write(name: String, secret: ByteArray) {
        secrets[name] = secret
    }

    @Synchronized
    override fun delete(name: String) {
        secrets.remove(name)
    }
}
