import SwiftUI
import TeaNodeKit

/// DeviceSetupView shares one account-wide service selection across every setup shortcut.
struct DeviceSetupView: View {
    let connection: Connection
    var preselectedMailboxId: String? = nil
    @Environment(AppModel.self) private var app
    @Environment(\.openURL) private var openURL
    @State private var capabilities: DeviceSetupCapabilities?
    @State private var selection: DeviceSetupSelection?
    @State private var profile: CreatedDeviceSetupProfile?
    @State private var hasOpenedProfile = false
    @State private var isCreating = false
    @State private var problemText: String?
    @State private var loadProblemText: String?

    var body: some View {
        Form {
            if let profile {
                Section {
                    TimelineView(.periodic(from: .now, by: 1)) { context in
                        VStack(alignment: .leading, spacing: 12) {
                            if profile.hasExpired(at: context.date) {
                                Label("Setup link expired", systemImage: "clock").foregroundStyle(.secondary)
                                Text("This setup link has expired. Existing device passwords remain active until you revoke them.")
                                    .foregroundStyle(.secondary)
                            } else {
                                Label("Your setup profile is ready", systemImage: "checkmark.circle")
                                if hasOpenedProfile {
                                    Text("Profile download started").foregroundStyle(.secondary)
                                } else {
                                    Button {
                                        Task { await download(profile) }
                                    } label: {
                                        Label("Download profile", systemImage: "arrow.down.circle")
                                            .lineLimit(nil).fixedSize(horizontal: false, vertical: true)
                                    }.buttonStyle(.glassProminent)
                                    if let profileURL = URL(string: profile.downloadLink) {
                                        ShareLink(item: profileURL).lineLimit(nil).fixedSize(horizontal: false, vertical: true)
                                    }
                                }
                            }
                        }
                    }
                    if let expiration = ServerTime.date(profile.expiresAt) {
                        LabeledContent("Download link expiration", value: expiration.formatted(date: .abbreviated, time: .shortened))
                    }
                } header: {
                    Text("Install the profile")
                } footer: {
                    Text("Safari downloads the profile. Open Settings, tap Profile Downloaded, then install it. The download link works once.")
                }
                Section("Access for this device") {
                    if profile.mailboxAppPassword != nil {
                        Label("Mail uses a mailbox app password", systemImage: "envelope")
                        if let mailboxId = profile.mailboxAppPassword?.mailboxId {
                            NavigationLink {
                                DeviceMailPasswordsScreen(connection: connection, username: app.account?.username ?? "", mailboxId: mailboxId)
                            } label: {
                                Label("Manage mail passwords", systemImage: "key")
                            }
                        }
                    }
                    if profile.syncCredential != nil {
                        Label("Collections use an account sync password", systemImage: "arrow.triangle.2.circlepath")
                        NavigationLink {
                            SyncCredentialsView(connection: connection)
                        } label: {
                            Label("Manage sync passwords", systemImage: "key")
                        }
                    }
                }
            } else if let capabilities, capabilities.hasAvailableService, let selection {
                Section("This iPhone") {
                    TextField("Device name", text: binding(\.deviceName, fallback: ""))
                        .textInputAutocapitalization(.words)
                }
                Section {
                    if capabilities.canSetUpMail {
                        Toggle("Mail", isOn: binding(\.hasMailAccess, fallback: false))
                        if selection.hasMailAccess {
                            Picker("Mailbox", selection: binding(\.mailboxId, fallback: nil)) {
                                Text("Choose a mailbox").tag(String?.none)
                                ForEach(capabilities.mailboxes) { mailbox in
                                    Text(mailbox.name.isEmpty ? mailbox.addresses.first?.address ?? String(localized: "Mailbox") : mailbox.name)
                                        .tag(Optional(mailbox.id))
                                }
                            }
                        }
                    }
                    if capabilities.canSetUpCalendar { Toggle("Calendar", isOn: binding(\.hasCalendarAccess, fallback: false)) }
                    if capabilities.canSetUpReminders { Toggle("Reminders", isOn: binding(\.hasRemindersAccess, fallback: false)) }
                    if capabilities.canSetUpContacts { Toggle("Contacts", isOn: binding(\.hasContactsAccess, fallback: false)) }
                } header: {
                    Text("Choose what to add")
                } footer: {
                    Text("Choose only the services you want on this iPhone. Calendar, Reminders and Contacts do not need a mailbox.")
                }
                Section {
                    Button {
                        Task { await create() }
                    } label: {
                        if isCreating { ProgressView() } else { Label("Create setup profile", systemImage: "iphone") }
                    }
                    .buttonStyle(.glassProminent)
                    .disabled(isCreating || !selection.canCreateProfile(capabilities: capabilities))
                } footer: {
                    Text(
                        "The profile signs in with separate mail and sync passwords. You can revoke either without changing your account password."
                    )
                }
            } else if capabilities != nil {
                ContentUnavailableView(
                    "No services available", systemImage: "iphone",
                    description: Text("No Mail, Calendar, Reminders or Contacts service is available for this account."))
            } else if let loadProblemText {
                ContentUnavailableView {
                    Label("Setup could not be read", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(loadProblemText)
                } actions: {
                    Button("Try again") {
                        Task { await load() }
                    }
                }
            } else {
                ProgressView()
            }
        }
        .toast(done: .constant(nil), problem: $problemText)
        .navigationTitle("Set up on this iPhone")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .disabled(isCreating)
    }

    private func binding<Value>(_ keyPath: WritableKeyPath<DeviceSetupSelection, Value>, fallback: Value) -> Binding<Value> {
        Binding(get: { selection?[keyPath: keyPath] ?? fallback }, set: { selection?[keyPath: keyPath] = $0 })
    }

    private func load() async {
        do {
            let available = try await connection.deviceSetupCapabilities()
            capabilities = available
            if profile == nil, let currentSelection = selection {
                selection = currentSelection.retainingAvailableServices(capabilities: available)
            } else if selection == nil {
                selection = DeviceSetupSelection(
                    deviceName: UIDevice.current.name, capabilities: available, preselectedMailboxId: preselectedMailboxId)
            }
            #if DEBUG
                if ["device-setup-profile", "device-setup-expired", "device-setup-receipt-unavailable"].contains(DemoServer.screen), profile == nil {
                    profile = try JSONDecoder().decode(
                        CreatedDeviceSetupProfile.self,
                        from: Data(
                            #"{"downloadPath":"/api/v1/mailbox/profile/demo.mobileconfig","downloadLink":"https://demo.example.com/api/v1/mailbox/profile/demo.mobileconfig","expiresAt":"2099-01-01T00:00:00Z","mailboxAppPassword":{"id":"password-demo","mailboxId":"mailbox-demo","name":"Phone","createdAt":"2000-01-01T00:00:00Z","lastUsedAt":null},"syncCredential":{"id":"sync-demo","credentialName":"Phone","hasCalendarAccess":true,"hasRemindersAccess":true,"hasContactsAccess":true}}"#
                                .replacingOccurrences(
                                    of: "2099-01-01T00:00:00Z",
                                    with: Date().addingTimeInterval(DemoServer.screen == "device-setup-expired" ? -300 : 300).formatted(.iso8601)
                                ).utf8))
                }
            #endif
            loadProblemText = nil
        } catch { loadProblemText = app.describe(error) }
    }

    private func create() async {
        guard !isCreating, let selection, let capabilities else { return }
        isCreating = true
        defer { isCreating = false }
        do {
            let created = try await connection.createDeviceSetupProfile(selection, capabilities: capabilities)
            profile = created
            problemText = nil
        } catch { problemText = app.describe(error) }
    }

    private func download(_ profile: CreatedDeviceSetupProfile) async {
        do {
            let profileURL = try profile.downloadURL(server: await connection.account.server)
            openURL(profileURL) { hasOpened in
                hasOpenedProfile = hasOpened
                problemText = hasOpened ? nil : String(localized: "Safari could not open the profile. Try downloading it again.")
            }
        } catch {
            if let expiration = ServerTime.date(profile.expiresAt), expiration <= Date() {
                problemText = String(localized: "This setup link has expired. Existing device passwords remain active until you revoke them.")
            } else {
                problemText = app.describe(error)
            }
        }
    }
}

private struct DeviceMailPasswordsScreen: View {
    @State private var mail: MailModel
    let mailboxId: String
    init(connection: Connection, username: String, mailboxId: String) {
        _mail = State(initialValue: MailModel(connection: connection, username: username))
        self.mailboxId = mailboxId
    }
    var body: some View {
        MailAppPasswordsView(mail: mail, mailboxId: mailboxId).task {
            await mail.load()
            if mail.entries.contains(where: { $0.mailbox?.id == mailboxId }) { mail.currentMailboxId = mailboxId }
        }
    }
}
