import AppIntents

// The intents that open the app somewhere. They are built into the app and
// into the widgets: a widget's button or a control names one, and iOS runs it
// in the app, where TEANODE_APP is set.

/// CallAgentIntent opens TeaNode on a call with the agent.
struct CallAgentIntent: AppIntent {
    static var title: LocalizedStringResource { "Call your agent" }
    static var description: IntentDescription { IntentDescription("Opens TeaNode and starts a voice call with your agent.") }
    static var openAppWhenRun: Bool { true }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if TEANODE_APP
            AppModel.shared.openedConversationId = ""
            AppModel.shared.isCallRequested = true
        #endif
        return .result()
    }
}

/// OpenSectionIntent opens TeaNode on one of its parts.
struct OpenSectionIntent: AppIntent {
    static var title: LocalizedStringResource { "Open TeaNode" }
    static var openAppWhenRun: Bool { true }

    @Parameter(title: "Part")
    var part: TeaNodePart

    init() {}

    init(part: TeaNodePart) {
        self.part = part
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if TEANODE_APP
            switch part {
            case .conversation: AppModel.shared.openedConversationId = ""
            case .mail: AppModel.shared.openedSection = .mail
            case .calendar: AppModel.shared.openedSection = .calendar
            }
        #endif
        return .result()
    }
}

/// OpenConversationIntent opens TeaNode on one conversation: the one a
/// question or an approval waits in.
struct OpenConversationIntent: AppIntent {
    static var title: LocalizedStringResource { "Open a conversation" }
    static var openAppWhenRun: Bool { true }
    static var isDiscoverable: Bool { false }

    @Parameter(title: "Conversation")
    var conversationId: String

    init() {}

    init(conversationId: String) {
        self.conversationId = conversationId
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if TEANODE_APP
            AppModel.shared.openedConversationId = conversationId
        #endif
        return .result()
    }
}

/// TeaNodePart is a part of TeaNode a widget or a shortcut opens.
enum TeaNodePart: String, AppEnum {
    case conversation, mail, calendar

    static var typeDisplayRepresentation: TypeDisplayRepresentation { "Part of TeaNode" }
    static var caseDisplayRepresentations: [TeaNodePart: DisplayRepresentation] {
        [.conversation: "Main conversation", .mail: "Mail", .calendar: "Calendar"]
    }
}
