import AVFoundation
import SwiftUI
import TeaNodeKit

/// AgentSettingsModel holds the agent's settings while their screens are
/// open: read when the first one opens, and replaced by what each change
/// answers with, so every screen shows what the server keeps.
@MainActor
@Observable
final class AgentSettingsModel {
    let connection: Connection
    var settings: AgentSettings?
    /// The voices answers may be read in; nil where the server does not
    /// speak.
    var voices: VoiceAvailability?
    var isLoaded = false
    var doneText: String?
    var problemText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    var agent: AgentSettings.Agent? { settings?.agent }

    func load() async {
        do {
            settings = try await connection.agentSettings()
        } catch {
            problemText = error.localizedDescription
        }
        // The voices are offered only where the server speaks; a refusal
        // hides them, as the dashboard does.
        if let available = try? await connection.speechVoices(), available.isVoiceAvailable {
            voices = available
        } else {
            voices = nil
        }
        isLoaded = true
    }

    /// Runs a change that answers with the settings, and says what it did.
    func change(saying said: String, _ work: @MainActor () async throws -> AgentSettings) async -> Bool {
        do {
            settings = try await work()
            doneText = said
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    func update(_ agentChange: AgentChange, saying said: String) async -> Bool {
        await change(saying: said) { try await connection.updateAgent(agentChange) }
    }

    /// Saves a mailbox's whole policy, as each of its forms does.
    func savePolicy(_ policy: AgentSettings.MailboxPolicy, mailboxId: String) async -> Bool {
        await change(saying: String(localized: "Saved")) { try await connection.grantAgentMailbox(mailboxId, policy: policy) }
    }
}

/// AgentSettingsView is the dashboard's agent page, its Overview and its
/// Mail and calendars tabs, as a list of settings with a screen each.
struct AgentSettingsView: View {
    @State private var model: AgentSettingsModel
    @State private var isConfirmingForget = false

    init(connection: Connection) {
        _model = State(initialValue: AgentSettingsModel(connection: connection))
    }

    var body: some View {
        List {
            if let settings = model.settings, settings.allowed.enabled {
                if let agent = settings.agent {
                    agentSections(settings, agent: agent)
                } else {
                    Section {
                        Text("An agent of your own: it can sort what arrives in your mailbox, summarize conversations, draft and, if you let it, answer replies, and do for you whatever you could do yourself. It is off until you turn it on, and it reaches nothing until you grant it a mailbox.")
                            .foregroundStyle(Color(.secondaryLabel))
                        Button("Turn on my agent") {
                            Task { _ = await model.update(AgentChange(enabled: true), saying: String(localized: "Your agent is on.")) }
                        }
                    }
                }
            }
        }
        .overlay {
            if !model.isLoaded {
                ProgressView()
            } else if let settings = model.settings, !settings.allowed.enabled {
                ContentUnavailableView {
                    Label("Your agent", systemImage: "person.crop.circle.badge.xmark")
                } description: {
                    Text("The operator has not enabled agents on this server.")
                }
            } else if model.settings == nil {
                ContentUnavailableView {
                    Label("Your agent could not be read", systemImage: "exclamationmark.triangle")
                } description: {
                    Text("Check the connection, then try again.")
                } actions: {
                    Button("Try again") {
                        Task { await model.load() }
                    }
                }
            }
        }
        .navigationTitle("Your agent")
        .task {
            if !model.isLoaded { await model.load() }
        }
        .refreshable { await model.load() }
        .confirmationDialog("Forget everything", isPresented: $isConfirmingForget, titleVisibility: .visible) {
            Button("Forget everything", role: .destructive) {
                Task {
                    _ = await model.change(saying: String(localized: "Forgotten.")) { try await model.connection.forgetAgent() }
                }
            }
        } message: {
            Text("This deletes your agent and everything it learned: memories, conversations, run records, and every mailbox's insights and summaries. Your mail is untouched.")
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    @ViewBuilder
    private func agentSections(_ settings: AgentSettings, agent: AgentSettings.Agent) -> some View {
        Section {
            NavigationLink {
                AgentAboutView(model: model)
            } label: {
                LabeledContent {
                    Text(agent.enabled ? String(localized: "On") : String(localized: "Off"))
                } label: {
                    Label("About your agent", systemImage: "person.crop.circle")
                }
            }
            NavigationLink {
                AgentInstructionsView(model: model)
            } label: {
                Label("Standing instructions", systemImage: "text.quote")
            }
            NavigationLink {
                AgentLanguageView(model: model)
            } label: {
                LabeledContent {
                    Text(AgentSettingsWords.languageName(settings.language))
                } label: {
                    Label("Language", systemImage: "globe")
                }
            }
            NavigationLink {
                AgentWritingVoiceView(model: model)
            } label: {
                Label("Voice", systemImage: "text.bubble")
            }
            if let voices = model.voices {
                NavigationLink {
                    AgentSpeechVoiceView(model: model)
                } label: {
                    LabeledContent {
                        Text(verbatim: AgentSettingsWords.heardVoice(agent: agent, voices: voices))
                    } label: {
                        Label("Speaking voice", systemImage: "speaker.wave.2")
                    }
                }
            }
        } footer: {
            if agent.isOperatorDisabled {
                Label("Switched off by your administrator. It stays off until they lift that.", systemImage: "exclamationmark.triangle")
            }
        }
        Section("Mail and calendars") {
            NavigationLink {
                AgentSourcesView(model: model)
            } label: {
                LabeledContent {
                    Text(settings.sources.filter(\.isGranted).count + settings.collections.filter(\.granted).count, format: .number)
                } label: {
                    Label("What it may reach", systemImage: "tray.2")
                }
            }
            NavigationLink {
                AgentCategoriesView(model: model)
            } label: {
                LabeledContent {
                    Text(agent.categories.count, format: .number)
                } label: {
                    Label("Your own categories", systemImage: "tag")
                }
            }
        }
        Section("Advanced") {
            if !settings.choices.isEmpty {
                NavigationLink {
                    AgentModelView(model: model)
                } label: {
                    LabeledContent {
                        Text(verbatim: (agent.askModel ?? "").isEmpty ? String(localized: "The server's choice") : agent.askModel ?? "")
                    } label: {
                        Label("Model for conversations", systemImage: "cpu")
                    }
                }
            }
            NavigationLink {
                AgentSpeakFirstView(model: model)
            } label: {
                Label("Speaking first", systemImage: "bubble.left.and.text.bubble.right")
            }
            NavigationLink {
                AgentConfirmView(model: model)
            } label: {
                LabeledContent {
                    Text(agent.confirm.count, format: .number)
                } label: {
                    Label("Ask me first", systemImage: "hand.raised")
                }
            }
            NavigationLink {
                AgentUnattendedView(model: model)
            } label: {
                LabeledContent {
                    Text(AgentUnattendedDraft(agent).orderedAllowedRisks.count, format: .number)
                } label: {
                    Label("When you are not there", systemImage: "moon")
                }
            }
        }
        Section {
            Button("Forget everything", role: .destructive) { isConfirmingForget = true }
        } footer: {
            Text("Removes the agent with its memory, conversations, schedules and connections. Your mail is untouched.")
        }
    }
}

/// AgentSettingsWords are the words the agent's settings are shown in, the
/// dashboard's.
enum AgentSettingsWords {
    /// A language by its own name, or its code when it is not one offered.
    static func languageName(_ code: String) -> String {
        if code.isEmpty { return String(localized: "Same as my account") }
        return AgentSettings.languages.first { $0.code == code }?.name ?? code
    }

    /// The voice answers are read in: the person's, or the server's.
    static func heardVoice(agent: AgentSettings.Agent, voices: VoiceAvailability) -> String {
        let chosen = agent.speechVoice ?? ""
        return chosen.isEmpty ? voices.serverSpeechVoice ?? "" : chosen
    }

    static func unattendedTitle(_ unattendedRisk: String) -> String {
        switch unattendedRisk {
        case "outward": String(localized: "Speak for you")
        case "money": String(localized: "Spend or move money")
        case "destructive": String(localized: "Do what cannot be undone")
        case "granting": String(localized: "Give access")
        default: String(localized: "Use the tools under Ask me first")
        }
    }

    static func unattendedHint(_ unattendedRisk: String) -> String {
        switch unattendedRisk {
        case "outward": String(localized: "Send mail, post a message or reply as you, or act in your browser tab.")
        case "money": String(localized: "Pay, buy, sell, trade, or place or cancel an order, a transfer or a payment.")
        case "destructive": String(localized: "Delete for good, overwrite, move or delete files, and commands judged destructive.")
        case "granting": String(localized: "Share something, or give someone access to what is yours or take it away.")
        default: String(localized: "The tools you listed to always ask about. Those the operator listed always wait for you.")
        }
    }

    /// What a calendar or an address book holds, in a line.
    static func collectionSummary(_ collection: AgentSettings.CollectionSource) -> String {
        if collection.isCalendar {
            return String(localized: "Calendar") + " · " + String(localized: "\(collection.items) events")
        }
        return String(localized: "Address book") + " · " + String(localized: "\(collection.items) people")
    }

    static func onOrOff(_ isOn: Bool) -> String {
        isOn ? String(localized: "On") : String(localized: "Off")
    }
}

/// AgentSaveToolbar is the Save of a settings form: a spinner while it
/// saves, and nothing to press until there is something to save.
struct AgentSaveToolbar: ToolbarContent {
    let isSaving: Bool
    let canSave: Bool
    let save: () -> Void

    var body: some ToolbarContent {
        ToolbarItem(placement: .confirmationAction) {
            if isSaving {
                ProgressView()
            } else {
                Button("Save", action: save).disabled(!canSave)
            }
        }
    }
}

/// AgentSwitch is a switch saved as it is flipped: it shows the new
/// position while the server takes it, and goes back if it is refused.
struct AgentSwitch<SwitchLabel: View>: View {
    let model: AgentSettingsModel
    let isOn: Bool
    let change: (Bool) -> AgentChange
    let saying: (Bool) -> String
    @ViewBuilder let label: () -> SwitchLabel
    @State private var pendingIsOn: Bool?

    var body: some View {
        Toggle(isOn: Binding(get: { pendingIsOn ?? isOn }, set: { newIsOn in
            pendingIsOn = newIsOn
            let agentChange = change(newIsOn)
            let said = saying(newIsOn)
            Task {
                _ = await model.update(agentChange, saying: said)
                pendingIsOn = nil
            }
        }), label: label)
    }
}

// MARK: About the agent

/// AgentAboutView is the agent itself: on or off, what to call it, and
/// today's use of its budget.
struct AgentAboutView: View {
    let model: AgentSettingsModel
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            if let settings = model.settings, let agent = settings.agent {
                Section {
                    AgentSwitch(
                        model: model, isOn: agent.enabled, change: { AgentChange(enabled: $0) },
                        saying: { isOn in
                            isOn
                                ? String(localized: "Your agent is on.")
                                : String(localized: "Your agent is off. Queued work was cancelled; what it learned is kept.")
                        }
                    ) {
                        Text("On")
                    }
                    .disabled(agent.isOperatorDisabled)
                } footer: {
                    if agent.isOperatorDisabled {
                        Label("Switched off by your administrator. It stays off until they lift that.", systemImage: "exclamationmark.triangle")
                    }
                }
                Section("What to call it") {
                    TextField("What to call it", text: $name)
                        .onChange(of: name) { _, typed in
                            if typed.count > AgentSettings.agentNameLength { name = String(typed.prefix(AgentSettings.agentNameLength)) }
                        }
                }
                Section("Today") {
                    if let budget = settings.budget {
                        BudgetSummary(budget: budget, timezone: settings.timezone)
                    } else {
                        Text("No daily budget is set.").foregroundStyle(Color(.secondaryLabel))
                    }
                    LabeledContent("Time zone", value: settings.timezone)
                    LabeledContent("Language", value: AgentSettingsWords.languageName(settings.language))
                }
            }
        }
        .navigationTitle("About your agent")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.agent != nil && name != model.agent?.name) { save() }
        }
        .onAppear {
            guard !isFilled, let agent = model.agent else { return }
            name = agent.name
            isFilled = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func save() {
        isSaving = true
        let agentChange = AgentChange(name: name)
        Task {
            let isSaved = await model.update(agentChange, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentInstructionsView is what the agent is always told about the
/// person: who they are, how they write, what matters to them.
struct AgentInstructionsView: View {
    let model: AgentSettingsModel
    @Environment(\.dismiss) private var dismiss
    @State private var instructions = ""
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            Section {
                TextField(
                    "Standing instructions", text: $instructions,
                    prompt: Text("Who you are, how you write, what matters to you. \"I am the treasurer of the sailing club; anything from the club is important; sign as Z.\""),
                    axis: .vertical
                )
                .lineLimit(8...)
            }
        }
        .navigationTitle("Standing instructions")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.agent != nil && instructions != (model.agent?.instructions ?? "")) { save() }
        }
        .onAppear {
            guard !isFilled, let agent = model.agent else { return }
            instructions = agent.instructions ?? ""
            isFilled = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func save() {
        isSaving = true
        let agentChange = AgentChange(instructions: instructions)
        Task {
            let isSaved = await model.update(agentChange, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentLanguageView is the language the agent talks in, and the one it
/// works in.
struct AgentLanguageView: View {
    let model: AgentSettingsModel
    @Environment(\.dismiss) private var dismiss
    @State private var language = ""
    @State private var knowledgeLanguage = ""
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            Section("Talks with you in") {
                AgentLanguagePicker(title: "Talks with you in", followingTitle: "Same as my account", language: $language)
            }
            Section {
                AgentLanguagePicker(title: "Works in", followingTitle: "Same as it talks in, if set", language: $knowledgeLanguage)
            } header: {
                Text("Works in")
            } footer: {
                Text("Conversations follow the language you talk and read the dashboard in. Everything it does on its own (notes, sorting, summaries, research, drafts and briefs) is written in this one language, so what it keeps stays in one language.")
            }
        }
        .navigationTitle("Language")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.agent != nil && hasChanges) { save() }
        }
        .onAppear {
            guard !isFilled, let agent = model.agent else { return }
            language = agent.language ?? ""
            knowledgeLanguage = agent.knowledgeLanguage ?? ""
            isFilled = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private var hasChanges: Bool {
        language != (model.agent?.language ?? "") || knowledgeLanguage != (model.agent?.knowledgeLanguage ?? "")
    }

    private func save() {
        isSaving = true
        let agentChange = AgentChange(language: language, knowledgeLanguage: knowledgeLanguage)
        Task {
            let isSaved = await model.update(agentChange, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentLanguagePicker chooses a language by its own name, or follows
/// another when none is chosen; a language not listed is typed as its code,
/// as the dashboard allows.
struct AgentLanguagePicker: View {
    let title: LocalizedStringKey
    let followingTitle: LocalizedStringKey
    @Binding var language: String
    @State private var isTypingCode = false

    /// The choice that opens the field for a code.
    private static let otherLanguageTag = "\u{1}other"

    private var isOtherLanguage: Bool {
        isTypingCode || (!language.isEmpty && !AgentSettings.languages.contains { $0.code == language })
    }

    var body: some View {
        Picker(title, selection: Binding(get: { isOtherLanguage ? Self.otherLanguageTag : language }, set: { chosen in
            if chosen == Self.otherLanguageTag {
                isTypingCode = true
            } else {
                isTypingCode = false
                language = chosen
            }
        })) {
            Text(followingTitle).tag("")
            ForEach(AgentSettings.languages) { choice in
                Text(verbatim: choice.name).tag(choice.code)
            }
            Text("Another language").tag(Self.otherLanguageTag)
        }
        if isOtherLanguage {
            TextField("Language code", text: $language, prompt: Text(verbatim: "sv"))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        }
    }
}

/// AgentWritingVoiceView is how the agent sounds when it writes for the
/// person: the replies it drafts and the answers it sends.
struct AgentWritingVoiceView: View {
    let model: AgentSettingsModel
    @Environment(\.dismiss) private var dismiss
    @State private var draft = AgentVoiceDraft(nil)
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            Section {
                Picker("Tone", selection: $draft.tone) {
                    Text("Neutral").tag("")
                    Text("Formal").tag("formal")
                    Text("Casual").tag("casual")
                }
                Picker("Length", selection: $draft.length) {
                    Text("Medium").tag("")
                    Text("Short").tag("short")
                    Text("Long").tag("long")
                }
            } footer: {
                Text("How it sounds when it writes for you: replies it drafts and answers it sends.")
            }
            Section {
                TextField("Greeting", text: $draft.greeting)
                TextField("Sign-off", text: $draft.signoff)
            }
        }
        .navigationTitle("Voice")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.agent != nil && draft != AgentVoiceDraft(model.agent?.voice)) { save() }
        }
        .onAppear {
            guard !isFilled, let agent = model.agent else { return }
            draft = AgentVoiceDraft(agent.voice)
            isFilled = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func save() {
        isSaving = true
        let agentChange = AgentChange(voice: draft.voice)
        Task {
            let isSaved = await model.update(agentChange, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentSpeechVoiceView is the voice answers are read aloud in on a call,
/// chosen as it is picked, with a few words in it to hear first.
struct AgentSpeechVoiceView: View {
    let model: AgentSettingsModel
    @State private var chosenVoice: String?
    @State private var player: AVAudioPlayer?
    @State private var isPlaying = false

    var body: some View {
        Form {
            if let voices = model.voices, let agent = model.agent {
                Section {
                    Picker("Speaking voice", selection: Binding(get: { chosenVoice ?? agent.speechVoice ?? "" }, set: { choose($0) })) {
                        Text("The server's (\(voices.serverSpeechVoice ?? ""))").tag("")
                        ForEach(voices.speechVoices ?? [], id: \.self) { speechVoice in
                            Text(verbatim: speechVoice).tag(speechVoice)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                } footer: {
                    Text("The voice your answers are read aloud in when you call your agent. You can also ask the agent to change it mid-call.")
                }
                Section {
                    Button {
                        let chosen = chosenVoice ?? agent.speechVoice ?? ""
                        let heard = chosen.isEmpty ? voices.serverSpeechVoice ?? "" : chosen
                        Task { await play(heard) }
                    } label: {
                        Label(isPlaying ? String(localized: "Playing…") : String(localized: "Listen"), systemImage: "speaker.wave.2")
                    }
                    .disabled(isPlaying)
                }
            }
        }
        .navigationTitle("Speaking voice")
        .onDisappear { player?.stop() }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func choose(_ speechVoice: String) {
        chosenVoice = speechVoice
        Task {
            _ = await model.update(AgentChange(speechVoice: speechVoice), saying: String(localized: "Saved"))
            chosenVoice = nil
        }
    }

    /// Plays the server's sample of a voice. It is paid for like any speech.
    private func play(_ speechVoice: String) async {
        isPlaying = true
        do {
            let sample = try await model.connection.speechVoiceSample(speechVoice)
            try AVAudioSession.sharedInstance().setCategory(.playback)
            let samplePlayer = try AVAudioPlayer(data: sample)
            player = samplePlayer
            samplePlayer.play()
            try? await Task.sleep(for: .seconds(samplePlayer.duration))
        } catch {
            model.problemText = error.localizedDescription
        }
        isPlaying = false
    }
}

// MARK: Advanced

/// AgentModelView is the model conversations use, chosen as it is picked.
struct AgentModelView: View {
    let model: AgentSettingsModel
    @State private var chosenModel: String?

    var body: some View {
        Form {
            if let settings = model.settings, let agent = settings.agent {
                Section {
                    Picker("Model for conversations", selection: Binding(get: { chosenModel ?? agent.askModel ?? "" }, set: { choose($0) })) {
                        Text("The server's choice").tag("")
                        ForEach(settings.choices, id: \.self) { choice in
                            Text(verbatim: choice).tag(choice)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
            }
        }
        .navigationTitle("Model for conversations")
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func choose(_ askModel: String) {
        chosenModel = askModel
        Task {
            _ = await model.update(AgentChange(askModel: askModel), saying: String(localized: "Saved"))
            chosenModel = nil
        }
    }
}

/// AgentSpeakFirstView is the two ways the agent may start a conversation
/// of its own, each a switch saved as it is flipped.
struct AgentSpeakFirstView: View {
    let model: AgentSettingsModel

    var body: some View {
        Form {
            if let agent = model.agent {
                Section {
                    AgentSwitch(
                        model: model, isOn: agent.isMemoryCheckEnabled, change: { AgentChange(isMemoryCheckEnabled: $0) },
                        saying: { $0 ? String(localized: "Memory checks are on.") : String(localized: "Memory checks are off.") }
                    ) {
                        Text("Memory checks")
                    }
                } footer: {
                    Text("Now and then your agent asks to check a few things it remembers about you, and keeps your answers to grade itself against.")
                }
                Section {
                    AgentSwitch(
                        model: model, isOn: agent.isIdeasEnabled, change: { AgentChange(isIdeasEnabled: $0) },
                        saying: { $0 ? String(localized: "Your agent will offer ideas.") : String(localized: "Your agent will not offer ideas on its own.") }
                    ) {
                        Text("Offer ideas")
                    }
                } footer: {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("While you are idle, your agent now and then offers one of its ideas: something it could do for you. The whole list is under Ideas.")
                        Text("Besides introducing itself, your agent can start a conversation in two ways. It only does so while you have the dashboard open, and at most once a day.")
                    }
                }
            }
        }
        .navigationTitle("Speaking first")
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }
}

/// AgentConfirmView is the tools the agent must always ask about first, by
/// family: each family and each tool either as usual or ask me first, a
/// tool left as usual following its family.
struct AgentConfirmView: View {
    let model: AgentSettingsModel
    @Environment(\.dismiss) private var dismiss
    @State private var tools: [AgentTool] = []
    @State private var isToolsLoaded = false
    @State private var draft = AgentConfirmDraft(confirm: [])
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            Section {
                Text("Beyond what always needs a yes (sending, deleting, anything outward), the tools it must ask about every time.")
                    .foregroundStyle(Color(.secondaryLabel))
            }
            ForEach(AgentTool.families(tools), id: \.self) { family in
                let members = tools.filter { $0.family == family }
                Section {
                    Picker(selection: askFirstBinding(family)) {
                        Text("As usual").tag(false)
                        Text("Ask me first").tag(true)
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: family).foregroundStyle(Color(.label))
                            Text(members.count == 1 ? String(localized: "1 tool") : String(localized: "\(members.count) tools"))
                                .font(.footnote)
                                .foregroundStyle(Color(.secondaryLabel))
                        }
                    }
                    DisclosureGroup("Tools") {
                        ForEach(members) { tool in
                            toolRow(tool, isFamilyAskingFirst: draft.isAskingFirst(family))
                        }
                        if members.isEmpty || family == "servers" {
                            Text("Connected servers add their own tools to this family as they connect, named the way they name themselves; the family word covers all of them.")
                                .font(.footnote)
                                .foregroundStyle(Color(.secondaryLabel))
                        }
                    }
                }
            }
        }
        .overlay {
            if !isToolsLoaded { ProgressView() }
        }
        .navigationTitle("Ask me first")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.agent != nil && draft != AgentConfirmDraft(confirm: model.agent?.confirm ?? [])) {
                save()
            }
        }
        .onAppear {
            guard !isFilled, let agent = model.agent else { return }
            draft = AgentConfirmDraft(confirm: agent.confirm)
            isFilled = true
        }
        .task {
            guard !isToolsLoaded else { return }
            do {
                tools = try await model.connection.agentTools()
            } catch {
                model.problemText = error.localizedDescription
            }
            isToolsLoaded = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func askFirstBinding(_ name: String) -> Binding<Bool> {
        Binding(get: { draft.isAskingFirst(name) }, set: { draft.setAskingFirst(name, isAskingFirst: $0) })
    }

    /// One tool: its name, what it does and the verbs it takes, and its
    /// word, which as usual follows the family's.
    private func toolRow(_ tool: AgentTool, isFamilyAskingFirst: Bool) -> some View {
        Picker(selection: askFirstBinding(tool.name)) {
            Text(isFamilyAskingFirst ? String(localized: "As the family: \(String(localized: "Ask me first"))") : String(localized: "As usual"))
                .tag(false)
            Text("Ask me first").tag(true)
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(verbatim: tool.name).foregroundStyle(Color(.label))
                    if tool.confirms {
                        Text("always asks").font(.caption).foregroundStyle(Color(.secondaryLabel))
                    }
                }
                if !tool.description.isEmpty {
                    Text(verbatim: tool.description).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                }
                if let actions = tool.actions, !actions.isEmpty {
                    Text(verbatim: actions.joined(separator: " · ")).font(.caption.monospaced()).foregroundStyle(Color(.secondaryLabel))
                }
            }
        }
    }

    private func save() {
        isSaving = true
        let agentChange = AgentChange(confirm: draft.askFirstNames)
        Task {
            let isSaved = await model.update(agentChange, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentUnattendedView is which kinds of action needing the person's word
/// the agent may take when they are not there to give it. Each is off
/// until they turn it on.
struct AgentUnattendedView: View {
    let model: AgentSettingsModel
    @Environment(\.dismiss) private var dismiss
    @State private var draft = AgentUnattendedDraft(nil)
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            Section {
                ForEach(AgentSettings.unattendedRisks, id: \.self) { unattendedRisk in
                    Toggle(isOn: Binding(get: { draft.isAllowed(unattendedRisk) }, set: { draft.setAllowed(unattendedRisk, isAllowed: $0) })) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(AgentSettingsWords.unattendedTitle(unattendedRisk)).foregroundStyle(Color(.label))
                            Text(AgentSettingsWords.unattendedHint(unattendedRisk)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                        }
                    }
                }
            } footer: {
                Text("What your agent may do without asking, in a schedule, a goal or overnight, when there is nobody to confirm. Anything not ticked is refused then, and your agent tells you what it would have done. Mail sorting and replies drafted from incoming mail only ever read, whatever is ticked.")
            }
        }
        .navigationTitle("When you are not there")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.agent != nil && draft != AgentUnattendedDraft(model.agent)) { save() }
        }
        .onAppear {
            guard !isFilled, let agent = model.agent else { return }
            draft = AgentUnattendedDraft(agent)
            isFilled = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func save() {
        isSaving = true
        let agentChange = AgentChange(unattendedAllowedRisks: draft.orderedAllowedRisks)
        Task {
            let isSaved = await model.update(agentChange, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

// MARK: Mail and calendars

/// AgentSourcesView is everything the agent may be given: each mailbox,
/// opening on what the agent does there, and each calendar and address
/// book, a switch.
struct AgentSourcesView: View {
    let model: AgentSettingsModel
    /// The switches flipped and not yet answered, by collection.
    @State private var pendingGrants: [String: Bool] = [:]

    var body: some View {
        List {
            if let settings = model.settings {
                Section {
                    ForEach(settings.sources) { source in
                        NavigationLink {
                            AgentMailboxView(model: model, mailboxId: source.mailboxId)
                        } label: {
                            LabeledContent {
                                Text(AgentSettingsWords.onOrOff(source.isGranted))
                            } label: {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(verbatim: source.name)
                                    Text(verbatim: source.addresses.joined(separator: ", "))
                                        .font(.footnote)
                                        .foregroundStyle(Color(.secondaryLabel))
                                }
                            }
                        }
                    }
                } header: {
                    Text("Mailboxes")
                } footer: {
                    Text("Each mailbox you own, and what the agent does there. Nothing from a mailbox you have not granted is ever sent to a model.")
                }
                if !settings.collections.isEmpty {
                    Section("Calendars and address books") {
                        ForEach(settings.collections) { collection in
                            Toggle(isOn: grantBinding(collection)) {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(verbatim: collection.name).foregroundStyle(Color(.label))
                                    Text(AgentSettingsWords.collectionSummary(collection))
                                        .font(.footnote)
                                        .foregroundStyle(Color(.secondaryLabel))
                                }
                            }
                            .disabled(settings.agent == nil)
                        }
                    }
                }
            }
        }
        .navigationTitle("What it may reach")
        .refreshable { await model.load() }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func grantBinding(_ collection: AgentSettings.CollectionSource) -> Binding<Bool> {
        Binding(get: { pendingGrants[collection.id] ?? collection.granted }, set: { isGranted in
            pendingGrants[collection.id] = isGranted
            let said = isGranted ? String(localized: "It can read this now.") : String(localized: "It can no longer read this.")
            Task {
                _ = await model.change(saying: said) { try await model.connection.grantAgentCollection(collection, isGranted: isGranted) }
                pendingGrants[collection.id] = nil
            }
        })
    }
}

/// AgentMailboxView is one mailbox as a source: whether the agent may reach
/// it, and what it does there, one subject a screen.
struct AgentMailboxView: View {
    let model: AgentSettingsModel
    let mailboxId: String
    @State private var isWorking = false
    @State private var isConfirmingRevoke = false

    var body: some View {
        List {
            if let source = model.settings?.source(mailboxId) {
                if source.isGranted {
                    let policy = source.shownPolicy
                    Section {
                        NavigationLink {
                            AgentSortingView(model: model, mailboxId: mailboxId)
                        } label: {
                            LabeledContent {
                                Text(AgentSettingsWords.onOrOff(policy.triage?.enabled == true))
                            } label: {
                                Label("Sorting", systemImage: "line.3.horizontal.decrease")
                            }
                        }
                        NavigationLink {
                            AgentSummariesView(model: model, mailboxId: mailboxId)
                        } label: {
                            LabeledContent {
                                Text(AgentSettingsWords.onOrOff(policy.summaries?.enabled == true))
                            } label: {
                                Label("Summaries", systemImage: "text.alignleft")
                            }
                        }
                        NavigationLink {
                            AgentMailHelpView(model: model, mailboxId: mailboxId)
                        } label: {
                            Label("Replies and search", systemImage: "magnifyingglass")
                        }
                        NavigationLink {
                            AgentAnsweringView(model: model, mailboxId: mailboxId)
                        } label: {
                            LabeledContent {
                                Text(AgentSettingsWords.onOrOff(policy.autoReply?.enabled == true))
                            } label: {
                                Label("Answering for me", systemImage: "arrowshape.turn.up.left")
                            }
                        }
                    } footer: {
                        Text(verbatim: source.addresses.joined(separator: ", "))
                    }
                    Section {
                        Button("Revoke", role: .destructive) { isConfirmingRevoke = true }
                            .disabled(isWorking)
                    }
                } else {
                    Section {
                        Button("Let it reach this mailbox") {
                            Task { await grant() }
                        }
                        .disabled(isWorking || model.agent == nil)
                    } footer: {
                        Text("The agent cannot reach this mailbox.")
                    }
                }
            }
        }
        .navigationTitle(model.settings?.source(mailboxId)?.name ?? String(localized: "Mailbox"))
        .confirmationDialog("Revoke", isPresented: $isConfirmingRevoke, titleVisibility: .visible) {
            Button("Revoke", role: .destructive) {
                Task { await revoke() }
            }
        } message: {
            Text("Take \(model.settings?.source(mailboxId)?.name ?? "") away from the agent? Queued work for it is cancelled; what it has sorted stays.")
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func grant() async {
        isWorking = true
        _ = await model.change(saying: String(localized: "Granted. Sorting begins with the next message.")) {
            try await model.connection.grantAgentMailbox(mailboxId)
        }
        isWorking = false
    }

    private func revoke() async {
        isWorking = true
        _ = await model.change(saying: String(localized: "Revoked. Queued work for this mailbox was cancelled.")) {
            try await model.connection.revokeAgentMailbox(mailboxId)
        }
        isWorking = false
    }
}

/// AgentSortingView is how the agent sorts what arrives in one mailbox, and
/// whether it tells the person about what cannot wait.
struct AgentSortingView: View {
    let model: AgentSettingsModel
    let mailboxId: String
    @Environment(\.dismiss) private var dismiss
    @State private var triage = AgentSettings.MailboxPolicy.Triage()
    @State private var isAlerting = true
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            let allowed = model.settings?.allowed ?? .nothing
            Section {
                Toggle("Sort what arrives: category, priority, whether a reply is needed", isOn: $triage.enabled)
                    .disabled(!allowed.triage)
                // Alerts follow the sorting: without it there is nothing to
                // tell.
                Toggle(
                    "Tell me about what arrives here that I should know now",
                    isOn: Binding(get: { isAlerting && triage.enabled }, set: { isAlerting = $0 })
                )
                .disabled(!allowed.triage || !triage.enabled)
            }
            Section {
                Picker("Needs a reply when", selection: Binding(get: { Self.chosen(triage.replyExpectation, or: "direct") }, set: { triage.replyExpectation = $0 })) {
                    Text("I am addressed directly").tag("direct")
                    Text("Anything, copies included").tag("any")
                }
                Picker("Mail already here", selection: Binding(get: { Self.chosen(triage.backfill, or: "recent") }, set: { triage.backfill = $0 })) {
                    Text("Sort the newest 200").tag("recent")
                    Text("Leave it").tag("none")
                    Text("Sort everything, over days").tag("all")
                }
            }
        }
        .navigationTitle("Sorting")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.settings?.source(mailboxId) != nil) { save() }
        }
        .onAppear {
            guard !isFilled, let policy = model.settings?.source(mailboxId)?.shownPolicy else { return }
            triage = policy.triage ?? AgentSettings.MailboxPolicy.Triage()
            isAlerting = policy.isAlertsEnabled
            isFilled = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    /// A word as a picker shows it: what is stored, or the default the
    /// server reads an empty one as.
    static func chosen(_ stored: String?, or defaultWord: String) -> String {
        let word = stored ?? ""
        return word.isEmpty ? defaultWord : word
    }

    private func save() {
        guard var policy = model.settings?.source(mailboxId)?.shownPolicy else { return }
        policy.triage = triage
        policy.alerts = AgentSettings.MailboxPolicy.Alerts(enabled: isAlerting)
        let saving = policy
        isSaving = true
        Task {
            let isSaved = await model.savePolicy(saving, mailboxId: mailboxId)
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentSummariesView is whether the agent summarizes the conversations in
/// one mailbox, from how long, and how.
struct AgentSummariesView: View {
    let model: AgentSettingsModel
    let mailboxId: String
    @Environment(\.dismiss) private var dismiss
    @State private var summaries = AgentSettings.MailboxPolicy.Summaries()
    @State private var minimumMessagesText = ""
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            Section {
                Toggle("Summarize conversations", isOn: $summaries.enabled)
                    .disabled(!(model.settings?.allowed.summaries ?? false))
            }
            Section {
                LabeledContent("From how many messages") {
                    TextField("From how many messages", text: $minimumMessagesText, prompt: Text(verbatim: "3"))
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                }
                Picker("Style", selection: Binding(get: { AgentSortingView.chosen(summaries.style, or: "brief") }, set: { summaries.style = $0 })) {
                    Text("Brief").tag("brief")
                    Text("Detailed").tag("detailed")
                }
            }
        }
        .navigationTitle("Summaries")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.settings?.source(mailboxId) != nil) { save() }
        }
        .onAppear {
            guard !isFilled, let policy = model.settings?.source(mailboxId)?.shownPolicy else { return }
            summaries = policy.summaries ?? AgentSettings.MailboxPolicy.Summaries()
            let minimumMessages = summaries.minimumMessages ?? 0
            minimumMessagesText = minimumMessages > 0 ? String(minimumMessages) : ""
            isFilled = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func save() {
        guard var policy = model.settings?.source(mailboxId)?.shownPolicy else { return }
        var saved = summaries
        saved.minimumMessages = AgentSettings.wholeNumber(minimumMessagesText)
        policy.summaries = saved
        let saving = policy
        isSaving = true
        Task {
            let isSaved = await model.savePolicy(saving, mailboxId: mailboxId)
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentMailHelpView is what the agent does in one mailbox when asked, or
/// when its sorting says so: drafting replies, searching by meaning, and
/// looking things up.
struct AgentMailHelpView: View {
    let model: AgentSettingsModel
    let mailboxId: String
    @Environment(\.dismiss) private var dismiss
    @State private var isDraftingReplies = true
    @State private var isSearching = true
    @State private var isResearching = false
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            let allowed = model.settings?.allowed ?? .nothing
            Section {
                Toggle("Draft a reply when I ask", isOn: $isDraftingReplies).disabled(!allowed.draftReplies)
                Toggle("Search by meaning", isOn: $isSearching).disabled(!allowed.search)
                Toggle("Look things up for a message when sorting says it would help", isOn: $isResearching).disabled(!allowed.research)
            }
        }
        .navigationTitle("Replies and search")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.settings?.source(mailboxId) != nil) { save() }
        }
        .onAppear {
            guard !isFilled, let policy = model.settings?.source(mailboxId)?.shownPolicy else { return }
            isDraftingReplies = policy.draftReplies
            isSearching = policy.search
            isResearching = policy.research
            isFilled = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func save() {
        guard var policy = model.settings?.source(mailboxId)?.shownPolicy else { return }
        policy.draftReplies = isDraftingReplies
        policy.search = isSearching
        policy.research = isResearching
        let saving = policy
        isSaving = true
        Task {
            let isSaved = await model.savePolicy(saving, mailboxId: mailboxId)
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentAnsweringView is the agent answering mail in one mailbox on the
/// person's behalf: when, to whom, and how much. Each reply waits in Drafts
/// for the hold, where the person can stop it.
struct AgentAnsweringView: View {
    let model: AgentSettingsModel
    let mailboxId: String
    @Environment(\.dismiss) private var dismiss
    @State private var reply = AgentSettings.MailboxPolicy.AutoReply()
    /// The lists and numbers as typed; read when saved, or a comma would
    /// vanish under the cursor.
    @State private var allowText = ""
    @State private var neverText = ""
    @State private var categoriesText = ""
    @State private var holdMinutesText = ""
    @State private var dailyLimitText = ""
    @State private var isFilled = false
    @State private var isSaving = false

    var body: some View {
        Form {
            Section {
                Toggle("Answer mail on my behalf, under the policy below", isOn: $reply.enabled)
                    .disabled(!(model.settings?.allowed.autoReply ?? false))
            } footer: {
                Text("Under the policy here it sends replies on your behalf. Each waits in Drafts for the hold, where you can stop it.")
            }
            Section("When and how to answer") {
                TextField(
                    "When and how to answer", text: Binding(get: { reply.guidance ?? "" }, set: { reply.guidance = $0 }),
                    prompt: Text("\"Confirm appointments and answer simple questions about opening hours; anything about money, wait for me.\""),
                    axis: .vertical
                )
                .lineLimit(3...)
            }
            Section {
                Picker("Whom", selection: Binding(get: { AgentSortingView.chosen(reply.scope, or: "known") }, set: { reply.scope = $0 })) {
                    Text("People I have written to before").tag("known")
                    Text("Everyone").tag("everyone")
                    Text("Only the allow list").tag("list")
                }
                Picker("When", selection: Binding(get: { AgentSortingView.chosen(reply.when, or: "always") }, set: { reply.when = $0 })) {
                    Text("Always").tag("always")
                    Text("Outside my hours").tag("outsideHours")
                    Text("Only while my out-of-office reply is on").tag("whenAway")
                }
                if reply.when == "outsideHours" {
                    DatePicker("From", selection: hourBinding(isStart: true), displayedComponents: .hourAndMinute)
                    DatePicker("Until", selection: hourBinding(isStart: false), displayedComponents: .hourAndMinute)
                }
            }
            Section("Allow list (addresses or domains)") {
                TextField("Allow list (addresses or domains)", text: $allowText, prompt: Text(verbatim: "example.com, someone@example.org"), axis: .vertical)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            Section("Never answer (addresses or domains)") {
                TextField("Never answer (addresses or domains)", text: $neverText, prompt: Text(verbatim: "example.net"), axis: .vertical)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            Section("Only these categories") {
                TextField("Only these categories", text: $categoriesText, prompt: Text("any"), axis: .vertical)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            Section {
                LabeledContent("Hold for (minutes)") {
                    TextField("Hold for (minutes)", text: $holdMinutesText, prompt: Text(verbatim: "10"))
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                }
                LabeledContent("At most per day") {
                    TextField("At most per day", text: $dailyLimitText, prompt: Text(verbatim: "20"))
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                }
            }
        }
        .navigationTitle("Answering for me")
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: model.settings?.source(mailboxId) != nil) { save() }
        }
        .onAppear {
            guard !isFilled, let policy = model.settings?.source(mailboxId)?.shownPolicy else { return }
            reply = policy.autoReply ?? AgentSettings.MailboxPolicy.AutoReply()
            allowText = AgentSettings.joinList(reply.allow)
            neverText = AgentSettings.joinList(reply.never)
            categoriesText = AgentSettings.joinList(reply.categories)
            holdMinutesText = (reply.holdMinutes ?? 0) > 0 ? String(reply.holdMinutes ?? 0) : ""
            dailyLimitText = (reply.dailyLimit ?? 0) > 0 ? String(reply.dailyLimit ?? 0) : ""
            isFilled = true
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    /// One end of the working hours as a time of day; outside hours starts
    /// from nine to half past five, Monday to Friday.
    private func hourBinding(isStart: Bool) -> Binding<Date> {
        Binding(
            get: {
                let hours = reply.hours ?? .workingHours
                let parts = AgentSettings.clockParts(isStart ? hours.from : hours.until) ?? (hour: 9, minute: 0)
                return Calendar.current.date(bySettingHour: parts.hour, minute: parts.minute, second: 0, of: Date()) ?? Date()
            },
            set: { date in
                let components = Calendar.current.dateComponents([.hour, .minute], from: date)
                var hours = reply.hours ?? .workingHours
                let clock = AgentSettings.clockText(hour: components.hour ?? 0, minute: components.minute ?? 0)
                if isStart {
                    hours.from = clock
                } else {
                    hours.until = clock
                }
                reply.hours = hours
            })
    }

    private func save() {
        guard var policy = model.settings?.source(mailboxId)?.shownPolicy else { return }
        var saved = reply
        saved.allow = AgentSettings.splitList(allowText)
        saved.never = AgentSettings.splitList(neverText)
        saved.categories = AgentSettings.splitList(categoriesText)
        saved.holdMinutes = AgentSettings.wholeNumber(holdMinutesText)
        saved.dailyLimit = AgentSettings.wholeNumber(dailyLimitText)
        policy.autoReply = saved
        let saving = policy
        isSaving = true
        Task {
            let isSaved = await model.savePolicy(saving, mailboxId: mailboxId)
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentCategoriesView is the person's own categories beside the fixed
/// ones: added and changed in a sheet, removed with a swipe, and saved as
/// each is done.
struct AgentCategoriesView: View {
    let model: AgentSettingsModel
    @State private var editing: EditedCategory?
    @State private var removing: RemovedCategory?

    /// EditedCategory is a category open in the editor: its place in the
    /// list, or none for a new one.
    struct EditedCategory: Identifiable {
        let id = UUID()
        let index: Int?
        let category: AgentSettings.Category
    }

    /// RemovedCategory is a category waiting for the person to confirm its
    /// removal.
    struct RemovedCategory: Identifiable {
        let id = UUID()
        let index: Int
        let name: String
    }

    var body: some View {
        List {
            if let settings = model.settings, let agent = settings.agent {
                Section {
                    if agent.categories.isEmpty {
                        Text("None of your own yet. The fixed ones cover most mail.").foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(Array(agent.categories.enumerated()), id: \.offset) { index, category in
                        Button {
                            editing = EditedCategory(index: index, category: category)
                        } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: category.name).foregroundStyle(Color(.label))
                                if let description = category.description, !description.isEmpty {
                                    Text(verbatim: description).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                                }
                            }
                        }
                        .swipeActions {
                            Button("Remove", systemImage: "trash", role: .destructive) {
                                removing = RemovedCategory(index: index, name: category.name)
                            }
                        }
                    }
                } footer: {
                    Text("Beside the fixed ones (\(settings.fixedCategoryNames.joined(separator: ", "))), you can declare your own, each with a line saying what belongs in it. They work in rules like the fixed ones.")
                }
            }
        }
        .navigationTitle("Your own categories")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("Add a category", systemImage: "plus") {
                    editing = EditedCategory(index: nil, category: AgentSettings.Category())
                }
                .disabled(model.agent == nil)
            }
        }
        .sheet(item: $editing) { edited in
            NavigationStack {
                AgentCategoryEditor(model: model, edited: edited) { category in
                    var categories = model.agent?.categories ?? []
                    if let index = edited.index, categories.indices.contains(index) {
                        categories[index] = category
                    } else {
                        categories.append(category)
                    }
                    return await save(categories)
                }
            }
        }
        .confirmationDialog(
            "Remove the category", isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
            titleVisibility: .visible, presenting: removing
        ) { removed in
            Button("Remove", role: .destructive) {
                var categories = model.agent?.categories ?? []
                if categories.indices.contains(removed.index) { categories.remove(at: removed.index) }
                let remaining = categories
                Task { _ = await save(remaining) }
            }
        } message: { removed in
            Text("Remove \(removed.name)? Mail already sorted into it keeps the label; new mail is sorted without it.")
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func save(_ categories: [AgentSettings.Category]) async -> Bool {
        await model.update(AgentChange(categories: categories), saying: String(localized: "Saved"))
    }
}

/// AgentCategoryEditor is one category: its name and what belongs in it.
/// Nothing is saved until Save.
struct AgentCategoryEditor: View {
    let model: AgentSettingsModel
    let onSave: (AgentSettings.Category) async -> Bool
    private let isNew: Bool
    @State private var category: AgentSettings.Category
    @State private var isSaving = false
    @Environment(\.dismiss) private var dismiss

    init(model: AgentSettingsModel, edited: AgentCategoriesView.EditedCategory, onSave: @escaping (AgentSettings.Category) async -> Bool) {
        self.model = model
        self.onSave = onSave
        self.isNew = edited.index == nil
        self._category = State(initialValue: edited.category)
    }

    var body: some View {
        Form {
            Section {
                TextField("Name", text: $category.name)
                    .textInputAutocapitalization(.never)
                    .onChange(of: category.name) { _, typed in
                        if typed.count > AgentSettings.categoryNameLength { category.name = String(typed.prefix(AgentSettings.categoryNameLength)) }
                    }
                TextField("What belongs in it", text: Binding(get: { category.description ?? "" }, set: { category.description = $0 }), axis: .vertical)
            }
        }
        .navigationTitle(isNew ? "Add a category" : "Edit the category")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
            AgentSaveToolbar(isSaving: isSaving, canSave: !category.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) {
                isSaving = true
                let saved = category
                Task {
                    let isSaved = await onSave(saved)
                    isSaving = false
                    if isSaved { dismiss() }
                }
            }
        }
        .toast(done: .constant(nil), problem: Bindable(model).problemText)
    }
}
