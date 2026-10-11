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
        .toolbarVisibility(isOpen ? .hidden : .automatic, for: .tabBar)
        .sensoryFeedback(.selection, trigger: isOpen)
    }

    private func setOpen(_ isOpened: Bool) {
        isOpen = isOpened
    }
}
