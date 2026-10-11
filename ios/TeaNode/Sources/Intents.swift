import AppIntents
import TeaNodeKit

/// AskAgentIntent asks the agent something from Siri or a shortcut, in the
/// main conversation, and says the answer when it comes in time. A longer
/// turn goes on, and its answer arrives as a notification.
struct AskAgentIntent: AppIntent {
    static var title: LocalizedStringResource { "Ask your agent" }
    static var description: IntentDescription {
        IntentDescription("Asks your agent something in your main conversation, and says the answer.")
    }

    @Parameter(title: "Request", requestValueDialog: "What should I ask?")
    var request: String

    static var parameterSummary: some ParameterSummary {
        Summary("Ask your agent \(\.$request)")
    }

    /// How long Siri waits for the answer before leaving it to a
    /// notification.
    static let waitSeconds: TimeInterval = 25

    func perform() async throws -> some IntentResult & ReturnsValue<String> & ProvidesDialog {
        let connection = try IntentConnection.make()
        let asked = try await connection.ask(request, in: nil, surface: Surface.siri, drawerId: "siri-\(UUID().uuidString)")
        let answer = try await connection.answer(to: asked.runId, until: Date().addingTimeInterval(Self.waitSeconds))
        let said: String
        switch answer {
        case .answered(let text) where !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty:
            said = text
        case .answered:
            said = String(localized: "Done.")
        case .waitingForPerson:
            said = String(localized: "Your agent needs you to look at something in TeaNode first.")
        case .failed(let problem):
            said = problem.isEmpty ? String(localized: "That turn failed.") : problem
        case .stillWorking:
            said = String(localized: "Still working on it. The answer comes as a notification.")
        }
        return .result(value: said, dialog: "\(said)")
    }
}

/// TeaNodeShortcuts are the phrases Siri knows without being set up.
struct TeaNodeShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: AskAgentIntent(), phrases: ["Ask \(.applicationName)", "Ask my agent in \(.applicationName)"],
            shortTitle: "Ask", systemImageName: "bubble.left")
        AppShortcut(
            intent: CallAgentIntent(), phrases: ["Call \(.applicationName)", "Talk to \(.applicationName)"],
            shortTitle: "Call", systemImageName: "waveform")
    }
}

/// IntentConnection is the connection an intent uses: the sign-in the app
/// keeps in the keychain, read again each time.
enum IntentConnection {
    static func make() throws -> Connection {
        guard let account = try KeychainAccountStore().load() else { throw IntentProblem.notConnected }
        return Connection(account: account, transport: URLSessionTransport(), store: KeychainAccountStore())
    }
}

enum IntentProblem: Error, CustomLocalizedStringResourceConvertible {
    case notConnected

    var localizedStringResource: LocalizedStringResource {
        switch self {
        case .notConnected: "Open TeaNode and connect it to your server first."
        }
    }
}
