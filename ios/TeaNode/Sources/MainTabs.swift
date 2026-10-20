import SwiftUI
import TeaNodeKit
import UIKit

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
        case .everyDay: [.agent, .mail, .photos, .calendar, .contacts, .location, .health]
        case .work: [.coding, .knowledge, .recordings]
        case .plans: [.goals, .ideas]
        case .money: [.finance]
        }
    }
}

/// TabBarChoice is which parts the person keeps in the iPhone's tab bar, in
/// their order, kept on this device. Every other part is under the More tab,
/// and Settings is the last row of it. Three at most, as the iPhone's tab bar
/// holds five and More and Search are two: a sixth tab makes iOS put the last
/// ones under a More of its own.
enum TabBarChoice {
    static let storageKey = "tabBarSections"
    static let defaultSections: [AppSection] = [.agent, .mail, .photos]
    static var defaultText: String { text(of: defaultSections) }
    static let mostCount = 3

    /// The parts written in text, as `text(of:)` writes them: known ones,
    /// each once, Settings never, three at most.
    static func sections(from text: String) -> [AppSection] {
        var chosen: [AppSection] = []
        for name in text.split(separator: ",") {
            guard let section = AppSection(rawValue: String(name)), section != .settings, !chosen.contains(section) else { continue }
            chosen.append(section)
        }
        return Array(chosen.prefix(mostCount))
    }

    static func text(of sections: [AppSection]) -> String {
        sections.map(\.rawValue).joined(separator: ",")
    }
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
    @AppStorage(TabBarChoice.storageKey) private var tabBarText = TabBarChoice.defaultText
    @State private var isEditingTabBar = MoreList.isEditingAtFirst

    var body: some View {
        NavigationStack {
            List {
                ForEach(groups) { group in
                    Section(group.title) {
                        ForEach(sections(in: group)) { section in
                            MoreRow(section: section, waitingCount: waitingCount(of: section)) {
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
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button("Edit") { isEditingTabBar = true }
                        .accessibilityLabel(Text("Edit the tab bar"))
                }
            }
            .sheet(isPresented: $isEditingTabBar) {
                TabBarEditor(tabBarText: $tabBarText)
                    .environment(app)
            }
        }
    }

    /// The demo's `-TeaNodeScreen tab-bar-edit` opens on the editor.
    private static var isEditingAtFirst: Bool {
        #if DEBUG
            DemoServer.isRequested && DemoServer.screen == "tab-bar-edit"
        #else
            false
        #endif
    }

    /// What waits in a part: unread chats, when Chats is under More, and the
    /// coding sessions that ask something.
    private func waitingCount(of section: AppSection) -> Int {
        switch section {
        case .agent: app.unreadChatCount
        case .coding: app.codingAskingCount
        default: 0
        }
    }

    /// The parts of a group that are not in the tab bar, and that the server
    /// offers.
    private func sections(in group: AppSectionGroup) -> [AppSection] {
        let tabBarSections = TabBarChoice.sections(from: tabBarText)
        return group.sections.filter { app.isSectionAvailable($0) && !tabBarSections.contains($0) }
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

/// TabBarEditor is where the person chooses the parts in the tab bar, as the
/// iPhone's Music app chooses its tabs: the ones in the bar, in their order,
/// which can be moved or taken out, and under them the others, any of which
/// can be put in while there is room.
struct TabBarEditor: View {
    @Binding var tabBarText: String
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss

    /// The parts in the bar. One the server turned off has no tab, so it
    /// holds no place in the bar while it is off.
    private var chosen: [AppSection] {
        let sections = TabBarChoice.sections(from: tabBarText)
        return app.services == nil ? sections : sections.filter { app.isSectionAvailable($0) }
    }

    /// The parts that could be put in the bar: offered by the server, not in
    /// it, in the order More lists them.
    private var others: [AppSection] {
        AppSectionGroup.allCases.flatMap(\.sections).filter { app.isSectionAvailable($0) && !chosen.contains($0) }
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(chosen) { section in
                        TabBarPartLabel(section: section)
                    }
                    .onMove { offsets, destination in
                        var moved = chosen
                        moved.move(fromOffsets: offsets, toOffset: destination)
                        tabBarText = TabBarChoice.text(of: moved)
                    }
                    .onDelete { offsets in
                        var kept = chosen
                        kept.remove(atOffsets: offsets)
                        tabBarText = TabBarChoice.text(of: kept)
                    }
                } header: {
                    Text("In the tab bar")
                } footer: {
                    Text("Up to three. More and Search are always there.")
                }
                Section("In More") {
                    ForEach(others) { section in
                        TabBarAddRow(section: section, isFull: chosen.count >= TabBarChoice.mostCount) {
                            tabBarText = TabBarChoice.text(of: chosen + [section])
                        }
                    }
                }
            }
            .environment(\.editMode, .constant(.active))
            .navigationTitle("Tab bar")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}

/// TabBarAddRow puts a part in the tab bar. Its green plus is the action,
/// across from the red minus of the parts in the bar, and turns grey while
/// the bar is full; the name stays in the text's color so it can be read.
struct TabBarAddRow: View {
    let section: AppSection
    let isFull: Bool
    let onAdd: () -> Void

    var body: some View {
        Button {
            onAdd()
        } label: {
            HStack(spacing: 12) {
                Image(systemName: "plus.circle.fill")
                    .font(.title3)
                    .symbolRenderingMode(.palette)
                    .foregroundStyle(Color.white, isFull ? Color.secondary : Color.green)
                    .accessibilityHidden(true)
                TabBarPartLabel(section: section)
            }
            .contentShape(.rect)
        }
        .disabled(isFull)
        .accessibilityHint(Text("Puts it in the tab bar"))
    }
}

/// TabBarPartLabel is a part's symbol and name in the tab bar editor. At the
/// accessibility text sizes it is the name alone: beside the editing controls
/// the symbol leaves too little room for a whole word.
struct TabBarPartLabel: View {
    let section: AppSection
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if dynamicTypeSize.isAccessibilitySize {
            Text(section.title)
                .lineLimit(1)
                .minimumScaleFactor(0.75)
                .foregroundStyle(Color.primary)
        } else {
            Label(section.title, systemImage: section.systemImage)
                .foregroundStyle(Color.primary)
        }
    }
}

/// MoreSwipeBack is the swipe back from the first screen of a part opened
/// under More to More's list. More shows the part in place of its list rather
/// than pushing it (every part has a stack of its own, and stacks are not
/// nested), so the system's swipe has nothing to go back to there; this one
/// does it instead, following the finger as the system's does. It starts at
/// the left edge, as the system's first did: further in, a sideways drag picks
/// a day on a chart or moves a map. And only where the stack under the finger
/// is on its first screen, so on a screen pushed inside the part the system's
/// own swipe goes back within the part; never over a sheet, or on a sideways
/// list that can still scroll back.
struct MoreSwipeBack: UIViewRepresentable {
    /// How far the part has been swiped toward the back, in points.
    let onSwipe: (CGFloat) -> Void
    /// Whether the swipe went far or fast enough to go back.
    let onEnd: (Bool) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(parent: self) }

    func makeUIView(context: Context) -> InstallingView {
        let view = InstallingView()
        view.isUserInteractionEnabled = false
        view.coordinator = context.coordinator
        return view
    }

    func updateUIView(_ view: InstallingView, context: Context) {
        context.coordinator.parent = self
    }

    static func dismantleUIView(_ view: InstallingView, coordinator: Coordinator) {
        coordinator.uninstall()
    }

    /// InstallingView puts the swipe on the window while the part is on
    /// screen, and takes it off when the part leaves the window, as it does
    /// when another tab is chosen.
    final class InstallingView: UIView {
        weak var coordinator: Coordinator?

        override func didMoveToWindow() {
            super.didMoveToWindow()
            coordinator?.install(on: window)
        }
    }

    @MainActor
    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        var parent: MoreSwipeBack
        private var recognizer: UIPanGestureRecognizer?

        init(parent: MoreSwipeBack) {
            self.parent = parent
        }

        func install(on window: UIWindow?) {
            uninstall()
            guard let window else { return }
            let recognizer = UIPanGestureRecognizer(target: self, action: #selector(swiped(_:)))
            recognizer.delegate = self
            recognizer.maximumNumberOfTouches = 1
            window.addGestureRecognizer(recognizer)
            self.recognizer = recognizer
        }

        func uninstall() {
            if let recognizer { recognizer.view?.removeGestureRecognizer(recognizer) }
            recognizer = nil
        }

        /// How far in from the left edge a swipe back may start, in points.
        private let edgeWidth: CGFloat = 44

        func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
            guard let pan = gestureRecognizer as? UIPanGestureRecognizer, let window = pan.view as? UIWindow else { return false }
            // Not while a sheet or an alert is over the part.
            if window.rootViewController?.presentedViewController != nil { return false }
            // From the left edge, mostly sideways and to the right.
            let location = pan.location(in: window)
            let startX = location.x - pan.translation(in: window).x
            guard startX <= edgeWidth else { return false }
            let velocity = pan.velocity(in: window)
            guard velocity.x > 0, velocity.x > abs(velocity.y) * 1.5 else { return false }
            guard let touched = window.hitTest(location, with: nil) else { return false }
            // Not on a sideways list that can still scroll back.
            var view: UIView? = touched
            while let current = view {
                if let scrollView = current as? UIScrollView, scrollView.contentSize.width > scrollView.bounds.width + 1,
                    scrollView.contentOffset.x > -scrollView.adjustedContentInset.left + 1
                {
                    return false
                }
                view = current.superview
            }
            // Only where the stack under the finger is on its first screen.
            var responder: UIResponder? = touched
            while let current = responder {
                if let navigation = current as? UINavigationController {
                    return navigation.viewControllers.count <= 1
                }
                responder = current.next
            }
            return false
        }

        func gestureRecognizer(_ gestureRecognizer: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool {
            // A list under the finger scrolls up and down as it did; the
            // swipe only reads how far the finger went sideways.
            other.view is UIScrollView
        }

        @objc private func swiped(_ pan: UIPanGestureRecognizer) {
            guard let view = pan.view else { return }
            let distance = max(0, pan.translation(in: view).x)
            switch pan.state {
            case .began, .changed:
                parent.onSwipe(distance)
            case .ended:
                parent.onEnd(distance > view.bounds.width / 3 || pan.velocity(in: view).x > 600)
            case .cancelled, .failed:
                parent.onEnd(false)
            default:
                break
            }
        }
    }
}
