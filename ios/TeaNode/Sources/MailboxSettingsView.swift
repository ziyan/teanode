import SwiftUI
import TeaNodeKit

/// MailboxSettingsView is the dashboard's mailbox settings: the name and
/// signature, the auto-reply, the folders, and the passwords mail programs
/// use.
struct MailboxSettingsView: View {
    let mail: MailModel

    var body: some View {
        List {
            if let mailbox = mail.mailbox {
                Section {
                    NavigationLink {
                        MailboxGeneralView(mail: mail, mailbox: mailbox)
                    } label: {
                        Label("Name and signature", systemImage: "signature")
                    }
                    NavigationLink {
                        AutoReplyView(mail: mail, mailbox: mailbox)
                    } label: {
                        LabeledContent {
                            Text(mailbox.autoReply?.enabled == true ? String(localized: "On") : String(localized: "Off"))
                        } label: {
                            Label("Auto-reply", systemImage: "arrowshape.turn.up.left.2")
                        }
                    }
                    NavigationLink {
                        MailFoldersView(mail: mail)
                    } label: {
                        Label("Folders", systemImage: "folder")
                    }
                    NavigationLink {
                        MailAppPasswordsView(mail: mail, mailboxId: mailbox.id)
                    } label: {
                        Label("Mail programs", systemImage: "laptopcomputer.and.iphone")
                    }
                } footer: {
                    Text("Addresses: \(mailbox.sendingAddresses.joined(separator: ", "))")
                }
            }
        }
        .navigationTitle(mail.mailbox?.name ?? String(localized: "Mailbox"))
        .toast(done: Bindable(mail).doneText, problem: Bindable(mail).problemText)
    }
}

/// MailboxGeneralView changes the mailbox's name and signature.
struct MailboxGeneralView: View {
    let mail: MailModel
    let mailbox: Mailbox
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var signature = ""

    var body: some View {
        Form {
            TextField("Name", text: $name)
            Section {
                TextField("Signature", text: $signature, axis: .vertical).lineLimit(3...)
            } footer: {
                Text("Added to new messages, above what they quote.")
            }
        }
        .navigationTitle("Name and signature")
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Save") {
                    let newName = name.trimmed
                    let newSignature = signature
                    Task {
                        let isSaved = await mail.act(saying: String(localized: "Saved")) {
                            try await mail.connection.updateMailbox(mailbox.id, name: newName, signature: newSignature)
                        }
                        if isSaved { dismiss() }
                    }
                }
                .disabled(name.trimmed.isEmpty)
            }
        }
        .onAppear {
            name = mailbox.name
            signature = mailbox.signatureText
        }
    }
}

/// AutoReplyView is the mailbox's auto-reply: on or off, between two
/// times, to everyone or only the person's own domains, and what it says.
struct AutoReplyView: View {
    let mail: MailModel
    let mailbox: Mailbox
    @Environment(\.dismiss) private var dismiss
    @State private var draft = MailAutoReplyDraft()

    var body: some View {
        Form {
            Toggle("Reply automatically", isOn: $draft.isEnabled)
            Section {
                OptionalDatePicker(title: String(localized: "From"), date: $draft.from)
                OptionalDatePicker(title: String(localized: "Until"), date: $draft.until)
                Toggle("Only to my own domains", isOn: $draft.isSameDomainOnly)
            }
            Section {
                TextField("Subject (empty is “Auto: ” and theirs)", text: $draft.subject)
                TextField("Message", text: $draft.text, axis: .vertical).lineLimit(5...)
            }
        }
        .navigationTitle("Auto-reply")
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Save") {
                    let reply = draft
                    Task {
                        let isSaved = await mail.act(saying: String(localized: "Saved")) {
                            try await mail.connection.updateMailbox(mailbox.id, autoReply: reply)
                        }
                        if isSaved { dismiss() }
                    }
                }
                .disabled(draft.isEnabled && draft.text.trimmed.isEmpty)
            }
        }
        .onAppear { draft = MailAutoReplyDraft(mailbox.autoReply) }
    }
}

/// MailFoldersView is the folders: new, renamed, pinned, deleted.
struct MailFoldersView: View {
    let mail: MailModel
    @State private var isAdding = false
    @State private var newName = ""
    @State private var renaming: MailFolder?
    @State private var renamed = ""
    @State private var deleting: MailFolder?

    var body: some View {
        List {
            ForEach(MailFolders.rows(mail.folders), id: \.folder.id) { row in
                let folder = row.folder
                HStack {
                    Label(MailNames.folder(folder), systemImage: MailNames.symbol(folder))
                        .padding(.leading, CGFloat(min(row.depth, 3)) * 16)
                    Spacer()
                    if folder.pinnedAt != nil {
                        Image(systemName: "pin.fill").font(.caption).foregroundStyle(.secondary)
                    }
                    Text(folder.total, format: .number).font(.footnote).foregroundStyle(.secondary)
                }
                .swipeActions {
                    if folder.kind.isEmpty {
                        Button("Delete", systemImage: "trash", role: .destructive) { deleting = folder }
                        Button("Rename", systemImage: "pencil") {
                            renamed = folder.name
                            renaming = folder
                        }
                        .tint(.indigo)
                    }
                }
                .swipeActions(edge: .leading) {
                    if folder.kind != "inbox" {
                        Button(folder.pinnedAt == nil ? LocalizedStringKey("Pin") : "Unpin", systemImage: folder.pinnedAt == nil ? "pin" : "pin.slash") {
                            Task {
                                _ = await mail.act(saying: folder.pinnedAt == nil ? String(localized: "Pinned") : String(localized: "Unpinned")) {
                                    try await mail.connection.pinMailFolder(folder.id, isPinned: folder.pinnedAt == nil)
                                }
                            }
                        }
                        .tint(.orange)
                    }
                }
            }
        }
        .navigationTitle("Folders")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    newName = ""
                    isAdding = true
                } label: {
                    Label("New folder", systemImage: "folder.badge.plus")
                }
            }
        }
        .alert("New folder", isPresented: $isAdding) {
            TextField("Name", text: $newName)
            Button("Cancel", role: .cancel) {}
            Button("Add") {
                guard let mailboxId = mail.mailbox?.id, !newName.trimmed.isEmpty else { return }
                let name = newName.trimmed
                Task {
                    _ = await mail.act(saying: String(localized: "Folder added")) {
                        try await mail.connection.createMailFolder(mailboxId: mailboxId, name: name, parentId: nil)
                    }
                }
            }
        }
        .alert("Rename", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField("Name", text: $renamed)
            Button("Cancel", role: .cancel) {}
            Button("Save") {
                guard let folder = renaming, !renamed.trimmed.isEmpty else { return }
                let name = renamed.trimmed
                Task {
                    _ = await mail.act(saying: String(localized: "Saved")) {
                        try await mail.connection.renameMailFolder(folder.id, name: name)
                    }
                }
            }
        }
        .confirmationDialog(
            "Delete \(deleting?.name ?? "")?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible
        ) {
            Button("Delete folder", role: .destructive) {
                guard let folder = deleting else { return }
                Task {
                    _ = await mail.act(saying: String(localized: "Folder deleted")) {
                        try await mail.connection.deleteMailFolder(folder.id)
                    }
                }
            }
        } message: {
            Text("Its \(deleting?.total ?? 0) messages are deleted with it.")
        }
    }
}

/// MailAppPasswordsView is the passwords mail programs use, and how to set
/// one up.
struct MailAppPasswordsView: View {
    let mail: MailModel
    let mailboxId: String
    @State private var passwords: [MailAppPassword] = []
    @State private var settings: MailProgramSettings?
    @State private var isAdding = false
    @State private var newName = ""
    @State private var created: (password: String, username: String)?
    @State private var revoking: MailAppPassword?

    var body: some View {
        List {
            Section {
                ForEach(passwords) { password in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(password.name)
                        Text(ServerTime.date(password.lastUsedAt).map { String(localized: "Last used \($0.formatted(.relative(presentation: .named)))") } ?? String(localized: "Never used"))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .swipeActions {
                        Button("Revoke", systemImage: "trash", role: .destructive) { revoking = password }
                    }
                }
            } header: {
                Text("Devices")
            } footer: {
                if passwords.isEmpty { Text("No mail program has a password yet.") }
            }
            if let settings {
                Section("Settings for a mail program") {
                    LabeledContent("Incoming (IMAP)", value: "\(settings.imapHost):\(settings.imapsPort)")
                    LabeledContent("Outgoing (SMTP)", value: "\(settings.submissionHost):\(settings.submissionPort)")
                    if !settings.davHost.isEmpty {
                        LabeledContent("Contacts and calendar", value: settings.davHost)
                    }
                    LabeledContent("User name", value: mail.mailbox?.sendingAddresses.first ?? "")
                }
                .textSelection(.enabled)
            }
        }
        .navigationTitle("Mail programs")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    newName = ""
                    isAdding = true
                } label: {
                    Label("New password", systemImage: "plus")
                }
            }
        }
        .task { await load() }
        .alert("New app password", isPresented: $isAdding) {
            TextField("Device name", text: $newName)
            Button("Cancel", role: .cancel) {}
            Button("Create") {
                let name = newName.trimmed
                guard !name.isEmpty else { return }
                Task {
                    do {
                        created = try await mail.connection.createMailAppPassword(mailboxId: mailboxId, name: name)
                        await load()
                    } catch {
                        mail.problemText = error.localizedDescription
                    }
                }
            }
        }
        .sheet(isPresented: Binding(get: { created != nil }, set: { if !$0 { created = nil } })) {
            if let created {
                NavigationStack {
                    List {
                        Section {
                            LabeledContent("User name", value: created.username)
                            LabeledContent("Password") {
                                Text(created.password).font(.body.monospaced()).textSelection(.enabled)
                            }
                            Button("Copy password", systemImage: "doc.on.doc") { UIPasteboard.general.string = created.password }
                        } footer: {
                            Text("It is shown this once. Type it into the mail program now.")
                        }
                    }
                    .navigationTitle("App password")
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("Done") { self.created = nil }
                        }
                    }
                }
                .interactiveDismissDisabled(true)
            }
        }
        .confirmationDialog(
            "Revoke \(revoking?.name ?? "")?", isPresented: Binding(get: { revoking != nil }, set: { if !$0 { revoking = nil } }), titleVisibility: .visible
        ) {
            Button("Revoke", role: .destructive) {
                guard let password = revoking else { return }
                Task {
                    _ = await mail.act(saying: String(localized: "Revoked")) { try await mail.connection.deleteMailAppPassword(password.id) }
                    await load()
                }
            }
        } message: {
            Text("The mail program using it stops reaching the mailbox.")
        }
    }

    private func load() async {
        do {
            let read = try await mail.connection.mailAppPasswords(mailboxId: mailboxId)
            passwords = read.passwords
            settings = read.settings
        } catch {
            mail.problemText = error.localizedDescription
        }
    }
}
