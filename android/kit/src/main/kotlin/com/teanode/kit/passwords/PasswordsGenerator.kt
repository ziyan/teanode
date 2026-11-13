package com.teanode.kit.passwords

import java.security.SecureRandom

// The password generator, with the rules of web/src/passwords/generator.ts: every character is drawn at random with no
// character likelier than another (a plain remainder favors the first characters whenever the alphabet does not divide
// 2^32, so a number from the unfair end of the range is drawn again), each kind asked for appears at least once, and the
// order is then shuffled.

/** PasswordsGeneratorFailure is why a password could not be generated. */
enum class PasswordsGeneratorFailure {
    /** No kind of character was chosen. */
    NoKindChosen,

    /** The length is below the number of kinds chosen, or above the most allowed. */
    LengthOutOfRange,
}

class PasswordsGeneratorException(val failure: PasswordsGeneratorFailure) : Exception(failure.name)

/** What the person chooses for a new password. */
data class PasswordsGeneratorOptions(
    val length: Int = 20,
    val shouldIncludeLetters: Boolean = true,
    val shouldIncludeDigits: Boolean = true,
    val shouldIncludeSymbols: Boolean = true,
    /** Leaves out the characters that read as one another in many typefaces: I l 1 | O 0 o. */
    val shouldAvoidLookAlikes: Boolean = false,
) {
    /** How many kinds of character are chosen: lower case and upper case letters count as two, so each appears at least once. */
    val kindCount: Int
        get() = (if (shouldIncludeLetters) 2 else 0) + (if (shouldIncludeDigits) 1 else 0) + (if (shouldIncludeSymbols) 1 else 0)

    companion object {
        /** The options a new password starts with. */
        val standard = PasswordsGeneratorOptions()
    }
}

/** PasswordsGenerator makes random passwords. */
object PasswordsGenerator {
    const val MINIMUM_LENGTH = 4
    const val MAXIMUM_LENGTH = 128

    internal const val LOWERCASE_LETTERS = "abcdefghijklmnopqrstuvwxyz"
    internal const val UPPERCASE_LETTERS = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
    internal const val DIGITS = "0123456789"
    internal const val SYMBOLS = "!@#$%^&*()-_=+[]{}.,:;?"
    internal const val LOOK_ALIKES = "Il1|O0o"

    private val secureRandom = SecureRandom()

    /** A random 32 bit number as an unsigned value in a Long. */
    private val systemRandom: () -> Long = { secureRandom.nextInt().toLong() and 0xFFFF_FFFFL }

    /**
     * A whole number from 0 up to but not including `limit`, every one equally likely. `nextRandom` gives random 32 bit
     * numbers (0 to 4294967295); the default is the system's secure generator, and a test gives its own to count draws.
     */
    fun randomIndex(limit: Int, nextRandom: () -> Long = systemRandom): Int {
        require(limit >= 1) { "The limit must be a whole number from 1 to 2^32" }
        val limitValue = limit.toLong()
        // Numbers at or above the largest multiple of the limit would make the low indexes likelier, so they are drawn again.
        val largestMultiple = (0x1_0000_0000L / limitValue) * limitValue
        while (true) {
            val drawn = nextRandom()
            if (drawn < largestMultiple) return (drawn % limitValue).toInt()
        }
    }

    private fun alphabet(characters: String, options: PasswordsGeneratorOptions): List<Char> =
        if (options.shouldAvoidLookAlikes) characters.filter { character -> !LOOK_ALIKES.contains(character) }.toList() else characters.toList()

    /** A password of `options.length` characters. Each kind asked for appears at least once, and the order is shuffled. */
    fun generate(options: PasswordsGeneratorOptions = PasswordsGeneratorOptions.standard, nextRandom: () -> Long = systemRandom): String {
        val alphabets = mutableListOf<List<Char>>()
        if (options.shouldIncludeLetters) {
            alphabets.add(alphabet(LOWERCASE_LETTERS, options))
            alphabets.add(alphabet(UPPERCASE_LETTERS, options))
        }
        if (options.shouldIncludeDigits) alphabets.add(alphabet(DIGITS, options))
        if (options.shouldIncludeSymbols) alphabets.add(alphabet(SYMBOLS, options))
        if (alphabets.isEmpty()) throw PasswordsGeneratorException(PasswordsGeneratorFailure.NoKindChosen)
        if (options.length < alphabets.size || options.length > MAXIMUM_LENGTH) {
            throw PasswordsGeneratorException(PasswordsGeneratorFailure.LengthOutOfRange)
        }
        val everyCharacter = alphabets.flatten()
        val characters = alphabets.map { kind -> kind[randomIndex(kind.size, nextRandom)] }.toMutableList()
        while (characters.size < options.length) characters.add(everyCharacter[randomIndex(everyCharacter.size, nextRandom)])
        // Fisher-Yates, so the characters guaranteed above do not sit at the front.
        var index = characters.size - 1
        while (index > 0) {
            val otherIndex = randomIndex(index + 1, nextRandom)
            val held = characters[index]
            characters[index] = characters[otherIndex]
            characters[otherIndex] = held
            index -= 1
        }
        return characters.joinToString("")
    }
}

/**
 * A hint about a passphrase, not a promise: length counts most, and one kind of character only counts a step less. It is
 * the dashboard's rule (web/src/pages/passwords/passwordsProtocol.ts).
 */
enum class PasswordsPassphraseStrength {
    Empty,
    Short,
    Weak,
    Fair,
    Strong;

    companion object {
        /** The fewest characters a passphrase may have. */
        const val MINIMUM_LENGTH = PasswordsCrypto.MINIMUM_PASSPHRASE_LENGTH

        fun of(passphrase: String): PasswordsPassphraseStrength {
            val length = passphrase.codePointCount(0, passphrase.length)
            if (length == 0) return Empty
            if (length < MINIMUM_LENGTH) return Short
            fun isAscii(character: Char) = character.code < 128
            var kindCount = 0
            if (passphrase.any { character -> isAscii(character) && character.isLowerCase() }) kindCount += 1
            if (passphrase.any { character -> isAscii(character) && character.isUpperCase() }) kindCount += 1
            if (passphrase.any { character -> isAscii(character) && character.isDigit() }) kindCount += 1
            if (passphrase.any { character -> !(isAscii(character) && (character.isLetter() || character.isDigit())) }) kindCount += 1
            val steps = listOf(Weak, Fair, Strong)
            var step = if (length >= 16) 2 else if (length >= 12) 1 else 0
            if (kindCount == 1 && length < 20) step = maxOf(0, step - 1)
            return steps[step]
        }
    }
}

/**
 * Why a sign-in is gone from the phone. What the phone remembers about other people's keys and about each item is kept
 * per server and signed-in person, and is forgotten only when the person removes the account from the phone: a server
 * that ends a sign-in must not be able to make the phone forget it.
 */
enum class PasswordsSignInEnd {
    /** The person removed the account from the phone (signed out, or removed it from the list). */
    RemovedByPerson,

    /** The server ended the sign-in. */
    EndedByServer;

    /** Whether the pinned keys and item memories of the sign-in are removed. */
    val isForgettingKeysAndItemMemories: Boolean get() = this == RemovedByPerson
}
