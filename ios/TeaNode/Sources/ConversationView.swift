import PhotosUI
import SwiftUI
import TeaNodeKit

/// ConversationView is one conversation: its messages, and the composer
/// floating over them.
struct ConversationView: View {
    @State var model: ConversationModel

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 16) {
                ForEach(model.transcript.lines) { line in
                    TranscriptLineView(line: line, model: model)
                        .id(line.id)
                }
                if model.runningRunId != nil, model.transcript.lines.last?.isStreaming != true {
                    ProgressView()
                        .accessibilityLabel("Your agent is working")
                }
            }
            .padding()
        }
        .defaultScrollAnchor(.bottom)
        .defaultScrollAnchor(.bottom, for: .sizeChanges)
        .scrollDismissesKeyboard(.interactively)
        .safeAreaInset(edge: .bottom) {
            Composer(model: model)
        }
        .overlay {
            if !model.isLoaded {
                if let problemText = model.loadProblemText {
                    ContentUnavailableView("Cannot reach your agent", systemImage: "wifi.exclamationmark", description: Text(problemText))
                } else {
                    ProgressView()
                }
            } else if model.transcript.lines.isEmpty {
                ContentUnavailableView("Say something", systemImage: "bubble.left.and.text.bubble.right", description: Text("Ask your agent anything: your mail, your calendar, a picture of a receipt."))
            }
        }
        .navigationTitle(model.title)
        .navigationBarTitleDisplayMode(.inline)
        .task { await model.follow() }
        .sensoryFeedback(.success, trigger: model.sentCount)
        .sensoryFeedback(.warning, trigger: model.sendProblemText) { _, new in new != nil }
        .alert(
            "Something went wrong",
            isPresented: $model.isShowingSendProblem,
            presenting: model.sendProblemText
        ) { _ in
            Button("OK", role: .cancel) {}
        } message: { text in
            Text(text)
        }
    }
}

/// TranscriptLineView is one line: the person's bubble, the agent's answer as
/// plain text, a tool on one quiet line, or a card waiting for the person.
struct TranscriptLineView: View {
    let line: TranscriptLine
    let model: ConversationModel

    var body: some View {
        switch line.kind {
        case .person:
            PersonBubble(line: line)
        case .answer:
            Text(Self.markdown(line.text))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        case .tool:
            ToolLine(line: line)
        case .checkIn:
            Label(Self.withoutMarker(line.text), systemImage: "bell")
                .font(.footnote)
                .foregroundStyle(.secondary)
        case .note:
            Text(line.text)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity)
                .multilineTextAlignment(.center)
        case .approval, .question:
            CardView(line: line, model: model)
        }
    }

    /// An answer's Markdown, with its inline styles and its line breaks.
    static func markdown(_ text: String) -> AttributedString {
        (try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)))
            ?? AttributedString(text)
    }

    /// A check-in's text without the marker that says where it came from.
    static func withoutMarker(_ text: String) -> String {
        guard let origin = CheckInOrigin.of(text) else { return text }
        return text.dropFirst(origin.rawValue.count).trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

/// PersonBubble is what the person said: on the right, in the accent color.
struct PersonBubble: View {
    let line: TranscriptLine

    var body: some View {
        HStack {
            Spacer(minLength: 48)
            VStack(alignment: .trailing, spacing: 4) {
                ForEach(line.attachments, id: \.id) { attachment in
                    Label(attachment.name, systemImage: "photo")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                if !line.text.isEmpty {
                    Text(line.text)
                        .textSelection(.enabled)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 10)
                        .foregroundStyle(.white)
                        .background(Color.accentColor, in: .rect(cornerRadius: 20, style: .continuous))
                }
            }
        }
    }
}

/// ToolLine is a tool the agent called, one line until tapped.
struct ToolLine: View {
    let line: TranscriptLine
    @State private var isExpanded = false

    var body: some View {
        DisclosureGroup(isExpanded: $isExpanded) {
            Text(line.text.isEmpty ? String(localized: "Nothing came back yet.") : String(line.text.prefix(4000)))
                .font(.footnote.monospaced())
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        } label: {
            Label {
                Text(line.detail)
            } icon: {
                if line.isDone {
                    Image(systemName: "checkmark.circle")
                } else {
                    ProgressView().controlSize(.mini)
                }
            }
            .font(.footnote)
            .foregroundStyle(.secondary)
        }
        .tint(.secondary)
    }
}

/// CardView is something waiting for the person: a tool call to approve, or
/// the agent's question.
struct CardView: View {
    let line: TranscriptLine
    let model: ConversationModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if line.kind == .approval {
                Label(line.detail.isEmpty ? String(localized: "Approve?") : line.detail, systemImage: "hand.raised")
                    .font(.headline)
            } else {
                Label("Your agent asks", systemImage: "questionmark.bubble")
                    .font(.headline)
            }
            Text(TranscriptLineView.markdown(line.text))
                .textSelection(.enabled)
            if line.isDone {
                Label(resolutionText, systemImage: "checkmark.circle")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            } else if line.kind == .approval {
                HStack {
                    Button("Do it") {
                        Task { await model.approve(line, isApproved: true) }
                    }
                    .buttonStyle(.glassProminent)
                    Button("Don’t", role: .destructive) {
                        Task { await model.approve(line, isApproved: false) }
                    }
                    .buttonStyle(.glass)
                }
            } else {
                ForEach(line.choices, id: \.self) { choice in
                    Button(choice) {
                        Task { await model.answer(line, with: choice) }
                    }
                    .buttonStyle(.glass)
                }
                Text("Or type your answer below.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 16, style: .continuous))
    }

    private var resolutionText: String {
        switch line.resolution {
        case "approved": String(localized: "Approved")
        case "declined": String(localized: "Declined")
        default: line.resolution
        }
    }
}

/// Composer is where the person writes: a glass field floating over the
/// conversation, with pictures to add and a button to send, or to stop the
/// turn that is running.
struct Composer: View {
    @Bindable var model: ConversationModel
    @State private var pickedItems: [PhotosPickerItem] = []
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(spacing: 8) {
            if !model.pictures.isEmpty {
                ScrollView(.horizontal) {
                    HStack {
                        ForEach(model.pictures) { picture in
                            Image(uiImage: picture.preview)
                                .resizable()
                                .scaledToFill()
                                .frame(width: 56, height: 56)
                                .clipShape(.rect(cornerRadius: 10, style: .continuous))
                                .overlay(alignment: .topTrailing) {
                                    Button {
                                        model.pictures.removeAll { $0.id == picture.id }
                                    } label: {
                                        Image(systemName: "xmark.circle.fill")
                                            .symbolRenderingMode(.hierarchical)
                                    }
                                    .accessibilityLabel("Remove picture")
                                }
                        }
                    }
                }
                .scrollIndicators(.hidden)
            }
            GlassEffectContainer {
                HStack(alignment: .bottom, spacing: 8) {
                    PhotosPicker(selection: $pickedItems, maxSelectionCount: 4, matching: .images) {
                        Image(systemName: "plus")
                            .font(.title3)
                            .frame(width: 44, height: 44)
                    }
                    .glassEffect(.regular.interactive(), in: .circle)
                    .accessibilityLabel("Add pictures")

                    TextField(placeholder, text: $model.draft, axis: .vertical)
                        .lineLimit(1...6)
                        .focused($isFocused)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                        .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 22, style: .continuous))

                    if model.runningRunId != nil, model.draft.isEmpty, model.pictures.isEmpty {
                        Button {
                            Task { await model.stop() }
                        } label: {
                            Image(systemName: "stop.fill")
                                .font(.title3)
                                .frame(width: 44, height: 44)
                        }
                        .glassEffect(.regular.interactive(), in: .circle)
                        .accessibilityLabel("Stop")
                    } else {
                        Button {
                            Task { await model.send() }
                        } label: {
                            Image(systemName: "arrow.up")
                                .font(.title3.bold())
                                .foregroundStyle(.white)
                                .frame(width: 44, height: 44)
                        }
                        .glassEffect(.regular.tint(.accentColor).interactive(), in: .circle)
                        .disabled(!model.canSend)
                        .opacity(model.canSend ? 1 : 0.5)
                        .accessibilityLabel("Send")
                    }
                }
            }
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
        .onChange(of: pickedItems) { _, items in
            guard !items.isEmpty else { return }
            pickedItems = []
            Task { await add(items) }
        }
    }

    private var placeholder: String {
        model.transcript.openQuestion == nil ? String(localized: "Ask your agent…") : String(localized: "Your answer")
    }

    private func add(_ items: [PhotosPickerItem]) async {
        for item in items {
            guard let data = try? await item.loadTransferable(type: Data.self),
                let picture = PickedPicture(data: data, index: model.pictures.count)
            else { continue }
            model.pictures.append(picture)
        }
    }
}
