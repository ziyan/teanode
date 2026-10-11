import PhotosUI
import QuickLook
import SwiftUI
import TeaNodeKit

/// The switches that choose how much of a conversation shows, kept on the
/// phone as the dashboard keeps them in the browser.
enum TranscriptPreference {
    static let showToolCalls = "showToolCalls"
    static let showUsage = "showUsage"
    static let showWorkingNotes = "showWorkingNotes"

    /// Notes about how the agent works rather than what it did, shown only
    /// with the working notes.
    static let workingNoteKinds: Set<String> = ["depth", "call_unreadable"]
}

/// ConversationView is one conversation, laid out like a messaging app: the
/// person's messages on the right, the agent's on the left, and the composer
/// floating over them.
struct ConversationView: View {
    @State var model: ConversationModel
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase
    @AppStorage(TranscriptPreference.showToolCalls) private var isShowingToolCalls = true
    @AppStorage(TranscriptPreference.showUsage) private var isShowingUsage = false
    @AppStorage(TranscriptPreference.showWorkingNotes) private var isShowingWorkingNotes = false
    @State private var isAtEnd = true
    /// Where the transcript is scrolled: jumping to its bottom edge works
    /// however far up it is, where scrolling to the last line needs that
    /// line built, which a long lazy list has not done.
    @State private var scrollPosition = ScrollPosition(edge: .bottom)
    @State private var status = StatusModel()
    @State private var isShowingStatus = false

    private static let endId = "end"

    var body: some View {
        framed
            .modifier(ConversationFollowing(model: model, status: status))
            .modifier(ConversationAlerts(model: model))
    }

    /// The conversation with its bars, title and status.
    private var framed: some View {
        transcript
            .safeAreaInset(edge: .top, spacing: 0) {
                ConversationBanners(model: model)
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                bottomBar
            }
            .overlay { loading }
            .navigationTitle(model.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    StatusButton(status: status) { isShowingStatus = true }
                }
            }
            .sheet(isPresented: $isShowingStatus) {
                if let connection = app.connection {
                    StatusSheet(status: status, connection: connection)
                }
            }
    }

    /// The messages, scrolling, with the jump to the end.
    private var transcript: some View {
        ScrollViewReader { scroller in
            ScrollView {
                messages(scroller)
                    .padding(.horizontal)
                    .padding(.vertical, 12)
            }
            .scrollPosition($scrollPosition)
            .defaultScrollAnchor(.bottom)
            .defaultScrollAnchor(.bottom, for: .sizeChanges)
            .scrollDismissesKeyboard(.immediately)
            .overlay(alignment: .bottomTrailing) {
                if !isAtEnd {
                    Button {
                        withAnimation { scrollPosition.scrollTo(edge: .bottom) }
                    } label: {
                        Image(systemName: "arrow.down")
                            .font(.headline)
                            .frame(minWidth: 44, minHeight: 44)
                    }
                    .glassEffect(.regular.interactive(), in: .circle)
                    .accessibilityLabel("Jump to the end")
                    .padding()
                }
            }
            .onChange(of: model.sentCount) {
                withAnimation { scrollPosition.scrollTo(edge: .bottom) }
            }
        }
    }

    private func messages(_ scroller: ScrollViewProxy) -> some View {
        LazyVStack(alignment: .leading, spacing: 10) {
            if model.earlierCount > 0 {
                // Reaching the top reads the page before it, and the
                // line that was first stays where it was.
                Button {
                    loadEarlier(scroller)
                } label: {
                    if model.isLoadingEarlier {
                        ProgressView()
                    } else {
                        Text("\(model.earlierCount) earlier messages")
                    }
                }
                .buttonStyle(.glass)
                .frame(maxWidth: .infinity)
                .onAppear { loadEarlier(scroller) }
            }
            ForEach(ConversationItem.items(of: shownLines)) { item in
                ConversationItemView(
                    item: item, model: model, isShowingUsage: isShowingUsage,
                    isShowingToolCalls: isShowingToolCalls || model.conversation?.jobKind == "mcp")
                    .id(item.id)
            }
            if model.runningRunId != nil, model.transcript.lines.last?.isStreaming != true {
                TypingBubble()
            }
            if model.runningRunId == nil, !model.transcript.offeredSuggestions.isEmpty {
                SuggestionsRow(suggestions: model.transcript.offeredSuggestions) { suggestion in
                    Task { await model.sendSuggestion(suggestion) }
                }
            }
            if model.isLoaded, model.transcript.lines.isEmpty {
                EmptyConversation(model: model)
            }
            // The end of the conversation: whether it is on screen
            // decides whether the jump to the end is offered.
            Color.clear.frame(height: 1).id(Self.endId)
                .onScrollVisibilityChange(threshold: 0.01) { isVisible in
                    isAtEnd = isVisible
                }
        }
    }

    /// The agent's steps, the message replied to, and the composer.
    private var bottomBar: some View {
        VStack(spacing: 8) {
            if !model.todos.isEmpty, model.runningRunId != nil || model.todos.contains(where: { !$0.isDone }) {
                TodoPanel(todos: model.todos)
            }
            if let reply = model.replyingTo {
                if let subject = reply.subject {
                    ReferenceBar(subject: subject, from: reply.from ?? "") { model.replyingTo = nil }
                } else {
                    ReplyBar(reply: reply) { model.replyingTo = nil }
                }
            }
            if let call = model.call {
                CallBar(call: call)
            } else {
                Composer(model: model)
            }
        }
    }

    @ViewBuilder
    private var loading: some View {
        if !model.isLoaded {
            if let problemText = model.loadProblemText {
                ContentUnavailableView("Cannot reach your agent", systemImage: "wifi.exclamationmark", description: Text(problemText))
            } else {
                ProgressView()
            }
        }
    }

    private func loadEarlier(_ scroller: ScrollViewProxy) {
        guard model.isLoaded, !model.isLoadingEarlier else { return }
        let firstId = ConversationItem.items(of: shownLines).first?.id
        Task {
            await model.loadEarlier()
            if let firstId {
                scroller.scrollTo(firstId, anchor: .top)
            }
        }
    }

    /// The lines the switches leave on screen.
    private var shownLines: [TranscriptLine] {
        let isMCPRecord = model.conversation?.jobKind == "mcp"
        return model.transcript.lines.filter { line in
            switch line.kind {
            case .tool:
                return isShowingToolCalls || isMCPRecord || !AgentFile.of(toolResult: line.text).isEmpty
            case .checkIn:
                return isShowingWorkingNotes || line.detail == CheckInOrigin.goalNeedsYou.rawValue
            case .note:
                return isShowingWorkingNotes || !TranscriptPreference.workingNoteKinds.contains(line.detail)
            default:
                return true
            }
        }
    }
}

/// ConversationFollowing keeps a conversation and its head up to date while
/// the app is in front: iOS closes the feed in the background, and coming
/// back opens it again at once.
struct ConversationFollowing: ViewModifier {
    let model: ConversationModel
    let status: StatusModel
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content
            .task(id: scenePhase == .active) {
                if scenePhase == .active, let connection = app.connection {
                    await status.follow(connection: connection) { model.conversationId }
                }
            }
            .task(id: scenePhase == .active) {
                if scenePhase == .active {
                    await model.follow()
                }
            }
            .environment(\.openURL, OpenURLAction { address in app.route(address) })
            .environment(\.pictureConversationId, model.conversationId)
            .onChange(of: app.askingAbout, initial: true) { _, reference in
                // A mail asked about elsewhere becomes what the next message
                // refers to.
                guard let reference else { return }
                model.replyingTo = reference
                app.askingAbout = nil
            }
    }
}

/// ConversationAlerts are what a conversation says when something goes
/// wrong, the haptics of sending, and files opened in Quick Look.
struct ConversationAlerts: ViewModifier {
    @Bindable var model: ConversationModel
    @Environment(AppModel.self) private var app

    func body(content: Content) -> some View {
        @Bindable var app = app
        return content
            .quickLookPreview($app.previewAddress)
            .alert("Cannot open the file", isPresented: $app.isShowingPreviewProblem, presenting: app.previewProblemText) { _ in
                Button("OK", role: .cancel) {}
            } message: { text in
                Text(text)
            }
            .sensoryFeedback(.success, trigger: model.sentCount)
            .alert("Something went wrong", isPresented: $model.isShowingSendProblem, presenting: model.sendProblemText) { _ in
                Button("OK", role: .cancel) {}
            } message: { text in
                Text(text)
            }
    }
}

/// ConversationBanners say what kind of conversation this is when it is not
/// an ordinary one: somebody else's agent, a record of what the agent did on
/// its own, a goal waiting for the person.
struct ConversationBanners: View {
    let model: ConversationModel
    @Environment(AppModel.self) private var app

    var body: some View {
        VStack(spacing: 6) {
            if let actingAs = model.actingAs {
                Banner(systemImage: "person.badge.key", text: String(localized: "This is \(actingAs)'s agent. What you say here reaches it as \(actingAs), and the server log says you did."))
            }
            if model.conversation?.kind == "run" {
                Banner(systemImage: "clock.arrow.circlepath", text: String(localized: "What your agent did on its own, as it happened. Ask about it here, or go back to the main chat.")) {
                    app.openedConversationId = ""
                }
            }
            if let conversation = model.conversation, conversation.kind == "goal", let goal = conversation.goal, !goal.isEmpty {
                Banner(systemImage: "target", text: GoalWords.summary(of: conversation))
                if conversation.goalState == "waiting", let note = conversation.goalNote, !note.isEmpty {
                    Banner(systemImage: "hand.raised", text: note)
                }
            }
        }
        .padding(.horizontal)
    }
}

/// Banner is one line of what kind of conversation this is.
struct Banner: View {
    let systemImage: String
    let text: String
    var action: (() -> Void)?

    var body: some View {
        Group {
            if let action {
                Button(action: action) { label }
                    .buttonStyle(.plain)
            } else {
                label
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .glassEffect(in: .rect(cornerRadius: 14, style: .continuous))
    }

    private var label: some View {
        Label(text, systemImage: systemImage)
            .font(.footnote)
            .foregroundStyle(.secondary)
    }
}

/// GoalWords say where a goal stands, in the dashboard's words.
enum GoalWords {
    static func state(_ goalState: String?) -> String {
        switch goalState {
        case "working": String(localized: "working")
        case "waiting": String(localized: "waiting for you")
        case "met": String(localized: "met")
        case "dropped": String(localized: "dropped")
        default: goalState ?? ""
        }
    }

    static func summary(of conversation: AgentConversation) -> String {
        "\(state(conversation.goalState)) · \(conversation.goal ?? "")"
    }
}

/// ConversationItem is one thing on the screen: a line of the transcript,
/// or the day between two lines on different days.
enum ConversationItem: Identifiable {
    case line(TranscriptLine)
    case day(Date)

    var id: String {
        switch self {
        case .line(let line): line.id
        case .day(let date): "day-\(date.timeIntervalSince1970)"
        }
    }

    static func items(of lines: [TranscriptLine]) -> [ConversationItem] {
        var items: [ConversationItem] = []
        var lastDay: Date?
        for line in lines {
            if let date = ServerTime.date(line.at) {
                let day = Calendar.current.startOfDay(for: date)
                if day != lastDay {
                    items.append(.day(day))
                    lastDay = day
                }
            }
            items.append(.line(line))
        }
        return items
    }
}

struct ConversationItemView: View {
    let item: ConversationItem
    let model: ConversationModel
    let isShowingUsage: Bool
    let isShowingToolCalls: Bool

    var body: some View {
        switch item {
        case .day(let date):
            Text(Self.dayText(date))
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity)
                .padding(.top, 6)
        case .line(let line):
            switch line.kind {
            case .person:
                PersonBubble(line: line)
                    .contextMenu { LineActions(line: line, model: model) }
            case .answer:
                VStack(alignment: .leading, spacing: 4) {
                    AgentBubble {
                        MarkdownView(text: line.text)
                    }
                    .contextMenu { LineActions(line: line, model: model) }
                    if isShowingUsage, let usage = line.usage {
                        Text(UsageWords.turn(usage))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .padding(.leading, 8)
                    }
                }
            case .tool:
                VStack(alignment: .leading, spacing: 6) {
                    if isShowingToolCalls {
                        ToolLine(line: line)
                    }
                    ToolFiles(line: line)
                }
            case .checkIn:
                CheckInLine(line: line)
            case .note:
                NoteLine(line: line)
            case .approval, .question:
                AgentBubble {
                    CardView(line: line, model: model)
                }
            case .codingQuestion:
                AgentBubble {
                    CodingQuestionCard(line: line, model: model)
                }
            }
        }
    }

    static func dayText(_ date: Date) -> String {
        if Calendar.current.isDateInToday(date) { return String(localized: "Today") }
        if Calendar.current.isDateInYesterday(date) { return String(localized: "Yesterday") }
        return date.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day())
    }
}

/// LineActions are what a long press on a message offers.
struct LineActions: View {
    let line: TranscriptLine
    let model: ConversationModel

    var body: some View {
        Button {
            UIPasteboard.general.string = line.text
        } label: {
            Label("Copy", systemImage: "doc.on.doc")
        }
        if line.messageId != nil {
            Button {
                model.reply(to: line)
            } label: {
                Label("Reply", systemImage: "arrowshape.turn.up.left")
            }
        }
    }
}

/// UsageWords say what a turn cost: tokens in and out, and the money.
enum UsageWords {
    static func count(_ count: Int) -> String {
        count.formatted(.number.notation(.compactName))
    }

    static func money(_ cost: Double) -> String {
        cost.formatted(.currency(code: "USD").precision(.fractionLength(cost < 0.01 ? 4 : 2)))
    }

    static func turn(_ usage: AgentUsage) -> String {
        String(localized: "\(count(usage.promptTokens)) in, \(count(usage.completionTokens)) out · \(money(usage.cost))")
    }
}

/// PersonBubble is what the person said: on the right, in the accent color,
/// with what it replies to above it.
struct PersonBubble: View {
    let line: TranscriptLine

    var body: some View {
        HStack {
            Spacer(minLength: 56)
            VStack(alignment: .trailing, spacing: 4) {
                if !line.quotedText.isEmpty {
                    Text(line.quotedText)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(3)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .overlay(alignment: .leading) {
                            Capsule().fill(.tertiary).frame(width: 3)
                        }
                }
                ForEach(line.references, id: \.self) { reference in
                    Label(reference.label, systemImage: reference.merchantName != nil ? "creditcard" : reference.path != nil ? "brain" : "envelope")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                ForEach(line.attachments, id: \.id) { attachment in
                    AttachmentView(attachment: attachment)
                }
                if !line.text.isEmpty {
                    Text(line.text)
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
        .accessibilityLabel("Thinking…")
    }
}

/// SuggestionsRow is the replies the last answer offers, each sent as it is
/// with a tap.
struct SuggestionsRow: View {
    let suggestions: [String]
    let onChoose: (String) -> Void

    var body: some View {
        ScrollView(.horizontal) {
            HStack {
                ForEach(suggestions, id: \.self) { suggestion in
                    Button(suggestion) { onChoose(suggestion) }
                        .buttonStyle(.glass)
                }
            }
        }
        .scrollIndicators(.hidden)
    }
}

/// EmptyConversation is what a conversation with nothing in it offers: what
/// to ask, and a few of the agent's ideas.
struct EmptyConversation: View {
    let model: ConversationModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Ask about your mail, or ask for something to be done.")
                .foregroundStyle(.secondary)
            if !model.ideas.isEmpty, model.draft.isEmpty {
                Text("Or start with one of these:")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                ForEach(model.ideas) { idea in
                    Button {
                        Task { await model.start(idea) }
                    } label: {
                        HStack(alignment: .firstTextBaseline, spacing: 10) {
                            Text(idea.emoji ?? "💡")
                            VStack(alignment: .leading, spacing: 2) {
                                Text(idea.headline).font(.headline)
                                if let body = idea.body, !body.isEmpty {
                                    Text(body).font(.footnote).foregroundStyle(.secondary)
                                }
                            }
                            Spacer(minLength: 0)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(.glass)
                }
            }
        }
        .padding(.top, 40)
    }
}

/// TodoPanel is the agent's own list of steps for this conversation, while
/// it works through them.
struct TodoPanel: View {
    let todos: [AgentTodo]
    @State private var expanded: Set<String> = []

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(todos) { todo in
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Image(systemName: todo.isDone ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(todo.isDone ? Color.accentColor : Color.secondary)
                            .accessibilityLabel(todo.isDone ? Text("Done") : Text("Still to do"))
                        Text(todo.text)
                            .strikethrough(todo.isDone)
                            .foregroundStyle(todo.isDone ? .secondary : .primary)
                            .lineLimit(expanded.contains(todo.id) ? nil : 1)
                            .onTapGesture {
                                if expanded.contains(todo.id) { expanded.remove(todo.id) } else { expanded.insert(todo.id) }
                            }
                    }
                    .font(.footnote)
                }
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .frame(maxHeight: 120)
        .fixedSize(horizontal: false, vertical: true)
        .glassEffect(in: .rect(cornerRadius: 16, style: .continuous))
        .padding(.horizontal)
        .accessibilityLabel("The agent's steps")
    }
}

/// ReplyBar is the message being replied to, above the composer.
struct ReplyBar: View {
    let reply: AgentReference
    let onCancel: () -> Void
    @Environment(AppModel.self) private var app

    var body: some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 2) {
                Text(reply.quotedRole == "user" ? String(localized: "Replying to your own message") : String(localized: "Replying to \(app.agentTitle)"))
                    .font(.caption.bold())
                Text(reply.quotedText ?? "")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            Spacer()
            Button(action: onCancel) {
                Image(systemName: "xmark.circle.fill")
                    .symbolRenderingMode(.hierarchical)
                    .frame(minWidth: 44, minHeight: 44)
            }
            .accessibilityLabel("Cancel")
        }
        .padding(.leading, 12)
        .glassEffect(in: .rect(cornerRadius: 16, style: .continuous))
        .padding(.horizontal)
    }
}

/// ReferenceBar is a mail the next message asks about.
struct ReferenceBar: View {
    let subject: String
    let from: String
    let onCancel: () -> Void

    var body: some View {
        HStack(alignment: .top) {
            Label {
                VStack(alignment: .leading, spacing: 2) {
                    Text(subject.isEmpty ? String(localized: "(no subject)") : subject)
                        .font(.caption.bold())
                        .lineLimit(1)
                    Text(from)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            } icon: {
                Image(systemName: "envelope")
            }
            Spacer()
            Button(action: onCancel) {
                Image(systemName: "xmark.circle.fill")
                    .symbolRenderingMode(.hierarchical)
                    .frame(minWidth: 44, minHeight: 44)
            }
            .accessibilityLabel("Cancel")
        }
        .padding(.leading, 12)
        .glassEffect(in: .rect(cornerRadius: 16, style: .continuous))
        .padding(.horizontal)
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
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
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
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                        }
                    }
                case .code(let text):
                    CodeBlock(text: text)
                case .table(let header, let rows):
                    ScrollView(.horizontal) {
                        Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 6) {
                            GridRow {
                                ForEach(Array(header.enumerated()), id: \.offset) { _, cell in
                                    Text(Self.inline(cell)).font(.subheadline.bold())
                                }
                            }
                            Divider()
                            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                                GridRow {
                                    ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                                        Text(Self.inline(cell)).font(.subheadline)
                                    }
                                }
                            }
                        }
                    }
                case .rule:
                    Divider()
                case .image(let alt, let address, let link):
                    AnswerPicture(alt: alt, address: address, link: link)
                }
            }
        }
    }

    static func inline(_ text: String) -> AttributedString {
        (try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)))
            ?? AttributedString(text)
    }
}

/// CodeBlock is code or a tool's JSON: monospaced, scrolling when long, and
/// copied with a tap.
struct CodeBlock: View {
    let text: String
    var maxHeight: CGFloat = 400

    var body: some View {
        ZStack(alignment: .topTrailing) {
            ScrollView([.horizontal, .vertical]) {
                Text(text)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(maxHeight: maxHeight)
            .fixedSize(horizontal: false, vertical: true)
            Button {
                UIPasteboard.general.string = text
            } label: {
                Image(systemName: "doc.on.doc")
                    .font(.caption)
                    .frame(minWidth: 32, minHeight: 32)
            }
            .accessibilityLabel("Copy")
        }
        .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 10, style: .continuous))
    }

    /// A tool's arguments or result made readable: the untrusted-data fence
    /// a result is wrapped in taken off, and JSON indented.
    static func tidy(_ text: String) -> String {
        var trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.hasPrefix("<untrusted-data>"), trimmed.hasSuffix("</untrusted-data>") {
            trimmed = String(trimmed.dropFirst("<untrusted-data>".count).dropLast("</untrusted-data>".count))
                .trimmingCharacters(in: .whitespacesAndNewlines)
        }
        if let data = trimmed.data(using: .utf8), let object = try? JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed]),
            !(object is String),
            let pretty = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
        {
            return String(decoding: pretty, as: UTF8.self)
        }
        return trimmed
    }
}

/// ToolLine is a tool the agent called: its name and how it ended on one
/// line, what it was given and what came back on a tap.
struct ToolLine: View {
    let line: TranscriptLine
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation { isExpanded.toggle() }
            } label: {
                HStack(spacing: 6) {
                    if line.isDone {
                        Image(systemName: "checkmark.circle")
                    } else {
                        ProgressView().controlSize(.mini)
                    }
                    Text(line.resolution.isEmpty ? line.detail : "\(line.detail) · \(line.resolution)")
                        .lineLimit(1)
                    Image(systemName: "chevron.right")
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            if isExpanded {
                if !line.arguments.isEmpty {
                    CodeBlock(text: CodeBlock.tidy(line.arguments), maxHeight: 240)
                }
                CodeBlock(text: line.text.isEmpty ? String(localized: "Nothing came back yet.") : CodeBlock.tidy(line.text), maxHeight: 240)
            }
        }
        .padding(.leading, 4)
    }
}

/// CheckInLine is a turn the agent took on its own, shown as where it came
/// from; a tap shows what started it.
struct CheckInLine: View {
    let line: TranscriptLine
    @State private var isExpanded = false

    var body: some View {
        VStack(spacing: 6) {
            Button {
                withAnimation { isExpanded.toggle() }
            } label: {
                Label(Self.label(of: line.detail), systemImage: Self.symbol(of: line.detail))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            if isExpanded {
                CodeBlock(text: line.text, maxHeight: 240)
            }
        }
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
        case .background, .herdrSession: "terminal"
        case .herdrQuestion, .alert: "exclamationmark.triangle"
        case .backgroundWork: "list.bullet"
        case .schedule: "calendar"
        case .speakFirst: "sparkles"
        case .approved: "checkmark"
        case .declined: "xmark"
        case nil: "bell"
        }
    }
}

/// NoteLine is something that happened, in the dashboard's words; the note
/// the earlier chat was folded into opens on a tap.
struct NoteLine: View {
    let line: TranscriptLine
    @State private var isExpanded = false

    var body: some View {
        VStack(spacing: 6) {
            if line.detail == "compacted", !line.text.isEmpty {
                Button {
                    withAnimation { isExpanded.toggle() }
                } label: {
                    Label(Self.words(of: line), systemImage: isExpanded ? "chevron.down" : "chevron.right")
                }
                .buttonStyle(.plain)
                if isExpanded {
                    AgentBubble { MarkdownView(text: line.text) }
                }
            } else {
                HStack(spacing: 6) {
                    if line.isPending { ProgressView().controlSize(.mini) }
                    Text(Self.words(of: line))
                }
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .multilineTextAlignment(.center)
        .frame(maxWidth: .infinity)
    }

    static func words(of line: TranscriptLine) -> String {
        let detail = line.text.trimmingCharacters(in: .whitespacesAndNewlines)
        switch line.detail {
        case "queued": return String(localized: "Waiting for the turn before it…")
        case "stopped": return detail.isEmpty ? String(localized: "Stopped.") : String(localized: "Stopped: \(detail)")
        case "failed": return detail.isEmpty ? String(localized: "The turn failed.") : String(localized: "The turn failed: \(detail)")
        case "call_unreadable": return String(localized: "A tool call could not be read, so it was asked for again.")
        case "repeated_failure": return String(localized: "Stopped: the same call failed three times.")
        case "round_limit": return String(localized: "Stopped after the most rounds a turn may take.")
        case "compacting": return String(localized: "Folding the earlier chat into a note…")
        case "compacted": return String(localized: "The earlier chat was folded into a note.")
        case "depth": return detail.isEmpty ? String(localized: "Looking into this carefully.") : String(localized: "Looking into this carefully: \(detail)")
        case "goal_set": return String(localized: "Goal set: \(detail)")
        case "goal_set_again": return String(localized: "Goal set again: \(detail)")
        case "goal_changed": return String(localized: "Goal changed: \(detail)")
        case "goal_cleared": return String(localized: "Goal cleared: \(detail)")
        case "goal_met": return String(localized: "Goal met: \(detail)")
        case "goal_stalled": return String(localized: "Goal stalled: \(detail) turns since you last wrote and it is not met. Write to keep going, or clear or change it.")
        default: return detail
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
                    if line.risk == "destructive" {
                        Button("Do it", role: .destructive) {
                            Task { await model.approve(line, isApproved: true) }
                        }
                        .buttonStyle(.glassProminent)
                        .tint(.red)
                    } else {
                        Button("Do it") {
                            Task { await model.approve(line, isApproved: true) }
                        }
                        .buttonStyle(.glassProminent)
                    }
                    Button("Don’t") {
                        Task { await model.approve(line, isApproved: false) }
                    }
                    .buttonStyle(.glass)
                }
            } else {
                ForEach(line.choices, id: \.self) { choice in
                    Button {
                        Task { await model.answer(line, with: choice) }
                    } label: {
                        Text(choice).frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(.glass)
                }
                Button("Chat about it") {
                    Task { await model.answer(line, with: ConversationModel.chatAboutIt) }
                }
                .buttonStyle(.borderless)
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
/// conversation, with files to add and a button to send, or to stop the
/// turn that is running.
struct Composer: View {
    @Bindable var model: ConversationModel
    @Environment(AppModel.self) private var app
    @State private var pickedItems: [PhotosPickerItem] = []
    @State private var isPickingPhotos = false
    @State private var isTakingPicture = false
    @State private var isPickingFiles = false
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(spacing: 8) {
            if !model.pendingFiles.isEmpty {
                ScrollView(.horizontal) {
                    HStack {
                        ForEach(model.pendingFiles) { file in
                            PendingFileChip(file: file) {
                                model.pendingFiles.removeAll { $0.id == file.id }
                            }
                        }
                    }
                }
                .scrollIndicators(.hidden)
            }
            GlassEffectContainer {
                HStack(alignment: .bottom, spacing: 8) {
                    Menu {
                        Button {
                            isPickingPhotos = true
                        } label: {
                            Label("Photo library", systemImage: "photo.on.rectangle")
                        }
                        if UIImagePickerController.isSourceTypeAvailable(.camera) {
                            Button {
                                isTakingPicture = true
                            } label: {
                                Label("Camera", systemImage: "camera")
                            }
                        }
                        Button {
                            isPickingFiles = true
                        } label: {
                            Label("Files", systemImage: "folder")
                        }
                    } label: {
                        Image(systemName: "plus")
                            .font(.title3)
                            .frame(minWidth: 44, minHeight: 44)
                    }
                    .glassEffect(.regular.interactive(), in: .circle)
                    .accessibilityLabel("Add files")

                    TextField(placeholder, text: $model.draft, axis: .vertical)
                        .lineLimit(1...6)
                        .focused($isFocused)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                        .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 22, style: .continuous))

                    if model.voiceSampleRate != nil, model.draft.isEmpty, model.pendingFiles.isEmpty, model.conversation?.kind != "run" {
                        Button {
                            model.startCall()
                        } label: {
                            Image(systemName: "phone")
                                .font(.title3)
                                .frame(minWidth: 44, minHeight: 44)
                        }
                        .glassEffect(.regular.interactive(), in: .circle)
                        .accessibilityLabel("Call your agent")
                    }
                    if model.runningRunId != nil, model.draft.isEmpty, model.pendingFiles.isEmpty {
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
        .photosPicker(isPresented: $isPickingPhotos, selection: $pickedItems, maxSelectionCount: 8, matching: .images)
        .fullScreenCover(isPresented: $isTakingPicture) {
            CameraPicker { picture in
                if let file = PendingFile(picture: picture, index: model.pendingFiles.count) {
                    model.pendingFiles.append(file)
                }
            }
            .ignoresSafeArea()
        }
        .fileImporter(isPresented: $isPickingFiles, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let addresses) = result else { return }
            for address in addresses {
                if let file = PendingFile(fileAt: address) {
                    model.pendingFiles.append(file)
                }
            }
        }
        .onChange(of: pickedItems) { _, items in
            guard !items.isEmpty else { return }
            pickedItems = []
            Task { await add(items) }
        }
    }

    private var placeholder: String {
        if model.freshQuestion != nil { return String(localized: "Your answer") }
        return app.agentName.isEmpty ? String(localized: "Ask your agent…") : String(localized: "Ask \(app.agentName)…")
    }

    private func add(_ items: [PhotosPickerItem]) async {
        for item in items {
            guard let data = try? await item.loadTransferable(type: Data.self),
                let file = PendingFile(pictureData: data, index: model.pendingFiles.count)
            else { continue }
            model.pendingFiles.append(file)
        }
    }
}
