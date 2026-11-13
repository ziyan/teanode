package com.teanode.kit.passwords

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class PasswordsGeneratorTest {
    private fun isAsciiLowercase(character: Char) = character.code < 128 && character.isLowerCase()

    private fun isAsciiUppercase(character: Char) = character.code < 128 && character.isUpperCase()

    private fun isAsciiDigit(character: Char) = character.code < 128 && character.isDigit()

    @Test fun `every kind asked for appears and the length is right`() {
        repeat(50) { attempt ->
            val password = PasswordsGenerator.generate(PasswordsGeneratorOptions(length = 8))
            assertEquals(8, password.length, "attempt $attempt")
            assertTrue(password.any(::isAsciiLowercase))
            assertTrue(password.any(::isAsciiUppercase))
            assertTrue(password.any(::isAsciiDigit))
            assertTrue(password.any { character -> PasswordsGenerator.SYMBOLS.contains(character) })
        }
    }

    @Test fun `look alikes are left out when asked`() {
        val options = PasswordsGeneratorOptions(length = 128, shouldAvoidLookAlikes = true)
        repeat(20) {
            val password = PasswordsGenerator.generate(options)
            assertFalse(password.any { character -> PasswordsGenerator.LOOK_ALIKES.contains(character) })
        }
    }

    @Test fun `only the kinds chosen are used`() {
        val options = PasswordsGeneratorOptions(length = 40, shouldIncludeLetters = false, shouldIncludeDigits = true, shouldIncludeSymbols = false)
        val password = PasswordsGenerator.generate(options)
        assertEquals(40, password.length)
        assertTrue(password.all(::isAsciiDigit))
    }

    @Test fun `no kind and an impossible length are refused`() {
        val none = PasswordsGeneratorOptions(length = 20, shouldIncludeLetters = false, shouldIncludeDigits = false, shouldIncludeSymbols = false)
        assertEquals(PasswordsGeneratorFailure.NoKindChosen, assertThrows<PasswordsGeneratorException> { PasswordsGenerator.generate(none) }.failure)
        assertEquals(
            PasswordsGeneratorFailure.LengthOutOfRange,
            assertThrows<PasswordsGeneratorException> { PasswordsGenerator.generate(PasswordsGeneratorOptions(length = 3)) }.failure,
        )
        assertEquals(
            PasswordsGeneratorFailure.LengthOutOfRange,
            assertThrows<PasswordsGeneratorException> { PasswordsGenerator.generate(PasswordsGeneratorOptions(length = 129)) }.failure,
        )
    }

    @Test fun `a number from the unfair end of the range is drawn again`() {
        // 2^32 is not a multiple of 10: the largest is 4_294_967_290, so a draw at or above it is thrown away.
        val draws = mutableListOf(4_294_967_295L, 4_294_967_290L, 13L)
        val index = PasswordsGenerator.randomIndex(10) { draws.removeAt(0) }
        assertEquals(3, index)
        assertTrue(draws.isEmpty())
    }

    @Test fun `passphrase strength`() {
        assertEquals(PasswordsPassphraseStrength.Empty, PasswordsPassphraseStrength.of(""))
        assertEquals(PasswordsPassphraseStrength.Short, PasswordsPassphraseStrength.of("abc"))
        assertEquals(PasswordsPassphraseStrength.Short, PasswordsPassphraseStrength.of("abcdefgh"))
        assertEquals(PasswordsPassphraseStrength.Short, PasswordsPassphraseStrength.of("abcdefghijk"))
        assertEquals(12, PasswordsPassphraseStrength.MINIMUM_LENGTH)
        assertEquals(PasswordsPassphraseStrength.Fair, PasswordsPassphraseStrength.of("Abcdefgh1234"))
        assertEquals(PasswordsPassphraseStrength.Strong, PasswordsPassphraseStrength.of("Abcdefgh12345678"))
        // One kind of character only counts a step less, until it is long.
        assertEquals(PasswordsPassphraseStrength.Weak, PasswordsPassphraseStrength.of("abcdefghijkl"))
        assertEquals(PasswordsPassphraseStrength.Strong, PasswordsPassphraseStrength.of("abcdefghijklmnopqrstu"))
    }

    @Test fun `a sign-in ended by the server keeps what the phone knew about keys`() {
        assertTrue(PasswordsSignInEnd.RemovedByPerson.isForgettingKeysAndItemMemories)
        assertFalse(PasswordsSignInEnd.EndedByServer.isForgettingKeysAndItemMemories)
    }
}
