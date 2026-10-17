import PhotosUI
import SwiftUI
import TeaNodeKit

/// PreferencesView is the dashboard's Profile: what to call the person, what
/// they sign in with, where the server writes to them, and their time zone.
struct PreferencesView: View {
    let connection: Connection
    /// Told the profile once it is saved, for the screens that show the
    /// person's name.
    var onSaved: (@MainActor (AccountProfile) -> Void)? = nil
    @State private var draft: ProfileDraft?
    @State private var isLoaded = false
    @State private var hasNoAccount = false
    @State private var isSaving = false
    @State private var isConfirmingRename = false
    /// The profile as the server last had it, for the picture: the form's
    /// draft keeps what is being typed, and a picture is saved on its own.
    @State private var pictureProfile: AccountProfile?
    @State private var pickedItem: PhotosPickerItem?
    @State private var isChangingPicture = false
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        Form {
            if let draft = Binding($draft) {
                if let pictureProfile {
                    Section {
                        AvatarView(
                            name: pictureProfile.displayName, avatarUrl: pictureProfile.avatarUrl,
                            colorIndex: pictureProfile.avatarColorIndex, size: 96
                        )
                        .frame(maxWidth: .infinity)
                        .listRowBackground(Color.clear)
                        PhotosPicker(selection: $pickedItem, matching: .images) {
                            Label("Choose a picture", systemImage: "photo")
                        }
                        .disabled(isChangingPicture)
                        if pictureProfile.avatarUrl?.isEmpty == false {
                            Button("Remove picture", systemImage: "trash", role: .destructive) {
                                Task { await removePicture() }
                            }
                            .disabled(isChangingPicture)
                        }
                    } header: {
                        Text("Picture")
                    } footer: {
                        Text("Shown beside your name to the people you share a circle with. Without one, your initial is shown.")
                    }
                }
                Section {
                    TextField("Name", text: draft.name, prompt: Text("how you would like to be greeted"))
                        .textContentType(.name)
                } header: {
                    Text("Name")
                } footer: {
                    Text("Shown in TeaNode. Optional: without one, your username is used.")
                }
                Section {
                    TextField("Username", text: draft.username)
                        .textContentType(.username)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } header: {
                    Text("Username")
                } footer: {
                    Text("What you sign in with. Changing it takes your sessions and API tokens with it, so nothing has to be signed in again.")
                }
                Section {
                    TextField("Notification address", text: draft.email, prompt: Text(verbatim: "you@example.com"))
                        .keyboardType(.emailAddress)
                        .textContentType(.emailAddress)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } header: {
                    Text("Notification address")
                } footer: {
                    Text("Where this server writes to you, such as when a domain’s DNS records stop resolving. Optional.")
                }
                Section {
                    Picker("Time zone", selection: timezoneModeBinding(draft)) {
                        Text("Follows my browser (\(automaticTimezone(draft.wrappedValue)))").tag(AccountProfile.TimezoneMode.auto)
                        Text("Always this zone").tag(AccountProfile.TimezoneMode.fixed)
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                    if draft.wrappedValue.timezoneMode == .fixed {
                        TextField("Time zone", text: draft.timezone, prompt: Text(verbatim: "Europe/Berlin"))
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    }
                } header: {
                    Text("Time zone")
                } footer: {
                    Text("Every time your agent tells you (a reminder, a brief, when a reply goes out) is in this zone. Following the browser means it moves when you travel.")
                }
            }
        }
        .overlay {
            if !isLoaded {
                ProgressView()
            } else if hasNoAccount {
                ContentUnavailableView {
                    Label("No profile", systemImage: "person.crop.circle.badge.questionmark")
                } description: {
                    Text("This server has no accounts, so there is no profile to change.")
                }
            }
        }
        .navigationTitle("Profile")
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                if isSaving {
                    ProgressView()
                } else {
                    Button("Save") {
                        // Renaming is the one change here that alters how the
                        // person signs in, so it is asked about first.
                        if draft?.isRenaming == true {
                            isConfirmingRename = true
                        } else {
                            Task { await save() }
                        }
                    }
                    .disabled(draft?.canSave != true)
                    .confirmationDialog("Change your username?", isPresented: $isConfirmingRename, titleVisibility: .visible) {
                        Button("Change it") {
                            Task { await save() }
                        }
                    } message: {
                        if let draft {
                            Text("You will sign in as \(draft.username.trimmed) instead of \(draft.profile.username) from now on. Your sessions and API tokens move with the account.")
                        }
                    }
                }
            }
        }
        .task { await load() }
        .onChange(of: pickedItem) { _, item in
            guard let item else { return }
            pickedItem = nil
            Task { await setPicture(item) }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    /// Reads the chosen picture, whatever the library kept it as (HEIC among
    /// them), and sends it as a JPEG.
    private func setPicture(_ item: PhotosPickerItem) async {
        isChangingPicture = true
        defer { isChangingPicture = false }
        do {
            guard let pictureData = try await item.loadTransferable(type: Data.self), let jpeg = AvatarPicture.jpeg(from: pictureData) else {
                problemText = String(localized: "That picture could not be read.")
                return
            }
            let saved = try await connection.setAccountAvatar(jpeg)
            pictureProfile = saved
            doneText = String(localized: "Picture saved")
            onSaved?(saved)
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func removePicture() async {
        isChangingPicture = true
        defer { isChangingPicture = false }
        do {
            let saved = try await connection.removeAccountAvatar()
            pictureProfile = saved
            doneText = String(localized: "Picture removed")
            onSaved?(saved)
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// The zone the account follows now, or this phone's when the dashboard
    /// has not said one yet.
    private func automaticTimezone(_ draft: ProfileDraft) -> String {
        let accountTimezone = draft.profile.timezone ?? ""
        return accountTimezone.isEmpty ? TimeZone.current.identifier : accountTimezone
    }

    private func timezoneModeBinding(_ draft: Binding<ProfileDraft>) -> Binding<AccountProfile.TimezoneMode> {
        Binding(
            get: { draft.wrappedValue.timezoneMode },
            set: { draft.wrappedValue.choose($0, deviceTimezone: TimeZone.current.identifier) })
    }

    private func load() async {
        do {
            let profile = try await connection.accountProfile()
            draft = profile.map { ProfileDraft($0) }
            pictureProfile = profile
            hasNoAccount = profile == nil
        } catch {
            problemText = error.localizedDescription
        }
        isLoaded = true
    }

    private func save() async {
        guard let saving = draft else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            let saved = try await connection.updateAccountProfile(saving)
            draft = ProfileDraft(saved)
            doneText = String(localized: "Saved")
            onSaved?(saved)
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// AvatarPicture makes the bytes of a chosen picture into what the server takes
/// as a person's picture.
enum AvatarPicture {
    /// The longest side a picture is sent at; the server keeps 512 pixels, so
    /// this is only to keep the upload small.
    static let longestSide: CGFloat = 2048

    /// A JPEG at most longestSide on its longest side, whatever the picture was
    /// taken as. Drawing it again also turns it upright and leaves every
    /// metadata block, the location included, behind. Nil when the bytes are
    /// not a picture.
    static func jpeg(from pictureData: Data) -> Data? {
        guard let image = UIImage(data: pictureData), image.size.width > 0, image.size.height > 0 else { return nil }
        let scale = min(1, longestSide / max(image.size.width, image.size.height))
        let size = CGSize(width: (image.size.width * scale).rounded(), height: (image.size.height * scale).rounded())
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let redrawn = UIGraphicsImageRenderer(size: size, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: size))
        }
        return redrawn.jpegData(compressionQuality: 0.9)
    }
}

/// ChangePasswordView changes the person's password. The current one is
/// asked for even though the phone is signed in, because a phone can be
/// left unlocked.
struct ChangePasswordView: View {
    let connection: Connection
    @State private var draft = PasswordChangeDraft()
    @State private var username = ""
    @State private var isChanging = false
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        Form {
            Section {
                SecureField("Current password", text: $draft.currentPassword)
                    .textContentType(.password)
            } footer: {
                if !username.isEmpty {
                    Text("Signed in as \(username).")
                }
            }
            Section {
                SecureField("New password", text: $draft.newPassword)
                    .textContentType(.newPassword)
                SecureField("New password again", text: $draft.newPasswordAgain)
                    .textContentType(.newPassword)
            } footer: {
                if draft.doesConfirmationDiffer {
                    Label("These do not match.", systemImage: "exclamationmark.circle")
                }
            }
        }
        .navigationTitle("Password")
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                if isChanging {
                    ProgressView()
                } else {
                    Button("Change") {
                        Task { await change() }
                    }
                    .disabled(!draft.canChange)
                }
            }
        }
        .task {
            username = (try? await connection.username()) ?? ""
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private func change() async {
        let changing = draft
        isChanging = true
        defer { isChanging = false }
        do {
            try await connection.changePassword(currentPassword: changing.currentPassword, newPassword: changing.newPassword)
            draft = PasswordChangeDraft()
            doneText = String(localized: "Password changed.")
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// DevicesView is the one place for connecting a device, as the dashboard's
/// Devices page is: the TeaNode app on phones, and the built-in apps of this
/// iPhone, which the setup profile fills with mail, calendars and contacts.
struct DevicesView: View {
    let connection: Connection

    var body: some View {
        List {
            Section {
                NavigationLink {
                    PhonesView(connection: connection)
                } label: {
                    Label("Phones", systemImage: "iphone")
                }
            } header: {
                Text("TeaNode app")
            } footer: {
                Text("The TeaNode app on your phones, and what each is told about.")
            }
            Section {
                NavigationLink { DeviceSetupView(connection: connection) } label: {
                    Label("Set up on this iPhone", systemImage: "iphone")
                }
                NavigationLink { SyncCredentialsView(connection: connection) } label: {
                    Label("Sync passwords", systemImage: "key")
                }
            } header: {
                Text("Built-in apps")
            } footer: {
                Text("Add mail, calendars, reminders and contacts to the apps already on an iPhone or Mac.")
            }
        }
        .navigationTitle("Devices")
    }
}

/// PhonesView is the phones the app is connected on: what each is told
/// about, whether notifications reach it, a test, and removing one.
struct PhonesView: View {
    let connection: Connection
    @State private var phoneList: PairedPhoneList?
    @State private var isLoaded = false
    @State private var removing: PairedPhone?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let phoneList {
                Section {
                    if phoneList.phones.isEmpty {
                        Text("No phones yet. Connect one to talk to your agent from it and be told when something needs you.")
                            .foregroundStyle(.secondary)
                    }
                    ForEach(phoneList.phones) { phone in
                        NavigationLink {
                            PhoneSettingsView(connection: connection, phone: phone, allNotificationKinds: phoneList.notificationKinds) { said in
                                doneText = said
                                await load()
                            }
                        } label: {
                            PairedPhoneRow(phone: phone, allNotificationKinds: phoneList.notificationKinds)
                        }
                        .swipeActions {
                            Button("Remove", systemImage: "trash", role: .destructive) { removing = phone }
                            if phone.canBeNotified {
                                Button("Test", systemImage: "bell.badge") {
                                    Task { await test(phone) }
                                }
                                .tint(.indigo)
                            }
                        }
                        .contextMenu {
                            if phone.canBeNotified {
                                Button("Send a test notification", systemImage: "bell.badge") {
                                    Task { await test(phone) }
                                }
                            }
                            Button("Remove", systemImage: "trash", role: .destructive) { removing = phone }
                        }
                        .confirmationDialog(
                            "Remove this phone?", isPresented: Binding(get: { removing?.id == phone.id }, set: { if !$0 { removing = nil } }), titleVisibility: .visible
                        ) {
                            Button("Remove", role: .destructive) {
                                guard let phone = removing else { return }
                                Task { await remove(phone) }
                            }
                        } message: {
                            Text("\(PairedPhoneRow.displayName(removing)) is signed out of TeaNode and told nothing more. Connect it again to use it.")
                        }
                    }
                } footer: {
                    if phoneList.isPushEnabled {
                        Text("The phones the TeaNode app is connected on, and what each is told about.")
                    } else {
                        Text("Notifications to phones are off on this server, so phones are told nothing until the operator turns them on.")
                    }
                }
            }
        }
        .overlay {
            if !isLoaded { ProgressView() }
        }
        .navigationTitle("Phones")
        .task { await load() }
        .refreshable { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    private func load() async {
        do {
            phoneList = try await connection.pairedPhones()
        } catch {
            problemText = error.localizedDescription
        }
        isLoaded = true
    }

    private func test(_ phone: PairedPhone) async {
        do {
            try await connection.testPhone(phone.id)
            doneText = String(localized: "Sent \(PairedPhoneRow.displayName(phone)) a notification; it should arrive in a few seconds.")
            await load()
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func remove(_ phone: PairedPhone) async {
        do {
            try await connection.removePhone(phone.id)
            doneText = String(localized: "Removed \(PairedPhoneRow.displayName(phone)).")
            await load()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// PairedPhoneRow is one phone in the list: its name, and under it the
/// model, what it is told about and why the last notification failed.
/// Only a phone notifications do not reach is marked.
struct PairedPhoneRow: View {
    let phone: PairedPhone
    /// Every kind there is, so a phone told about all of them says "All
    /// notifications" rather than naming each.
    var allNotificationKinds: [String] = []

    var body: some View {
        // The warning is centered on the whole row, level with the
        // disclosure chevron, rather than on the name's line.
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 2) {
                Text(Self.displayName(phone))
                // Which notifications it would get is left out while it gets none, as on the dashboard: the
                // two read as a contradiction side by side.
                let detailText = [
                    phone.modelDisplayName, phone.notificationState == .notRegistered ? "" : notificationsText, phone.lastDeliveryError,
                ]
                    .filter { !$0.isEmpty }
                    .joined(separator: " · ")
                if !detailText.isEmpty {
                    Text(detailText)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            switch phone.notificationState {
            case .notRegistered:
                Label("Notifications off on this phone", systemImage: "bell.slash")
                    .labelStyle(.tag)
                    .font(.caption)
                    .foregroundStyle(.orange)
            case .lastFailed:
                Label("Last notification failed", systemImage: "exclamationmark.triangle")
                    .labelStyle(.tag)
                    .font(.caption)
                    .foregroundStyle(.red)
            case .notified:
                EmptyView()
            }
        }
        .symbolRenderingMode(.hierarchical)
    }

    /// The phone's name, or a stand-in for one that has none.
    static func displayName(_ phone: PairedPhone?) -> String {
        guard let phone, !phone.phoneName.isEmpty else { return String(localized: "A phone with no name") }
        return phone.phoneName
    }

    private var notificationsText: String {
        if phone.notificationKinds.isEmpty { return String(localized: "No notifications chosen") }
        if !allNotificationKinds.isEmpty, Set(allNotificationKinds).isSubset(of: Set(phone.notificationKinds)) {
            return String(localized: "All notifications")
        }
        return phone.notificationKinds.map { NotificationKindToggle.label(of: $0) }.joined(separator: ", ")
    }
}

/// PhoneSettingsView changes one phone's name and what it is told about.
struct PhoneSettingsView: View {
    let connection: Connection
    /// Every kind a phone may choose, in the server's order.
    let allNotificationKinds: [String]
    /// Told what to say once the phone is saved, before this screen closes.
    let onSaved: @MainActor (String) async -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var draft: PairedPhoneDraft
    @State private var isSaving = false
    @State private var doneText: String?
    @State private var problemText: String?

    init(connection: Connection, phone: PairedPhone, allNotificationKinds: [String], onSaved: @escaping @MainActor (String) async -> Void) {
        self.connection = connection
        self.allNotificationKinds = allNotificationKinds
        self.onSaved = onSaved
        _draft = State(initialValue: PairedPhoneDraft(phone))
    }

    var body: some View {
        Form {
            Section("Name") {
                TextField("Name", text: $draft.phoneName)
            }
            Section("Tell this phone about") {
                ForEach(allNotificationKinds, id: \.self) { notificationKind in
                    Toggle(NotificationKindToggle.label(of: notificationKind), isOn: notificationKindBinding(notificationKind))
                }
            }
            if draft.phone.canBeNotified {
                Section {
                    Button("Send a test notification", systemImage: "bell.badge") {
                        Task { await test() }
                    }
                }
            }
        }
        .navigationTitle("Edit phone")
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                if isSaving {
                    ProgressView()
                } else {
                    Button("Save") {
                        Task { await save() }
                    }
                    .disabled(!draft.canSave)
                }
            }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private func notificationKindBinding(_ notificationKind: String) -> Binding<Bool> {
        Binding(
            get: { draft.notificationKinds.contains(notificationKind) },
            set: { isOn in
                if isOn {
                    draft.notificationKinds.insert(notificationKind)
                } else {
                    draft.notificationKinds.remove(notificationKind)
                }
            })
    }

    /// Sends only what changed: the name and the kinds are saved apart.
    private func save() async {
        let saving = draft
        let phoneId = saving.phone.id
        isSaving = true
        defer { isSaving = false }
        do {
            if saving.hasNameChanged {
                try await connection.renamePhone(phoneId, phoneName: saving.phoneName.trimmed)
            }
            if saving.hasNotificationKindsChanged {
                _ = try await connection.updatePhone(phoneId, notificationKinds: saving.orderedNotificationKinds(among: allNotificationKinds))
            }
            await onSaved(String(localized: "Saved"))
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func test() async {
        do {
            try await connection.testPhone(draft.phone.id)
            doneText = String(localized: "Sent \(PairedPhoneRow.displayName(draft.phone)) a notification; it should arrive in a few seconds.")
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// PasskeysView is the passkeys that can sign in as the person, and removing
/// one. New passkeys are added on the dashboard: a passkey belongs to the
/// server's own domain, and this app can only make passkeys for the domains
/// it was built with.
struct PasskeysView: View {
    let connection: Connection
    @State private var passkeys: [Passkey] = []
    @State private var policy: PasskeyPolicy?
    @State private var isLoaded = false
    @State private var removing: Passkey?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if isLoaded {
                Section {
                    if passkeys.isEmpty {
                        Text("No passkeys. You sign in with your password.")
                            .foregroundStyle(.secondary)
                    }
                    ForEach(passkeys) { passkey in
                        PasskeyRow(passkey: passkey)
                            .swipeActions {
                                Button("Remove", systemImage: "trash", role: .destructive) { removing = passkey }
                            }
                            .contextMenu {
                                Button("Remove", systemImage: "trash", role: .destructive) { removing = passkey }
                            }
                            .confirmationDialog(
                                "Remove this passkey?", isPresented: Binding(get: { removing?.id == passkey.id }, set: { if !$0 { removing = nil } }), titleVisibility: .visible
                            ) {
                                Button("Remove", role: .destructive) {
                                    guard let passkey = removing else { return }
                                    Task { await remove(passkey) }
                                }
                            } message: {
                                Text("\(removing?.name ?? "") stops being able to sign in. Your password still works, so this does not lock you out.")
                            }
                    }
                } footer: {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("A passkey signs a challenge with a key your phone, laptop or security key holds and never gives out. There is no shared secret, so there is nothing to leak or be phished out of you.")
                        if policy?.enabled == false {
                            Text("This server does not offer passkeys.")
                        } else {
                            Text("New passkeys are added on the dashboard, in Settings, then Passkeys.")
                        }
                    }
                }
            }
        }
        .overlay {
            if !isLoaded { ProgressView() }
        }
        .navigationTitle("Passkeys")
        .task { await load() }
        .refreshable { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    private func load() async {
        do {
            let read = try await connection.passkeys()
            passkeys = read.passkeys
            policy = read.policy
        } catch {
            problemText = error.localizedDescription
        }
        isLoaded = true
    }

    private func remove(_ passkey: Passkey) async {
        do {
            try await connection.deletePasskey(passkey.id)
            doneText = String(localized: "Removed \(passkey.name).")
            await load()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// PasskeyRow is one passkey: its name, whether it is synced to the
/// person's other devices or kept on one, when it was registered and when
/// it last signed in.
struct PasskeyRow: View {
    let passkey: Passkey

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline) {
                Text(passkey.name)
                Spacer()
                // Synced or not decides what losing the device costs.
                Label(
                    passkey.isSynced ? LocalizedStringKey("Synced") : LocalizedStringKey("This device only"),
                    systemImage: passkey.isSynced ? "icloud" : "key")
                    .labelStyle(.tag)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .symbolRenderingMode(.hierarchical)
            }
            Text(detailText)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    private var detailText: String {
        var parts: [String] = []
        if let createdDate = passkey.createdDate {
            parts.append(String(localized: "Registered \(createdDate.formatted(date: .abbreviated, time: .shortened))"))
        }
        if let lastUsedDate = passkey.lastUsedDate {
            let lastUsed = String(localized: "Last used \(lastUsedDate.formatted(.relative(presentation: .named)))")
            let address = passkey.ip ?? ""
            parts.append(address.isEmpty ? lastUsed : "\(lastUsed) (\(address))")
        } else {
            parts.append(String(localized: "Never used"))
        }
        return parts.joined(separator: " · ")
    }
}
