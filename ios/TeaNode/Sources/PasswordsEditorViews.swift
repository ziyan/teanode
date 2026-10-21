import SwiftUI
import TeaNodeKit

// The editor for a login, a card or a note, and the password generator. The
// editor works on a draft of plain text fields; saving turns the draft into the
// fields of the item's kind, which the session seals on this iPhone.

/// PasswordsDraftError is why a draft could not become an item.
enum PasswordsDraftError: Error {
    /// The one-time code text is neither an `otpauth://` address nor a secret key.
    case invalidOneTimeCode
}

/// PasswordsDraft is an item being edited: one text for each field of any
/// kind, of which the editor shows the ones the kind has.
struct PasswordsDraft {
    var name = ""
    var folder = ""
    var notes = ""
    // A login.
    var userName = ""
    var password = ""
    var sitesText = ""
    var oneTimeCodeText = ""
    /// The passkeys of a login being changed: the editor does not show them, and keeps them.
    var passkeys: [PasswordsPasskeyFields] = []
    // A card.
    var cardholderName = ""
    var number = ""
    var expiryMonth = ""
    var expiryYear = ""
    var securityCode = ""
    var brand = ""
    // A note.
    var text = ""

    init(item: PasswordItem?) {
        guard let item else { return }
        switch item.fields {
        case .login(let login):
            name = login.name
            folder = login.folder
            notes = login.notes
            userName = login.userName
            password = login.password
            sitesText = login.sites.joined(separator: "\n")
            oneTimeCodeText = login.oneTimeCodeUri
            passkeys = login.passkeys
        case .card(let card):
            name = card.name
            folder = card.folder
            notes = card.notes
            cardholderName = card.cardholderName
            number = card.number
            expiryMonth = card.expiryMonth
            expiryYear = card.expiryYear
            securityCode = card.securityCode
            brand = card.brand
        case .note(let note):
            name = note.name
            folder = note.folder
            text = note.text
        }
    }

    /// The fields of an item of this kind, from the text typed.
    func fields(for itemKind: PasswordsItemKind) throws -> PasswordsItemFields {
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedFolder = folder.trimmingCharacters(in: .whitespacesAndNewlines)
        switch itemKind {
        case .login:
            let sites = sitesText.split(whereSeparator: \.isNewline)
                .map { $0.trimmingCharacters(in: .whitespaces) }
                .filter { !$0.isEmpty }
            return .login(
                PasswordsLoginFields(
                    name: trimmedName, userName: userName.trimmingCharacters(in: .whitespaces), password: password, sites: sites,
                    oneTimeCodeUri: try Self.oneTimeCodeUri(from: oneTimeCodeText, name: trimmedName), notes: notes, passkeys: passkeys,
                    folder: trimmedFolder))
        case .card:
            return .card(
                PasswordsCardFields(
                    name: trimmedName, cardholderName: cardholderName.trimmingCharacters(in: .whitespaces),
                    number: number.filter { $0.isNumber || $0 == " " }.trimmingCharacters(in: .whitespaces),
                    expiryMonth: expiryMonth.trimmingCharacters(in: .whitespaces),
                    expiryYear: expiryYear.trimmingCharacters(in: .whitespaces),
                    securityCode: securityCode.trimmingCharacters(in: .whitespaces), brand: brand.trimmingCharacters(in: .whitespaces),
                    notes: notes, folder: trimmedFolder))
        case .note:
            return .note(PasswordsNoteFields(name: trimmedName, text: text, folder: trimmedFolder))
        }
    }

    /// This draft moved onto the item as it is now (`fresh`, opened again after
    /// the item changed on another device): a field the person edited here
    /// keeps what they typed, every other field takes the fresh item's value, and
    /// the passkeys (which the editor does not show) are the fresh item's.
    /// `original` is the draft as it was when the person opened the item.
    func rebased(from original: PasswordsDraft, onto fresh: PasswordsDraft) -> PasswordsDraft {
        var result = fresh
        func keepEdit<FieldValue: Equatable>(_ field: WritableKeyPath<PasswordsDraft, FieldValue>) {
            if self[keyPath: field] != original[keyPath: field] { result[keyPath: field] = self[keyPath: field] }
        }
        keepEdit(\.name)
        keepEdit(\.folder)
        keepEdit(\.notes)
        keepEdit(\.userName)
        keepEdit(\.password)
        keepEdit(\.sitesText)
        keepEdit(\.oneTimeCodeText)
        keepEdit(\.cardholderName)
        keepEdit(\.number)
        keepEdit(\.expiryMonth)
        keepEdit(\.expiryYear)
        keepEdit(\.securityCode)
        keepEdit(\.brand)
        keepEdit(\.text)
        return result
    }

    /// The `otpauth://` address a one-time code field holds. An address is
    /// checked and kept as it is; a bare secret key (spaces and letter case do
    /// not matter) becomes an address with the item's name.
    static func oneTimeCodeUri(from typedText: String, name: String) throws -> String {
        let trimmed = typedText.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return "" }
        if trimmed.lowercased().hasPrefix("otpauth://") {
            guard (try? PasswordsOneTimeCode.parse(uri: trimmed)) != nil else { throw PasswordsDraftError.invalidOneTimeCode }
            return trimmed
        }
        let secret = trimmed.filter { !$0.isWhitespace && $0 != "-" }.uppercased()
        let label = name.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? ""
        let uri = "otpauth://totp/\(label)?secret=\(secret)"
        guard (try? PasswordsOneTimeCode.parse(uri: uri)) != nil else { throw PasswordsDraftError.invalidOneTimeCode }
        return uri
    }
}

/// PasswordsEditorScreen adds a login, card or note, or changes one the
/// person owns.
struct PasswordsEditorScreen: View {
    let model: PasswordsModel
    let editing: PasswordsEditing
    @State private var draft: PasswordsDraft
    /// The draft as it was loaded, to tell what the person edited when the item changed under them.
    @State private var loadedDraft: PasswordsDraft
    /// The revision the changes are based on; moves to the fresh one after a conflict.
    @State private var baseRevision: Int64?
    @State private var isShowingPassword = false
    @State private var isGenerating = false
    @State private var isSaving = false
    /// Said inside the sheet: the toast of the screen behind it is hidden.
    @State private var problemText: String?
    @State private var doneText: String?
    @Environment(\.dismiss) private var dismiss

    init(model: PasswordsModel, editing: PasswordsEditing) {
        self.model = model
        self.editing = editing
        _draft = State(initialValue: PasswordsDraft(item: editing.item))
        _loadedDraft = State(initialValue: PasswordsDraft(item: editing.item))
        _baseRevision = State(initialValue: editing.item?.revision)
    }

    private var editorTitle: String {
        switch (editing.itemKind, editing.item == nil) {
        case (.login, true): String(localized: "New login")
        case (.login, false): String(localized: "Edit login")
        case (.card, true): String(localized: "New card")
        case (.card, false): String(localized: "Edit card")
        case (.note, true): String(localized: "New note")
        case (.note, false): String(localized: "Edit note")
        }
    }

    var body: some View {
        NavigationStack {
            Form {
                switch editing.itemKind {
                case .login: loginFields
                case .card: cardFields
                case .note: noteFields
                }
                Section {
                    TextField("Folder", text: $draft.folder)
                } footer: {
                    Text("Optional. Items in the same folder can be shown together.")
                }
            }
            .navigationTitle(editorTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }.disabled(isSaving)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isSaving {
                        ProgressView()
                    } else {
                        Button("Save") { Task { await save() } }
                            .disabled(draft.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    }
                }
            }
            .toast(done: $doneText, problem: $problemText)
            .sheet(isPresented: $isGenerating) {
                PasswordsGeneratorSheet(model: model) { generated in
                    draft.password = generated
                    isShowingPassword = true
                }
            }
            #if DEBUG
                .onAppear {
                    // `-TeaNodeScreen passwords-generator`: the generator over the editor.
                    if DemoServer.isRequested, DemoServer.screen == "passwords-generator" { isGenerating = true }
                }
            #endif
        }
        .interactiveDismissDisabled(isSaving)
        .passwordsAppSwitcherCover(model: model)
    }

    // MARK: Fields

    @ViewBuilder
    private var loginFields: some View {
        Section {
            TextField("Item name", text: $draft.name)
            TextField("User name", text: $draft.userName)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            passwordField
            Button("Generate a password", systemImage: "wand.and.stars") { isGenerating = true }
                .actionColor()
        }
        Section {
            TextField("One address on each line", text: $draft.sitesText, axis: .vertical)
                .lineLimit(1...5)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        } header: {
            Text("Sites")
        } footer: {
            Text("The sites this login is for. AutoFill and the agent use it only on these sites.")
        }
        Section {
            TextField("One-time code", text: $draft.oneTimeCodeText)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        } header: {
            Text("One-time code")
        } footer: {
            Text("Paste the otpauth address or the secret key the site showed.")
        }
        notesField
    }

    private var passwordField: some View {
        HStack {
            if isShowingPassword {
                TextField("Password", text: $draft.password)
                    .font(.body.monospaced())
            } else {
                SecureField("Password", text: $draft.password)
            }
            Button {
                isShowingPassword.toggle()
            } label: {
                Image(systemName: isShowingPassword ? "eye.slash" : "eye")
                    .frame(minWidth: 44, minHeight: 44)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(isShowingPassword ? Text("Hide password") : Text("Show password"))
        }
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
    }

    @ViewBuilder
    private var cardFields: some View {
        Section {
            TextField("Item name", text: $draft.name)
            TextField("Name on card", text: $draft.cardholderName)
            TextField("Card number", text: $draft.number)
                .keyboardType(.numberPad)
            TextField("Expiry month", text: $draft.expiryMonth)
                .keyboardType(.numberPad)
            TextField("Expiry year", text: $draft.expiryYear)
                .keyboardType(.numberPad)
            SecureField("Security code", text: $draft.securityCode)
                .keyboardType(.numberPad)
            TextField("Card brand", text: $draft.brand)
        } footer: {
            Text("A card is kept and shown here. Nothing fills it into other apps.")
        }
        notesField
    }

    @ViewBuilder
    private var noteFields: some View {
        Section {
            TextField("Item name", text: $draft.name)
        }
        Section {
            TextField("Note", text: $draft.text, axis: .vertical)
                .lineLimit(5...15)
        } header: {
            Text("Note")
        }
    }

    private var notesField: some View {
        Section {
            TextField("Notes", text: $draft.notes, axis: .vertical)
                .lineLimit(2...8)
        } header: {
            Text("Notes")
        }
    }

    // MARK: Saving

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        do {
            let fields = try draft.fields(for: editing.itemKind)
            let saved = try await model.save(fields: fields, id: editing.item?.id, baseRevision: baseRevision)
            model.doneText = String(localized: "\(saved.name) was saved.")
            dismiss()
        } catch PasswordsDraftError.invalidOneTimeCode {
            problemText = String(localized: "That is not a one-time code. Paste the otpauth address or the secret key the site showed.")
        } catch PasswordsError.itemChangedOnAnotherDevice {
            rebaseOntoFreshItem()
        } catch {
            problemText = PasswordsWords.problemText(error, orSay: String(localized: "The item could not be saved."))
        }
    }
}

extension PasswordsEditorScreen {
    /// The item changed on another device while it was open here. The model has
    /// read it again; the draft keeps what the person edited, takes the fresh
    /// revision and everything the person did not touch, and can be saved again.
    fileprivate func rebaseOntoFreshItem() {
        guard let itemId = editing.item?.id, let fresh = model.item(id: itemId), !fresh.isDeleted else {
            problemText = PasswordsWords.problemText(PasswordsError.itemNotFound, orSay: "")
            return
        }
        guard fresh.revision != baseRevision else {
            // The read did not get through, so there is nothing new to base the changes on.
            problemText = String(localized: "This item changed on another device, but it could not be read again. Check your connection and try again.")
            return
        }
        let freshDraft = PasswordsDraft(item: fresh)
        draft = draft.rebased(from: loadedDraft, onto: freshDraft)
        loadedDraft = freshDraft
        baseRevision = fresh.revision
        problemText = String(localized: "This item changed on another device. Your changes are kept. Check them, then save again.")
    }
}

// MARK: The generator

/// PasswordsGeneratorSheet makes a random password with the length and the
/// kinds of character the person chooses.
struct PasswordsGeneratorSheet: View {
    let model: PasswordsModel
    let onUse: (String) -> Void
    @State private var options = PasswordsGeneratorOptions.standard
    @State private var generated = ""
    @Environment(\.dismiss) private var dismiss

    private var canGenerate: Bool { options.kindCount > 0 }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    // Not selectable: the text on screen has zero-width spaces in it, which a selection would copy.
                    // A long press copies the password itself.
                    PasswordsValueText(generated)
                        .font(.body.monospaced())
                        .contextMenu {
                            Button("Copy", systemImage: "doc.on.doc") { PasswordsClipboard.copy(generated) }
                        }
                        .accessibilityLabel(Text("Generated password"))
                        .accessibilityValue(Text(verbatim: generated))
                    Button("Another password", systemImage: "arrow.clockwise") { generate() }
                        .actionColor()
                        .disabled(!canGenerate)
                } header: {
                    Text("Generated password")
                } footer: {
                    if !canGenerate { Text("Choose at least one kind of character.") }
                }
                Section {
                    Stepper("Length: \(options.length)", value: $options.length, in: 8...PasswordsGenerator.maximumLength)
                    Toggle("Letters", isOn: $options.shouldIncludeLetters)
                    Toggle("Digits", isOn: $options.shouldIncludeDigits)
                    Toggle("Symbols", isOn: $options.shouldIncludeSymbols)
                    Toggle("Leave out look-alike characters", isOn: $options.shouldAvoidLookAlikes)
                }
            }
            // A large title, so that it reads in full whatever the text size: an inline one is cut between the two buttons.
            .navigationTitle("Password generator")
            .navigationBarTitleDisplayMode(.large)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Use") {
                        onUse(generated)
                        dismiss()
                    }
                    .disabled(generated.isEmpty || !canGenerate)
                }
            }
            .onChange(of: options) { generate() }
            .onAppear { generate() }
        }
        .presentationDetents([.large])
        .passwordsAppSwitcherCover(model: model)
    }

    private func generate() {
        generated = (try? PasswordsGenerator.generate(options: options)) ?? ""
    }
}
