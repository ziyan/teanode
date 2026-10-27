import SwiftUI
import TeaNodeKit

// The two kinds of text a day, a week, a month and a year all have: the
// person's own words, and the story TeaNode wrote. Each is the rows of a
// section, so that the screens give them their own headings.

/// JournalWordsRows is the person's words: the touched-up version or the
/// original, with the choice between them when touching up is on, and the
/// notes from their notes folder.
struct JournalWordsRows: View {
    let words: JournalWords?
    /// Whether the original is shown in place of the touched-up version.
    @Binding var isShowingOriginal: Bool

    private var hasNotes: Bool { !(words?.journalNotes ?? []).isEmpty }

    var body: some View {
        if let words, words.isTouchUpOn {
            Picker("Version", selection: $isShowingOriginal) {
                Text("Touched up").tag(false)
                Text("Your original").tag(true)
            }
            .pickerStyle(.segmented)
        }
        if let words, words.touchUpPending {
            HStack(spacing: 8) {
                ProgressView()
                Text("Touching up your words…").foregroundStyle(.secondary)
            }
        }
        if let words, words.hasOriginal {
            wordsText(words)
        }
        ForEach(words?.journalNotes ?? []) { note in
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: note.title).font(.subheadline.weight(.semibold))
                Text(verbatim: note.text).fontDesign(.serif)
                Text("From your notes").font(.footnote).foregroundStyle(.secondary)
            }
        }
        if !(words?.hasOriginal ?? false), !hasNotes {
            Text("Nothing written yet").foregroundStyle(.secondary)
        }
    }

    private func wordsText(_ words: JournalWords) -> some View {
        let isTouchedUpShown = words.isTouchedUpShown && !isShowingOriginal
        return VStack(alignment: .leading, spacing: 6) {
            if isTouchedUpShown {
                Text("Touched up by TeaNode from your original")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Text(verbatim: isTouchedUpShown ? words.touchedUp : words.original)
                .fontDesign(.serif)
                .textSelection(.enabled)
            if isTouchedUpShown, words.isTouchedUpStale, !words.touchUpPending {
                Text("You changed your original after this was touched up.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
    }
}

/// JournalNarrativeRows is what TeaNode wrote: the story with the records it
/// mentions, when it was written and from how much, what the person said was
/// wrong with it (each to take back), and the buttons to say something is
/// wrong, to have it written again or to remove it.
struct JournalNarrativeRows: View {
    let narrative: JournalNarrative?
    /// What to say when there is no story and none is being written.
    let emptyText: String
    /// Whether the story can be asked for again. Today's can: it is the day
    /// so far.
    let canWrite: Bool
    /// What the button says when there is no story yet.
    var writeNowTitle = String(localized: "Write now")
    let isBusy: Bool
    let open: (JournalRecord) -> Void
    let correct: () -> Void
    let writeAgain: () -> Void
    let removeStory: () -> Void
    let removeCorrection: (Int) -> Void
    @State private var isConfirmingRemoval = false

    private var hasNarrative: Bool { narrative?.hasNarrative ?? false }
    private var isWriting: Bool { narrative?.narrativePending ?? false }

    var body: some View {
        if isWriting {
            HStack(spacing: 8) {
                ProgressView()
                Text("Writing…").foregroundStyle(.secondary)
            }
        }
        if let narrative, hasNarrative {
            JournalNarrativeView(narrative: narrative, open: open)
            if let writtenText = writtenText(narrative) {
                Text(verbatim: writtenText)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } else if !isWriting {
            Text(verbatim: emptyText).foregroundStyle(.secondary)
        }
        if let failure = narrative?.failure, !failure.isEmpty, !hasNarrative, !isWriting {
            Text(verbatim: failure.asReasonSentence).font(.footnote).foregroundStyle(.secondary)
        }
        correctionRows
        if hasNarrative {
            Button("Something wrong?", systemImage: "exclamationmark.bubble", action: correct)
                .actionColor()
        }
        if canWrite, hasNarrative || !isWriting {
            Button(hasNarrative ? String(localized: "Write again") : writeNowTitle, systemImage: "arrow.clockwise", action: writeAgain)
                .actionColor()
                .disabled(isBusy || isWriting)
        }
        if hasNarrative {
            Button(role: .destructive) {
                isConfirmingRemoval = true
            } label: {
                Label("Remove this story", systemImage: "trash")
            }
            .tint(.red)
            .actionColor(isDestructive: true)
            .disabled(isBusy || isWriting)
            .confirmationDialog("Remove this story?", isPresented: $isConfirmingRemoval, titleVisibility: .visible) {
                Button("Remove", role: .destructive, action: removeStory)
            } message: {
                Text("TeaNode writes a new one in a later dream. Your corrections are kept.")
            }
        }
    }

    private var correctionRows: some View {
        ForEach(Array((narrative?.corrections ?? []).enumerated()), id: \.offset) { index, correction in
            Text("You said: \(correction.correctionText)")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .swipeActions {
                    Button(role: .destructive) {
                        removeCorrection(index)
                    } label: {
                        Label("Delete", systemImage: "trash")
                    }
                    .tint(.red)
                }
                .contextMenu {
                    Button(role: .destructive) {
                        removeCorrection(index)
                    } label: {
                        Label("Delete", systemImage: "trash")
                    }
                    .tint(.red)
                }
        }
    }

    /// "Written Oct 23, 3:12 AM from 71 things".
    private func writtenText(_ narrative: JournalNarrative) -> String? {
        guard let writtenAt = ServerTime.date(narrative.writtenAt) else { return nil }
        let when = writtenAt.formatted(date: .abbreviated, time: .shortened)
        return String(localized: "Written \(when) from \(narrative.sourceCount) things")
    }
}
