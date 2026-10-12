import Foundation

/// EmojiReaction tells emoji from everything else typed on the emoji
/// keyboard, so a reaction can be any one of them. The server takes one or
/// two emoji of at most 32 bytes (`ValidateReactionEmoji`); a person picks
/// one, so the first emoji typed is the reaction.
public enum EmojiReaction {
    /// The most bytes the server stores for one reaction.
    public static let bytesMost = 32

    private static let textPresentationSelector: Unicode.Scalar = "\u{FE0E}"
    private static let keycapMark: Unicode.Scalar = "\u{20E3}"
    private static let regionalIndicators: ClosedRange<UInt32> = 0x1F1E6...0x1F1FF

    /// Whether one character, as a person sees it, is an emoji: a picture
    /// drawn in color by default, or a base the emoji selector, a joiner, a
    /// skin tone or a flag's second half turns into one. A digit, a letter or
    /// a symbol that is drawn as text is not.
    public static func isEmoji(_ character: Character) -> Bool {
        let scalars = character.unicodeScalars
        guard let first = scalars.first else { return false }
        // The selector that asks for the plain-text drawing says it is not.
        if scalars.contains(textPresentationSelector) { return false }
        // A regional indicator alone is a letter in a box; two make a flag.
        if regionalIndicators.contains(first.value) {
            return scalars.count == 2 && regionalIndicators.contains(scalars[scalars.index(after: scalars.startIndex)].value)
        }
        // A digit, # or * is an emoji only inside its keycap.
        if first.properties.numericType != nil || first == "#" || first == "*" {
            return scalars.contains(keycapMark)
        }
        if first.properties.isEmojiPresentation { return true }
        return first.properties.isEmoji && scalars.count > 1
    }

    /// The first emoji in what was typed, or nil when there is none or it is
    /// longer than the server takes.
    public static func firstEmoji(in text: String) -> String? {
        guard let character = text.first(where: isEmoji) else { return nil }
        let emoji = String(character)
        return emoji.utf8.count <= bytesMost ? emoji : nil
    }
}
