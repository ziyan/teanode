import SwiftUI
import TeaNodeKit

/// ConnectView connects the phone to a TeaNode server with a code from the
/// server's dashboard: scanned from a computer's screen, or the link pasted
/// on a phone that is the only device at hand.
struct ConnectView: View {
    @Environment(AppModel.self) private var app
    @State private var isScanning = false
    @State private var isConnecting = false
    @State private var pending: PendingConnection?
    @State private var problemText: String?

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
                    if let text = problemText ?? app.signedOutText {
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
                    Text("Only have this phone? Open your dashboard in Safari, copy the code's link, and paste it here.")
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
            .navigationTitle("TeaNode")
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
            .task(id: app.openedLink?.url) {
                guard let link = app.openedLink else { return }
                app.openedLink = nil
                await connect(link.url.absoluteString)
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
