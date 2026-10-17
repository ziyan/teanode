import SwiftUI

/// Toast is what the dashboard's toasts say, floating over the bottom of a
/// screen: done for four and a half seconds, a problem for nine, as the
/// dashboard keeps them.
struct Toast: ViewModifier {
    @Binding var doneText: String?
    @Binding var problemText: String?
    /// What undoes the action just done, offered as a button beside it.
    var undo: Binding<(@MainActor () async -> Void)?>?

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .bottom) {
                if let text = shownProblemText ?? doneText {
                    let isProblem = shownProblemText != nil
                    HStack(spacing: 12) {
                        Label(text, systemImage: isProblem ? "exclamationmark.triangle" : "checkmark.circle")
                            .foregroundStyle(isProblem ? AnyShapeStyle(.red) : AnyShapeStyle(.primary))
                        if !isProblem, let action = undo?.wrappedValue {
                            Button("Undo") {
                                undo?.wrappedValue = nil
                                clear()
                                Task { await action() }
                            }
                            .fontWeight(.semibold)
                        }
                    }
                    .font(.callout)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 12)
                    .glassEffect(in: .capsule)
                    .padding()
                    .onTapGesture { clear() }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .task(id: text) {
                        try? await Task.sleep(for: .seconds(isProblem ? 9 : 4.5))
                        clear()
                    }
                    .sensoryFeedback(isProblem ? .warning : .success, trigger: text)
                }
            }
            .animation(.default, value: shownProblemText ?? doneText)
            .onChange(of: problemText) { _, text in
                if let text, Self.isCancellation(text) { problemText = nil }
            }
    }

    /// The problem, unless it is a request that was called off: leaving a
    /// screen, pulling to refresh or typing in a search box cancels what
    /// was being read, which iOS words as "cancelled". Nothing went wrong,
    /// so nothing is shown.
    private var shownProblemText: String? {
        guard let problemText, !Self.isCancellation(problemText) else { return nil }
        return problemText
    }

    /// Whether a problem's text is a request called off. Matched on what
    /// URLSession actually says: a URLError made here has no message of
    /// its own and reads "(NSURLErrorDomain error -999.)", so comparing
    /// with one never matched the "cancelled" a real request says.
    static func isCancellation(_ text: String) -> Bool {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty || trimmed == CancellationError().localizedDescription { return true }
        if trimmed.contains("NSURLErrorDomain error -999") { return true }
        // The last words, after any "what failed: " the caller put first.
        let lastWords = (trimmed.components(separatedBy: ": ").last ?? trimmed)
            .trimmingCharacters(in: CharacterSet(charactersIn: ".。 ")).lowercased()
        return cancelledWords.contains(lastWords)
    }

    /// "cancelled" as URLSession words it in the app's languages, and the
    /// American spelling some frameworks use.
    private static let cancelledWords: Set<String> = ["cancelled", "canceled", "已取消", "キャンセルしました", "キャンセルされました"]

    private func clear() {
        doneText = nil
        problemText = nil
        undo?.wrappedValue = nil
    }
}

extension View {
    func toast(done doneText: Binding<String?>, problem problemText: Binding<String?>) -> some View {
        modifier(Toast(doneText: doneText, problemText: problemText))
    }

    func toast(
        done doneText: Binding<String?>, problem problemText: Binding<String?>, undo: Binding<(@MainActor () async -> Void)?>
    ) -> some View {
        modifier(Toast(doneText: doneText, problemText: problemText, undo: undo))
    }
}

/// LoadFailedView is what a screen says when its first read did not get
/// through: that it did not, why, and a way to try again. A read that failed is
/// not an empty answer, so a screen draws this and not "nothing here".
struct LoadFailedView: View {
    /// The server's reason, or the words that say what to do next.
    let problemText: String
    let retry: () async -> Void
    @State private var isRetrying = false

    var body: some View {
        ContentUnavailableView {
            Label("This could not be loaded", systemImage: "wifi.exclamationmark")
        } description: {
            Text(verbatim: problemText)
        } actions: {
            Button {
                isRetrying = true
                Task {
                    await retry()
                    isRetrying = false
                }
            } label: {
                if isRetrying { ProgressView() } else { Text("Try again") }
            }
            .buttonStyle(.borderedProminent)
            .disabled(isRetrying)
        }
    }
}

extension Error {
    /// Why a read failed, in words that say what to do next: the server's own
    /// reason when it answered and refused, else the connection advice.
    var loadProblemText: String {
        refusalReason(orSay: String(localized: "Check your connection and try again."))
    }
}

/// SidePanel slides a screen aside to show a panel under it from the left,
/// as messaging apps keep their lists: the button in the bar opens it, a
/// swipe from the left edge opens it, and a tap on the screen or a swipe
/// back closes it. The tab bar steps aside while it is open.
///
/// SwiftUI has no such container, so this one is drawn by hand.
struct SidePanel<Panel: View, Content: View>: View {
    @Binding var isOpen: Bool
    /// Whether a drag to the right anywhere opens it, as in the Claude app,
    /// or only one from the left edge, for screens whose rows swipe.
    var isOpenedFromAnywhere = true
    @ViewBuilder var panel: () -> Panel
    @ViewBuilder var content: () -> Content
    @State private var dragged: CGFloat = 0
    @Environment(\.accessibilityReduceMotion) private var isReducingMotion

    /// How far from the left edge a swipe must start to open the panel.
    static var edgeWidth: CGFloat { 28 }

    var body: some View {
        GeometryReader { geometry in
            let width = min(geometry.size.width * 0.84, 380)
            let shift = max(0, min(width, (isOpen ? width : 0) + dragged))
            ZStack(alignment: .leading) {
                panel()
                    .frame(width: width)
                    .opacity(shift > 0 ? 1 : 0)
                    .accessibilityHidden(!isOpen)
                content()
                    // While the panel follows a finger, the screen under it
                    // holds still rather than scrolling with the same drag.
                    .scrollDisabled(dragged != 0)
                    .frame(width: geometry.size.width)
                    .clipShape(.rect(cornerRadius: shift > 0 ? 36 : 0, style: .continuous))
                    .shadow(color: .black.opacity(shift > 0 ? 0.3 : 0), radius: 16)
                    .overlay {
                        if isOpen {
                            Color.clear
                                .contentShape(.rect)
                                .onTapGesture { setOpen(false) }
                                .accessibilityLabel("Close the list")
                                .accessibilityAddTraits(.isButton)
                        }
                    }
                    .offset(x: shift)
            }
            // A UIKit pan rather than a SwiftUI drag: once it takes a
            // sideways drag it cancels the touch under the finger, so the
            // row, link or button the swipe began on is not pressed when
            // the finger lifts. It still lets the screen scroll up and down.
            .gesture(
                SidePanelPan(
                    shouldBegin: { start, velocity in
                        guard abs(velocity.x) > abs(velocity.y) else { return false }
                        if isOpen { return velocity.x < 0 }
                        return velocity.x > 0 && (isOpenedFromAnywhere || start.x < Self.edgeWidth)
                    },
                    onChange: { translation in
                        dragged = isOpen ? min(0, translation) : max(0, translation)
                    },
                    onEnd: { translation, velocity in
                        let projected = translation + velocity * 0.2
                        dragged = 0
                        if isOpen, projected < -width / 3 {
                            setOpen(false)
                        } else if !isOpen, projected > width / 3 {
                            setOpen(true)
                        }
                    }))
            .animation(isReducingMotion ? nil : .spring(duration: 0.3), value: isOpen)
        }
        // The whole screen, to its edges: measured inside the safe area, the
        // screens under it ended above the home indicator, over a black band.
        // They keep their own safe areas.
        .ignoresSafeArea()
        .toolbarVisibility(isOpen ? .hidden : .automatic, for: .tabBar)
        .sensoryFeedback(.selection, trigger: isOpen)
    }

    private func setOpen(_ isOpened: Bool) {
        isOpen = isOpened
    }
}

/// SidePanelPan is the side panel's drag, as a UIKit pan so that it can
/// cancel the touches of what is under the finger when it starts.
struct SidePanelPan: UIGestureRecognizerRepresentable {
    /// Whether a drag starting there with that velocity is the panel's.
    let shouldBegin: (CGPoint, CGPoint) -> Bool
    /// How far it has moved sideways.
    let onChange: (CGFloat) -> Void
    /// How far it moved sideways, and how fast it was going when it ended.
    let onEnd: (CGFloat, CGFloat) -> Void

    func makeUIGestureRecognizer(context: Context) -> UIPanGestureRecognizer {
        let recognizer = UIPanGestureRecognizer()
        recognizer.cancelsTouchesInView = true
        recognizer.delegate = context.coordinator
        return recognizer
    }

    func makeCoordinator(converter: CoordinateSpaceConverter) -> Coordinator {
        Coordinator(shouldBegin: shouldBegin)
    }

    func updateUIGestureRecognizer(_ recognizer: UIPanGestureRecognizer, context: Context) {
        context.coordinator.shouldBegin = shouldBegin
    }

    func handleUIGestureRecognizerAction(_ recognizer: UIPanGestureRecognizer, context: Context) {
        let translation = recognizer.translation(in: recognizer.view).x
        switch recognizer.state {
        case .began:
            recognizer.view?.endEditing(true)
            onChange(translation)
        case .changed:
            onChange(translation)
        case .ended:
            onEnd(translation, recognizer.velocity(in: recognizer.view).x)
        case .cancelled, .failed:
            onEnd(0, 0)
        default:
            break
        }
    }

    @MainActor
    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        var shouldBegin: (CGPoint, CGPoint) -> Bool

        init(shouldBegin: @escaping (CGPoint, CGPoint) -> Bool) {
            self.shouldBegin = shouldBegin
        }

        func gestureRecognizerShouldBegin(_ recognizer: UIGestureRecognizer) -> Bool {
            guard let pan = recognizer as? UIPanGestureRecognizer, let view = pan.view else { return false }
            let start = CGPoint(x: pan.location(in: view).x - pan.translation(in: view).x, y: pan.location(in: view).y)
            // A drag on a chart reads its values; it never opens the panel.
            let startInWindow = CGPoint(x: pan.location(in: nil).x - pan.translation(in: nil).x, y: pan.location(in: nil).y)
            if SidePanelExclusions.shared.contains(startInWindow) { return false }
            return shouldBegin(start, pan.velocity(in: view))
        }

        /// Up and down still scroll the screen; only a sideways drag is
        /// the panel's, and it begins only then.
        func gestureRecognizer(_ recognizer: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool {
            true
        }
    }
}

/// SidePanelExclusions is where on the screen a sideways drag belongs to
/// what is under it, such as a chart read by dragging along it, so the side
/// panel does not open from there.
@MainActor
final class SidePanelExclusions {
    static let shared = SidePanelExclusions()
    private var frames: [UUID: CGRect] = [:]

    func set(_ frame: CGRect?, for id: UUID) { frames[id] = frame }
    func contains(_ point: CGPoint) -> Bool { frames.values.contains { $0.contains(point) } }
}

/// KeepsSidePanelClosed keeps the view's place on the screen among the side
/// panel's exclusions while the view is shown.
struct KeepsSidePanelClosed: ViewModifier {
    @State private var id = UUID()

    func body(content: Content) -> some View {
        content
            .onGeometryChange(for: CGRect.self) { $0.frame(in: .global) } action: { SidePanelExclusions.shared.set($0, for: id) }
            .onDisappear { SidePanelExclusions.shared.set(nil, for: id) }
    }
}

extension View {
    /// A sideways drag on this view is its own, never the side panel's.
    func keepsSidePanelClosed() -> some View { modifier(KeepsSidePanelClosed()) }
}

/// MenuListStyle is the side menu's look: a plain list on one background,
/// as the Claude app's menu, instead of grouped boxes.
struct MenuListStyle: ViewModifier {
    let isMenu: Bool

    @ViewBuilder
    func body(content: Content) -> some View {
        if isMenu {
            content
                .listStyle(.plain)
                .scrollContentBackground(.hidden)
                .background(Color(.systemBackground))
                .listRowSeparator(.hidden)
        } else {
            content
        }
    }
}

/// WithoutSwipeBack turns off going back by swiping in the navigation stack
/// it is in: from the edge, and, since iOS 26, from anywhere on the screen.
/// The side menu opens with a drag to the right from anywhere, and the two
/// moved together, opening the menu and leaving the page at once. The back
/// button still goes back. SwiftUI has no switch for it, so the navigation
/// controller is found from a view controller placed in the stack.
struct WithoutSwipeBack: UIViewControllerRepresentable {
    func makeUIViewController(context: Context) -> Controller { Controller() }
    func updateUIViewController(_ controller: Controller, context: Context) {}

    final class Controller: UIViewController {
        override func viewWillAppear(_ animated: Bool) {
            super.viewWillAppear(animated)
            navigationController?.interactivePopGestureRecognizer?.isEnabled = false
            navigationController?.interactiveContentPopGestureRecognizer?.isEnabled = false
        }
    }
}

extension View {
    /// No going back by swiping in this view's navigation stack; see
    /// WithoutSwipeBack.
    func withoutSwipeBack() -> some View {
        background(WithoutSwipeBack().frame(width: 0, height: 0).accessibilityHidden(true))
    }
}

/// TagLabelStyle draws a small label that tells a row's state, such as
/// "Disabled" or "No password", as one piece: its symbol just before its
/// words. In a list a plain Label takes the row's icon column, which set the
/// words far from their symbol.
struct TagLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 4) {
            configuration.icon
            configuration.title
        }
    }
}

extension LabelStyle where Self == TagLabelStyle {
    static var tag: TagLabelStyle { TagLabelStyle() }
}

extension View {
    /// The side menu's plain list, when the list is shown in it.
    func menuListStyle(_ isMenu: Bool = true) -> some View {
        modifier(MenuListStyle(isMenu: isMenu))
    }

    /// A row of the side menu: no separator, and the one chosen on a
    /// rounded highlight inset from the edges.
    func menuRow(isSelected: Bool) -> some View {
        listRowSeparator(.hidden)
            .listRowInsets(EdgeInsets(top: 8, leading: 20, bottom: 8, trailing: 20))
            .listRowBackground(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(isSelected ? Color(.tertiarySystemFill) : .clear)
                    .padding(.horizontal, 10)
            )
    }
}
