package com.teanode.app.session

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Test

class SecretFileNameTest {
    @Test
    fun aNameWithTheServerAddressIsOneFileName() {
        val fileName = secretFileName("photoBackupCredential|https://teanode.example.com|alex")
        assertFalse(fileName.contains('/'))
        assertEquals("photoBackupCredential|https:%2F%2Fteanode.example.com|alex", fileName)
    }

    @Test
    fun aPlainNameStaysWhereItWas() {
        assertEquals("accounts", secretFileName("accounts"))
    }

    @Test
    fun twoNamesNeverShareAFile() {
        assertFalse(secretFileName("a/b") == secretFileName("a%2Fb"))
    }
}
