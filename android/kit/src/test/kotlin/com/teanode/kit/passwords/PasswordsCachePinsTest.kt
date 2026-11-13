package com.teanode.kit.passwords

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.api.io.TempDir
import java.io.File

/** The pinned keys of other people live apart from the copy, are never read as another person's, and are never written over when they cannot be read. */
class PasswordsCachePinsTest {
    private fun contents(): PasswordsCacheContents = PasswordsCacheContents(userId = "user-ada", publicKey = "key", privateKeyByAccountKey = "sealed", lockAfterMinutes = 15)

    @Test fun `pinned keys are in the pins file not in the copy`(@TempDir directory: File) {
        val cache = PasswordsCache(FilePasswordsStorage(directory))
        cache.save(contents())
        cache.recordKnownIfAbsent("user-ada", "user-grace", "grace-key")
        cache.recordCompared("user-ada", "user-linus", "linus-key")

        val pins = cache.loadPins("user-ada")
        assertEquals(mapOf("user-grace" to "grace-key", "user-linus" to "linus-key"), pins.knownPublicKeys)
        assertEquals(mapOf("user-linus" to "linus-key"), pins.comparedPublicKeys)
        // The copy holds no key.
        val copyText = File(directory, "passwords.json").readText()
        assertFalse(copyText.contains("grace-key"))
        assertFalse(copyText.contains("linus-key"))
        // The key seen first is not replaced by a later first sight; only an accepted change replaces it.
        cache.recordKnownIfAbsent("user-ada", "user-grace", "other-key")
        assertEquals("grace-key", cache.loadPins("user-ada").knownPublicKeys["user-grace"])
        cache.recordChangedKey("user-ada", "user-grace", "other-key")
        var changedPins = cache.loadPins("user-ada")
        assertEquals("other-key", changedPins.knownPublicKeys["user-grace"])
        assertEquals("grace-key", changedPins.changedPublicKeys["user-grace"], "the key it replaced is kept for the notice")
        // Comparing codes marks the key and clears the notice; a dismissed notice keeps the new key.
        cache.recordCompared("user-ada", "user-grace", "other-key")
        changedPins = cache.loadPins("user-ada")
        assertEquals("other-key", changedPins.comparedPublicKeys["user-grace"])
        assertTrue(changedPins.isCompared("user-grace"))
        assertEquals(emptyMap<String, String>(), changedPins.changedPublicKeys)
        cache.recordChangedKey("user-ada", "user-grace", "third-key")
        changedPins = cache.loadPins("user-ada")
        assertFalse(changedPins.isCompared("user-grace"), "a new key is not the compared one")
        assertEquals("other-key", changedPins.changedPublicKeys["user-grace"])
        cache.dismissChangedKey("user-ada", "user-grace")
        changedPins = cache.loadPins("user-ada")
        assertEquals(emptyMap<String, String>(), changedPins.changedPublicKeys)
        assertEquals("third-key", changedPins.knownPublicKeys["user-grace"])
    }

    @Test fun `a pins file that cannot be read is never written over or taken for empty`(@TempDir directory: File) {
        val cache = PasswordsCache(FilePasswordsStorage(directory))
        cache.save(contents())
        cache.recordCompared("user-ada", "user-grace", "grace-key")
        val pinsFile = File(directory, "passwords.keys.json")
        val stored = pinsFile.readText()
        val damaged = """{"formatVersion":1,"userId":"user-ada","knownPublicKeys":5}"""
        pinsFile.writeText(damaged)
        val error = assertThrows<PasswordsCacheException> { cache.loadPins("user-ada") }
        assertEquals(PasswordsCacheFailure.Unreadable, error.failure)
        assertThrows<PasswordsCacheException> { cache.recordKnownIfAbsent("user-ada", "user-linus", "linus-key") }
        assertThrows<PasswordsCacheException> { cache.recordCompared("user-ada", "user-linus", "linus-key") }
        assertEquals(damaged, pinsFile.readText())
        pinsFile.writeText(stored)
        assertEquals(mapOf("user-grace" to "grace-key"), cache.loadPins("user-ada").comparedPublicKeys)
    }

    @Test fun `erasing keeping pins leaves the pins and erasing removes everything`(@TempDir directory: File) {
        val cache = PasswordsCache(FilePasswordsStorage(directory))
        cache.save(contents())
        cache.recordKnownIfAbsent("user-ada", "user-grace", "grace-key")
        cache.eraseKeepingPins()
        assertNull(cache.load())
        assertEquals(mapOf("user-grace" to "grace-key"), cache.loadPins("user-ada").knownPublicKeys)
        // Another person's keys are never read as this person's.
        assertEquals(PasswordsPins(userId = "user-other"), cache.loadPins("user-other"))
        cache.erase()
        assertEquals(PasswordsPins(userId = "user-ada"), cache.loadPins("user-ada"))
        assertFalse(File(directory, "passwords.keys.json").exists())
        cache.erase()
    }
}
