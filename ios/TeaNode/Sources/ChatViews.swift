import SwiftUI
import TeaNodeKit

// Chats in the conversation screens. A chat is one more conversation: it is
// listed with the others, opens in ConversationView and is written to with
// the same composer. These are the parts only a chat needs: its people's
// faces, who has read what, who is typing, and the sheets for starting one
// and changing who is in it.

/// ChatWords are the words a chat is described in.
enum ChatWords {
    static var deletedAccountNote: String { String(localized: "(deleted account)") }

    /// "Ada's agent", in the person's language.
    static func agentOf(_ ownerName: String) -> String { String(localized: "\(ownerName)'s agent") }

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

/// ChatFacesView is a chat's people: the other person's face for a direct
/// chat, two overlapped for a group.
struct ChatFacesView: View {
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
            .foregroundStyle(.white)
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .background(.tint, in: .capsule)
            .accessibilityLabel(Text("\(unreadCount) unread"))
    }
}

/// ReadMarks are the small faces under a message of the people who have
/// read as far as it.
struct ReadMarks: View {
    let readers: [ChatMember]
    private static let mostFaces = 4

    var body: some View {
        if !readers.isEmpty {
            HStack(spacing: -3) {
                ForEach(readers.prefix(Self.mostFaces)) { member in
                    AvatarView(name: member.displayName, avatarUrl: member.avatarUrl, colorIndex: member.avatarColorIndex, size: 14)
                        .overlay(Circle().stroke(Color(.systemBackground), lineWidth: 1))
                }
                if readers.count > Self.mostFaces {
                    Text(verbatim: "+\(readers.count - Self.mostFaces)")
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

/// MentionRow offers the agents an @ in the composer can ask, above the
/// keyboard: the person's own first, then those whose owners let the others
/// ask. A tap puts the agent's name in the words and addresses the message to
/// it.
struct MentionRow: View {
    let agents: [AddressableAgent]
    let onChoose: (AddressableAgent) -> Void

    var body: some View {
        ScrollView(.horizontal) {
            HStack {
                ForEach(agents) { agent in
                    Button {
                        onChoose(agent)
                    } label: {
                        // Another person's agent is named by whose it is, as
                        // it is above its messages; two of them are often both
                        // called Agent.
                        Label(
                            agent.isOwn
                                ? agent.name
                                : AgentSpeaker.name(agentName: agent.name, ownerName: agent.ownerName, ownedWords: ChatWords.agentOf),
                            systemImage: "sparkle")
                    }
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.capsule)
                    .font(.subheadline)
                    .accessibilityHint(agent.isOwn ? String(localized: "Your agent") : String(localized: "\(agent.ownerName)'s agent"))
                }
            }
        }
        .scrollIndicators(.hidden)
        .accessibilityLabel("Agents you can ask here")
    }
}

/// ChatLineFrame puts a message of a chat where it belongs: the person's
/// own on the right, and everybody else's on the left with their face and,
/// above the first message of a run, their name. The bubble is the content.
struct ChatLineFrame<Content: View>: View {
    let line: TranscriptLine
    let author: ChatAuthor
    let startsRun: Bool
    let readers: [ChatMember]
    @ViewBuilder let content: Content

    private static var faceSize: CGFloat { 30 }

    var body: some View {
        if author.isOwnWords {
            VStack(alignment: .trailing, spacing: 4) {
                content
                ReadMarks(readers: readers)
                    .padding(.trailing, 6)
            }
        } else {
            HStack(alignment: .top, spacing: 8) {
                if startsRun {
                    AvatarView(name: name, avatarUrl: author.authorAvatarUrl, colorIndex: author.authorAvatarColorIndex, size: Self.faceSize)
                } else {
                    Color.clear.frame(width: Self.faceSize, height: 1)
                }
                VStack(alignment: .leading, spacing: 3) {
                    if startsRun {
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
                    content
                        // A continuation shows no name, so VoiceOver is told
                        // whose it is, as it is for the first of a run.
                        .overlay(alignment: .topLeading) {
                            if !startsRun {
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

/// ChatMenu is what can be done about the chat on screen: who is in it, its
/// name if it is a group, its sound, and leaving it. A circle's chat follows
/// the circle, so it is neither renamed nor left from here.
struct ChatMenu: View {
    let model: ConversationModel
    @Environment(AppModel.self) private var app
    @State private var isShowingMembers = false
    @State private var isRenaming = false
    @State private var newTitle = ""
    @State private var isConfirmingLeave = false

    /// How long "mute for a while" lasts.
    private static let mutedForSeconds: TimeInterval = 8 * 60 * 60

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
                if chat.isMuted {
                    Button {
                        Task { await model.mute(until: nil) }
                    } label: {
                        Label("Unmute", systemImage: "bell")
                    }
                } else {
                    Button {
                        Task { await model.mute(until: Date().addingTimeInterval(Self.mutedForSeconds)) }
                    } label: {
                        Label("Mute for 8 hours", systemImage: "bell.slash")
                    }
                    Button {
                        Task { await model.mute(until: Date.distantFuture) }
                    } label: {
                        Label("Mute until I unmute", systemImage: "bell.slash")
                    }
                }
                if chat.canBeLeft {
                    Divider()
                    Button(role: .destructive) {
                        isConfirmingLeave = true
                    } label: {
                        Label("Leave group", systemImage: "rectangle.portrait.and.arrow.right")
                    }
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
            .sheet(isPresented: $isShowingMembers) {
                ChatMembersSheet(model: model)
            }
        }
    }
}

/// ChatMembersSheet lists who is in a chat. A group's admins can add people
/// and take them out; a circle's chat follows the circle, so there it is a
/// list.
struct ChatMembersSheet: View {
    let model: ConversationModel
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
                            }
                        }
                    }
                    if let own = chat.member(model.ownUserId ?? ""), !own.isRemoved, !chat.isRemoved {
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
        .presentationDetents([.medium, .large])
    }

    /// What the person's agent does in this chat, as two switches that are the
    /// person's alone, each with the sentence that says what it means.
    private func agentSection(_ own: ChatMember) -> some View {
        Section {
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
        } header: {
            Text("Your agent in this chat")
        } footer: {
            Text(
                "Remembering: from now on your agent reads what is said here with the rest of what it knows. What it learned stays if you turn this off, and it reads nothing said while this is off.\n\nAsking: anyone in this chat can address your agent with @, and it answers them in front of everybody. It can read this chat and look things up on the web, and nothing of yours: no mail, notes, files or calendar. Its work is counted as yours."
            )
        }
    }

    private func row(_ member: ChatMember, in chat: ChatSummary, canRemove: Bool) -> some View {
        HStack(spacing: 12) {
            AvatarView(name: member.displayName, avatarUrl: member.avatarUrl, colorIndex: member.avatarColorIndex, size: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(ChatWords.name(of: member))
                if member.isAdmin, chat.isGroup {
                    Text("Admin")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
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
            Spacer(minLength: 0)
        }
        .swipeActions(edge: .trailing) {
            if canRemove {
                Button(role: .destructive) {
                    removing = member
                } label: {
                    Label("Remove", systemImage: "person.badge.minus")
                }
            }
        }
        .contextMenu {
            if canRemove {
                Button(role: .destructive) {
                    removing = member
                } label: {
                    Label("Remove", systemImage: "person.badge.minus")
                }
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
                if people == nil {
                    ProgressView()
                } else if choices.isEmpty {
                    ContentUnavailableView(
                        "No one to message yet", systemImage: "person.2",
                        description: Text("You do not share a circle with anyone yet. Invite people to a circle, then message them from here."))
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
            .task {
                do {
                    people = try await connection.chatPeople()
                } catch {
                    people = []
                    problemText = app.describe(error)
                }
            }
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
