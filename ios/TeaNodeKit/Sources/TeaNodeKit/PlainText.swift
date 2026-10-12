import Foundation

/// HiddenMarkers are the HTML comments an answer carries for the dashboard
/// and the app to read and never to show: the suggested replies it ends
/// with, and any other. The server takes them off wherever it sends an
/// answer as plain text (`models.StripHiddenMarkers`); this is the same for
/// what the app puts on a screen or says aloud outside the chat.
public enum HiddenMarkers {
    /// A fenced code block, a code span, or a comment, whole or cut off by
    /// the end of the text. Code is matched so that a comment shown as an
    /// example inside it is left alone.
    private static let pattern = try! NSRegularExpression(pattern: #"```[\s\S]*?(?:```|\z)|`[^`\n]*`|<!--[\s\S]*?(?:-->|\z)"#)

    /// The text without its comments.
    public static func removed(from text: String) -> String {
        var result = text
        // Back to front, so that the ranges of the matches before the one
        // removed stay where they were found.
        for match in pattern.matches(in: text, range: NSRange(text.startIndex..., in: text)).reversed() {
            guard let range = Range(match.range, in: result), result[range].hasPrefix("<!--") else { continue }
            result.removeSubrange(range)
        }
        return result
    }
}

/// PlainText is an answer written in Markdown as a surface that draws none
/// shows it: a widget, a line in a list. Nothing hidden, a link or a picture
/// as its words, and no bold or code marks (`models.PlainText`).
public enum PlainText {
    public static func of(_ markdown: String) -> String {
        var text = HiddenMarkers.removed(from: markdown)
        text = text.replacingOccurrences(of: #"!?\[([^\]]*)\]\([^)]*\)"#, with: "$1", options: .regularExpression)
        for mark in ["**", "__", "`"] {
            text = text.replacingOccurrences(of: mark, with: "")
        }
        return text.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
