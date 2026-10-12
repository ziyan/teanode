import AuthenticationServices
import SwiftUI
import TeaNodeKit

/// ConnectView connects the phone to a TeaNode server with a code from the
/// server's dashboard: scanned from a computer's screen, or the link pasted
/// on a phone that is the only device at hand.
struct ConnectView: View {
    /// Shown over the signed-in app to add another account, rather than as
    /// the app's only screen.
    var isAdding = false
    @Environment(AppModel.self) private var app
    @State private var isScanning = false
    @State private var isConnecting = false
    @State private var pending: PendingConnection?
    @State private var problemText: String?
    @State private var isAskingForServer = false
    @State private var serverText = ""
    @Environment(\.webAuthenticationSession) private var webAuthenticationSession

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    VStack(alignment: .leading, spacing: 8) {
                        Image(systemName: "leaf.fill")
                            .font(.largeTitle)
                            .foregroundStyle(.tint)
                            .accessibilityHidden(true)
                        Text("Talk to your agent from your phone.")
                            .font(.title2)
                            .bold()
                        Text("Connect this phone to your TeaNode server with a code from its dashboard.")
                            .foregroundStyle(.secondary)
                    }
                    VStack(alignment: .leading, spacing: 12) {
                        Label("On a computer, open your dashboard. In Settings, open Phones and choose Connect a phone.", systemImage: "1.circle")
                        Label("Scan the code it shows.", systemImage: "2.circle")
                    }
                    .symbolRenderingMode(.hierarchical)
                    if let text = problemText ?? (isAdding ? nil : app.signedOutText) {
                        Label(text, systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.red)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding()
            }
            .safeAreaInset(edge: .bottom) {
                VStack(spacing: 12) {
                    Button {
                        problemText = nil
                        isScanning = true
                    } label: {
                        Label("Scan code", systemImage: "qrcode.viewfinder")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.glassProminent)
                    .controlSize(.large)
                    PasteButton(payloadType: String.self) { pasted in
                        guard let text = pasted.first else { return }
                        Task { await connect(text) }
                    }
                    .buttonBorderShape(.capsule)
                    Button("Sign in with a password", systemImage: "key") {
                        problemText = nil
                        isAskingForServer = true
                    }
                    .buttonStyle(.glass)
                    .buttonBorderShape(.capsule)
                    .controlSize(.large)
                    Text("Only have this phone? Sign in with your password, or open your dashboard in Safari, copy the code's link, and paste it here.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
                .padding()
                .disabled(isConnecting)
            }
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
            .sheet(isPresented: $isAskingForServer) {
                ServerSheet(serverText: $serverText) {
                    isAskingForServer = false
                    Task { await signInWithPassword() }
                }
            }
            .sensoryFeedback(.warning, trigger: problemText) { _, new in new != nil }
            #if DEBUG
                .task {
                    // The screenshots photograph the server sheet open.
                    if DemoServer.screen == "server-address" { isAskingForServer = true }
                }
            #endif
            .task(id: app.openedLink?.url) {
                guard let link = app.openedLink else { return }
                app.openedLink = nil
                await connect(link.url.absoluteString)
            }
        }
    }

    /// Signs in on the server's own page; the person allows the app there,
    /// so nothing is asked again here.
    private func signInWithPassword() async {
        let written = serverText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !written.isEmpty else { return }
        guard let server = try? ServerAddress(parsing: written) else {
            problemText = String(localized: "That is not a server address.")
            return
        }
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
}

/// ServerSheet asks for the server to sign in to with a password. It is a
/// sheet rather than an alert: an alert's text field draws past the alert's
/// edge on iOS 26, and an address wants room to be read as it is typed.
struct ServerSheet: View {
    @Binding var serverText: String
    let onContinue: () -> Void
    @Environment(\.dismiss) private var dismiss
    @FocusState private var isFocused: Bool

    private var isEmpty: Bool {
        serverText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("mail.example.com", text: $serverText)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                        .textContentType(.URL)
                        .submitLabel(.continue)
                        .focused($isFocused)
                        .onSubmit { if !isEmpty { onContinue() } }
                } footer: {
                    Text("The address of your TeaNode dashboard. Its sign-in page opens next.")
                }
            }
            .navigationTitle("Your server")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Continue", action: onContinue)
                        .disabled(isEmpty)
                }
            }
            .onAppear { isFocused = true }
        }
        .presentationDetents([.medium])
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
