import SwiftUI
import TeaNodeKit

/// The app. For now it has one screen: the address of the server to connect
/// to. Signing in comes with Milestone 5 of
/// docs/planning/ios-app-execplan.md.
@main
struct TeaNodeApp: App {
    var body: some Scene {
        WindowGroup {
            ConnectView()
        }
    }
}

/// ConnectView asks which TeaNode server to connect to, and says what is
/// wrong with an address before anything is sent to it.
struct ConnectView: View {
    @State private var typedAddress = ""
    @State private var server: ServerAddress?
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("mail.example.com", text: $typedAddress)
                        .textContentType(.URL)
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onSubmit(check)
                    Button("Connect", action: check)
                        .disabled(typedAddress.trimmingCharacters(in: .whitespaces).isEmpty)
                } header: {
                    Text("Your TeaNode server")
                } footer: {
                    if let problemText {
                        Text(problemText).foregroundStyle(.red)
                    } else if let server {
                        Text("Connecting to \(server.description) comes next.")
                    } else {
                        Text("The address of the dashboard you sign in to.")
                    }
                }
            }
            .navigationTitle("TeaNode")
        }
    }

    private func check() {
        do {
            server = try ServerAddress(parsing: typedAddress)
            problemText = nil
        } catch let error as ServerAddressError {
            server = nil
            problemText = Self.explanation(of: error)
        } catch {
            server = nil
            problemText = "That is not a server's address."
        }
    }

    static func explanation(of error: ServerAddressError) -> String {
        switch error {
        case .isEmpty:
            return "Type your server's address."
        case .isNotAnAddress:
            return "That is not a server's address."
        case .isNotEncrypted:
            return "The address has to start with https://, so what you say to your agent is encrypted on the way."
        case .hasCredentials:
            return "Leave your name and password out of the address; you sign in next."
        }
    }
}
