import SwiftUI

/// Toast is what the dashboard's toasts say, floating over the bottom of a
/// screen: done for four and a half seconds, a problem for nine, as the
/// dashboard keeps them.
struct Toast: ViewModifier {
    @Binding var doneText: String?
    @Binding var problemText: String?
    /// What undoes the action just done, offered as a button beside it.
    var undo: Binding<(@MainActor () async -> Void)?>?
    /// What can be done next, offered as buttons beside the words: two at
    /// most fit beside them.
    var buttons: Binding<[ToastButton]>?

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .bottom) {
                if let text = shownProblemText?.asReasonSentence ?? doneText {
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
                        if !isProblem, let shownButtons = buttons?.wrappedValue {
                            ForEach(shownButtons) { shownButton in
                                Button(shownButton.title) {
                                    clear()
                                    Task { await shownButton.run() }
                                }
                                .fontWeight(.semibold)
                            }
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
        buttons?.wrappedValue = []
    }
}

/// ToastButton is a button beside a toast's words, for what can be done next.
struct ToastButton: Identifiable {
    let title: String
    let run: @MainActor () async -> Void
    var id: String { title }
}

extension String {
    /// A reason the server or the system gave, written as the sentence a
    /// screen shows: trimmed, with a capital first letter and a full stop when
    /// it ends without one. They come as phrases such as "photo storage is
    /// unavailable". A text that is already a sentence is returned as it is.
    var asReasonSentence: String {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        guard let first = trimmed.first else { return trimmed }
        let capitalized = first.uppercased() + trimmed.dropFirst()
        guard let last = capitalized.last, !".!?。！？…".contains(last) else { return capitalized }
        return capitalized + "."
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

    func toast(done doneText: Binding<String?>, problem problemText: Binding<String?>, buttons: Binding<[ToastButton]>) -> some View {
        modifier(Toast(doneText: doneText, problemText: problemText, buttons: buttons))
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
            Text(verbatim: problemText.asReasonSentence)
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
        refusalReason(orSay: String(localized: "Check your connection and try again.")).asReasonSentence
    }
}

/// ReadableWidth keeps what is read in lines, such as a conversation or a
/// mail, from running the whole width of a wide window, where a line of
/// text is too long to follow. It is centered, and only where the window is
/// wide: on the iPhone and in a narrow window it changes nothing.
struct ReadableWidth: ViewModifier {
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass

    /// The widest the content gets, in points.
    static var maximumWidth: CGFloat { 720 }

    @ViewBuilder
    func body(content: Content) -> some View {
        if horizontalSizeClass == .regular {
            content
                .frame(maxWidth: Self.maximumWidth)
                .frame(maxWidth: .infinity)
        } else {
            content
        }
    }
}

extension View {
    /// Content kept to a width that reads well, centered; see ReadableWidth.
    func readableWidth() -> some View { modifier(ReadableWidth()) }
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
    /// The line under a row starts at the row's edge, as under a row with no
    /// symbol, instead of after the symbol, where a list starts it by default.
    /// For the rows with a symbol in a section that also has rows without one,
    /// so that every line in the section is as long.
    func fullWidthRowSeparator() -> some View {
        alignmentGuide(.listRowSeparatorLeading) { _ in 0 }
    }

    /// A button's symbol and words in one color: green while it can be
    /// pressed, or red for one that destroys, and grey while it cannot
    /// (ios/DESIGN.md). In a list neither comes by itself: a plain list draws
    /// the words in the text color beside a green symbol, a disabled button in
    /// a form turns its words black and leaves its symbol green, and a
    /// destructive one in a form row keeps a green symbol beside red words
    /// even with `.tint(.red)`.
    func actionColor(isDestructive: Bool = false) -> some View {
        modifier(ActionColor(isDestructive: isDestructive))
    }
}

/// ActionColor is `actionColor()`. It reads whether the button can be
/// pressed from below `.disabled`, which says so to everything inside it.
private struct ActionColor: ViewModifier {
    let isDestructive: Bool
    @Environment(\.isEnabled) private var isEnabled

    func body(content: Content) -> some View {
        content.foregroundStyle(!isEnabled ? AnyShapeStyle(.secondary) : isDestructive ? AnyShapeStyle(.red) : AnyShapeStyle(.tint))
    }
}
