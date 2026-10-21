import Foundation

// Showing a stored value (a user name, a site, a password, a note) on a narrow
// screen without SwiftUI adding characters to it. At large text sizes SwiftUI
// hyphenates a word that does not fit a line, so a site name can come out
// with a hyphen in the middle of a word, and a value must never show a
// character it does not have.
// SwiftUI has no switch that turns the hyphenation off, so the value is shown
// with a zero-width space wherever a line may end: after "@", "/" and the like,
// and between the characters of a word too long to be a word. A line then
// breaks at one of those places and nothing is ever hyphenated. The text with
// the spaces in it is only for showing: copying, searching and speaking use
// the value itself.

/// PasswordsDisplayText makes a value breakable for showing.
public enum PasswordsDisplayText {
    /// U+200B: invisible, and a place a line may end.
    public static let zeroWidthSpace = "\u{200B}"

    /// The longest run of characters, in a user name, a site or a password,
    /// that is left whole. A longer one is a random string and may break anywhere.
    public static let identifierWordLength = 8

    /// The same for prose, such as a note, where only a string that is not a
    /// word at all (a pasted key) is broken inside.
    public static let proseWordLength = 16

    /// Characters a line may end after, when a word comes before them.
    private static let breakAfterCharacters: Set<Character> = ["@", "/", "-", "_", ":", "?", "&", "=", "#", "%", "+"]

    /// The value with zero-width spaces where a line may end. A value that has
    /// no such place is returned as it is, so the result differs from the value
    /// only in those spaces. `isProse` is for sentences (a note), `false` for
    /// a name, a site or a password.
    public static func breakable(_ value: String, isProse: Bool = false) -> String {
        let longestWholeWord = isProse ? proseWordLength : identifierWordLength
        let characters = Array(value)
        var output = ""
        var word: [Character] = []

        func finishWord() {
            if word.count > longestWholeWord {
                output += word.map { String($0) }.joined(separator: zeroWidthSpace)
            } else {
                output += String(word)
            }
            word = []
        }

        for (index, character) in characters.enumerated() {
            let nextCharacter = index + 1 < characters.count ? characters[index + 1] : nil
            let isFollowedByText = nextCharacter.map { !$0.isWhitespace } ?? false
            if character.isWhitespace {
                finishWord()
                output.append(character)
            } else if isBreakPoint(character, next: nextCharacter, followsWord: !word.isEmpty) {
                finishWord()
                output.append(character)
                if isFollowedByText { output += zeroWidthSpace }
            } else {
                word.append(character)
            }
        }
        finishWord()
        return output
    }

    /// The value without the spaces `breakable` put in.
    public static func plain(_ shown: String) -> String {
        shown.replacingOccurrences(of: zeroWidthSpace, with: "")
    }

    /// Whether a line may end after the character. A dot does only between
    /// words ("example.com"), not in a number or at the end of a sentence.
    private static func isBreakPoint(_ character: Character, next: Character?, followsWord: Bool) -> Bool {
        guard followsWord else { return false }
        if character == "." { return next?.isLetter ?? false }
        return breakAfterCharacters.contains(character)
    }
}
