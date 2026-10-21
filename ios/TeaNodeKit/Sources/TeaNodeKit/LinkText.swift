import Foundation

/// LinkText finds the web links in a message's words and makes them
/// followable: shown as their short text, opened without their tracking
/// parameters. It is the same work as linkText.ts in the dashboard.
public enum LinkText {
    /// A link runs to the next white space, an angle bracket, a quote, or a
    /// character of the CJK and full-width punctuation blocks, so that a link
    /// written straight against Chinese or Japanese words does not carry them
    /// off.
    private static let barePattern = try! NSRegularExpression(
        pattern: #"https?://[^\s<>"'`\x{3000}-\x{303F}\x{FF00}-\x{FFEF}]+"#, options: [.caseInsensitive])

    private static let hostPattern = try! NSRegularExpression(pattern: #"^https?://[^/?#]"#, options: [.caseInsensitive])

    private static let closers: [Character: Character] = [")": "(", "]": "[", "}": "{"]

    /// The link with the punctuation that ends a sentence taken off its end,
    /// and a closing bracket that has no opening one in the link: the full
    /// stop after "see https://example.com/a." and the bracket in "(see
    /// https://example.com/a)" are the writer's, a bracket in
    /// "https://example.com/wiki/Thing_(film)" is the link's.
    public static func trimmedEnd(of link: String) -> String {
        var characters = Array(link)
        while let last = characters.last {
            if ".,;:!?*_~".contains(last) {
                characters.removeLast()
                continue
            }
            if let opener = closers[last] {
                let closing = characters.filter { $0 == last }.count
                let opening = characters.filter { $0 == opener }.count
                if closing > opening {
                    characters.removeLast()
                    continue
                }
            }
            break
        }
        return String(characters)
    }

    /// One link in a text: where it is, and the link itself.
    public struct Span: Equatable, Sendable {
        public let range: Range<String.Index>
        public let link: String
    }

    /// Where each web link is in the text, in order.
    public static func links(in text: String) -> [Span] {
        var spans: [Span] = []
        let whole = NSRange(text.startIndex..., in: text)
        for match in barePattern.matches(in: text, range: whole) {
            guard let matched = Range(match.range, in: text) else { continue }
            let link = trimmedEnd(of: String(text[matched]))
            // A link needs a host: "https://" and nothing after it is not one.
            guard hostPattern.firstMatch(in: link, range: NSRange(link.startIndex..., in: link)) != nil,
                let end = text.index(matched.lowerBound, offsetBy: link.count, limitedBy: matched.upperBound)
            else { continue }
            spans.append(Span(range: matched.lowerBound..<end, link: link))
        }
        return spans
    }

    private static let fencePattern = try! NSRegularExpression(pattern: #"```[\s\S]*?(?:```|\z)"#)
    private static let codePattern = try! NSRegularExpression(pattern: #"`[^`]*`"#)
    private static let picturePattern = try! NSRegularExpression(pattern: #"!\[[^\]]*\]\([^)]*\)"#)

    /// The first web link in the words of a message, or nil. In words written
    /// as Markdown, code and pictures are not links to show a card for, so
    /// they are left out first.
    public static func firstLink(in text: String, isMarkdown: Bool) -> String? {
        var words = text
        if isMarkdown {
            for pattern in [fencePattern, codePattern, picturePattern] {
                words = pattern.stringByReplacingMatches(in: words, range: NSRange(words.startIndex..., in: words), withTemplate: " ")
            }
        }
        return links(in: words).first?.link
    }

    /// The text made followable: a web link that is only written out is
    /// shown as its short text, and every web link, written out or given with
    /// words of its own, opens without its tracking parameters. The words of a
    /// link are left as they are, and so is any text `isProtected` says is not
    /// to be touched: the screen says which is code, since what marks it is the
    /// platform's own Markdown reading.
    public static func linked(_ text: AttributedString, isProtected: (AttributedSubstring) -> Bool = { _ in false }) -> AttributedString {
        var result = text
        // Links the text already holds: their address is cleaned, their
        // words are the writer's.
        var cleanedAddresses: [(range: Range<AttributedString.Index>, address: URL)] = []
        for run in text.runs {
            guard let address = run.link, let scheme = address.scheme?.lowercased(), scheme == "http" || scheme == "https" else { continue }
            let cleaned = LinkClean.cleaned(address.absoluteString)
            if cleaned != address.absoluteString, let cleanedAddress = URL(string: cleaned) {
                cleanedAddresses.append((run.range, cleanedAddress))
            }
        }
        for (range, address) in cleanedAddresses { result[range].link = address }

        // Links only written out, from the end so that the places found stay
        // where they are while an earlier one is replaced.
        let written = String(result.characters)
        for span in links(in: written).reversed() {
            let offset = written.distance(from: written.startIndex, to: span.range.lowerBound)
            let length = written.distance(from: span.range.lowerBound, to: span.range.upperBound)
            let start = result.index(result.startIndex, offsetByCharacters: offset)
            let end = result.index(start, offsetByCharacters: length)
            let covered = result[start..<end]
            guard !hasOwnWords(covered, notWrittenAs: span.link), !isProtected(covered), let address = URL(string: LinkClean.cleaned(span.link)) else { continue }
            var replacement = AttributedString(LinkClean.displayText(span.link))
            if let first = covered.runs.first { replacement.mergeAttributes(first.attributes) }
            replacement.link = address
            result.replaceSubrange(start..<end, with: replacement)
        }
        return result
    }

    /// Whether the text is a link that has words of its own: it opens somewhere
    /// other than the address it is written as. A link whose words are its own
    /// address (the platform's Markdown reading may make one of a bare
    /// address) has none, and is shown short like any other.
    private static func hasOwnWords(_ text: AttributedSubstring, notWrittenAs link: String) -> Bool {
        let written = LinkClean.cleaned(link)
        return text.runs.contains { run in
            guard let address = run.link else { return false }
            return LinkClean.cleaned(address.absoluteString) != written
        }
    }

    /// Plain words (a person's message, typed) made followable.
    public static func linked(plain text: String) -> AttributedString {
        linked(AttributedString(text))
    }
}
