import SwiftUI
import TeaNodeKit

/// AppSectionGroup is a heading in the More tab and in the iPad's sidebar,
/// with the parts of TeaNode that belong under it.
enum AppSectionGroup: String, CaseIterable, Identifiable, Hashable {
    case everyDay, work, plans, money

    var id: String { rawValue }

    var title: String {
        switch self {
        case .everyDay: String(localized: "Every day")
        case .work: String(localized: "Work")
        case .plans: String(localized: "Plans")
        case .money: String(localized: "Money")
        }
    }

    /// The parts under the heading, in the order they are listed.
    var sections: [AppSection] {
        switch self {
        case .everyDay: [.calendar, .contacts, .location, .health]
        case .work: [.coding, .knowledge, .recordings]
        case .plans: [.goals, .ideas]
        case .money: [.finance]
        }
    }
}

extension AppSection {
    /// The parts that are tabs of their own on the iPhone. Every other part is
    /// under the More tab, and Settings is the last row of it. Three, as the
    /// iPhone's tab bar holds five and More and Search are two: a sixth tab
    /// makes iOS put the last ones under a More of its own.
    static let tabBarSections: [AppSection] = [.agent, .mail, .photos]
}

/// MainTab is what the tab bar has chosen: a part, the More tab, or Search.
enum MainTab: Hashable {
    case section(AppSection)
    case more
    case search
}

/// ChatPlace is where the Chats tab's stack can be: the main conversation, or
/// another one, pushed on the list of conversations.
enum ChatPlace: Hashable {
    case main
    case conversation(AgentConversation)

    /// The conversation to read; nil for the main one, which is asked for by
    /// having none.
    var conversation: AgentConversation? {
        switch self {
        case .main: nil
        case .conversation(let conversation): conversation
        }
    }
}

/// LeavePart goes back from a part opened under the More tab to the list of
/// parts. Only a part under More has one.
struct LeavePart {
    let action: () -> Void

    @MainActor
    func callAsFunction() {
        action()
    }
}

extension EnvironmentValues {
    @Entry var leavePart: LeavePart?
}

/// MoreBackButton is the button at the top left of the first screen of a part
/// that is opened under the More tab, which goes back to the list of parts.
/// A part that is a tab of its own has nothing to go back to, and the bar
/// shows no button.
struct MoreBackButton: ToolbarContent {
    @Environment(\.leavePart) private var leavePart

    var body: some ToolbarContent {
        if leavePart != nil {
            ToolbarItem(placement: .topBarLeading) {
                Button {
                    leavePart?.callAsFunction()
                } label: {
                    Label("More", systemImage: "chevron.backward")
                        .labelStyle(.titleAndIcon)
                }
            }
        }
    }
}

/// MoreList is the More tab's first screen: the parts that are not tabs of
/// their own, in short groups, and Settings. A part the server does not offer
/// is left out.
struct MoreList: View {
    let onOpen: (AppSection) -> Void
    @Environment(AppModel.self) private var app

    var body: some View {
        NavigationStack {
            List {
                ForEach(groups) { group in
                    Section(group.title) {
                        ForEach(sections(in: group)) { section in
                            MoreRow(section: section, waitingCount: section == .coding ? app.codingAskingCount : 0) {
                                onOpen(section)
                            }
                        }
                    }
                }
                Section {
                    MoreRow(section: .settings, waitingCount: 0) {
                        onOpen(.settings)
                    }
                }
            }
            .navigationTitle("More")
        }
    }

    private func sections(in group: AppSectionGroup) -> [AppSection] {
        group.sections.filter { app.isSectionAvailable($0) }
    }

    private var groups: [AppSectionGroup] {
        AppSectionGroup.allCases.filter { !sections(in: $0).isEmpty }
    }
}

/// MoreRow is a part in the More list: its symbol and name, how many things
/// in it wait for the person, and a chevron, since it opens the part.
struct MoreRow: View {
    let section: AppSection
    let waitingCount: Int
    let onOpen: () -> Void

    var body: some View {
        Button {
            onOpen()
        } label: {
            HStack {
                // A place to go, in the text's color: inside a button the
                // primary style is the button's green, a color is not.
                Label(section.title, systemImage: section.systemImage)
                    .foregroundStyle(Color.primary)
                Spacer(minLength: 8)
                if waitingCount > 0 {
                    Text(waitingCount, format: .number)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
                Image(systemName: "chevron.forward")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
            .contentShape(.rect)
        }
    }
}

/// ChatPlaceScreen is a conversation pushed on the Chats tab's list: the main
/// one, which waits until it is known whether the agent is on and says so
/// when it is not, or another, read as it is. The conversation has its own
/// bar at the bottom, so the tab bar gives way to it.
struct ChatPlaceScreen: View {
    let place: ChatPlace
    let connection: Connection
    /// Whether the services, the agent's identity and the chats have been
    /// read once. Until then it is not known whether the agent is on, and
    /// drawing the conversation or the offer to turn the agent on would be a
    /// guess that the next read undoes.
    let hasReadAgentFacts: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        content
            .toolbarVisibility(.hidden, for: .tabBar)
    }

    @ViewBuilder
    private var content: some View {
        if case .main = place, !hasReadAgentFacts {
            ServicesLoadingView()
        } else if case .main = place, !app.isAgentOn {
            AgentOffView()
        } else {
            ConversationView(model: ConversationModel(conversation: place.conversation, app: app, connection: connection))
        }
    }
}
