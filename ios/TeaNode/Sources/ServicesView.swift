import SwiftUI
import TeaNodeKit

struct ServerServicesView: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @State private var selection: ServiceSelection?
    @State private var isReviewing = false
    @State private var isSaving = false
    @State private var problemText: String?

    var body: some View {
        Form {
            if let selection {
                Section("Services") {
                    Toggle("Mail routing", isOn: binding(\.isMailRoutingEnabled))
                        .onChange(of: self.selection?.isMailRoutingEnabled) { if self.selection?.isMailRoutingEnabled == false { self.selection?.isMailboxEnabled = false } }
                    Toggle("Mailboxes", isOn: binding(\.isMailboxEnabled)).disabled(!selection.isMailRoutingEnabled)
                    Toggle("Calendar", isOn: binding(\.isCalendarEnabled))
                    Toggle("Reminders", isOn: binding(\.isRemindersEnabled))
                    Toggle("Contacts", isOn: binding(\.isContactsEnabled))
                }
                Section {
                    Button("Review changes") { isReviewing = true }.disabled(isSaving)
                    .confirmationDialog("Apply service changes?", isPresented: $isReviewing, titleVisibility: .visible) {
                        Button("Apply changes") {
                            guard let selection else { return }
                            isSaving = true
                            Task {
                                defer { isSaving = false }
                                do { try await connection.updateServices(selection); await app.refreshServices() } catch { problemText = app.describe(error) }
                            }
                        }
                    }
                } footer: {
                    Text("Turning a service off keeps its data and pauses its pending work. A restart is needed to enable new services.")
                }
                Section {
                    NavigationLink {
                        ServerAboutView(connection: connection)
                    } label: {
                        Label("Restart it on the About page", systemImage: "arrow.clockwise")
                    }
                }
            } else { ProgressView() }
            if let problemText { Text(problemText).foregroundStyle(.red) }
        }
        .navigationTitle("Services")
        .task { do { selection = try await connection.serviceSelection() } catch { problemText = app.describe(error) } }
    }
    private func binding(_ keyPath: WritableKeyPath<ServiceSelection, Bool>) -> Binding<Bool> {
        Binding(get: { selection?[keyPath: keyPath] ?? false }, set: { selection?[keyPath: keyPath] = $0 })
    }
}

struct SyncCredentialsView: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @State private var credentials: [SyncCredential] = []
    @State private var credentialName = ""
    @State private var hasCalendarAccess = false
    @State private var hasRemindersAccess = false
    @State private var hasContactsAccess = false
    @State private var created: CreatedSyncCredential?
    @State private var revoking: SyncCredential?
    @State private var isWorking = false
    @State private var problemText: String?

    var body: some View {
        Form {
            Section {
                NavigationLink { DeviceSetupView(connection: connection) } label: {
                    Label("Set up on this iPhone", systemImage: "iphone")
                }
            } footer: {
                Text("Choose Mail, Calendar, Reminders and Contacts, then install one profile. A mailbox is optional.")
            }
            Section("Manual setup") {
                TextField("Device name", text: $credentialName)
                if app.services?.isCalendarEnabled == true { Toggle("Calendar", isOn: $hasCalendarAccess) }
                if app.services?.isRemindersEnabled == true { Toggle("Reminders", isOn: $hasRemindersAccess) }
                if app.services?.isContactsEnabled == true { Toggle("Contacts", isOn: $hasContactsAccess) }
                Button("Create sync password") {
                    isWorking = true
                    Task {
                        defer { isWorking = false }
                        do { created = try await connection.createSyncCredential(credentialName: credentialName, hasCalendarAccess: hasCalendarAccess, hasRemindersAccess: hasRemindersAccess, hasContactsAccess: hasContactsAccess); await load() } catch { problemText = app.describe(error) }
                    }
                }.disabled(isWorking || credentialName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !(hasCalendarAccess || hasRemindersAccess || hasContactsAccess))
            }

            if let created {
                Section("Save this password now") {
                    Text(created.username).textSelection(.enabled)
                    Text(created.password).monospaced().textSelection(.enabled)
                    Text(created.principalPath).monospaced().textSelection(.enabled)
                }
            }
            Section("Devices") {
                ForEach(credentials.newestFirst()) { credential in
                    HStack {
                        DevicePasswordRow(name: credential.credentialName, scopes: scopeText(credential), password: credential)
                        Spacer()
                        Button("Revoke", role: .destructive) { revoking = credential }.disabled(isWorking)
                        .confirmationDialog("Revoke this device's sync access?", isPresented: Binding(get: { revoking?.id == credential.id }, set: { if !$0 { revoking = nil } }), titleVisibility: .visible) {
                            if let revoking {
                                Button("Revoke", role: .destructive) {
                                    isWorking = true
                                    Task { defer { isWorking = false }; do { try await connection.revokeSyncCredential(revoking.id); await load() } catch { problemText = app.describe(error) } }
                                }
                            }
                        }
                    }
                }
            }
            if let problemText { Text(problemText).foregroundStyle(.red) }
        }
        .navigationTitle("Device sync")
        .task { await load() }
    }
    private func scopeText(_ credential: SyncCredential) -> String {
        [
            credential.hasCalendarAccess ? String(localized: "Calendar") : nil,
            credential.hasRemindersAccess ? String(localized: "Reminders") : nil,
            credential.hasContactsAccess ? String(localized: "Contacts") : nil,
        ].compactMap { $0 }.joined(separator: ", ")
    }
    private func load() async { do { credentials = try await connection.syncCredentials() } catch { problemText = app.describe(error) } }
}
