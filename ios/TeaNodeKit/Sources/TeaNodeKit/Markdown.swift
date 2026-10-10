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
    /// A table: its header cells, then its rows.
    case table(header: [String], rows: [[String]])
    case rule

    public struct ListItem: Equatable, Sendable {
        /// "•" for a bullet, or the item's own number with its dot.
        public let marker: String
        public var text: String
    }

    static let listItemPattern = try! NSRegularExpression(pattern: #"^\s*([-*+]|\d{1,3}[.)])\s+(.*)$"#)
    static let tableSeparatorPattern = try! NSRegularExpression(pattern: #"^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$"#)

    static func cells(of line: String) -> [String] {
        var trimmed = line.trimmingCharacters(in: .whitespaces)
        if trimmed.hasPrefix("|") { trimmed.removeFirst() }
        if trimmed.hasSuffix("|") { trimmed.removeLast() }
        return trimmed.components(separatedBy: "|").map { $0.trimmingCharacters(in: .whitespaces) }
    }

    static func isTableSeparator(_ line: String) -> Bool {
        line.contains("-") && tableSeparatorPattern.firstMatch(in: line, range: NSRange(line.startIndex..., in: line)) != nil
    }

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

        let allLines = text.components(separatedBy: "\n")
        var index = 0
        var table: (header: [String], rows: [[String]])?
        func flushTable() {
            if let table { blocks.append(.table(header: table.header, rows: table.rows)) }
            table = nil
        }
        while index < allLines.count {
            let line = allLines[index]
            index += 1
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if table != nil {
                if code == nil, trimmed.contains("|") {
                    table?.rows.append(cells(of: trimmed))
                    continue
                }
                flushTable()
            }
            if code == nil, trimmed.contains("|"), index < allLines.count, isTableSeparator(allLines[index]) {
                flush()
                table = (cells(of: trimmed), [])
                index += 1
                continue
            }
            if code == nil, ["---", "***", "___"].contains(where: { trimmed.hasPrefix($0) && trimmed.allSatisfy { $0 == trimmed.first || $0 == " " } }) {
                flush()
                blocks.append(.rule)
                continue
            }
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
        flushTable()
        flush()
        return blocks
    }
}
