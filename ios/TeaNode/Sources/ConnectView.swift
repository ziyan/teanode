import AuthenticationServices
import SwiftUI
import TeaNodeKit

/// ConnectView connects the phone to a TeaNode server: its address typed
/// here and signed in to on the server's own page, or a code from the
/// server's dashboard, scanned from a computer's screen or its link pasted.
struct ConnectView: View {
    /// Shown over the signed-in app to add another account, rather than as
    /// the app's only screen.
    var isAdding = false
    @Environment(AppModel.self) private var app
    @State private var isScanning = false
    @State private var isConnecting = false
    @State private var pending: PendingConnection?
    @State private var problemText: String?
    @State private var serverText = ""
    @FocusState private var isServerFocused: Bool
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.webAuthenticationSession) private var webAuthenticationSession

    private var isServerEmpty: Bool {
        serverText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 28) {
                    VStack(alignment: .leading, spacing: 8) {
                        Image(systemName: "leaf.fill")
                            .font(.largeTitle)
                            .foregroundStyle(.tint)
                            .accessibilityHidden(true)
                        Text("Talk to your agent from your phone.")
                            .font(.title2)
                            .bold()
                        Text("Sign in to your TeaNode server.")
                            .foregroundStyle(.secondary)
                    }
                    // The address first: it is all a password sign-in needs,
                    // and the server's own page asks for the rest.
                    VStack(alignment: .leading, spacing: 12) {
                        TextField("mail.example.com", text: $serverText)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .keyboardType(.URL)
                            .textContentType(.URL)
                            .submitLabel(.go)
                            .focused($isServerFocused)
                            .onSubmit { if !isServerEmpty { signInWithPassword() } }
                            .onChange(of: serverText) { problemText = nil }
                            .padding(.horizontal, 16)
                            .frame(minHeight: 52)
                            .background(Color(.secondarySystemBackground), in: .capsule)
                            .accessibilityLabel("Server address")
                        Button {
                            signInWithPassword()
                        } label: {
                            Text("Sign in").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.glassProminent)
                        .buttonBorderShape(.capsule)
                        .controlSize(.large)
                        .disabled(isServerEmpty || isConnecting)
                        if let text = problemText ?? (isAdding ? nil : app.signedOutText) {
                            Label(text, systemImage: "exclamationmark.triangle")
                                .font(.footnote)
                                .foregroundStyle(.red)
                        } else {
                            Text("The address of your TeaNode dashboard. Its sign-in page opens next.")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                    // A code from the dashboard, for a phone set up from a
                    // computer, or a link copied on this phone.
                    VStack(alignment: .leading, spacing: 12) {
                        Text("Or connect with a code from your dashboard")
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(.secondary)
                        // Side by side, or one above the other at the
                        // largest text sizes, where both would not fit.
                        let codeButtonLayout =
                            dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: 12)) : AnyLayout(HStackLayout(spacing: 12))
                        codeButtonLayout {
                            Button {
                                problemText = nil
                                isScanning = true
                            } label: {
                                Label("Scan code", systemImage: "qrcode.viewfinder").frame(maxWidth: .infinity)
                            }
                            Button {
                                problemText = nil
                                if let text = UIPasteboard.general.string {
                                    Task { await connect(text) }
                                } else {
                                    problemText = ConnectProblem.isNotALink.errorDescription
                                }
                            } label: {
                                Label("Paste link", systemImage: "doc.on.clipboard").frame(maxWidth: .infinity)
                            }
                        }
                        .buttonStyle(.glass)
                        .buttonBorderShape(.capsule)
                        .controlSize(.large)
                        Text("In the dashboard, open Settings, then Phones, and choose Connect a phone.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding()
                .disabled(isConnecting)
            }
            .scrollDismissesKeyboard(.interactively)
            .overlay {
                if isConnecting {
                    ProgressView("Connecting…")
                        .padding()
                        .glassEffect(in: .rect(cornerRadius: 16))
                }
            }
            .navigationTitle(isAdding ? String(localized: "Add account") : "TeaNode")
            .toolbar {
                if isAdding {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") {
                            app.openedLink = nil
                            app.isAddingAccount = false
                        }
                    }
                }
            }
            .sheet(isPresented: $isScanning) {
                ScanSheet { text in
                    isScanning = false
                    Task { await connect(text) }
                }
            }
            .sheet(item: $pending) { pending in
                ConfirmConnectionSheet(pending: pending) { isConfirmed in
                    self.pending = nil
                    Task {
                        if isConfirmed {
                            await app.confirm(pending)
                        } else {
                            await app.decline(pending)
                        }
                    }
                }
            }
            .sensoryFeedback(.warning, trigger: problemText) { _, new in new != nil }
            // A link opened from Safari or a message. The connection runs in a
            // task of its own: clearing the link ends any task keyed on it,
            // which cancelled the request half way (NSURLErrorDomain -999).
            .onChange(of: app.openedLink?.url, initial: true) {
                guard let link = app.openedLink else { return }
                app.openedLink = nil
                Task { await connect(link.url.absoluteString) }
            }
        }
    }

    /// Signs in on the server's own page; the person allows the app there,
    /// so nothing is asked again here. A bad address is said under the field.
    private func signInWithPassword() {
        let server: ServerAddress
        do {
            server = try ServerAddress(parsing: serverText)
        } catch {
            problemText = addressProblemText(for: error)
            return
        }
        problemText = nil
        isServerFocused = false
        Task {
            isConnecting = true
            defer { isConnecting = false }
            do {
                let pending = try await app.signIn(to: server, with: webAuthenticationSession)
                await app.confirm(pending)
            } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
                return
            } catch {
                problemText = app.describe(error)
            }
        }
    }

    private func connect(_ text: String) async {
        guard let link = URL(string: text.trimmingCharacters(in: .whitespacesAndNewlines)).flatMap(ConnectLink.init) else {
            problemText = ConnectProblem.isNotALink.errorDescription
            return
        }
        isConnecting = true
        defer { isConnecting = false }
        do {
            pending = try await app.exchange(link)
            problemText = nil
        } catch {
            problemText = app.describe(error)
        }
    }

    private func addressProblemText(for error: Error) -> String {
        switch error as? ServerAddressError {
        case .isNotEncrypted:
            return String(localized: "That address is not encrypted. Use an https address.")
        case .hasCredentials:
            return String(localized: "Leave the username and password out of the address. You sign in on the next page.")
        default:
            return String(localized: "That is not a server address.")
        }
    }
}

/// ScanSheet is the camera looking for the dashboard's code.
struct ScanSheet: View {
    let onFound: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Group {
                if CodeScanner.isAvailable {
                    CodeScanner(onFound: onFound)
                        .ignoresSafeArea()
                } else {
                    ContentUnavailableView(
                        "The camera is not available",
                        systemImage: "camera",
                        description: Text("Allow TeaNode to use the camera in Settings, or paste the code's link instead."))
                }
            }
            .navigationTitle("Scan code")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                }
            }
        }
    }
}

/// ConfirmConnectionSheet asks whether this is the right server and the
/// right account before the phone keeps the sign-in: a link names the server
/// it connects to, so one sent by somebody else could name theirs.
struct ConfirmConnectionSheet: View {
    let pending: PendingConnection
    let onAnswer: (Bool) -> Void

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "iphone.gen3.badge.checkmark")
                .font(.largeTitle)
                .foregroundStyle(.tint)
                .symbolRenderingMode(.hierarchical)
                .accessibilityHidden(true)
            Text("Connect to \(pending.account.server.host)?")
                .font(.title2)
                .bold()
                .multilineTextAlignment(.center)
            Text("This phone will be signed in as \(pending.account.username), and can do anything you can do on the dashboard.")
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            Spacer(minLength: 0)
            Button {
                onAnswer(true)
            } label: {
                Text("Connect").frame(maxWidth: .infinity)
            }
            .buttonStyle(.glassProminent)
            .controlSize(.large)
            Button(role: .cancel) {
                onAnswer(false)
            } label: {
                Text("Not this one").frame(maxWidth: .infinity)
            }
            .buttonStyle(.glass)
            .controlSize(.large)
        }
        .padding(24)
        .presentationDetents([.medium, .large])
        .interactiveDismissDisabled()
        .sensoryFeedback(.success, trigger: pending.id)
    }
}
