import SwiftUI

/// ServerSetupGuideView tells somebody without a server how to run one: the
/// server is free and open source, it runs on a computer they control, and
/// this app signs in to it once it has an https address.
struct ServerSetupGuideView: View {
    /// The one command that installs and starts the assistant server.
    static let installCommand = "curl -fsSL https://teanode.com/get.sh | sh"
    static let guideAddress = URL(string: "https://teanode.com/doc/getting-started")!
    static let installScriptAddress = URL(string: "https://github.com/ziyan/teanode/blob/main/deploy/get.sh")!
    static let sourceAddress = URL(string: "https://github.com/ziyan/teanode")!

    @State private var hasCopiedCommand = false

    var body: some View {
        List {
            Section {
                Text("TeaNode is free and open source. You run the server yourself, on a computer you control, and this app signs in to it. There is nothing to buy and no TeaNode account to make.")
            }
            Section {
                Text("A Linux computer you own or rent, or a Mac with Docker Desktop. The assistant needs no domain and no mail ports.")
            } header: {
                Text("1. A computer that stays on")
            }
            Section {
                Text(Self.installCommand)
                    .font(.body.monospaced())
                    .textSelection(.enabled)
                Button {
                    UIPasteboard.general.string = Self.installCommand
                    hasCopiedCommand = true
                } label: {
                    Label(
                        hasCopiedCommand ? LocalizedStringKey("Copied") : "Copy command",
                        systemImage: hasCopiedCommand ? "checkmark" : "doc.on.doc"
                    )
                    .contentTransition(.symbolEffect(.replace))
                    .actionColor()
                }
                Link(destination: Self.installScriptAddress) {
                    Label("Read the script first", systemImage: "doc.text")
                        .foregroundStyle(.primary)
                }
            } header: {
                Text("2. Install TeaNode")
            } footer: {
                Text("Run it on that computer. It installs Docker on Linux if it is missing, then starts TeaNode and its database.")
            }
            Section {
                // Addresses to type are code, so they are not links: on the phone
                // they would open the phone itself.
                Text("Open `http://127.0.0.1:8080` on that computer. The first person to open it creates the administrator account. Then choose the model service your agent thinks with, OpenAI, Anthropic, Gemini or your own, and press Start my assistant.")
            } header: {
                Text("3. Claim the first account")
            }
            Section {
                Text("This app signs in only over https. Put a reverse proxy with a certificate in front of port 8080, such as Caddy, nginx with Let's Encrypt, or a tunnel service, on a name such as `teanode.example.com`.")
            } header: {
                Text("4. Give it an https address")
            }
            Section {
                Text("Go back and type that address, or open Settings, then Devices, then Connect a phone in the dashboard and scan its code.")
            } header: {
                Text("5. Sign in here")
            }
            Section {
                Link(destination: Self.guideAddress) {
                    Label("Full setup guide", systemImage: "book")
                        .foregroundStyle(.primary)
                }
                Link(destination: Self.sourceAddress) {
                    Label("Source code on GitHub", systemImage: "chevron.left.forwardslash.chevron.right")
                        .foregroundStyle(.primary)
                }
            } footer: {
                Text("The full guide also covers hosting your own mail.")
            }
        }
        .sensoryFeedback(.success, trigger: hasCopiedCommand) { _, new in new }
        .readableListWidth()
        .navigationTitle("Set up a server")
        .navigationBarTitleDisplayMode(.inline)
    }
}

#Preview("Light") {
    NavigationStack { ServerSetupGuideView() }
}

#Preview("Dark") {
    NavigationStack { ServerSetupGuideView() }
        .preferredColorScheme(.dark)
}
