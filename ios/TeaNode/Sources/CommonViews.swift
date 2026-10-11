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
                if let text = problemText ?? doneText {
                    let isProblem = problemText != nil
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
            .animation(.default, value: problemText ?? doneText)
    }

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
    @GestureState private var dragged: CGFloat = 0
    @State private var claim = SwipeClaim()
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
                    .environment(\.swipeClaim, claim)
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
            // Alongside the screen's own scrolling, which would otherwise
            // take every drag that starts over it; only a mostly sideways
            // drag moves the panel.
            .simultaneousGesture(
                DragGesture(minimumDistance: 16)
                    .updating($dragged) { value, dragged, _ in
                        // A message being swiped keeps the finger.
                        guard !claim.isClaimed, abs(value.translation.width) > abs(value.translation.height) else { return }
                        if isOpen {
                            dragged = min(0, value.translation.width)
                        } else if isOpenedFromAnywhere || value.startLocation.x < Self.edgeWidth {
                            dragged = max(0, value.translation.width)
                        }
                    }
                    .onEnded { value in
                        guard !claim.isClaimed, abs(value.translation.width) > abs(value.translation.height) else { return }
                        if isOpen, value.predictedEndTranslation.width < -width / 3 {
                            setOpen(false)
                        } else if !isOpen, isOpenedFromAnywhere || value.startLocation.x < Self.edgeWidth,
                            value.predictedEndTranslation.width > width / 3
                        {
                            setOpen(true)
                        }
                    })
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

/// SwipeClaim says a line of a conversation is being swiped, so the side
/// panel does not open under the same finger.
@MainActor
@Observable
final class SwipeClaim {
    var isClaimed = false
}

extension EnvironmentValues {
    @Entry var swipeClaim: SwipeClaim?
}

/// SwipeToReplyAndCopy is the dashboard drawer's swipe on a message: carried
/// left past where letting go acts, it replies; carried right, it copies,
/// and a tick says so. The line follows the finger, heavier past that
/// point, and a tap of haptics marks it.
struct SwipeToReplyAndCopy: ViewModifier {
    let isReplying: Bool
    let onReply: () -> Void
    let onCopy: () -> Void
    @Environment(\.swipeClaim) private var claim
    @State private var offset: CGFloat = 0
    @State private var isCopied = false

    /// Where letting go acts, and the farthest a line is carried, as the
    /// dashboard has them.
    static var actionDistance: CGFloat { 56 }
    static var farthest: CGFloat { 96 }

    func body(content: Content) -> some View {
        content
            .offset(x: offset)
            .background(alignment: offset < 0 ? .trailing : .leading) {
                if offset != 0 || isCopied {
                    Image(systemName: offset < 0 ? "arrowshape.turn.up.left" : isCopied ? "checkmark" : "doc.on.doc")
                        .font(.body)
                        .foregroundStyle(abs(offset) >= Self.actionDistance || isCopied ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
                        .opacity(min(1, abs(offset) / Self.actionDistance + (isCopied ? 1 : 0)))
                        .padding(.horizontal, 8)
                }
            }
            .simultaneousGesture(
                DragGesture(minimumDistance: 12)
                    .onChanged { value in
                        guard abs(value.translation.width) > abs(value.translation.height) * 1.5 else { return }
                        if !isReplying, value.translation.width < 0 { return }
                        claim?.isClaimed = true
                        offset = Self.carried(value.translation.width)
                    }
                    .onEnded { _ in
                        // Let go a moment later, after the panel's own drag
                        // has ended, or it would open with the copy.
                        defer {
                            Task {
                                try? await Task.sleep(for: .milliseconds(80))
                                claim?.isClaimed = false
                            }
                        }
                        let reached = offset
                        withAnimation(.spring(duration: 0.25)) { offset = 0 }
                        if reached <= -Self.actionDistance {
                            onReply()
                        } else if reached >= Self.actionDistance {
                            onCopy()
                            isCopied = true
                            Task {
                                try? await Task.sleep(for: .milliseconds(900))
                                isCopied = false
                            }
                        }
                    })
            .sensoryFeedback(.impact(weight: .light), trigger: abs(offset) >= Self.actionDistance) { _, isPast in isPast }
    }

    /// One for one up to where letting go acts, then heavier.
    static func carried(_ across: CGFloat) -> CGFloat {
        let distance = abs(across)
        let carried = distance <= actionDistance ? distance : actionDistance + (distance - actionDistance) * 0.35
        return (across < 0 ? -1 : 1) * min(carried, farthest)
    }
}

extension View {
    func swipeToReplyAndCopy(isReplying: Bool, onReply: @escaping () -> Void, onCopy: @escaping () -> Void) -> some View {
        modifier(SwipeToReplyAndCopy(isReplying: isReplying, onReply: onReply, onCopy: onCopy))
    }
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

extension View {
    /// The side menu's plain list, when the list is shown in it.
    func menuListStyle(_ isMenu: Bool = true) -> some View {
        modifier(MenuListStyle(isMenu: isMenu))
    }

    /// A row of the side menu: no separator, and the one chosen on a
    /// rounded highlight inset from the edges.
    func menuRow(isSelected: Bool) -> some View {
        listRowSeparator(.hidden)
            .listRowBackground(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(isSelected ? Color(.tertiarySystemFill) : .clear)
                    .padding(.horizontal, 10)
            )
    }
}
