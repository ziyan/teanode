import AuthenticationServices
import SwiftUI
import TeaNodeKit

// The settings of Passwords: when it locks, Face ID, the passphrase and the
// recovery key, how to turn on AutoFill, and erasing it.

/// PasswordsSettingsSheet is a sheet the settings can open.
enum PasswordsSettingsSheet: String, Identifiable {
    case faceID
    case changePassphrase
    case replaceRecoveryKey

    var id: String { rawValue }
}

/// PasswordsSettingsScreen is what the person decides about Passwords.
struct PasswordsSettingsScreen: View {
    @Bindable var model: PasswordsModel
    @State private var sheet: PasswordsSettingsSheet?
    @State private var isConfirmingErase = false
    @State private var isErasing = false

    private var isOpen: Bool { model.phase == .unlocked }

    var body: some View {
        Form {
            if isOpen {
                lockSection
                faceIDSection
                keysSection
            } else {
                Section {
                    Text("Open Passwords to change its lock time, passphrase or recovery key.")
                        .foregroundStyle(.secondary)
                }
            }
            autoFillSection
            eraseSection
        }
        .navigationTitle("Settings")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $sheet) { shown in
            switch shown {
            case .faceID: faceIDSheet
            case .changePassphrase: PasswordsChangePassphraseSheet(model: model)
            case .replaceRecoveryKey: PasswordsReplaceRecoveryKeySheet(model: model)
            }
        }
    }

    // MARK: Locking

    private var lockChoices: [Int] {
        Set(PasswordsWords.lockAfterChoices + [model.lockAfterMinutes]).sorted()
    }

    private var lockSection: some View {
        Section {
            Picker(
                "Lock after",
                selection: Binding(
                    get: { model.lockAfterMinutes },
                    set: { minutes in Task { await setLockAfter(minutes) } })
            ) {
                ForEach(lockChoices, id: \.self) { minutes in
                    Text(verbatim: PasswordsWords.lockAfterText(minutes: minutes)).tag(minutes)
                }
            }
        } header: {
            Text("Lock")
        } footer: {
            Text("Passwords locks itself after this long without use. It asks for your passphrase or Face ID again.")
        }
    }

    private func setLockAfter(_ minutes: Int) async {
        do {
            try await model.updateLockAfterMinutes(minutes)
            model.doneText = String(localized: "Lock time saved.")
        } catch {
            model.report(error, orSay: String(localized: "The setting could not be saved."))
        }
    }

    // MARK: Face ID

    private var faceIDSection: some View {
        Section {
            Toggle(
                "Open with Face ID",
                isOn: Binding(
                    get: { model.hasFaceIDKey },
                    set: { isOn in
                        if isOn {
                            sheet = .faceID
                        } else {
                            Task { await model.disableFaceID() }
                        }
                    })
            )
            .disabled(!model.canUseFaceID)
        } footer: {
            if model.canUseFaceID {
                Text("Face ID opens Passwords on this iPhone without the passphrase. Only this iPhone keeps the key, and it is removed if you change the faces or fingers Face ID knows.")
            } else {
                Text("Face ID is not set up on this iPhone.")
            }
        }
    }

    private var faceIDSheet: some View {
        PasswordsConfirmPassphraseSheet(
            title: String(localized: "Open with Face ID"),
            explanation: String(localized: "Type your passphrase once so this iPhone can keep the key behind Face ID."),
            actionTitle: String(localized: "Turn on")
        ) { passphrase in
            try await model.enableFaceID(passphrase: passphrase)
        }
    }

    // MARK: Passphrase and recovery key

    private var keysSection: some View {
        Section {
            Button("Change passphrase", systemImage: "lock.rotation") { sheet = .changePassphrase }
                .actionColor()
            Button("Replace recovery key", systemImage: "arrow.triangle.2.circlepath") { sheet = .replaceRecoveryKey }
                .actionColor()
        } header: {
            Text("Passphrase and recovery key")
        } footer: {
            Text("Change the passphrase, or make a new recovery key. Your items stay as they are.")
        }
    }

    // MARK: AutoFill

    private var autoFillSection: some View {
        Section {
            Text("To fill logins, passkeys and one-time codes in Safari and other apps, open Settings, General, AutoFill & Passwords, then turn on TeaNode.")
            Button("Open AutoFill settings", systemImage: "gearshape") {
                ASSettingsHelper.openCredentialProviderAppSettings(completionHandler: nil)
            }
            .actionColor()
        } header: {
            Text("AutoFill")
        }
    }

    // MARK: Erasing

    private var eraseSection: some View {
        Section {
            Button(role: .destructive) {
                isConfirmingErase = true
            } label: {
                Label("Erase Passwords", systemImage: "trash").foregroundStyle(.red)
            }
            .tint(.red)
            .actionColor(isDestructive: true)
            .disabled(isErasing)
            .confirmationDialog("Erase all of Passwords?", isPresented: $isConfirmingErase, titleVisibility: .visible) {
                Button("Erase everything", role: .destructive) { Task { await erase() } }
            } message: {
                Text(
                    "Every login, card, note and passkey, every share, and your passphrase and recovery key are deleted from this server. This cannot be undone. If you lost both your passphrase and your recovery key, this is the only way to start again."
                )
            }
        } footer: {
            Text("Deletes every item, share and key from this server.")
        }
    }

    private func erase() async {
        isErasing = true
        defer { isErasing = false }
        do {
            try await model.erase()
        } catch {
            model.report(error, orSay: String(localized: "Passwords could not be erased."))
        }
    }
}

// MARK: Sheets

/// PasswordsConfirmPassphraseSheet asks for the passphrase once, to do one
/// thing with it.
struct PasswordsConfirmPassphraseSheet: View {
    let title: String
    let explanation: String
    let actionTitle: String
    let perform: (String) async throws -> Void
    @State private var passphrase = ""
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField("Passphrase", text: $passphrase)
                } footer: {
                    Text(verbatim: explanation)
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }.disabled(isWorking)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isWorking {
                        ProgressView()
                    } else {
                        Button(actionTitle) { Task { await run() } }
                            .disabled(passphrase.isEmpty)
                    }
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
        .presentationDetents([.medium])
        .interactiveDismissDisabled(isWorking)
    }

    private func run() async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await perform(passphrase)
            dismiss()
        } catch {
            problemText = PasswordsWords.problemText(error, orSay: String(localized: "The setting could not be saved."))
        }
    }
}

/// PasswordsChangePassphraseSheet changes the passphrase. The items are not
/// sealed again: the passphrase only seals the account key.
struct PasswordsChangePassphraseSheet: View {
    let model: PasswordsModel
    @State private var currentPassphrase = ""
    @State private var newPassphrase = ""
    @State private var repeated = ""
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?
    @Environment(\.dismiss) private var dismiss

    private var isLongEnough: Bool { newPassphrase.count >= PasswordsPassphraseStrength.minimumLength }
    private var canChange: Bool { !currentPassphrase.isEmpty && isLongEnough && newPassphrase == repeated }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField("Current passphrase", text: $currentPassphrase)
                }
                Section {
                    SecureField("New passphrase", text: $newPassphrase)
                    SecureField("Type it again", text: $repeated)
                    if !repeated.isEmpty, newPassphrase != repeated {
                        Text("The two passphrases are not the same.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                } footer: {
                    Text("Use at least 8 characters. Longer is better.")
                }
            }
            .navigationTitle("Change passphrase")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }.disabled(isWorking)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isWorking {
                        ProgressView()
                    } else {
                        Button("Change") { Task { await change() } }
                            .disabled(!canChange)
                    }
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
        .interactiveDismissDisabled(isWorking)
    }

    private func change() async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await model.changePassphrase(currentPassphrase: currentPassphrase, newPassphrase: newPassphrase)
            model.doneText = String(localized: "Your passphrase was changed.")
            dismiss()
        } catch {
            problemText = PasswordsWords.problemText(error, orSay: String(localized: "The passphrase could not be changed."))
        }
    }
}

/// PasswordsReplaceRecoveryKeySheet makes a new recovery key after the
/// passphrase is typed, and shows it once. The old one stops working.
struct PasswordsReplaceRecoveryKeySheet: View {
    let model: PasswordsModel
    @State private var passphrase = ""
    @State private var newRecoveryKeyText: String?
    @State private var isSaved = false
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                if let newRecoveryKeyText {
                    PasswordsRecoveryKeySections(model: model, recoveryKeyText: newRecoveryKeyText, isSaved: $isSaved)
                } else {
                    Section {
                        SecureField("Passphrase", text: $passphrase)
                    } footer: {
                        Text("Type your passphrase to make a new recovery key. The old one stops working once you finish.")
                    }
                }
            }
            .navigationTitle("Replace recovery key")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if newRecoveryKeyText == nil {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }.disabled(isWorking)
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isWorking {
                        ProgressView()
                    } else if newRecoveryKeyText == nil {
                        Button("Continue") { Task { await makeKey() } }
                            .disabled(passphrase.isEmpty)
                    } else {
                        Button("Done") {
                            model.doneText = String(localized: "Your recovery key was replaced.")
                            dismiss()
                        }
                        .disabled(!isSaved)
                    }
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
        // The new key is shown once: it cannot be swiped away before it is kept.
        .interactiveDismissDisabled(isWorking || newRecoveryKeyText != nil)
    }

    private func makeKey() async {
        isWorking = true
        defer { isWorking = false }
        do {
            newRecoveryKeyText = try await model.replaceRecoveryKey(passphrase: passphrase)
            passphrase = ""
        } catch {
            problemText = PasswordsWords.problemText(error, orSay: String(localized: "The recovery key could not be replaced."))
        }
    }
}
