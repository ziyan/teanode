import Foundation

/// MarkdownBlock is one block of an answer's Markdown: what the platform's
/// own Markdown reading leaves out, since it reads only inline styles. Each
/// block's text still carries its inline styles, for the screen to read.
public enum MarkdownBlock: Equatable, Sendable {
    case paragraph(String)
    case heading(String)
    case quote(String)
    case list([ListItem])
    case code(String)

    public struct ListItem: Equatable, Sendable {
        /// "•" for a bullet, or the item's own number with its dot.
        public let marker: String
        public var text: String
    }

    static let listItemPattern = try! NSRegularExpression(pattern: #"^\s*([-*+]|\d{1,3}[.)])\s+(.*)$"#)

    /// The blocks of a Markdown text, in order.
    public static func parse(_ text: String) -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var paragraph: [String] = []
        var quote: [String] = []
        var items: [ListItem] = []
        var code: [String]?

        func flush() {
            if !paragraph.isEmpty { blocks.append(.paragraph(paragraph.joined(separator: "\n"))) }
            if !quote.isEmpty { blocks.append(.quote(quote.joined(separator: "\n"))) }
            if !items.isEmpty { blocks.append(.list(items)) }
            paragraph = []
            quote = []
            items = []
        }

        for line in text.components(separatedBy: "\n") {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("```") {
                if let fenced = code {
                    blocks.append(.code(fenced.joined(separator: "\n")))
                    code = nil
                } else {
                    flush()
                    code = []
                }
                continue
            }
            if code != nil {
                code?.append(line)
                continue
            }
            if trimmed.isEmpty {
                flush()
                continue
            }
            if trimmed.hasPrefix("#") {
                flush()
                blocks.append(.heading(trimmed.drop(while: { $0 == "#" }).trimmingCharacters(in: .whitespaces)))
                continue
            }
            if trimmed.hasPrefix(">") {
                if quote.isEmpty { flush() }
                quote.append(trimmed.dropFirst().trimmingCharacters(in: .whitespaces))
                continue
            }
            let range = NSRange(line.startIndex..., in: line)
            if let match = listItemPattern.firstMatch(in: line, range: range),
                let markerRange = Range(match.range(at: 1), in: line), let textRange = Range(match.range(at: 2), in: line)
            {
                if items.isEmpty { flush() }
                let marker = String(line[markerRange])
                items.append(ListItem(marker: marker.first?.isNumber == true ? marker : "•", text: String(line[textRange])))
                continue
            }
            if !items.isEmpty, line.first == " " || line.first == "\t" {
                items[items.count - 1].text += " " + trimmed
                continue
            }
            if !quote.isEmpty || !items.isEmpty { flush() }
            paragraph.append(line)
        }
        if let fenced = code {
            blocks.append(.code(fenced.joined(separator: "\n")))
        }
        flush()
        return blocks
    }
}
