import SwiftUI
import TeaNodeKit

// Chats in the conversation screens. A chat is one more conversation: it is
// listed with the others, opens in ConversationView and is written to with
// the same composer. These are the parts only a chat needs: its people's
// pictures, who has read what, who is typing, and the sheets for starting one
// and changing who is in it.

/// ChatWords are the words a chat is described in.
enum ChatWords {
    static var deletedAccountNote: String { String(localized: "(deleted account)") }

    /// "Ada's agent", in the person's language.
    static func agentOf(_ ownerName: String) -> String { String(localized: "\(ownerName)'s agent") }

    /// The word after an @ that mentions everybody in a chat, in the language
    /// the app is shown in.
    static var allMentionWord: String { String(localized: "all", comment: "After an @, mentions everybody in a chat") }

    /// What each notification level is called.
    static func name(of notificationLevel: ChatNotificationLevel) -> String {
        switch notificationLevel {
        case .all: String(localized: "All messages")
        case .mentions: String(localized: "Only mentions")
        case .off: String(localized: "Off")
        }
    }

    /// The symbol of each notification level: a bell, the @, a bell crossed out.
    static func symbol(of notificationLevel: ChatNotificationLevel) -> String {
        switch notificationLevel {
        case .all: "bell"
        case .mentions: "at"
        case .off: "bell.slash"
        }
    }

    /// A member's name, with the note when their account is gone.
    static func name(of member: ChatMember) -> String {
        ChatSummary.name(of: member, deletedAccountNote: deletedAccountNote)
    }

    /// Who is typing, in a line.
    static func typing(_ names: [String]) -> String {
        switch names.count {
        case 0: ""
        case 1: String(localized: "\(names[0]) is typing")
        case 2: String(localized: "\(names[0]) and \(names[1]) are typing")
        default: String(localized: "Several people are typing")
        }
    }

    /// Which agents are working on an answer, in a line: an agent is
    /// thinking where a person is typing.
    static func thinking(_ names: [String]) -> String {
        switch names.count {
        case 0: ""
        case 1: String(localized: "\(names[0]) is thinking")
        case 2: String(localized: "\(names[0]) and \(names[1]) are thinking")
        default: String(localized: "Several agents are thinking")
        }
    }
}

/// ChatAvatarsView is a chat's people: the other person's picture for a direct
/// chat, two overlapped for a group.
struct ChatAvatarsView: View {
    let chat: ChatSummary
    let ownUserId: String?
    var size: CGFloat = 30

    var body: some View {
        let others = chat.otherMembers(ownUserId: ownUserId)
        let shown = Array(others.prefix(chat.isGroup ? 2 : 1))
        HStack(spacing: -size * 0.3) {
            if shown.isEmpty {
                AvatarView(name: chat.title ?? "?", avatarUrl: nil, colorIndex: nil, size: size)
            }
            ForEach(shown) { member in
                AvatarView(name: member.displayName, avatarUrl: member.avatarUrl, colorIndex: member.avatarColorIndex, size: size)
                    .overlay(Circle().stroke(Color(.systemBackground), lineWidth: 2))
            }
        }
        .accessibilityHidden(true)
    }
}

/// UnreadBadge is how many messages wait in a chat.
struct UnreadBadge: View {
    let unreadCount: Int

    var body: some View {
        Text(verbatim: unreadCount > 99 ? "99+" : "\(unreadCount)")
            .font(.caption.weight(.semibold))
            .monospacedDigit()
            // The window's own color on the tint, light in dark mode and dark in
            // light mode, so that it holds against the tint in both.
            .foregroundStyle(.background)
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .background(.tint, in: .capsule)
            .accessibilityLabel(Text("\(unreadCount) unread"))
    }
}

/// ReadMarks are the small pictures under a message of the people who have
/// read as far as it.
struct ReadMarks: View {
    let readers: [ChatMember]
    private static let mostAvatars = 4

    var body: some View {
        if !readers.isEmpty {
            HStack(spacing: -3) {
                ForEach(readers.prefix(Self.mostAvatars)) { member in
                    AvatarView(name: member.displayName, avatarUrl: member.avatarUrl, colorIndex: member.avatarColorIndex, size: 14)
                        .overlay(Circle().stroke(Color(.systemBackground), lineWidth: 1))
                }
                if readers.count > Self.mostAvatars {
                    Text(verbatim: "+\(readers.count - Self.mostAvatars)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .padding(.leading, 6)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text("Read by \(names)"))
        }
    }

    private var names: String {
        readers.map { ChatWords.name(of: $0) }.joined(separator: ", ")
    }
}

/// ChatTypingLine says who is writing, and which agent is thinking.
struct ChatTypingLine: View {
    let names: [String]
    var agentNames: [String] = []
    @Environment(\.accessibilityReduceMotion) private var isReducingMotion

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if !names.isEmpty { line(ChatWords.typing(names)) }
            if !agentNames.isEmpty { line(ChatWords.thinking(agentNames)) }
        }
    }

    private func line(_ words: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: "ellipsis")
                .symbolEffect(.variableColor.iterative, isActive: !isReducingMotion)
            Text(words)
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.leading, 4)
        .accessibilityElement(children: .combine)
    }
}

/// MentionMarks marks the mentions in a message's words: each @ and name
/// semibold, in the words' own color (green is kept for what can be pressed),
/// and the reader's own on a highlighter's wash so it stands out of the rest.
enum MentionMarks {
    static func marked(_ attributed: AttributedString, with mentions: [ChatMentionWord]) -> AttributedString {
        guard !mentions.isEmpty else { return attributed }
        let text = String(attributed.characters)
        guard text.contains("@") else { return attributed }
        var marked = attributed
        for range in ChatMention.ranges(of: mentions.map(\.word), in: text) {
            let start = marked.index(marked.startIndex, offsetByCharacters: text.distance(from: text.startIndex, to: range.lowerBound))
            let end = marked.index(start, offsetByCharacters: text.distance(from: range.lowerBound, to: range.upperBound))
            let said = text[range].dropFirst().lowercased()
            marked[start..<end].inlinePresentationIntent = .stronglyEmphasized
            if mentions.contains(where: { $0.isReader && $0.word.lowercased() == said }) {
                marked[start..<end][AttributeScopes.SwiftUIAttributes.BackgroundColorAttribute.self] = Color(.systemYellow).opacity(0.35)
            }
        }
        return marked
    }
}

/// NotificationLevelPicker chooses what the person's phone is told of in a
/// chat: every message, only mentions, or nothing. In a menu it is a submenu
/// with the level ticked; in a list, rows with a tick.
struct NotificationLevelPicker: View {
    let level: ChatNotificationLevel
    let onChoose: (ChatNotificationLevel) -> Void

    var body: some View {
        Picker(selection: Binding(get: { level }, set: { onChoose($0) })) {
            ForEach(ChatNotificationLevel.allCases) { choice in
                Label(ChatWords.name(of: choice), systemImage: ChatWords.symbol(of: choice)).tag(choice)
            }
        } label: {
            Label("Notifications", systemImage: ChatWords.symbol(of: level))
        }
        .pickerStyle(.menu)
    }
}

/// MentionMenu offers what an @ in the composer can mention or ask, as the
/// dashboard's list does: a row for each with its symbol, its name and what
/// it is. The person's own agent comes first, then everybody in a group, then
/// each of the others followed by their agent when its owner lets the others
/// ask. A person and their agent are two rows, named apart: mentioning a
/// person tells them and asks nothing of their agent. It sits above the
/// composer on glass, shows about five rows (one at the largest text sizes)
/// and scrolls for more. A tap puts the name in the words.
struct MentionMenu: View {
    let choices: [MentionChoice]
    let onChoose: (MentionChoice) -> Void
    @ScaledMetric(relativeTo: .body) private var rowHeight: CGFloat = 56
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    /// The height of all the rows, measured, since a scroll view would
    /// otherwise take all the room it is offered even for one row.
    @State private var rowsHeight: CGFloat = 0

    /// Rows shown before the rest scroll: one at the largest text sizes, where
    /// a row is about three times as tall, so the menu above the keyboard
    /// leaves room for the chat.
    private var visibleRowCount: CGFloat { dynamicTypeSize.isAccessibilitySize ? 1 : 5 }

    var body: some View {
        ScrollView {
            VStack(spacing: 0) {
                ForEach(choices) { choice in
                    MentionMenuRow(choice: choice, isLast: choice.id == choices.last?.id) { onChoose(choice) }
                }
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { rowsHeight = $0 }
        }
        .scrollBounceBehavior(.basedOnSize)
        .frame(height: min(rowsHeight, rowHeight * visibleRowCount))
        .clipShape(.rect(cornerRadius: 18, style: .continuous))
        // Glass, as the composer under it is: the menu floats over the chat.
        .glassEffect(.regular, in: .rect(cornerRadius: 18, style: .continuous))
        // Not drawn until the rows are measured, so it does not show as an
        // empty strip for a moment first.
        .opacity(rowsHeight == 0 ? 0 : 1)
        .padding(.horizontal)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("People and agents you can mention")
    }
}

/// MentionMenuRow is one choice in the mention menu, a plain row button across
/// the width of the menu.
private struct MentionMenuRow: View {
    let choice: MentionChoice
    let isLast: Bool
    let onChoose: () -> Void
    @ScaledMetric(relativeTo: .body) private var symbolSide: CGFloat = 34

    private static var horizontalPadding: CGFloat { 16 }
    private static var symbolSpacing: CGFloat { 12 }

    /// An agent's sparkles, a person, or the people for everybody.
    private var symbol: String {
        switch choice {
        case .agent: "sparkles"
        case .person: "person"
        case .all: "person.2"
        }
    }

    /// What the row is under its name. Most agents keep the name "Agent", so
    /// whose it is tells two of them apart, in the dashboard's words; a
    /// person's row says that mentioning tells them.
    private var detail: String {
        switch choice {
        case .agent(let agent): agent.isOwn ? String(localized: "Yours") : ChatWords.agentOf(agent.ownerName)
        case .person: String(localized: "Tells them")
        case .all: String(localized: "Everyone in this chat")
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            Button(action: onChoose) {
                HStack(spacing: Self.symbolSpacing) {
                    Image(systemName: symbol)
                        .symbolRenderingMode(.hierarchical)
                        .foregroundStyle(.primary)
                        .frame(width: symbolSide, height: symbolSide)
                        .background(Color(.tertiarySystemFill), in: .circle)
                        // Read as the name and what it is, not the symbol's name.
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(ChatMention.word(of: choice, allWord: ChatWords.allMentionWord))
                            .font(.body.weight(.semibold))
                            .foregroundStyle(.primary)
                        Text(detail)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 0)
                }
                .padding(.horizontal, Self.horizontalPadding)
                .padding(.vertical, 8)
                .frame(minHeight: 44)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(MentionRowButtonStyle())
            if !isLast {
                Divider().padding(.leading, Self.horizontalPadding + symbolSide + Self.symbolSpacing)
            }
        }
    }
}

/// MentionRowButtonStyle darkens a row while it is pressed, since a plain
/// button shows nothing.
private struct MentionRowButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.background(configuration.isPressed ? Color(.tertiarySystemFill) : Color.clear)
    }
}

/// ChatLineFrame puts a message of a chat where it belongs: the person's
/// own on the right, and everybody else's on the left with their picture and,
/// above the first message of a run, their name. The bubble is the content.
struct ChatLineFrame<Content: View>: View {
    let line: TranscriptLine
    let author: ChatAuthor
    let isRunStart: Bool
    let readers: [ChatMember]
    @ViewBuilder let content: Content

    private static var avatarSide: CGFloat { 30 }

    /// The person to open the card of when their picture or name is tapped:
    /// somebody else's account. Not the agent, not the person themselves, not
    /// an account that is gone.
    private var cardUserId: String? {
        guard author.isPerson, !author.isMine, !author.authorUserId.isEmpty else { return nil }
        return author.authorUserId
    }

    var body: some View {
        if author.isOwnWords {
            VStack(alignment: .trailing, spacing: 4) {
                content
                ReadMarks(readers: readers)
                    .padding(.trailing, 6)
            }
        } else {
            HStack(alignment: .top, spacing: 8) {
                if isRunStart {
                    OpensPersonCard(userId: cardUserId) {
                        AvatarView(name: name, avatarUrl: author.authorAvatarUrl, colorIndex: author.authorAvatarColorIndex, size: Self.avatarSide)
                    }
                } else {
                    Color.clear.frame(width: Self.avatarSide, height: 1)
                }
                VStack(alignment: .leading, spacing: 3) {
                    if isRunStart {
                        OpensPersonCard(userId: cardUserId) {
                            HStack(spacing: 4) {
                                Text(name)
                                if author.isAgent {
                                    Image(systemName: "sparkles")
                                        .accessibilityLabel("Agent")
                                }
                            }
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                            .padding(.leading, 6)
                        }
                    }
                    content
                        // A continuation shows no name, so VoiceOver is told
                        // whose it is, as it is for the first of a run.
                        .overlay(alignment: .topLeading) {
                            if !isRunStart {
                                Color.clear.frame(width: 1, height: 1).allowsHitTesting(false).accessibilityElement().accessibilityLabel(name)
                            }
                        }
                    ReadMarks(readers: readers)
                        .padding(.leading, 6)
                }
            }
        }
    }

    /// Who said it, with the note when their account is gone.
    private var name: String {
        author.isDeletedAccount
            ? "\(author.authorName) \(ChatWords.deletedAccountNote)" : author.speakerName(ownedWords: ChatWords.agentOf)
    }
}

/// OpensPersonCard makes what it holds, a person's picture or name, open their
/// card when tapped. With no person to open (the agent, the person's own
/// words, an account that is gone) it is the content and nothing more.
struct OpensPersonCard<Content: View>: View {
    let userId: String?
    @ViewBuilder let content: Content
    @Environment(AppModel.self) private var app

    var body: some View {
        if let userId {
            Button {
                app.openedPerson = .account(userId: userId)
            } label: {
                content
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint("Opens their card")
        } else {
            content
        }
    }
}

/// ChatMenu is what can be done about the chat on screen: who is in it, its
/// name if it is a group, its sound, and leaving it. A circle's chat follows
/// the circle, so it is neither renamed nor left from here.
struct ChatMenu: View {
    let model: ConversationModel
    @Environment(AppModel.self) private var app
    @State private var isShowingMembers = false
    /// The member whose card a tap in the members sheet asked for, opened when
    /// the sheet has closed.
    @State private var pendingPersonUserId: String?
    @State private var isRenaming = false
    @State private var newTitle = ""
    @State private var isConfirmingLeave = false

    /// Opens the card of the member tapped in the members sheet, now that the
    /// sheet is gone.
    private func openPendingPerson() {
        guard let userId = pendingPersonUserId else { return }
        pendingPersonUserId = nil
        app.openedPerson = .account(userId: userId)
    }

    var body: some View {
        if let chat = model.chat {
            Menu {
                Button {
                    isShowingMembers = true
                } label: {
                    Label("Members", systemImage: "person.2")
                }
                if chat.canBeRenamed {
                    Button {
                        newTitle = chat.title ?? ""
                        isRenaming = true
                    } label: {
                        Label("Rename group", systemImage: "pencil")
                    }
                }
                // What the phone is told of here, a submenu of the three
                // levels with the current one ticked.
                NotificationLevelPicker(level: chat.level) { level in
                    Task { await model.setNotificationLevel(level) }
                }
                if chat.canBeLeft {
                    Divider()
                    Button(role: .destructive) {
                        isConfirmingLeave = true
                    } label: {
                        Label("Leave group", systemImage: "rectangle.portrait.and.arrow.right")
                    }
                    .tint(.red)
                }
            } label: {
                Label("Chat options", systemImage: "ellipsis.circle")
            }
            // On iOS 26 a confirmation points at the control that asked, and
            // the item goes away with its menu, so it hangs on the menu.
            .confirmationDialog("Leave this group?", isPresented: $isConfirmingLeave, titleVisibility: .visible) {
                Button("Leave group", role: .destructive) {
                    Task {
                        if await model.leave() { app.openedConversationId = "" }
                    }
                }
            } message: {
                Text("You will stop getting its messages. Someone in the group can add you again.")
            }
            .alert("Rename group", isPresented: $isRenaming) {
                TextField("Group name", text: $newTitle)
                Button("Cancel", role: .cancel) {}
                Button("Save") {
                    let title = newTitle.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !title.isEmpty else { return }
                    Task { await model.rename(to: title) }
                }
            }
            .sheet(isPresented: $isShowingMembers, onDismiss: openPendingPerson) {
                ChatMembersSheet(model: model) { userId in pendingPersonUserId = userId }
            }
            #if DEBUG
                // `-TeaNodeScreen chat-members`: the members sheet open over the chat.
                .task {
                    guard DemoServer.isRequested, DemoServer.screen == "chat-members" else { return }
                    try? await Task.sleep(for: .seconds(1))
                    isShowingMembers = true
                }
            #endif
        }
    }
}

/// ChatMembersSheet lists who is in a chat. A group's admins can add people
/// and take them out; a circle's chat follows the circle, so there it is a
/// list.
struct ChatMembersSheet: View {
    let model: ConversationModel
    /// Asks for a member's card; the menu that shows this sheet opens it once
    /// the sheet is gone.
    let onOpenPerson: (String) -> Void
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var isAdding = false
    @State private var removing: ChatMember?
    /// A refusal of a change made here, which the sheet covers the screen to
    /// say.
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            List {
                if let chat = model.chat {
                    let canChange = chat.canChangeMembers(ownUserId: model.ownUserId)
                    Section {
                        ForEach(chat.presentMembers) { member in
                            row(member, in: chat, canRemove: canChange && member.userId != model.ownUserId)
                        }
                    }
                    if canChange {
                        Section {
                            Button {
                                isAdding = true
                            } label: {
                                Label("Add people", systemImage: "person.badge.plus")
                                    .actionColor()
                            }
                        }
                    }
                    if let own = chat.member(model.ownUserId ?? ""), !own.isRemoved, !chat.isRemoved {
                        notificationSection(chat)
                        agentSection(own)
                    }
                }
            }
            .navigationTitle("Members")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .toast(done: .constant(nil), problem: $problemText)
            .sheet(isPresented: $isAdding) {
                ChatPeopleSheet(
                    connection: model.connection, purpose: .add, leftOut: Set(model.chat?.presentMembers.map(\.userId) ?? [])
                ) { userIds, _ in
                    try await model.addMembers(userIds)
                }
            }
        }
        .presentationDetents(detents)
    }

    /// The sheet opens half way and can be pulled up. A screenshot of it
    /// (`-TeaNodeScreen chat-members`) opens it whole, to show the switches
    /// under the members.
    private var detents: Set<PresentationDetent> {
        #if DEBUG
            if DemoServer.isRequested, DemoServer.screen == "chat-members" { return [.large] }
        #endif
        return [.medium, .large]
    }

    /// What the person's phone is told of in this chat, the three levels as
    /// rows with the chosen one ticked, as Settings chooses among a few.
    private func notificationSection(_ chat: ChatSummary) -> some View {
        Section {
            Picker(
                "Notifications",
                selection: Binding(get: { chat.level }, set: { level in Task { await model.setNotificationLevel(level) } })
            ) {
                ForEach(ChatNotificationLevel.allCases) { level in
                    Text(ChatWords.name(of: level)).tag(level)
                }
            }
            .pickerStyle(.inline)
            .labelsHidden()
        } header: {
            Text("Notifications")
        } footer: {
            Text("Only mentions: you are told when someone writes @ and your name, or @all. It stays as you set it.")
        }
    }

    /// What the person's agent does in this chat, as two switches that are the
    /// person's alone, each with the sentence that says what it means.
    private func agentSection(_ own: ChatMember) -> some View {
        Section {
            if own.isAgentActive == false {
                // Switches for an agent that is off would be accepted and do
                // nothing, so the section says it is off, as the dashboard does.
                Text("Your agent is off.")
                // Where it is turned on, as the screen with no agent offers it.
                // A person with no agent at all has nothing to turn on.
                if app.hasAgent {
                    Button {
                        dismiss()
                        app.openedSection = .settings
                    } label: {
                        Label("Open Settings", systemImage: "gearshape")
                            .actionColor()
                    }
                }
            } else {
                Toggle(
                    "My agent remembers this chat",
                    isOn: Binding(
                        get: { own.isAgentReading },
                        set: { isOn in Task { problemText = await model.setAgentReading(isOn) } }))
                Toggle(
                    "Others may ask my agent here",
                    isOn: Binding(
                        get: { own.isAgentAskableByOthers == true },
                        set: { isOn in Task { problemText = await model.setAgentAskableByOthers(isOn) } }))
            }
        } header: {
            Text("Your agent in this chat")
        } footer: {
            if own.isAgentActive != false {
                Text(
                    "Remembering: from now on your agent reads what is said here with the rest of what it knows. What it learned stays if you turn this off, and it reads nothing said while this is off.\n\nAsking: anyone in this chat can address your agent with @, and it answers them in front of everybody. It can read this chat and look things up on the web, and nothing of yours: no mail, notes, files or calendar. Its work is counted as yours."
                )
            }
        }
    }

    private func row(_ member: ChatMember, in chat: ChatSummary, canRemove: Bool) -> some View {
        tappableRow(member, in: chat)
        .swipeActions(edge: .trailing) {
            if canRemove {
                Button(role: .destructive) {
                    removing = member
                } label: {
                    Label("Remove", systemImage: "person.badge.minus")
                }
                .tint(.red)
            }
        }
        .contextMenu {
            if canRemove {
                Button(role: .destructive) {
                    removing = member
                } label: {
                    Label("Remove", systemImage: "person.badge.minus")
                }
                .tint(.red)
            }
        }
        .confirmationDialog(
            "Remove this person?", isPresented: Binding(get: { removing?.id == member.id }, set: { if !$0 { removing = nil } }),
            titleVisibility: .visible, presenting: removing
        ) { person in
            Button("Remove", role: .destructive) {
                Task { problemText = await model.removeMember(person) }
            }
        } message: { person in
            Text("\(ChatWords.name(of: person)) will not see new messages in this group. What was said stays.")
        }
    }

    /// The row, opening the member's card when tapped; the person's own row,
    /// and one whose account is gone, open nothing.
    @ViewBuilder
    private func tappableRow(_ member: ChatMember, in chat: ChatSummary) -> some View {
        if member.userId.isEmpty || member.userId == model.ownUserId {
            rowLabel(member, in: chat)
        } else {
            Button {
                open(member)
            } label: {
                rowLabel(member, in: chat)
            }
            .foregroundStyle(.primary)
            .accessibilityHint("Opens their card")
        }
    }

    /// Opens their card once this sheet is closed: a card asked for while
    /// this sheet is still up would not be shown.
    private func open(_ member: ChatMember) {
        guard !member.userId.isEmpty, member.userId != model.ownUserId else { return }
        onOpenPerson(member.userId)
        dismiss()
    }

    private func rowLabel(_ member: ChatMember, in chat: ChatSummary) -> some View {
        HStack(spacing: 12) {
            AvatarView(name: member.displayName, avatarUrl: member.avatarUrl, colorIndex: member.avatarColorIndex, size: 36)
            VStack(alignment: .leading, spacing: 2) {
                // The person's own row says so, as a circle's members do and
                // as the dashboard's list of a chat's members does.
                HStack(spacing: 6) {
                    Text(ChatWords.name(of: member))
                    if member.userId == model.ownUserId {
                        Text("You").font(.caption).foregroundStyle(Color(.secondaryLabel))
                    }
                }
                if member.isAdmin, chat.isGroup {
                    Text("Admin")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                // The person's own agent is said by the switches below.
                if member.userId != model.ownUserId {
                    if member.isAgentReading {
                        Text("Their agent remembers this chat")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    if member.isAgentAskableByOthers == true {
                        Text("Others may ask their agent")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            Spacer(minLength: 0)
        }
        .contentShape(.rect)
    }
}

/// ChatPeopleSheet is the one sheet for choosing people: to message one, to
/// start a group with several and a name, or to add to a group that is
/// there. Those in it already are left out of the list.
struct ChatPeopleSheet: View {
    enum Purpose {
        case direct
        case group
        case add
    }

    let connection: Connection
    let purpose: Purpose
    var leftOut: Set<String> = []
    /// Does what was chosen: the people, and the group's name for a group.
    /// A refusal is thrown and told here, the sheet staying open.
    let perform: (_ userIds: [String], _ groupName: String) async throws -> Void
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var app
    @State private var people: [ChatPerson]?
    /// Why the people could not be read, to be said with a way to try again: an
    /// empty list would say the person shares a circle with nobody.
    @State private var loadProblemText: String?
    @State private var chosen: Set<String> = []
    @State private var groupName = ""
    @State private var isBusy = false
    @State private var problemText: String?

    private var choices: [ChatPerson] { (people ?? []).filter { !leftOut.contains($0.userId) } }

    private var canSubmit: Bool {
        !chosen.isEmpty && (purpose != .group || !groupName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
    }

    var body: some View {
        NavigationStack {
            List {
                if purpose == .group {
                    Section {
                        TextField("Group name", text: $groupName)
                    }
                }
                Section {
                    ForEach(choices) { person in
                        Button {
                            tap(person)
                        } label: {
                            HStack(spacing: 12) {
                                AvatarView(name: person.displayName, avatarUrl: person.avatarUrl, colorIndex: person.avatarColorIndex, size: 36)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(person.displayName)
                                        .foregroundStyle(.primary)
                                    if person.displayName != person.username {
                                        Text(person.username)
                                            .font(.footnote)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                                Spacer(minLength: 0)
                                if purpose != .direct, chosen.contains(person.userId) {
                                    Image(systemName: "checkmark")
                                        .foregroundStyle(.tint)
                                }
                            }
                            .contentShape(.rect)
                        }
                        .disabled(isBusy)
                        .accessibilityAddTraits(chosen.contains(person.userId) ? .isSelected : [])
                    }
                }
            }
            .overlay {
                if let loadProblemText, people == nil {
                    LoadFailedView(problemText: loadProblemText) { await load() }
                } else if people == nil {
                    ProgressView()
                } else if choices.isEmpty {
                    if purpose == .add, !(people ?? []).isEmpty {
                        // Adding to a group the person's circles are all in
                        // already is not the same as sharing a circle with nobody.
                        ContentUnavailableView(
                            "Nobody left to add", systemImage: "person.2",
                            description: Text("Everyone you share a circle with is already in this group."))
                    } else {
                        ContentUnavailableView {
                            Label("No one to message yet", systemImage: "person.2")
                        } description: {
                            Text("You do not share a circle with anyone yet. Invite people to a circle, then message them from here.")
                        } actions: {
                            OpenCirclesLink(connection: connection)
                                .buttonStyle(.borderedProminent)
                        }
                    }
                }
            }
            .navigationTitle(navigationTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                if purpose != .direct {
                    ToolbarItem(placement: .confirmationAction) {
                        Button(purpose == .group ? "Create" : "Add") {
                            Task { await submit() }
                        }
                        .disabled(!canSubmit || isBusy)
                    }
                }
            }
            .alert(
                "Something went wrong", isPresented: Binding(get: { problemText != nil }, set: { if !$0 { problemText = nil } }),
                presenting: problemText
            ) { _ in
                Button("OK", role: .cancel) {}
            } message: { text in
                Text(text)
            }
            .task { await load() }
        }
    }

    private func load() async {
        do {
            people = try await connection.chatPeople()
            loadProblemText = nil
        } catch {
            loadProblemText = error.loadProblemText
        }
    }

    private var navigationTitle: LocalizedStringKey {
        switch purpose {
        case .direct: "Message someone"
        case .group: "New group"
        case .add: "Add people"
        }
    }

    private func tap(_ person: ChatPerson) {
        switch purpose {
        case .direct:
            chosen = [person.userId]
            Task { await submit() }
        case .group, .add:
            if chosen.contains(person.userId) { chosen.remove(person.userId) } else { chosen.insert(person.userId) }
        }
    }

    private func submit() async {
        guard !isBusy, !chosen.isEmpty else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            try await perform(chosen.sorted(), groupName.trimmingCharacters(in: .whitespacesAndNewlines))
            dismiss()
        } catch {
            problemText = app.describe(error)
        }
    }
}

/// EditMessageSheet is a message of the person's own opened to change what
/// it says.
struct EditMessageSheet: View {
    let line: TranscriptLine
    let model: ConversationModel
    @Environment(\.dismiss) private var dismiss
    @State private var text: String

    init(line: TranscriptLine, model: ConversationModel) {
        self.line = line
        self.model = model
        _text = State(initialValue: line.text)
    }

    private var canSave: Bool {
        let words = text.trimmingCharacters(in: .whitespacesAndNewlines)
        return !words.isEmpty && words != line.text
    }

    var body: some View {
        NavigationStack {
            TextEditor(text: $text)
                .padding(.horizontal)
                .navigationTitle("Edit message")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Save") {
                            let words = text
                            dismiss()
                            Task { await model.edit(line, to: words) }
                        }
                        .disabled(!canSave)
                    }
                }
        }
        .presentationDetents([.medium])
    }
}
