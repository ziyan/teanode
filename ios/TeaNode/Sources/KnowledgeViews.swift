import SwiftUI
import TeaNodeKit

/// KnowledgePlace is a list or a page of the agent's memory.
enum KnowledgePlace: Hashable {
    case folder(String)
    case page(String)
}

/// KnowledgeScreen is the dashboard's Knowledge page: the pages the agent
/// keeps, folder by folder, searched, each page whole, and asking what the
/// memory and the sources hold.
struct KnowledgeScreen: View {
    let connection: Connection
    @Binding var path: NavigationPath
    @State private var isRecalling = false
    @State private var isSearchingDocuments = false

    var body: some View {
        KnowledgeFolder(connection: connection, folderPath: "")
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Button("Ask the memory", systemImage: "brain") { isRecalling = true }
                        Button("Search documents", systemImage: "doc.text.magnifyingglass") { isSearchingDocuments = true }
                    } label: {
                        Label("Ask", systemImage: "questionmark.bubble")
                    }
                }
            }
            .sheet(isPresented: $isRecalling) {
                RecallSheet(connection: connection) { pagePath in
                    isRecalling = false
                    path.append(KnowledgePlace.page(pagePath))
                }
            }
            .sheet(isPresented: $isSearchingDocuments) {
                DocumentsSheet(connection: connection)
            }
    }
}

/// KnowledgeDestination is where a knowledge place leads, for the stack
/// the knowledge is shown in.
struct KnowledgeDestination: ViewModifier {
    let connection: Connection
    @Binding var path: NavigationPath

    func body(content: Content) -> some View {
        content.navigationDestination(for: KnowledgePlace.self) { place in
            switch place {
            case .folder(let folderPath):
                KnowledgeFolder(connection: connection, folderPath: folderPath)
            case .page(let pagePath):
                KnowledgePageView(connection: connection, pagePath: pagePath, path: $path)
            }
        }
    }
}

/// KnowledgeFolder is a folder's pages, fifty at a time, or what a search
/// found when one is typed.
struct KnowledgeFolder: View {
    let connection: Connection
    let folderPath: String
    @Environment(AppModel.self) private var app
    @State private var rows: [KnowledgeRow] = []
    @State private var total = 0
    @State private var title = ""
    @State private var isLoaded = false
    @State private var problemText: String?
    @State private var searchText = ""
    @State private var found: KnowledgeSearch?
    @State private var foundNodes: [KnowledgeNode] = []
    @State private var foundFacts: [KnowledgeSearch.FoundFact] = []

    var body: some View {
        List {
            if !searchText.isEmpty {
                searchResults
            } else {
                ForEach(rows, id: \.node?.path) { row in
                    if let node = row.node {
                        NavigationLink(value: node.isFolder ? KnowledgePlace.folder(node.path) : KnowledgePlace.page(node.path)) {
                            KnowledgeRowView(row: row, selfName: app.account?.username)
                        }
                        .onAppear {
                            if node.path == rows.last?.node?.path, rows.count < total {
                                Task { await load(isMore: true) }
                            }
                        }
                    }
                }
            }
        }
        .navigationTitle(folderPath.isEmpty ? String(localized: "Knowledge") : title)
        .searchable(text: $searchText, prompt: "Search pages and facts")
        .task(id: searchText) {
            guard !searchText.trimmingCharacters(in: .whitespaces).isEmpty else {
                found = nil
                return
            }
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled else { return }
            await search(isMore: false)
        }
        .task { await load(isMore: false) }
        .refreshable { await load(isMore: false) }
        .overlay {
            if let problemText {
                ContentUnavailableView("This could not be read", systemImage: "exclamationmark.triangle", description: Text(problemText))
            } else if !isLoaded {
                ProgressView()
            } else if rows.isEmpty, searchText.isEmpty {
                ContentUnavailableView("Nothing filed here yet", systemImage: "folder")
            }
        }
    }

    @ViewBuilder
    private var searchResults: some View {
        let groups = Dictionary(grouping: foundNodes) { String($0.path.split(separator: "/").first ?? "") }
        ForEach(groups.keys.sorted(), id: \.self) { root in
            Section(root) {
                ForEach(groups[root] ?? []) { node in
                    NavigationLink(value: KnowledgePlace.page(node.path)) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(node.shownName)
                            Text(node.path).font(.caption.monospaced()).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
        if !foundFacts.isEmpty {
            Section("Facts") {
                ForEach(Array(foundFacts.enumerated()), id: \.offset) { _, found in
                    NavigationLink(value: KnowledgePlace.page(found.path)) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("#\(found.fact?.number ?? 0) \(found.fact?.text ?? "")").lineLimit(3)
                            Text(found.name.isEmpty ? found.path : found.name).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
        if let found {
            if found.nextOffset > 0 {
                Button("Show more") { Task { await search(isMore: true) } }
            } else if foundNodes.isEmpty, foundFacts.isEmpty {
                Text("Nothing matches").foregroundStyle(.secondary)
            }
        }
    }

    private func load(isMore: Bool) async {
        do {
            let page = try await connection.knowledgeChildren(of: folderPath, offset: isMore ? rows.count : 0)
            let fresh = page.rows.compactMap { $0 }
            rows = KnowledgeRow.ordered(isMore ? rows + fresh : fresh)
            total = page.total
            if !folderPath.isEmpty, title.isEmpty {
                title = (try? await connection.knowledgePage(folderPath))?.node?.shownName
                    ?? String(folderPath.split(separator: "/").last ?? "")
            }
            isLoaded = true
            problemText = nil
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func search(isMore: Bool) async {
        let words = searchText.trimmingCharacters(in: .whitespaces)
        do {
            let read = try await connection.searchKnowledge(words, offset: isMore ? (found?.nextOffset ?? 0) : 0)
            guard words == searchText.trimmingCharacters(in: .whitespaces) else { return }
            foundNodes = (isMore ? foundNodes : []) + read.nodes.compactMap { $0 }
            foundFacts = (isMore ? foundFacts : []) + read.facts.compactMap { $0 }
            found = read
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// KnowledgeRowView is a page in a folder: its name, a pin, what it is
/// about, and how many pages it holds.
struct KnowledgeRowView: View {
    let row: KnowledgeRow
    let selfName: String?

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    Image(systemName: KnowledgeSymbols.symbol(row.node?.kind ?? ""))
                        .foregroundStyle(.secondary)
                    Text(row.node?.isSelf == true ? (selfName ?? String(localized: "You")) : row.node?.shownName ?? "")
                    if row.node?.pinned == true {
                        Image(systemName: "pin.fill").font(.caption).foregroundStyle(.secondary)
                            .accessibilityLabel("Always in its prompt")
                    }
                }
                let line = row.line
                if !line.isEmpty {
                    Text(line).font(.footnote).foregroundStyle(.secondary).lineLimit(2)
                }
            }
            Spacer()
            if row.children > 0 {
                Text("\(row.children)").font(.footnote.monospacedDigit()).foregroundStyle(.secondary)
            }
        }
    }
}

enum KnowledgeSymbols {
    static func symbol(_ kind: String) -> String {
        switch kind {
        case "folder": "folder"
        case "self", "person": "person"
        case "organization": "building.2"
        case "project": "hammer"
        case "place": "mappin"
        case "period": "calendar"
        case "thing": "cube"
        default: "doc.text"
        }
    }
}

/// KnowledgePageModel is one page and its changes: each reads the page
/// again, and says what it did.
@MainActor
@Observable
final class KnowledgePageModel {
    let connection: Connection
    private(set) var pagePath: String
    private(set) var page: KnowledgePage?
    private(set) var neighbours: KnowledgeNeighbours?
    private(set) var history: [KnowledgeRevision]?
    private(set) var isMissing = false
    /// The record links past the first part the page read, as they are asked for.
    private(set) var laterRecordLinks: [KnowledgeRecordLink] = []
    var doneText: String?
    var problemText: String?

    /// The records the page is linked to that have been read so far.
    var recordLinks: [KnowledgeRecordLink] { (page?.recordLinks ?? []).compactMap { $0 } + laterRecordLinks }
    var recordLinkCount: Int { page?.recordLinkCount ?? 0 }

    /// Reads the next part of a page with more links than the page carries.
    func loadMoreRecordLinks() async {
        if let more = try? await connection.knowledgeRecordLinks(pagePath, first: 50, offset: recordLinks.count) {
            laterRecordLinks += more
        }
    }

    init(connection: Connection, pagePath: String) {
        self.connection = connection
        self.pagePath = pagePath
    }

    func load() async {
        do {
            page = try await connection.knowledgePage(pagePath)
            laterRecordLinks = []
            isMissing = page?.node == nil
            neighbours = try? await connection.knowledgeNeighbours(pagePath)
        } catch {
            problemText = error.localizedDescription
        }
    }

    func loadHistory() async {
        history = (try? await connection.knowledgeHistory(pagePath)) ?? []
    }

    /// Runs a change and reads the page again; false when it was refused.
    func act(saying said: String, _ work: @MainActor () async throws -> Void) async -> Bool {
        do {
            try await work()
            doneText = said
            await load()
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    func moved(to newPath: String) {
        pagePath = newPath
    }
}

/// KnowledgePageView is a page whole: its summary, overview, reflections,
/// facts, connections, the pages under it and its history, and every change
/// the dashboard makes to them.
struct KnowledgePageView: View {
    @State private var model: KnowledgePageModel
    @Binding var path: NavigationPath
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var isEditing = false
    @State private var editingFact: FactDraft?
    @State private var choosing: PageChoice?
    @State private var isLinking = false
    @State private var isLinkingContact = false
    @State private var isConfirmingForget = false
    @State private var mergingInto: String?
    @State private var forgettingFact: KnowledgeFact?
    @State private var shownFactCount = 50

    init(connection: Connection, pagePath: String, path: Binding<NavigationPath>) {
        _model = State(initialValue: KnowledgePageModel(connection: connection, pagePath: pagePath))
        _path = path
    }

    var body: some View {
        @Bindable var model = model
        List {
            if let page = model.page, let node = page.node {
                header(page, node)
                linkedRecords(node)
                reflections(page)
                facts(page)
                connections
                if !page.children.compactMap({ $0 }).isEmpty {
                    Section("Filed under this") {
                        ForEach(page.children.compactMap { $0 }) { child in
                            NavigationLink(value: child.isFolder ? KnowledgePlace.folder(child.path) : KnowledgePlace.page(child.path)) {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(child.shownName)
                                    if !child.summary.isEmpty {
                                        Text(child.summary).font(.footnote).foregroundStyle(.secondary).lineLimit(2)
                                    }
                                }
                            }
                        }
                    }
                }
                historySection
            }
        }
        .navigationTitle(model.page?.node.map { $0.isSelf ? (app.account?.username ?? $0.shownName) : $0.shownName } ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if model.isMissing {
                ContentUnavailableView("There is no page here", systemImage: "doc.questionmark")
            } else if model.page == nil, model.problemText == nil {
                ProgressView()
            }
        }
        .task { await model.load() }
        .refreshable { await model.load() }
        .toolbar { toolbar }
        .toast(done: $model.doneText, problem: $model.problemText)
        .sheet(isPresented: $isEditing) {
            if let node = model.page?.node {
                PageEditor(model: model, node: node)
            }
        }
        .sheet(item: $editingFact) { draft in
            FactEditor(model: model, draft: draft)
        }
        .sheet(item: $choosing) { choice in
            PagePicker(connection: model.connection, title: choice.title, excluding: model.pagePath, initial: choice.initial) { chosen in
                Task { await choose(choice, chosen) }
            }
        }
        .sheet(isPresented: $isLinking) {
            LinkEditor(model: model)
        }
        .sheet(isPresented: $isLinkingContact) {
            ContactPickerSheet(connection: model.connection, currentContactId: model.page?.node?.contactId ?? "") { chosen in
                let pagePath = model.pagePath
                Task {
                    let isLinked = await model.act(saying: chosen.isEmpty ? String(localized: "Contact unlinked") : String(localized: "Contact linked")) {
                        try await model.connection.setKnowledgeContact(pagePath, contactId: chosen)
                    }
                    if isLinked { isLinkingContact = false }
                }
            }
        }
        .confirmationDialog("Forget this page?", isPresented: $isConfirmingForget, titleVisibility: .visible) {
            Button("Forget page", role: .destructive) {
                Task {
                    let pagePath = model.pagePath
                    let isForgotten = await model.act(saying: String(localized: "Forgotten")) {
                        try await model.connection.deleteKnowledgeNode(pagePath)
                    }
                    if isForgotten { dismiss() }
                }
            }
        } message: {
            Text("Forget \(model.pagePath), its \(model.page?.presentFacts.count ?? 0) facts and the \(model.page?.children.count ?? 0) pages under it.")
        }
        .confirmationDialog(
            "Merge this page?", isPresented: Binding(get: { mergingInto != nil }, set: { if !$0 { mergingInto = nil } }), titleVisibility: .visible
        ) {
            Button("Merge", role: .destructive) {
                guard let into = mergingInto else { return }
                Task { await merge(into: into) }
            }
        } message: {
            Text("Fold \(model.pagePath) into \(mergingInto ?? ""). Its \(model.page?.presentFacts.count ?? 0) facts, its links and the \(model.page?.children.count ?? 0) pages under it move there, and \(model.pagePath) is gone afterwards.")
        }
        .confirmationDialog(
            "Forget this fact?", isPresented: Binding(get: { forgettingFact != nil }, set: { if !$0 { forgettingFact = nil } }), titleVisibility: .visible
        ) {
            Button("Forget", role: .destructive) {
                guard let fact = forgettingFact else { return }
                let pagePath = model.pagePath
                Task {
                    _ = await model.act(saying: String(localized: "Forgotten")) {
                        try await model.connection.deleteKnowledgeFact(path: pagePath, number: fact.number)
                    }
                }
            }
        } message: {
            Text("Your agent is shown it as an example of what not to keep: “\(forgettingFact?.text ?? "")”")
        }
    }

    // MARK: Sections

    @ViewBuilder
    private func header(_ page: KnowledgePage, _ node: KnowledgeNode) -> some View {
        Section {
            HStack(spacing: 6) {
                Text(node.path).font(.caption.monospaced()).foregroundStyle(.secondary)
                Chip(text: node.kind, isAlert: false)
                if node.pinned == true {
                    Chip(text: String(localized: "Always in its prompt"), isAlert: false)
                }
            }
            if let aliases = node.aliases, !aliases.isEmpty {
                Text("Also called \(aliases.joined(separator: ", "))").font(.footnote).foregroundStyle(.secondary)
            }
            if let contact = page.contact {
                // The linked card: it opens in Contacts.
                Button {
                    app.openRecord(dashboardPath: "/contacts?contact=\(contact.id)")
                } label: {
                    Label(
                        [contact.name, contact.emails.first ?? contact.organization].filter { !$0.isEmpty }.joined(separator: " · "),
                        systemImage: "person.crop.circle"
                    )
                    .font(.footnote)
                }
            }
            if node.kind == "person", !node.isSelf {
                Button(page.contact == nil ? LocalizedStringKey("Link contact") : LocalizedStringKey("Change contact"), systemImage: "person.crop.circle.badge.plus") {
                    isLinkingContact = true
                }
            }
            if node.summary.isEmpty {
                Text("No summary yet.").foregroundStyle(.secondary)
            } else {
                MarkdownView(text: node.summary)
            }
        }
        if let overview = node.overview, !overview.isEmpty {
            Section {
                MarkdownView(text: overview)
            } header: {
                Text("Overview")
            } footer: {
                if let written = ServerTime.date(node.overviewWrittenAt) {
                    Text("Written \(written.formatted(date: .abbreviated, time: .omitted))")
                }
            }
        }
    }

    /// The person's own records the page is linked to, by kind. A guess is
    /// marked and is confirmed or removed by swiping it or in its menu.
    @ViewBuilder
    private func linkedRecords(_ node: KnowledgeNode) -> some View {
        let links = model.recordLinks
        if !links.isEmpty {
            ForEach(KnowledgeRecordKinds.grouped(links)) { group in
                Section {
                    ForEach(group.links) { link in
                        LinkedRecordRow(link: link) { action in
                            let pagePath = model.pagePath
                            Task {
                                switch action {
                                case .confirm:
                                    _ = await model.act(saying: String(localized: "Link confirmed")) {
                                        try await model.connection.linkKnowledgeRecord(pagePath, link: link)
                                    }
                                case .remove:
                                    _ = await model.act(saying: String(localized: "Link removed")) {
                                        try await model.connection.unlinkKnowledgeRecord(pagePath, link: link)
                                    }
                                }
                            }
                        }
                    }
                } header: {
                    Label(KnowledgeRecordWords.kind(group.recordKind), systemImage: KnowledgeRecordKinds.symbol(group.recordKind))
                } footer: {
                    if group.recordKind == KnowledgeRecordKinds.grouped(links).last?.recordKind, links.count < model.recordLinkCount {
                        Button("Show \(model.recordLinkCount - links.count) more") { Task { await model.loadMoreRecordLinks() } }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func reflections(_ page: KnowledgePage) -> some View {
        let reflections = page.presentFacts.filter { $0.kind == "reflection" }
        if !reflections.isEmpty {
            Section("Reflections") {
                ForEach(reflections) { fact in
                    VStack(alignment: .leading, spacing: 4) {
                        if let kind = fact.reflectionKind {
                            Chip(text: kind, isAlert: false)
                        }
                        Text(fact.text)
                        ForEach(fact.citations, id: \.self) { citation in
                            Button(citation) {
                                let cited = String(citation.split(separator: "#").first ?? "")
                                path.append(KnowledgePlace.page(cited))
                            }
                            .font(.caption.monospaced())
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func facts(_ page: KnowledgePage) -> some View {
        let facts = page.presentFacts.filter { $0.kind != "reflection" }
        Section {
            ForEach(facts.prefix(shownFactCount)) { fact in
                Button {
                    editingFact = FactDraft(fact)
                } label: {
                    FactRow(fact: fact, attachments: page.attachments(of: fact))
                }
                .buttonStyle(.plain)
                .swipeActions {
                    Button("Forget", systemImage: "trash", role: .destructive) { forgettingFact = fact }
                    Button("Move", systemImage: "arrow.right.doc.on.clipboard") {
                        choosing = PageChoice(kind: .moveFact(fact.number), title: String(localized: "Move it to"), initial: "")
                    }
                    .tint(.indigo)
                }
                // Where the evidence is one of the person's own records, the
                // record opens, at the passage for a recording.
                ForEach(page.records(of: fact), id: \.evidenceIndex) { record in
                    EvidenceRecordRow(record: record)
                }
            }
            if facts.count > shownFactCount {
                Button("Show \(facts.count - shownFactCount) more") { shownFactCount += 50 }
            }
            ForEach(page.folded.compactMap { $0 }, id: \.fact?.id) { folded in
                Text("#\(folded.fact?.number ?? 0) \(folded.fact?.text ?? "") · folded into #\(folded.into)")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Button("Add a fact", systemImage: "plus") { editingFact = FactDraft() }
        } header: {
            Text("Facts")
        } footer: {
            if facts.isEmpty { Text("Nothing known here yet.") }
        }
    }

    @ViewBuilder
    private var connections: some View {
        Section {
            if let neighbours = model.neighbours {
                if let parent = neighbours.parent {
                    NavigationLink(value: KnowledgePlace.page(parent.path)) {
                        LabeledContent("Filed under", value: parent.shownName)
                    }
                }
                ForEach(Array(neighbours.neighbours.compactMap { $0 }.enumerated()), id: \.offset) { _, neighbour in
                    if let node = neighbour.node {
                        NavigationLink(value: KnowledgePlace.page(node.path)) {
                            VStack(alignment: .leading, spacing: 2) {
                                HStack {
                                    Text(neighbour.outward ? "\(neighbour.relation.replacingOccurrences(of: "_", with: " ")) →" : "← \(neighbour.relation.replacingOccurrences(of: "_", with: " "))")
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                    if neighbour.status == "proposed" {
                                        Chip(text: String(localized: "A guess"), isAlert: false)
                                    }
                                }
                                Text(node.shownName)
                                if !neighbour.note.isEmpty {
                                    Text(neighbour.note).font(.footnote).foregroundStyle(.secondary)
                                }
                            }
                        }
                        .swipeActions {
                            if neighbour.outward {
                                Button("Unlink", systemImage: "link.badge.plus", role: .destructive) {
                                    let pagePath = model.pagePath
                                    Task {
                                        _ = await model.act(saying: String(localized: "Unlinked")) {
                                            try await model.connection.unlinkKnowledgeNodes(path: pagePath, to: node.path, relation: neighbour.relation)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                let leftOut = neighbours.links - neighbours.neighbours.compactMap { $0 }.count
                if leftOut > 0 {
                    Text("\(leftOut) more links not shown").font(.footnote).foregroundStyle(.secondary)
                }
            }
            Button("Link to another page", systemImage: "link") { isLinking = true }
        } header: {
            Text("Connections")
        }
    }

    @ViewBuilder
    private var historySection: some View {
        Section("History") {
            if let history = model.history {
                if history.isEmpty {
                    Text("No changes recorded.").foregroundStyle(.secondary)
                }
                ForEach(history, id: \.revision) { revision in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(revision.change)
                        HStack {
                            Text(KnowledgeWordsShown.actor(revision.actor))
                            if let at = ServerTime.date(revision.createdAt) {
                                Text(at.formatted(date: .abbreviated, time: .shortened))
                            }
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        if !revision.before.isEmpty {
                            Text("was: \(String(revision.before.prefix(200)))").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            } else {
                Button("Show history") { Task { await model.loadHistory() } }
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                if let node = model.page?.node {
                    Button("Edit", systemImage: "pencil") { isEditing = true }
                    Button(node.pinned == true ? LocalizedStringKey("Unpin") : "Pin", systemImage: node.pinned == true ? "pin.slash" : "pin") {
                        let pagePath = model.pagePath
                        Task {
                            _ = await model.act(saying: node.pinned == true ? String(localized: "Unpinned") : String(localized: "Pinned")) {
                                try await model.connection.saveKnowledgeNode(
                                    path: pagePath, name: node.name, summary: node.summary, aliases: nil, isPinned: !(node.pinned ?? false))
                            }
                        }
                    }
                    if !node.parentPath.isEmpty {
                        Button("Move", systemImage: "folder") {
                            choosing = PageChoice(kind: .move, title: String(localized: "File it under"), initial: node.parentPath)
                        }
                        Button("Merge into…", systemImage: "arrow.triangle.merge") {
                            choosing = PageChoice(kind: .merge, title: String(localized: "Merge it into"), initial: "")
                        }
                    }
                    Button("Ask \(app.agentTitle)", systemImage: "bubble.left.and.text.bubble.right") {
                        app.askingAbout = AgentReference(path: node.path, name: node.shownName)
                    }
                    Button("Forget page", systemImage: "trash", role: .destructive) { isConfirmingForget = true }
                }
            } label: {
                Label("Page actions", systemImage: "ellipsis.circle")
            }
        }
    }

    // MARK: Actions

    private func choose(_ choice: PageChoice, _ chosen: String) async {
        let pagePath = model.pagePath
        switch choice.kind {
        case .move:
            var newPath: String?
            let isMoved = await model.act(saying: String(localized: "Moved")) {
                newPath = try await model.connection.moveKnowledgeNode(pagePath, under: chosen)
            }
            if isMoved, let newPath, newPath != pagePath {
                model.moved(to: newPath)
                await model.load()
            }
        case .merge:
            mergingInto = chosen
        case .moveFact(let number):
            _ = await model.act(saying: String(localized: "Moved")) {
                try await model.connection.moveKnowledgeFact(path: pagePath, number: number, to: chosen)
            }
        }
    }

    private func merge(into: String) async {
        let pagePath = model.pagePath
        var left: String?
        let isMerged = await model.act(saying: String(localized: "Merged")) {
            left = try await model.connection.mergeKnowledgeNodes(pagePath, into: into)
        }
        if isMerged {
            model.moved(to: left ?? into)
            await model.load()
        }
    }
}

enum KnowledgeWordsShown {
    /// Who made a change, in words.
    static func actor(_ actor: String) -> String {
        switch actor {
        case "person": String(localized: "you")
        case "agent": String(localized: "the agent")
        case "remember": String(localized: "filing a conversation")
        case "dream": String(localized: "a dream")
        case "ingest": String(localized: "reading a source")
        default: actor
        }
    }

    /// Where a fact came from, in words.
    static func source(_ kind: String) -> String {
        switch kind {
        case "conversation": String(localized: "from a conversation")
        case "mail": String(localized: "from mail")
        case "document": String(localized: "from a document")
        case "commit": String(localized: "from a commit")
        case "chat": String(localized: "from a chat")
        case "contact": String(localized: "from a contact")
        case "repository": String(localized: "from a repository")
        case "calendar_event": String(localized: "from a calendar event")
        case "reminder": String(localized: "from a reminder")
        case "conversation_message": String(localized: "from a conversation")
        case "finance_transaction": String(localized: "from a transaction")
        case "finance_receipt": String(localized: "from a receipt")
        case "recording_passage": String(localized: "from a recording")
        case "memory": String(localized: "from memory")
        case "person": String(localized: "from you")
        case "dream": String(localized: "from a dream")
        default: kind
        }
    }
}

/// FactRow is a fact: its number and words, its kind, whether it was
/// worked out, what it came from and when, and its files.
struct FactRow: View {
    let fact: KnowledgeFact
    let attachments: [KnowledgeAttachment]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("#\(fact.number) \(fact.text)")
            HStack(spacing: 4) {
                if fact.kind != "fact" {
                    Chip(text: fact.kind, isAlert: false)
                }
                if fact.inferred {
                    Chip(text: String(localized: "Worked out, not told"), isAlert: true)
                }
            }
            if let first = fact.evidence.first {
                Group {
                    if !first.quote.isEmpty {
                        Text("“\(first.quote)” \(KnowledgeWordsShown.source(first.kind))")
                    } else {
                        Text(KnowledgeWordsShown.source(first.kind))
                    }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(3)
            }
            if let happened = fact.happenedAt.flatMap({ ServerTime.date($0) }) {
                Text(happened.formatted(date: .abbreviated, time: .omitted)).font(.caption).foregroundStyle(.secondary)
            }
            ForEach(attachments, id: \.documentId) { file in
                if file.path.isEmpty {
                    Text("\(file.name) (missing)").font(.caption).foregroundStyle(.secondary)
                } else if file.contentType.hasPrefix("image/") {
                    RemotePicture(path: file.path, name: file.name, maxHeight: 160)
                } else {
                    PreviewLink(path: file.path, name: file.name)
                }
            }
        }
        .contentShape(.rect)
    }
}

/// PreviewLink opens a file the server keeps in Quick Look.
struct PreviewLink: View {
    let path: String
    let name: String
    @Environment(AppModel.self) private var app

    var body: some View {
        Button {
            Task { await app.preview(path, name: name) }
        } label: {
            Label(name, systemImage: "doc")
                .font(.caption)
        }
    }
}

/// PageEditor changes a page's name, other names and summary.
struct PageEditor: View {
    let model: KnowledgePageModel
    let node: KnowledgeNode
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var aliases = ""
    @State private var summary = ""

    var body: some View {
        NavigationStack {
            Form {
                TextField("Name", text: $name)
                Section {
                    TextField("Other names", text: $aliases)
                } footer: {
                    Text("Separated by commas.")
                }
                Section("Summary") {
                    TextField("Summary", text: $summary, axis: .vertical).lineLimit(6...)
                }
            }
            .navigationTitle("Edit page")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }.disabled(name.trimmed.isEmpty)
                }
            }
            .onAppear {
                name = node.name
                aliases = (node.aliases ?? []).joined(separator: ", ")
                summary = node.summary
            }
        }
    }

    private func save() {
        let list = aliases.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        // Other names are sent only when changed, so a change made elsewhere
        // meanwhile is not undone.
        let changed = list == (node.aliases ?? []) ? nil : list
        let pagePath = node.path
        let newName = name.trimmed
        let newSummary = summary.trimmed
        Task {
            let isSaved = await model.act(saying: String(localized: "Saved")) {
                try await model.connection.saveKnowledgeNode(path: pagePath, name: newName, summary: newSummary, aliases: changed, isPinned: nil)
            }
            if isSaved { dismiss() }
        }
    }
}

/// FactDraft is a fact being added or changed.
struct FactDraft: Identifiable {
    let id = UUID()
    var number: Int?
    var text = ""
    var kind = "fact"
    var happened = ""
    var audiences: Set<String> = []
    var originalAudiences: Set<String> = []

    init() {}

    init(_ fact: KnowledgeFact) {
        number = fact.number
        text = fact.text
        kind = KnowledgeWords.factKinds.contains(fact.kind) ? fact.kind : "fact"
        happened = String((fact.happenedAt ?? "").prefix(10))
        audiences = Set(fact.audiences ?? [])
        originalAudiences = audiences
    }
}

/// FactEditor adds a fact or changes one: its words, kind, when it was true
/// and which runs read it.
struct FactEditor: View {
    let model: KnowledgePageModel
    @State var draft: FactDraft
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("What is known", text: $draft.text, axis: .vertical).lineLimit(3...)
                    Picker("Kind", selection: $draft.kind) {
                        ForEach(KnowledgeWords.factKinds, id: \.self) { kind in
                            Text(verbatim: kind).tag(kind)
                        }
                    }
                    TextField("When it was true (2023-06)", text: $draft.happened)
                        .keyboardType(.numbersAndPunctuation)
                } footer: {
                    if !KnowledgeWords.isHappenedValid(draft.happened) {
                        Text("A year, a month or a day: 2023, 2023-06 or 2023-06-14.").foregroundStyle(.red)
                    }
                }
                Section("Which runs read it") {
                    ForEach(KnowledgeWords.audiences, id: \.self) { audience in
                        Toggle(audience, isOn: Binding(
                            get: { draft.audiences.contains(audience) },
                            set: { if $0 { draft.audiences.insert(audience) } else { draft.audiences.remove(audience) } }))
                    }
                }
            }
            .navigationTitle(draft.number == nil ? String(localized: "Add fact") : String(localized: "Edit fact"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(draft.text.trimmed.isEmpty || !KnowledgeWords.isHappenedValid(draft.happened))
                }
            }
        }
    }

    private func save() {
        let pagePath = model.pagePath
        let audiences = draft.audiences == draft.originalAudiences ? nil : KnowledgeWords.audiences.filter { draft.audiences.contains($0) }
        let number = draft.number
        let kind = draft.kind
        let text = draft.text.trimmed
        let happened = draft.happened
        Task {
            let isSaved = await model.act(saying: String(localized: "Saved")) {
                _ = try await model.connection.saveKnowledgeFact(
                    path: pagePath, number: number, kind: kind, text: text, happened: happened, audiences: audiences)
            }
            if isSaved { dismiss() }
        }
    }
}

/// PageChoice is what a chosen page is for.
struct PageChoice: Identifiable {
    enum Kind {
        case move, merge
        case moveFact(Int)
    }

    let id = UUID()
    let kind: Kind
    let title: String
    let initial: String
}

/// PagePicker chooses a page: searched, or typed as a path.
struct PagePicker: View {
    let connection: Connection
    let title: String
    let excluding: String
    let initial: String
    let onChoose: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var typed = ""
    @State private var found: [KnowledgeNode] = []

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField("Page path", text: $typed)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .font(.body.monospaced())
                }
                ForEach(found.filter { $0.path != excluding }) { node in
                    Button {
                        finish(node.path)
                    } label: {
                        VStack(alignment: .leading) {
                            Text(node.shownName)
                            Text(node.path).font(.caption.monospaced()).foregroundStyle(.secondary)
                        }
                    }
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Choose") { finish(typed.trimmed) }
                        .disabled(typed.trimmed.isEmpty || typed.trimmed == excluding || typed.trimmed == initial)
                }
            }
            .onAppear { typed = initial }
            .task(id: typed) {
                guard typed.trimmed.count >= 2 else {
                    found = []
                    return
                }
                try? await Task.sleep(for: .milliseconds(250))
                guard !Task.isCancelled else { return }
                found = (try? await connection.searchKnowledge(typed.trimmed, first: 20))?.nodes.compactMap { $0 } ?? []
            }
        }
    }

    private func finish(_ chosen: String) {
        onChoose(chosen)
        dismiss()
    }
}

/// LinkEditor links the page to another, with how they are related.
struct LinkEditor: View {
    let model: KnowledgePageModel
    @Environment(\.dismiss) private var dismiss
    @State private var to = ""
    @State private var relation = "related_to"
    @State private var note = ""
    @State private var isPicking = false

    var body: some View {
        NavigationStack {
            Form {
                Button {
                    isPicking = true
                } label: {
                    LabeledContent("Link to", value: to.isEmpty ? String(localized: "Choose a page") : to)
                }
                Picker("Relation", selection: $relation) {
                    ForEach(KnowledgeWords.relations, id: \.self) { relation in
                        Text(verbatim: relation.replacingOccurrences(of: "_", with: " ")).tag(relation)
                    }
                }
                TextField("Note (optional)", text: $note)
            }
            .navigationTitle("Link")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Link") { save() }.disabled(to.isEmpty || to == model.pagePath)
                }
            }
            .sheet(isPresented: $isPicking) {
                PagePicker(connection: model.connection, title: String(localized: "Link to"), excluding: model.pagePath, initial: "") { to = $0 }
            }
        }
    }

    private func save() {
        let pagePath = model.pagePath
        let target = to
        let chosenRelation = relation
        let writtenNote = note.trimmed
        Task {
            let isLinked = await model.act(saying: String(localized: "Linked")) {
                try await model.connection.linkKnowledgeNodes(path: pagePath, to: target, relation: chosenRelation, note: writtenNote)
            }
            if isLinked { dismiss() }
        }
    }
}

/// RecallSheet asks the agent's memory a question and shows the pages and
/// facts it brings up.
struct RecallSheet: View {
    let connection: Connection
    let onOpen: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var question = ""
    @State private var recall: KnowledgeRecall?
    @State private var isAsking = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField("What do you want to know?", text: $question, axis: .vertical)
                        .onSubmit { ask() }
                    Button("Ask") { ask() }.disabled(question.trimmed.isEmpty || isAsking)
                }
                if isAsking {
                    ProgressView()
                }
                if let problemText {
                    Text(problemText).foregroundStyle(.red)
                }
                if let recall {
                    if recall.pages.compactMap({ $0 }).isEmpty {
                        Text("Nothing in memory answers that.").foregroundStyle(.secondary)
                    }
                    ForEach(recall.pages.compactMap { $0 }, id: \.path) { page in
                        Section {
                            ForEach(page.facts.compactMap { $0 }, id: \.number) { fact in
                                Text("#\(fact.number) \(fact.text)").font(.callout)
                            }
                        } header: {
                            Button(page.path) { onOpen(page.path) }
                                .font(.caption.monospaced())
                        }
                    }
                }
            }
            .navigationTitle("Ask the memory")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } }
            }
            .onChange(of: question) { recall = nil }
        }
    }

    private func ask() {
        let asked = question.trimmed
        guard !asked.isEmpty else { return }
        isAsking = true
        Task {
            do {
                recall = try await connection.recallMemory(asked)
                problemText = nil
            } catch {
                problemText = error.localizedDescription
            }
            isAsking = false
        }
    }
}

/// DocumentsSheet searches what the agent's sources read, and reads a
/// document a slice at a time.
struct DocumentsSheet: View {
    let connection: Connection
    @Environment(\.dismiss) private var dismiss
    @State private var words = ""
    @State private var sources: [KnowledgeSource] = []
    @State private var sourceId = ""
    @State private var directory = ""
    @State private var found: DocumentSearch?
    @State private var passages: [DocumentSearch.Passage] = []
    @State private var problemText: String?
    @State private var isSearching = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField("Words", text: $words).onSubmit { search(isMore: false) }
                    Picker("Source", selection: $sourceId) {
                        Text("Any source").tag("")
                        ForEach(sources) { source in
                            Text(source.name).tag(source.id)
                        }
                    }
                    TextField("In a directory (optional)", text: $directory)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Button("Search") { search(isMore: false) }.disabled(words.trimmed.isEmpty || isSearching)
                }
                if let problemText {
                    Text(problemText).foregroundStyle(.red)
                }
                if let found {
                    if !found.meaningful {
                        Text("Found by words alone, not by meaning.").font(.footnote).foregroundStyle(.secondary)
                    }
                    let definitions = found.definitions.compactMap { $0 }
                    if !definitions.isEmpty {
                        Section("Defined in") {
                            ForEach(Array(definitions.enumerated()), id: \.offset) { _, definition in
                                NavigationLink {
                                    DocumentReader(connection: connection, documentId: definition.documentId)
                                } label: {
                                    Text("\(definition.symbol) · \(definition.kind) · \(definition.title)").font(.callout)
                                }
                            }
                        }
                    }
                    let directories = found.directories.compactMap { $0 }
                    if !directories.isEmpty {
                        Section("Where these are") {
                            ForEach(Array(directories.enumerated()), id: \.offset) { _, place in
                                Button("\(place.directory) · \(place.passageCount)") {
                                    directory = place.directory
                                    sourceId = place.sourceId
                                    search(isMore: false)
                                }
                                .font(.callout.monospaced())
                            }
                        }
                    }
                    let groups = Dictionary(grouping: passages, by: \.documentId)
                    let order = passages.map(\.documentId).reduce(into: [String]()) { if !$0.contains($1) { $0.append($1) } }
                    ForEach(order, id: \.self) { documentId in
                        let group = groups[documentId] ?? []
                        Section {
                            ForEach(Array(group.enumerated()), id: \.offset) { _, passage in
                                Text(passage.text).font(.callout).lineLimit(8)
                            }
                            NavigationLink("Read") {
                                DocumentReader(connection: connection, documentId: documentId)
                            }
                        } header: {
                            let first = group.first
                            Text([first?.title.isEmpty == false ? first?.title : first?.externalId, first?.author, first?.source]
                                .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                        }
                    }
                    if found.nextOffset > 0 {
                        Button("Show more") { search(isMore: true) }
                    }
                }
            }
            .navigationTitle("Search documents")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } }
            }
            .task { sources = (try? await connection.knowledgeSources()) ?? [] }
        }
    }

    private func search(isMore: Bool) {
        let asked = words.trimmed
        guard !asked.isEmpty else { return }
        isSearching = true
        Task {
            do {
                let read = try await connection.searchDocuments(
                    asked, sourceId: sourceId, directory: directory.trimmed, offset: isMore ? (found?.nextOffset ?? 0) : 0)
                passages = (isMore ? passages : []) + read.passages.compactMap { $0 }
                found = read
                problemText = nil
            } catch {
                problemText = error.localizedDescription
            }
            isSearching = false
        }
    }
}

/// DocumentReader reads a document a slice at a time.
struct DocumentReader: View {
    let connection: Connection
    let documentId: String
    @State private var extract: DocumentExtract?
    @State private var text = ""
    @State private var isGone = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if let extract {
                    Text([extract.author, extract.source].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Text(text).textSelection(.enabled)
                    if extract.next > 0 {
                        Button("Read on (\(extract.total - extract.next) more characters)") {
                            Task { await read(from: extract.next) }
                        }
                        .buttonStyle(.bordered)
                    }
                } else if isGone {
                    ContentUnavailableView("This document is no longer kept", systemImage: "doc.questionmark")
                } else {
                    ProgressView()
                }
            }
            .padding()
        }
        .navigationTitle(extract?.title ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .task { await read(from: 0) }
    }

    private func read(from: Int) async {
        guard let slice = try? await connection.readDocument(documentId, from: from) else {
            isGone = extract == nil
            return
        }
        text += slice.text
        extract = slice
    }
}


/// LinkedRecordRow is a record a page is linked to: its title, what the page
/// is to it, its date, and a guess marked as one.
struct LinkedRecordRow: View {
    enum Action { case confirm, remove }

    let link: KnowledgeRecordLink
    let onAct: (Action) -> Void
    @Environment(AppModel.self) private var app

    var body: some View {
        Button {
            app.openRecord(dashboardPath: link.dashboardPath)
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                if link.isUnavailable {
                    Text("No longer available").foregroundStyle(.secondary)
                } else {
                    Text(link.recordTitle.isEmpty ? KnowledgeRecordWords.kind(link.recordKind) : link.recordTitle)
                }
                HStack(spacing: 6) {
                    Text(KnowledgeRecordWords.relation(link.recordRelation))
                    if let date = link.date {
                        Text(date.formatted(date: .abbreviated, time: .omitted))
                    }
                    if link.isProposed {
                        Chip(text: String(localized: "A guess"), isAlert: false)
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
            }
        }
        .buttonStyle(.plain)
        .disabled(!link.canOpen)
        .swipeActions(edge: .trailing) {
            Button("Remove", systemImage: "minus.circle", role: .destructive) { onAct(.remove) }
            if link.isProposed {
                Button("Confirm", systemImage: "checkmark") { onAct(.confirm) }.tint(.accentColor)
            }
        }
        .contextMenu {
            if link.isProposed {
                Button("Confirm link", systemImage: "checkmark") { onAct(.confirm) }
            }
            Button("Remove link", systemImage: "minus.circle", role: .destructive) { onAct(.remove) }
        }
    }
}

/// EvidenceRecordRow is the record a fact's evidence names, under the fact.
struct EvidenceRecordRow: View {
    let record: KnowledgeEvidenceRecord
    @Environment(AppModel.self) private var app

    var body: some View {
        Button {
            app.openRecord(dashboardPath: record.dashboardPath)
        } label: {
            Label(
                record.isUnavailable
                    ? String(localized: "\(KnowledgeRecordWords.kind(record.recordKind)): no longer available")
                    : (record.recordTitle.isEmpty ? KnowledgeRecordWords.kind(record.recordKind) : record.recordTitle),
                systemImage: KnowledgeRecordKinds.symbol(record.recordKind)
            )
            .font(.footnote)
        }
        .disabled(!record.canOpen)
    }
}

/// ContactPickerSheet chooses the card a person's page is about, from the
/// cards in the person's own address books. It never makes a card.
struct ContactPickerSheet: View {
    let connection: Connection
    let currentContactId: String
    let onChoose: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var searchText = ""
    @State private var contacts: [ContactSummary] = []
    @State private var isLoaded = false

    var body: some View {
        NavigationStack {
            List {
                if !currentContactId.isEmpty {
                    Button("Unlink contact", systemImage: "person.crop.circle.badge.minus", role: .destructive) { onChoose("") }
                }
                ForEach(contacts) { contact in
                    Button {
                        onChoose(contact.id)
                    } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(contact.shownName)
                                if let email = contact.emails?.first, email != contact.shownName {
                                    Text(email).font(.footnote).foregroundStyle(.secondary)
                                }
                            }
                            Spacer()
                            if contact.id == currentContactId {
                                Image(systemName: "checkmark").foregroundStyle(.tint)
                            }
                        }
                    }
                    .buttonStyle(.plain)
                }
            }
            .overlay {
                if !isLoaded {
                    ProgressView()
                } else if contacts.isEmpty {
                    ContentUnavailableView.search(text: searchText)
                }
            }
            .searchable(text: $searchText, prompt: "Name, address or phone")
            .navigationTitle("Link a contact")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
            .task(id: searchText) {
                if !searchText.isEmpty {
                    try? await Task.sleep(for: .milliseconds(250))
                    guard !Task.isCancelled else { return }
                }
                let books = (try? await connection.addressBooks()) ?? []
                var found: [ContactSummary] = []
                for book in books {
                    found += (try? await connection.contacts(addressBookId: book.id, matching: searchText, first: 30)) ?? []
                }
                contacts = found
                isLoaded = true
            }
        }
    }
}
