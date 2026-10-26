import Foundation

// A narrative is a few paragraphs separated by blank lines, in which a record
// the agent mentions is written [[kind:id|the words its sentence shows]], or
// [[kind:id]]. The server answers each mention beside the narrative (its title
// and where it opens), and the screen draws the mention as its words, or the
// record's title when it has none, which opens the record where the app has a
// screen for it.

/// JournalNarrativePiece is a piece of a paragraph: plain words, or a mention
/// of a record with the words the story wrote for it (empty when it wrote
/// none, for the title to stand in).
public enum JournalNarrativePiece: Hashable, Sendable {
    case words(String)
    case mention(JournalRecord, words: String)
}

/// JournalNarrativeText reads a narrative into paragraphs of pieces.
public enum JournalNarrativeText {
    /// The narrative's paragraphs. A mention whose record the server resolved
    /// is a piece of its own; one it could not (the record is gone, or no
    /// longer the person's, or it is not among the references) is left out of
    /// the words, since its title is not known.
    public static func paragraphs(of narrative: String, references: [JournalRecord]) -> [[JournalNarrativePiece]] {
        var recordsByReference: [String: JournalRecord] = [:]
        for record in references { recordsByReference[record.reference] = record }
        return narrative
            .components(separatedBy: "\n\n")
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .map { pieces(of: $0, recordsByReference: recordsByReference) }
            .filter { !$0.isEmpty }
    }

    private static func pieces(of paragraph: String, recordsByReference: [String: JournalRecord]) -> [JournalNarrativePiece] {
        var pieces: [JournalNarrativePiece] = []
        var rest = Substring(paragraph)
        while let open = rest.range(of: "[[") {
            guard let close = rest[open.upperBound...].range(of: "]]") else { break }
            let inside = rest[open.upperBound..<close.lowerBound]
            let bar = inside.firstIndex(of: "|")
            let reference = String(inside[inside.startIndex..<(bar ?? inside.endIndex)])
            let mentionWords = bar.map { String(inside[inside.index(after: $0)...]).trimmingCharacters(in: .whitespaces) } ?? ""
            let before = String(rest[rest.startIndex..<open.lowerBound])
            if isReference(reference) {
                if !before.isEmpty { pieces.append(.words(before)) }
                if let record = recordsByReference[reference], !record.isUnavailable, !record.title.isEmpty {
                    pieces.append(.mention(record, words: mentionWords))
                }
                rest = rest[close.upperBound...]
            } else {
                // Not a mention: the brackets are the writer's own words.
                pieces.append(.words(before + "[["))
                rest = rest[open.upperBound...]
            }
        }
        if !rest.isEmpty { pieces.append(.words(String(rest))) }
        return tidy(pieces)
    }

    /// Whether text is kind:id: a kind in lowercase letters and underscores,
    /// then an id, which may itself hold colons and an @ (an occurrence of a
    /// repeating event is eventId@start).
    static func isReference(_ text: String) -> Bool {
        guard let colon = text.firstIndex(of: ":") else { return false }
        let kind = text[text.startIndex..<colon]
        let identifier = text[text.index(after: colon)...]
        return !kind.isEmpty && !identifier.isEmpty && kind.allSatisfy { $0.isLowercase && $0.isLetter || $0 == "_" } && !identifier.contains(" ")
    }

    /// Joins neighbouring words and takes the space a left-out mention would
    /// leave doubled.
    private static func tidy(_ pieces: [JournalNarrativePiece]) -> [JournalNarrativePiece] {
        var tidied: [JournalNarrativePiece] = []
        for piece in pieces {
            if case .words(let words) = piece, case .words(let previous)? = tidied.last {
                tidied[tidied.count - 1] = .words(previous + words)
            } else {
                tidied.append(piece)
            }
        }
        return tidied.compactMap { piece in
            guard case .words(let words) = piece else { return piece }
            let collapsed = words.replacingOccurrences(of: "  ", with: " ")
            return collapsed.isEmpty ? nil : .words(collapsed)
        }
    }
}

/// JournalRecordOpening is where the app takes a record a day mentions or
/// links to. The kinds are memory's record kinds, which the dashboard opens
/// with a path; the app has a screen for some of them and a part for others.
public enum JournalRecordOpening: Hashable, Sendable {
    case mailItem(String)
    case contact(String)
    case photo(String)
    /// A path the app follows as it follows the agent's open_page: a day on
    /// the map, a kind of Health data, a Finance part, a page of knowledge.
    case path(String)
    /// A part of the app by its raw name ("calendar", "ideas"), when the app
    /// has no screen for the one record.
    case part(String)
    /// The app has no screen for it.
    case nowhere

    /// Where the record opens. The kind decides first, since the app has a
    /// screen for that record; the dashboard's path decides for the parts
    /// the app shows by day or by kind of data.
    public static func of(recordKind: String, recordId: String, dashboardPath: String) -> JournalRecordOpening {
        switch recordKind {
        case "mail": return .mailItem(recordId)
        case "contact": return .contact(recordId)
        case "photo": return .photo(recordId)
        case "recording", "recording_passage": return .path("/recordings/\(recordId)")
        case "calendar_event", "calendar_occurrence", "reminder": return .part("calendar")
        case "finance_transaction", "finance_receipt": return .part("finance")
        case "idea": return .part("ideas")
        case "goal": return .part("goals")
        default: break
        }
        let pathParts = dashboardPath.split(separator: "?", maxSplits: 1, omittingEmptySubsequences: false)
        if let first = pathParts.first {
            let root = first.split(separator: "/").first.map(String.init) ?? ""
            if ["location", "health", "knowledge"].contains(root) { return .path(dashboardPath) }
        }
        return .nowhere
    }

    public static func of(record: JournalRecord) -> JournalRecordOpening {
        of(recordKind: record.recordKind, recordId: record.recordId, dashboardPath: record.dashboardPath)
    }

    public static func of(moment: JournalMoment) -> JournalRecordOpening {
        of(recordKind: moment.recordKind, recordId: moment.recordId, dashboardPath: moment.dashboardPath)
    }

    /// Whether the app has somewhere to take the person.
    public var isOpenable: Bool { self != .nowhere }
}
