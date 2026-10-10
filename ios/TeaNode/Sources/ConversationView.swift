import PhotosUI
import SwiftUI
import TeaNodeKit

/// ConversationView is one conversation, laid out like a messaging app: the
/// person's messages on the right, the agent's on the left, and the composer
/// floating over them.
struct ConversationView: View {
    @State var model: ConversationModel

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 10) {
                ForEach(ConversationItem.items(of: model.transcript.lines)) { item in
                    ConversationItemView(item: item, model: model)
                        .id(item.id)
                }
                if model.runningRunId != nil, model.transcript.lines.last?.isStreaming != true {
                    TypingBubble()
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 12)
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
        .alert("Something went wrong", isPresented: $model.isShowingSendProblem, presenting: model.sendProblemText) { _ in
            Button("OK", role: .cancel) {}
        } message: { text in
            Text(text)
        }
    }
}

/// ConversationItem is one thing on the screen: a line of the transcript,
/// the tools the agent used in a row, or the time between two lines far
/// apart.
enum ConversationItem: Identifiable {
    case line(TranscriptLine)
    case tools([TranscriptLine])
    case time(Date)

    var id: String {
        switch self {
        case .line(let line): line.id
        case .tools(let lines): "tools-" + (lines.first?.id ?? "")
        case .time(let date): "time-\(date.timeIntervalSince1970)"
        }
    }

    /// How far apart two lines are before the time is shown between them.
    static let timeGap: TimeInterval = 30 * 60

    static func items(of lines: [TranscriptLine]) -> [ConversationItem] {
        var items: [ConversationItem] = []
        var tools: [TranscriptLine] = []
        var lastDate: Date?
        for line in lines {
            if let date = ServerTime.date(line.at) {
                if lastDate.map({ date.timeIntervalSince($0) > timeGap }) ?? true {
                    if !tools.isEmpty {
                        items.append(.tools(tools))
                        tools = []
                    }
                    items.append(.time(date))
                }
                lastDate = date
            }
            if line.kind == .tool {
                tools.append(line)
                continue
            }
            if !tools.isEmpty {
                items.append(.tools(tools))
                tools = []
            }
            items.append(.line(line))
        }
        if !tools.isEmpty {
            items.append(.tools(tools))
        }
        return items
    }
}

struct ConversationItemView: View {
    let item: ConversationItem
    let model: ConversationModel

    var body: some View {
        switch item {
        case .time(let date):
            Text(Self.timeText(date))
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity)
                .padding(.top, 6)
        case .tools(let lines):
            ToolsLine(lines: lines)
        case .line(let line):
            switch line.kind {
            case .person:
                PersonBubble(line: line)
            case .answer:
                AgentBubble {
                    MarkdownView(text: line.text)
                }
            case .checkIn:
                CheckInLine(line: line)
            case .note:
                Text(line.text)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: .infinity)
            case .approval, .question:
                AgentBubble {
                    CardView(line: line, model: model)
                }
            case .codingQuestion:
                AgentBubble {
                    CodingQuestionCard(line: line, model: model)
                }
            case .tool:
                EmptyView()
            }
        }
    }

    static func timeText(_ date: Date) -> String {
        if Calendar.current.isDateInToday(date) {
            return date.formatted(date: .omitted, time: .shortened)
        }
        return date.formatted(date: .abbreviated, time: .shortened)
    }
}

/// PersonBubble is what the person said: on the right, in the accent color.
struct PersonBubble: View {
    let line: TranscriptLine

    var body: some View {
        HStack {
            Spacer(minLength: 56)
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
                        .padding(.vertical, 9)
                        .foregroundStyle(.white)
                        .background(Color.accentColor, in: .rect(cornerRadius: 20, style: .continuous))
                }
            }
        }
    }
}

/// AgentBubble is something from the agent: on the left, in gray.
struct AgentBubble<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        HStack {
            content
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 20, style: .continuous))
            Spacer(minLength: 40)
        }
    }
}

/// TypingBubble says the agent is working on an answer.
struct TypingBubble: View {
    var body: some View {
        AgentBubble {
            Image(systemName: "ellipsis")
                .font(.title3)
                .foregroundStyle(.secondary)
                .symbolEffect(.variableColor.iterative, options: .repeating)
                .frame(height: 20)
        }
        .accessibilityLabel("Your agent is working")
    }
}

/// MarkdownView draws an answer's Markdown: its blocks, each with its
/// inline styles. Headings are no larger than a headline.
struct MarkdownView: View {
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(MarkdownBlock.parse(text).enumerated()), id: \.offset) { _, block in
                switch block {
                case .paragraph(let text):
                    Text(Self.inline(text))
                case .heading(let text):
                    Text(Self.inline(text)).font(.headline)
                case .quote(let text):
                    HStack(spacing: 8) {
                        Capsule().fill(.tertiary).frame(width: 3)
                        Text(Self.inline(text)).foregroundStyle(.secondary)
                    }
                    .fixedSize(horizontal: false, vertical: true)
                case .list(let items):
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                            HStack(alignment: .firstTextBaseline, spacing: 6) {
                                Text(item.marker).monospacedDigit().foregroundStyle(.secondary)
                                Text(Self.inline(item.text))
                            }
                        }
                    }
                case .code(let text):
                    Text(text)
                        .font(.callout.monospaced())
                        .padding(8)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 8, style: .continuous))
                }
            }
        }
        .textSelection(.enabled)
    }

    static func inline(_ text: String) -> AttributedString {
        (try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)))
            ?? AttributedString(text)
    }
}

/// ToolsLine is the tools the agent used in a row, one quiet line until
/// tapped.
struct ToolsLine: View {
    let lines: [TranscriptLine]
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation { isExpanded.toggle() }
            } label: {
                HStack(spacing: 6) {
                    if lines.allSatisfy(\.isDone) {
                        Image(systemName: "checkmark.circle")
                    } else {
                        ProgressView().controlSize(.mini)
                    }
                    Text("Used \(Self.names(of: lines))")
                        .lineLimit(1)
                    Image(systemName: "chevron.right")
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            if isExpanded {
                ForEach(lines) { line in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(line.detail).font(.caption.bold()).foregroundStyle(.secondary)
                        Text(line.text.isEmpty ? String(localized: "Nothing came back yet.") : String(line.text.prefix(2000)))
                            .font(.caption.monospaced())
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                    .padding(8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 10, style: .continuous))
                }
            }
        }
        .padding(.leading, 4)
    }

    /// The tools' names, each once, in the order first used.
    static func names(of lines: [TranscriptLine]) -> String {
        var seen: [String] = []
        for line in lines where !seen.contains(line.detail) {
            seen.append(line.detail)
        }
        return seen.joined(separator: ", ")
    }
}

/// CheckInLine is a turn the agent took on its own, shown as where it came
/// from: what started it is the agent's business, and its answer follows.
struct CheckInLine: View {
    let line: TranscriptLine

    var body: some View {
        Label(Self.label(of: line.detail), systemImage: Self.symbol(of: line.detail))
            .font(.caption)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity)
            .padding(.top, 4)
    }

    static func label(of marker: String) -> String {
        switch CheckInOrigin(rawValue: marker) {
        case .goal: String(localized: "Goal check-in")
        case .goalNeedsYou: String(localized: "A goal needs you")
        case .background: String(localized: "Background command ended")
        case .backgroundWork: String(localized: "Background work finished")
        case .schedule: String(localized: "Scheduled turn")
        case .speakFirst: String(localized: "Started by the agent")
        case .alert: String(localized: "Alert from your mail")
        case .herdrQuestion: String(localized: "A coding session asks you")
        case .herdrSession: String(localized: "A coding session finished")
        case .approved: String(localized: "Approved after the turn ended")
        case .declined: String(localized: "Declined after the turn ended")
        case nil: marker
        }
    }

    static func symbol(of marker: String) -> String {
        switch CheckInOrigin(rawValue: marker) {
        case .goal, .goalNeedsYou: "target"
        case .background, .herdrSession, .herdrQuestion: "terminal"
        case .backgroundWork: "list.bullet"
        case .schedule: "calendar"
        case .speakFirst: "sparkles"
        case .alert: "bell"
        case .approved: "checkmark"
        case .declined: "xmark"
        case nil: "bell"
        }
    }
}

/// CardView is something waiting for the person: a tool call to approve, or
/// the agent's question.
struct CardView: View {
    let line: TranscriptLine
    let model: ConversationModel

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if line.kind == .approval {
                Label(line.detail.isEmpty ? String(localized: "Approve?") : line.detail, systemImage: "hand.raised")
                    .font(.headline)
            } else {
                Label("Your agent asks", systemImage: "questionmark.bubble")
                    .font(.headline)
            }
            MarkdownView(text: line.text)
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
    }

    private var resolutionText: String {
        switch line.resolution {
        case "approved": String(localized: "Approved")
        case "declined": String(localized: "Declined")
        default: line.resolution
        }
    }
}

/// CodingQuestionCard is a question one of the person's coding sessions
/// asked, with its options as buttons: one press answers, or with several
/// allowed, the chosen ones are sent together.
struct CodingQuestionCard: View {
    let line: TranscriptLine
    let model: ConversationModel
    @State private var chosen: Set<Int> = []

    var body: some View {
        if let question = line.codingQuestion {
            VStack(alignment: .leading, spacing: 10) {
                Label {
                    Text(verbatim: question.sessionName.isEmpty ? question.codingAgentName : "\(question.codingAgentName) · \(question.sessionName)")
                } icon: {
                    Image(systemName: "terminal")
                }
                .font(.subheadline.bold())
                .foregroundStyle(.secondary)
                MarkdownView(text: question.questionText)
                if line.isDone {
                    Label(line.resolution.isEmpty ? String(localized: "Answered") : line.resolution, systemImage: "checkmark.circle")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(question.choices, id: \.optionNumber) { option in
                        Button {
                            if question.isMultipleChoice {
                                if chosen.contains(option.optionNumber) {
                                    chosen.remove(option.optionNumber)
                                } else {
                                    chosen.insert(option.optionNumber)
                                }
                            } else {
                                Task { await model.answerCodingQuestion(line, options: [option], freeText: nil) }
                            }
                        } label: {
                            HStack {
                                if question.isMultipleChoice {
                                    Image(systemName: chosen.contains(option.optionNumber) ? "checkmark.circle.fill" : "circle")
                                }
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(option.optionLabel)
                                    if !option.optionDescription.isEmpty {
                                        Text(option.optionDescription).font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                                Spacer(minLength: 0)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .buttonStyle(.glass)
                    }
                    if question.isMultipleChoice {
                        Button("Send") {
                            let options = question.choices.filter { chosen.contains($0.optionNumber) }
                            Task { await model.answerCodingQuestion(line, options: options, freeText: nil) }
                        }
                        .buttonStyle(.glassProminent)
                        .disabled(chosen.isEmpty)
                    }
                    if question.options.contains(where: { $0.herdrOptionKind != "choice" }) {
                        Text("Or type your answer below.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
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
                            .frame(minWidth: 44, minHeight: 44)
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
                                .frame(minWidth: 44, minHeight: 44)
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
                                .frame(minWidth: 44, minHeight: 44)
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
