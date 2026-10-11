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
            // Alongside the screen's own scrolling, which would otherwise
            // take every drag that starts over it; only a mostly sideways
            // drag moves the panel.
            .simultaneousGesture(
                DragGesture(minimumDistance: 16)
                    .updating($dragged) { value, dragged, _ in
                        guard abs(value.translation.width) > abs(value.translation.height) else { return }
                        if isOpen {
                            dragged = min(0, value.translation.width)
                        } else if isOpenedFromAnywhere || value.startLocation.x < Self.edgeWidth {
                            dragged = max(0, value.translation.width)
                        }
                    }
                    .onEnded { value in
                        guard abs(value.translation.width) > abs(value.translation.height) else { return }
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
