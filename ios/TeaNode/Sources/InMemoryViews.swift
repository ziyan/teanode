import SwiftUI
import TeaNodeKit

/// InMemoryModel is the pages of the agent's memory that link to one record,
/// read when the record's screen opens. A screen that has one says so in a
/// line or a section of its own; one whose record no page is linked to says
/// nothing.
@MainActor
@Observable
final class InMemoryModel {
    private(set) var links: [KnowledgeRecordLink] = []

    /// Each page once, a statement kept over a guess.
    var pages: [KnowledgeRecordLink] { KnowledgeRecordKinds.pages(of: links) }

    func load(connection: Connection, recordKind: String, recordId: String, passageStartMS: Int64? = nil, passageEndMS: Int64? = nil) async {
        guard !recordId.isEmpty else { return }
        // A failure leaves the screen as it was: the record is still the
        // thing being looked at.
        links =
            (try? await connection.recordMemoryLinks(
                recordKind: recordKind, recordId: recordId, passageStartMS: passageStartMS, passageEndMS: passageEndMS)) ?? []
    }
}

/// InMemoryLoader reads the pages linked to a record when its screen opens.
private struct InMemoryLoader: ViewModifier {
    let model: InMemoryModel
    let recordKind: String
    let recordId: String
    let passageStartMS: Int64?
    let passageEndMS: Int64?
    @Environment(AppModel.self) private var app

    func body(content: Content) -> some View {
        content.task(id: recordId) {
            guard let connection = app.connection else { return }
            await model.load(
                connection: connection, recordKind: recordKind, recordId: recordId, passageStartMS: passageStartMS, passageEndMS: passageEndMS)
        }
    }
}

extension View {
    /// Reads which pages of memory a record is linked to, into the model the
    /// screen shows with InMemorySection or InMemoryCard.
    func inMemory(_ model: InMemoryModel, recordKind: String, recordId: String, passageStartMS: Int64? = nil, passageEndMS: Int64? = nil)
        -> some View
    {
        modifier(
            InMemoryLoader(model: model, recordKind: recordKind, recordId: recordId, passageStartMS: passageStartMS, passageEndMS: passageEndMS))
    }
}

/// InMemoryRow is one page of memory a record is linked to: its name, what it
/// is to the record, and a guess marked as one. A tap opens the page.
struct InMemoryRow: View {
    let link: KnowledgeRecordLink
    @Environment(AppModel.self) private var app

    var body: some View {
        Button {
            app.openedKnowledgePath = link.nodePath
        } label: {
            HStack {
                Label(link.nodeName.isEmpty ? link.nodePath : link.nodeName, systemImage: KnowledgeSymbols.symbol(link.nodeKind))
                    .actionColor()
                Spacer()
                if link.isProposed {
                    Chip(text: String(localized: "A guess"), isAlert: false)
                }
                Text(KnowledgeRecordWords.relation(link.recordRelation)).font(.footnote).foregroundStyle(.secondary)
            }
        }
        .accessibilityHint(Text("Opens the page in memory"))
    }
}

/// InMemorySection is the pages of memory a record is linked to, as a
/// section of the list its screen is.
struct InMemorySection: View {
    let model: InMemoryModel

    var body: some View {
        if !model.pages.isEmpty {
            Section("In memory") {
                ForEach(model.pages) { link in
                    InMemoryRow(link: link)
                }
            }
        }
    }
}

/// InMemoryCard is the same for a screen that is a stack of cards, not a
/// list: a heading and the pages under it.
struct InMemoryCard: View {
    let model: InMemoryModel

    var body: some View {
        if !model.pages.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("In memory").font(.footnote.weight(.semibold)).foregroundStyle(.secondary)
                ForEach(model.pages) { link in
                    InMemoryRow(link: link)
                }
            }
            .padding(.vertical, 8)
        }
    }
}

/// KnowledgeRecordWords says in a word or two what a page is to a record and
/// what a kind of record is called.
enum KnowledgeRecordWords {
    static func kind(_ recordKind: String) -> String {
        switch recordKind {
        case "contact": String(localized: "Contact card")
        case "mail": String(localized: "Mail")
        case "calendar_event": String(localized: "Calendar event")
        case "reminder": String(localized: "Reminder")
        case "conversation_message": String(localized: "Conversation")
        case "finance_transaction": String(localized: "Transaction")
        case "finance_receipt": String(localized: "Receipt")
        case "recording_passage": String(localized: "Recording")
        default: recordKind
        }
    }

    static func relation(_ recordRelation: String) -> String {
        switch recordRelation {
        case "is_contact": String(localized: "Contact card")
        case "wrote": String(localized: "Wrote")
        case "received": String(localized: "Received")
        case "attended": String(localized: "Attended")
        case "organized": String(localized: "Organized")
        case "spoke_in": String(localized: "Spoke in")
        case "mentioned_in": String(localized: "Mentioned in")
        case "paid": String(localized: "Was paid")
        case "paid_by": String(localized: "Paid")
        case "receipt_for": String(localized: "Receipt for")
        case "about": String(localized: "About")
        default: recordRelation.replacingOccurrences(of: "_", with: " ")
        }
    }
}
