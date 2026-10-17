import Foundation

/// AvatarColor picks which of the twelve colors a person without a picture is
/// drawn on. The server chooses one for every person from a hash of their id
/// and sends it as `avatarColorIndex`; this is the same function, for a screen
/// that shows someone before the server has said, and so the app and the
/// dashboard cannot disagree. The dashboard's `--avatar-0` to `--avatar-11` and
/// the app's palette are the twelve colors it indexes.
public enum AvatarColor {
    /// How many colors there are.
    public static let count = 12

    /// An FNV-1a hash of the UTF-8 bytes of the text, modulo the color count.
    public static func index(of text: String) -> Int {
        var hash: UInt32 = 0x811c_9dc5
        for byte in text.utf8 {
            hash ^= UInt32(byte)
            hash = hash &* 0x0100_0193
        }
        return Int(hash % UInt32(count))
    }

    /// The color to draw: the one the server chose when it sent a usable
    /// index, otherwise the one worked out from the text (the person's id, or
    /// their name when only that is known).
    public static func index(serverIndex: Int?, fallbackText: String) -> Int {
        if let serverIndex, (0..<count).contains(serverIndex) { return serverIndex }
        return index(of: fallbackText)
    }
}
