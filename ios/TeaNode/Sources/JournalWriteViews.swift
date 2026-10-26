import SwiftUI
import TeaNodeKit

// Writing in the journal: the person's own words on a day, or their looking
// back on a week, month or year, and telling TeaNode what its story got wrong.

/// JournalWriteRequest is a write sheet to show: whose words, as they were
/// last read.
struct JournalWriteRequest: Identifiable {
    let id = UUID()
    let target: JournalTarget
    let words: JournalWords?
}

/// JournalCorrectionRequest is a correction sheet to show.
struct JournalCorrectionRequest: Identifiable {
    let id = UUID()
    let target: JournalTarget
}

/// JournalWriteSheet is where the person writes the original of a day's words
/// or a period's looking back, asks TeaNode to touch it up, and saves.
/// Saving sends the time the words were read at, so that a day saved
/// somewhere else in between is refused rather than overwritten.
struct JournalWriteSheet: View {
    let connection: Connection
    let request: JournalWriteRequest
    /// Called after anything was saved or changed, to read the screen again.
    let didChange: () async -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var original: String
    /// The original as last saved, and the time it was saved at, which a
    /// save sends so that a day changed somewhere else is not overwritten.
    @State private var savedOriginal: String
    @State private var baseModifiedAt: String?
    @State private var how: String
    @State private var isTouchUpOn: Bool
    @State private var isWorking = false
    @State private var isConfirmingDiscard = false
    @State private var problemText: String?

    init(connection: Connection, request: JournalWriteRequest, didChange: @escaping () async -> Void) {
        self.connection = connection
        self.request = request
        self.didChange = didChange
        _original = State(initialValue: request.words?.original ?? "")
        _savedOriginal = State(initialValue: request.words?.original ?? "")
        _baseModifiedAt = State(initialValue: request.words?.modifiedAt)
        _how = State(initialValue: request.words?.touchUpHow ?? "")
        _isTouchUpOn = State(initialValue: request.words?.isTouchUpOn ?? false)
    }

    private var hasChanges: Bool { original != savedOriginal }
    private var isEmpty: Bool { original.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    private var isForDay: Bool {
        if case .day = request.target { return true }
        return false
    }

    private var titleText: String {
        isForDay ? String(localized: "Your words") : String(localized: "Looking back")
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextEditor(text: $original)
                        .fontDesign(.serif)
                        .frame(minHeight: 220)
                        .disabled(isWorking)
                        .accessibilityLabel(titleText)
                } header: {
                    Text("Your original")
                } footer: {
                    Text(footerText)
                }
                touchUpSection
            }
            .navigationTitle(titleText)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        if hasChanges { isConfirmingDiscard = true } else { dismiss() }
                    }
                    .disabled(isWorking)
                    .confirmationDialog("Discard what you wrote?", isPresented: $isConfirmingDiscard, titleVisibility: .visible) {
                        Button("Discard", role: .destructive) { dismiss() }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isWorking {
                        ProgressView()
                    } else {
                        Button("Save") { Task { await save() } }
                            .disabled(!hasChanges)
                    }
                }
            }
            .toast(done: .constant(nil), problem: $problemText)
        }
        .interactiveDismissDisabled(hasChanges || isWorking)
    }

    private var footerText: LocalizedStringKey {
        isForDay
            ? "What you want to remember, in your own voice. TeaNode never changes these words."
            : "Your own words about this stretch of days, if you want them."
    }

    private var touchUpSection: some View {
        Section {
            TextField("How? For example: shorter, or fix the grammar", text: $how)
                .disabled(isWorking)
            Button("Touch up", systemImage: "wand.and.stars") {
                Task { await touchUp() }
            }
            .actionColor()
            .disabled(isWorking || isEmpty)
            if isTouchUpOn {
                Button("Stop touching up", systemImage: "xmark.circle") {
                    Task { await stopTouchUp() }
                }
                .actionColor()
                .disabled(isWorking)
            }
        } header: {
            Text("Touched up")
        } footer: {
            Text("TeaNode makes a version of your original that reads better, and shows it first. Your original is kept, and it is the only text you edit.")
        }
    }

    // MARK: Changes

    /// Saves the original if it changed, then closes.
    private func save() async {
        guard await saveOriginal() else { return }
        await didChange()
        dismiss()
    }

    /// Saves the original and asks for it to be touched up, then closes: the
    /// touched-up version arrives while the day is on screen.
    private func touchUp() async {
        guard await saveOriginal() else { return }
        isWorking = true
        defer { isWorking = false }
        let trimmedHow = how.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            try await connection.touchUpJournalWords(for: request.target, how: trimmedHow.isEmpty ? nil : trimmedHow)
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not ask for your words to be touched up.")).asReasonSentence
            await didChange()
            return
        }
        await didChange()
        dismiss()
    }

    private func stopTouchUp() async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await connection.stopJournalTouchUp(for: request.target)
            isTouchUpOn = false
            await didChange()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not stop touching up.")).asReasonSentence
        }
    }

    /// Saves the original when it differs from the one that was read; whether
    /// it is saved now (or needed no saving).
    private func saveOriginal() async -> Bool {
        guard hasChanges else { return true }
        isWorking = true
        defer { isWorking = false }
        do {
            let savedAt = try await connection.setJournalWords(for: request.target, words: original, baseModifiedAt: baseModifiedAt)
            savedOriginal = original
            if let savedAt { baseModifiedAt = savedAt }
            return true
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not save your words.")).asReasonSentence
            return false
        }
    }
}

/// JournalCorrectionSheet takes what the person says a story got wrong. It is
/// kept with the day or the period and given to every later writing of the
/// story, which is written again at once.
struct JournalCorrectionSheet: View {
    let connection: Connection
    let request: JournalCorrectionRequest
    let didChange: () async -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var correctionText = ""
    @State private var isWorking = false
    @State private var problemText: String?

    /// The longest a correction can be, as the server takes it.
    private static let characterLimit = 500

    private var isSendable: Bool {
        let trimmed = correctionText.trimmingCharacters(in: .whitespacesAndNewlines)
        return !trimmed.isEmpty && trimmed.count <= Self.characterLimit
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextEditor(text: $correctionText)
                        .frame(minHeight: 140)
                        .disabled(isWorking)
                        .accessibilityLabel("What is wrong")
                } header: {
                    Text("What is wrong?")
                } footer: {
                    Text("For example: it was Sam at lunch, not a colleague. TeaNode writes the story again and keeps to what you say. Your own words are not changed.")
                }
            }
            .navigationTitle("Something wrong?")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }.disabled(isWorking)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isWorking {
                        ProgressView()
                    } else {
                        Button("Send") { Task { await send() } }
                            .disabled(!isSendable)
                    }
                }
            }
            .toast(done: .constant(nil), problem: $problemText)
        }
        .interactiveDismissDisabled(isWorking)
    }

    private func send() async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await connection.correctJournalNarrative(
                for: request.target, correctionText: correctionText.trimmingCharacters(in: .whitespacesAndNewlines))
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not send your correction.")).asReasonSentence
            return
        }
        await didChange()
        dismiss()
    }
}
