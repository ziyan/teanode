import SwiftUI
import TeaNodeKit

// Setting Passwords up (a passphrase, then the recovery key, shown once) and
// opening it again (the passphrase, the recovery key, or Face ID).

// MARK: Setting up

/// PasswordsSetUpScreen asks for the passphrase twice. Nothing about the
/// passphrase is sent to the server: the key made from it stays here, and the
/// server only keeps what that key sealed.
struct PasswordsSetUpScreen: View {
    let model: PasswordsModel
    @State private var passphrase = ""
    @State private var repeated = ""
    @State private var isWorking = false

    private var strength: PasswordsPassphraseStrength { PasswordsPassphraseStrength.of(passphrase) }
    private var isLongEnough: Bool { passphrase.count >= PasswordsPassphraseStrength.minimumLength }
    private var isMatching: Bool { passphrase == repeated }
    private var canContinue: Bool { isLongEnough && isMatching && !isWorking }

    var body: some View {
        Form {
            Section {
                SecureField("Passphrase", text: $passphrase)
                Text(verbatim: strengthText)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } header: {
                Text("Choose a passphrase")
            } footer: {
                Text(
                    "The passphrase opens Passwords on each device. It is separate from your TeaNode sign-in, and the server never sees it. A long phrase is better than a short, odd one."
                )
            }
            Section {
                SecureField("Type it again", text: $repeated)
                if !repeated.isEmpty, !isMatching {
                    Text("The two passphrases are not the same.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            } footer: {
                Text(PasswordsSetUpScreen.lossSentence)
            }
        }
        .navigationTitle("Set up Passwords")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                if isWorking {
                    ProgressView()
                } else {
                    Button("Continue") { Task { await makeKeys() } }
                        .disabled(!canContinue)
                }
            }
        }
        #if DEBUG
            .onAppear {
                // `-TeaNodeScreen passwords-setup`: the passphrase typed twice.
                if DemoServer.isRequested, DemoServer.screen == "passwords-setup", passphrase.isEmpty {
                    passphrase = PasswordsDemoStore.passphrase
                    repeated = PasswordsDemoStore.passphrase
                }
            }
        #endif
    }

    /// What losing the passphrase and the recovery key means, said wherever a
    /// person is asked to keep them.
    static var lossSentence: String {
        String(
            localized:
                "If you lose both your passphrase and your recovery key, your Passwords cannot be recovered. The only way out is to erase them and start again."
        )
    }

    private var strengthText: String {
        switch strength {
        case .empty: String(localized: "Use at least 8 characters. Longer is better.")
        case .short: String(localized: "Too short.")
        case .weak: String(localized: "Weak. Add more words or characters.")
        case .fair: String(localized: "Fair. A few more characters would make it strong.")
        case .strong: String(localized: "Strong.")
        }
    }

    private func makeKeys() async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await model.setUp(passphrase: passphrase)
        } catch {
            model.report(error, orSay: String(localized: "Passwords could not be set up."))
        }
    }
}

// MARK: The recovery key

/// PasswordsRecoveryKeySections shows a recovery key, with the ways to keep it
/// (copy, share) and the switch that says it was kept. Shown at setup and when
/// the person makes a new one.
struct PasswordsRecoveryKeySections: View {
    let model: PasswordsModel
    let recoveryKeyText: String
    @Binding var isSaved: Bool

    var body: some View {
        Section {
            PasswordsValueText(PasswordsFormat.recoveryKeyLines(recoveryKeyText).joined(separator: "\n"))
                .font(.body.monospaced())
                .textSelection(.enabled)
                .accessibilityLabel(Text("Recovery key"))
                .accessibilityValue(Text(verbatim: recoveryKeyText))
            Button("Copy recovery key", systemImage: "doc.on.doc") {
                model.copy(recoveryKeyText, fieldName: String(localized: "Recovery key"))
            }
            .actionColor()
            ShareLink(item: recoveryKeyText, subject: Text("TeaNode Passwords recovery key")) {
                Label("Share recovery key", systemImage: "square.and.arrow.up")
                    .actionColor()
            }
        } header: {
            Text("Your recovery key")
        } footer: {
            Text(
                "If you forget your passphrase, this key opens Passwords instead. It is shown only now. Copy it or share it, and keep it somewhere safe that is not this iPhone."
            )
        }
        Section {
            Toggle("I saved my recovery key", isOn: $isSaved)
        } footer: {
            Text(PasswordsSetUpScreen.lossSentence)
        }
    }
}

/// PasswordsRecoveryKeyScreen is the end of the setup: the recovery key, shown
/// once, and the way into Passwords once the person says they saved it.
struct PasswordsRecoveryKeyScreen: View {
    let model: PasswordsModel
    @State private var isSaved = false
    @State private var shouldRemember = true
    @State private var isFinishing = false

    var body: some View {
        Form {
            PasswordsRecoveryKeySections(model: model, recoveryKeyText: model.recoveryKeyText ?? "", isSaved: $isSaved)
            if model.canUseFaceID {
                Section {
                    Toggle("Use Face ID next time", isOn: $shouldRemember)
                } footer: {
                    Text("Face ID opens Passwords on this iPhone, so you type the passphrase only on other devices.")
                }
            }
        }
        .navigationTitle("Recovery key")
        .navigationBarTitleDisplayMode(.inline)
        .safeAreaInset(edge: .bottom) {
            PasswordsPrimaryButton(title: String(localized: "Done"), isWorking: isFinishing, isEnabled: isSaved) {
                Task { await finish() }
            }
        }
    }

    private func finish() async {
        isFinishing = true
        defer { isFinishing = false }
        await model.finishSetUp(shouldRemember: shouldRemember && model.canUseFaceID)
    }
}

/// PasswordsPrimaryButton is a screen's one prominent action, full width at
/// the bottom, outside the list.
struct PasswordsPrimaryButton: View {
    let title: String
    var isWorking = false
    var isEnabled = true
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            if isWorking {
                ProgressView().frame(maxWidth: .infinity)
            } else {
                Text(verbatim: title).frame(maxWidth: .infinity)
            }
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
        .disabled(!isEnabled || isWorking)
        .padding()
        .background(Color(.systemGroupedBackground))
    }
}

// MARK: Unlocking

/// PasswordsUnlockScreen is closed Passwords: the passphrase (or the recovery
/// key) opens it, and so does Face ID when this iPhone keeps the key.
struct PasswordsUnlockScreen: View {
    let model: PasswordsModel
    @State private var typedText = ""
    @State private var isUsingRecoveryKey = false
    @State private var shouldRemember = true
    @State private var isWorking = false

    var body: some View {
        Form {
            Section {
                VStack(spacing: 8) {
                    Image(systemName: "lock")
                        .font(.largeTitle)
                        .foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                    Text("Passwords is locked")
                        .font(.headline)
                }
                .frame(maxWidth: .infinity)
                .listRowBackground(Color.clear)
            }
            Section {
                if isUsingRecoveryKey {
                    TextField("Recovery key", text: $typedText)
                        .font(.body.monospaced())
                        .textInputAutocapitalization(.characters)
                        .autocorrectionDisabled()
                } else {
                    SecureField("Passphrase", text: $typedText)
                }
            } footer: {
                if isUsingRecoveryKey {
                    Text("Type your recovery key to open it.")
                } else {
                    Text("Type your passphrase to open it.")
                }
            }
            if !isUsingRecoveryKey, model.canUseFaceID, !model.hasFaceIDKey {
                Section {
                    Toggle("Use Face ID next time", isOn: $shouldRemember)
                }
            }
            Section {
                if model.hasFaceIDKey {
                    Button("Use Face ID", systemImage: "faceid") {
                        Task { await model.unlockWithFaceID() }
                    }
                    .actionColor()
                }
                Button(
                    isUsingRecoveryKey ? String(localized: "Use the passphrase") : String(localized: "Use the recovery key"),
                    systemImage: "key"
                ) {
                    typedText = ""
                    isUsingRecoveryKey.toggle()
                }
                .actionColor()
            } footer: {
                Text("If you lost both your passphrase and your recovery key, you can erase Passwords in its settings and start again.")
            }
        }
        .navigationTitle(PasswordsWords.title)
        .navigationBarTitleDisplayMode(.inline)
        .onSubmit { Task { await unlock() } }
        .safeAreaInset(edge: .bottom) {
            PasswordsPrimaryButton(title: String(localized: "Unlock"), isWorking: isWorking, isEnabled: !typedText.isEmpty) {
                Task { await unlock() }
            }
        }
    }

    private func unlock() async {
        guard !typedText.isEmpty, !isWorking else { return }
        isWorking = true
        defer { isWorking = false }
        do {
            if isUsingRecoveryKey {
                try await model.unlock(recoveryKey: typedText)
            } else {
                try await model.unlock(passphrase: typedText, shouldRemember: shouldRemember && model.canUseFaceID && !model.hasFaceIDKey)
            }
        } catch {
            let fallback =
                isUsingRecoveryKey
                ? String(localized: "Passwords could not be opened with that recovery key.")
                : String(localized: "Passwords could not be opened with that passphrase.")
            model.report(error, orSay: fallback)
        }
    }
}
