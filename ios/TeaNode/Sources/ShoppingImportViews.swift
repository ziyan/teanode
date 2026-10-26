import PhotosUI
import SwiftUI
import TeaNodeKit

/// ShoppingImportSheet reads a list out of pasted text and pictures (a
/// screenshot of a list in another app, a photo of one on paper), shows what
/// it found with what the list has already, and adds what the person keeps.
struct ShoppingImportSheet: View {
    let connection: Connection
    let initialListId: String?
    /// Says items were added, so the screen below reads its list again.
    let onAdded: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var lists: [ShoppingList]
    /// The list the items go on; empty for a new one.
    @State private var destinationListId: String
    @State private var newListName = ""
    @State private var importText = ""
    @State private var pickedItems: [PhotosPickerItem] = []
    @State private var files: [PendingFile] = []
    /// The attachment each picture was uploaded as, so reading again does not upload it again.
    @State private var attachmentIds: [UUID: String] = [:]
    @State private var drafts: [ShoppingImportDraft] = []
    @State private var hasRead = false
    @State private var isReading = false
    @State private var isAdding = false
    @State private var problemText: String?
    @State private var doneText: String?

    init(connection: Connection, initialListId: String?, onAdded: @escaping () -> Void) {
        self.connection = connection
        self.initialListId = initialListId
        self.onAdded = onAdded
        _lists = State(initialValue: [])
        _destinationListId = State(initialValue: initialListId ?? "")
    }

    var body: some View {
        NavigationStack {
            form
                .navigationTitle("Import")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                }
                .safeAreaInset(edge: .bottom) {
                    if hasRead, !includedDrafts.isEmpty {
                        addButton
                    }
                }
                .task {
                    lists = (try? await connection.shoppingLists()) ?? []
                    #if DEBUG
                        await openDemoImport()
                    #endif
                }
                .onChange(of: pickedItems) { _, picked in
                    guard !picked.isEmpty else { return }
                    pickedItems = []
                    Task { await add(pictures: picked) }
                }
                .toast(done: $doneText, problem: $problemText)
        }
    }

    private var includedDrafts: [ShoppingImportDraft] { drafts.filter(\.isIncluded) }

    private var hasInput: Bool {
        !importText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !files.isEmpty
    }

    private var isNameMissing: Bool {
        destinationListId.isEmpty && newListName.trimmingCharacters(in: .whitespaces).isEmpty
    }

    private var form: some View {
        Form {
            Section {
                TextField("Paste or type a list", text: $importText, axis: .vertical)
                    .lineLimit(4...10)
            } header: {
                Text("Text")
            } footer: {
                Text("One item on each line is easiest, but any list will do.")
            }
            Section {
                PhotosPicker(selection: $pickedItems, maxSelectionCount: 6, matching: .images) {
                    Label("Choose screenshots or photos", systemImage: "photo.on.rectangle")
                }
                if !files.isEmpty {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(files) { file in
                                PendingFileChip(file: file) { files.removeAll { $0.id == file.id } }
                            }
                        }
                    }
                }
            } header: {
                Text("Pictures")
            }
            Section("Add to") {
                Picker("List", selection: $destinationListId) {
                    ForEach(lists) { list in
                        Text(verbatim: list.listName).tag(list.id)
                    }
                    Text("A new list").tag("")
                }
                if destinationListId.isEmpty {
                    TextField("Name of the new list", text: $newListName)
                }
            }
            Section {
                Button {
                    Task { await read() }
                } label: {
                    if isReading {
                        HStack(spacing: 8) {
                            ProgressView()
                            Text("Reading…")
                        }
                    } else {
                        Label("Read the list", systemImage: "text.viewfinder")
                            .actionColor()
                    }
                }
                .disabled(!hasInput || isReading || isAdding)
            }
            if hasRead {
                previewSection
            }
        }
    }

    private var previewSection: some View {
        Section {
            if drafts.isEmpty {
                Text("No items were found. Check the text or the pictures and read again.")
                    .foregroundStyle(.secondary)
            }
            ForEach($drafts) { $draft in
                Toggle(isOn: $draft.isIncluded) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: draft.item.shownTitle)
                            if !draft.item.shownDetail.isEmpty {
                                Text(verbatim: draft.item.shownDetail)
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        Spacer(minLength: 8)
                        Text(verbatim: ShoppingWords.importMark(draft.item))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        } header: {
            Text("Found")
        } footer: {
            Text("An item the list has already is not added twice: it needs the larger of the two counts.")
        }
    }

    /// The screen's one prominent button, floating under the list.
    private var addButton: some View {
        Button {
            Task { await add() }
        } label: {
            Text("Add \(includedDrafts.count) items")
                .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .buttonBorderShape(.capsule)
        .controlSize(.large)
        .disabled(isAdding || isNameMissing)
        .padding(.horizontal)
        .padding(.bottom, 8)
    }

    // MARK: What they do

    private func add(pictures picked: [PhotosPickerItem]) async {
        for item in picked {
            if let data = try? await item.loadTransferable(type: Data.self), let file = PendingFile(pictureData: data, index: files.count) {
                files.append(file)
            }
        }
    }

    /// Uploads the pictures not yet uploaded, and has the server's model read the lot.
    private func read() async {
        isReading = true
        defer { isReading = false }
        do {
            var ids: [String] = []
            for file in files {
                if let known = attachmentIds[file.id] {
                    ids.append(known)
                } else {
                    let uploaded = try await connection.upload(name: file.name, contentType: file.contentType, data: file.data)
                    attachmentIds[file.id] = uploaded
                    ids.append(uploaded)
                }
            }
            let preview = try await connection.previewShoppingImport(
                shoppingListId: destinationListId.isEmpty ? nil : destinationListId, importText: importText, agentAttachmentIds: ids)
            drafts = ShoppingImportDraft.drafts(of: preview)
            hasRead = true
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    /// Adds the items kept, to a new list when that was chosen.
    private func add() async {
        let entries = ShoppingImportDraft.entries(of: drafts)
        guard !entries.isEmpty else { return }
        isAdding = true
        defer { isAdding = false }
        do {
            var listId = destinationListId
            if listId.isEmpty {
                listId = try await connection.createShoppingList(listName: newListName.trimmingCharacters(in: .whitespaces)).id
            }
            try await connection.addShoppingItems(shoppingListId: listId, entries: entries)
            onAdded()
            dismiss()
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    #if DEBUG
        /// `-TeaNodeScreen shopping-import` has a pasted list already read.
        private func openDemoImport() async {
            guard DemoServer.isRequested, DemoServer.screen == "shopping-import", !hasRead else { return }
            importText = "Produce\nApples x6\nTomatoes\nBakery\nSourdough loaf\nSoup, 2 cans\nNavel oranges"
            await read()
        }
    #endif
}
