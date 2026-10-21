import SwiftUI
import TeaNodeKit

// The extension's screens. The system presents them as a sheet over the
// browser or app that asked, so every screen has its own way out (Cancel, or
// Done on the configuration screen), and only the screens that work with a
// list of logins have a navigation bar. Strings are English keys of the app's
// String Catalog, like the rest of the app.

/// AutoFillRootView picks the screen for what the model is doing and tells the
/// person about a problem in an alert.
struct AutoFillRootView: View {
    @ObservedObject var model: AutoFillModel

    var body: some View {
        NavigationStack {
            screenContent
        }
        .alert(
            Text(model.problemMessage?.title ?? ""), isPresented: isShowingProblem,
            actions: { Button("OK", role: .cancel) {} },
            message: { Text(model.problemMessage?.message ?? "") })
    }

    private var isShowingProblem: Binding<Bool> {
        Binding(
            get: { model.problemMessage != nil },
            set: { isShowing in
                if !isShowing { model.problemMessage = nil }
            })
    }

    @ViewBuilder private var screenContent: some View {
        switch model.autoFillScreen {
        case .starting:
            AutoFillStartingView(model: model)
        case .locked:
            AutoFillLockedView(model: model)
        case .needsApp:
            AutoFillNeedsAppView(model: model)
        case .list:
            AutoFillListView(model: model)
        case .registration(let registration):
            AutoFillRegistrationView(model: model, registration: registration)
        case .configuration:
            AutoFillConfigurationView(model: model)
        }
    }
}

extension View {
    /// The way out of a screen that is not the registration or configuration one.
    fileprivate func autoFillCancelButton(model: AutoFillModel) -> some View {
        navigationTitle("Passwords")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { model.cancel() }
                }
            }
    }
}

// MARK: Waiting, locked, not ready

struct AutoFillStartingView: View {
    @ObservedObject var model: AutoFillModel

    var body: some View {
        ProgressView()
            .controlSize(.large)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .autoFillCancelButton(model: model)
    }
}

/// AutoFillMessageScreen is a message with one full-width button under it,
/// which scrolls so that it still fits at the largest text size.
struct AutoFillMessageScreen<Content: View>: View {
    let buttonTitle: LocalizedStringKey
    let buttonAction: () -> Void
    let content: Content

    init(buttonTitle: LocalizedStringKey, buttonAction: @escaping () -> Void, @ViewBuilder content: () -> Content) {
        self.buttonTitle = buttonTitle
        self.buttonAction = buttonAction
        self.content = content()
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                content
            }
            .frame(maxWidth: .infinity)
            .padding(.horizontal)
            .padding(.top, 48)
        }
        .safeAreaInset(edge: .bottom) {
            Button(action: buttonAction) {
                Text(buttonTitle)
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .padding()
        }
    }
}

/// AutoFillLockedView asks for Face ID. It also stands in when the person
/// cancelled the prompt that came up by itself.
struct AutoFillLockedView: View {
    @ObservedObject var model: AutoFillModel

    var body: some View {
        AutoFillMessageScreen(buttonTitle: "Unlock with Face ID", buttonAction: { model.unlockTapped() }) {
            Image(systemName: "faceid")
                .font(.largeTitle)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            Text("Passwords is locked")
                .font(.headline)
            Text("Unlock Passwords with Face ID to fill your logins, passkeys and one-time codes.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            problemLine
        }
        .autoFillCancelButton(model: model)
    }

    @ViewBuilder private var problemLine: some View {
        if let problemText = model.lockedProblemText {
            Text(problemText)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
    }
}

/// AutoFillNeedsAppView is for a phone where the app has not unlocked
/// Passwords yet: the extension cannot ask for the passphrase.
struct AutoFillNeedsAppView: View {
    @ObservedObject var model: AutoFillModel

    var body: some View {
        ContentUnavailableView {
            Label("Passwords is not ready", systemImage: "lock")
        } description: {
            Text("Open TeaNode and unlock Passwords once, then try again.")
        }
        .autoFillCancelButton(model: model)
    }
}

// MARK: The list

struct AutoFillListView: View {
    @ObservedObject var model: AutoFillModel

    var body: some View {
        let sections = model.sections
        List {
            ForEach(sections) { section in
                AutoFillSectionView(section: section, model: model)
            }
        }
        .overlay {
            if sections.isEmpty {
                emptyView
            }
        }
        .searchable(text: $model.searchText, prompt: Text("Search logins"))
        .disabled(model.isWorking)
        .autoFillCancelButton(model: model)
    }

    @ViewBuilder private var emptyView: some View {
        if model.searchText.isEmpty {
            ContentUnavailableView {
                Label(model.emptyListTitle, systemImage: "key")
            } description: {
                Text(model.emptyListDescription)
            }
        } else {
            ContentUnavailableView.search(text: model.searchText)
        }
    }
}

struct AutoFillSectionView: View {
    let section: AutoFillSection
    @ObservedObject var model: AutoFillModel

    var body: some View {
        Section {
            ForEach(section.rows) { row in
                Button {
                    model.select(row)
                } label: {
                    AutoFillRowView(row: row)
                }
            }
        } header: {
            if let title = section.title {
                Text(title)
            }
        }
    }
}

/// AutoFillValueText shows a value the person stored (a login's name, a user
/// name, a site) so that a line never ends in a hyphen SwiftUI made up: it may
/// end only where `PasswordsDisplayText` allows. It reads aloud as the value.
struct AutoFillValueText: View {
    let storedValue: String
    var isProse = false

    init(_ storedValue: String, isProse: Bool = false) {
        self.storedValue = storedValue
        self.isProse = isProse
    }

    var body: some View {
        Text(verbatim: PasswordsDisplayText.breakable(storedValue, isProse: isProse))
            .accessibilityLabel(storedValue)
    }
}

/// AutoFillRowView is a login: its name, and the user name under it. A
/// passkey has a key beside it.
struct AutoFillRowView: View {
    let row: AutoFillRow

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                AutoFillValueText(row.rowTitle, isProse: true)
                    .font(.body)
                    .foregroundStyle(.primary)
                if !row.rowSubtitle.isEmpty {
                    AutoFillValueText(row.rowSubtitle)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            passkeyMark
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var passkeyMark: some View {
        if row.isPasskey {
            Image(systemName: "person.badge.key")
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .accessibilityLabel(Text("Passkey"))
        }
    }
}

// MARK: Saving a passkey

/// AutoFillRegistrationView asks whether to save the passkey a website wants
/// to make. Nothing is saved before the person taps Save passkey.
struct AutoFillRegistrationView: View {
    @ObservedObject var model: AutoFillModel
    let registration: AutoFillPasskeyRegistration

    var body: some View {
        Form {
            Section {
                LabeledContent("Site") { AutoFillValueText(registration.relyingPartyId) }
                LabeledContent("User name") { AutoFillValueText(registration.userName) }
            } footer: {
                Text("The passkey is saved with your logins in TeaNode Passwords.")
            }
        }
        .safeAreaInset(edge: .bottom) {
            buttons
        }
        .navigationTitle("Save a passkey")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var buttons: some View {
        VStack(spacing: 12) {
            Button {
                model.savePasskey()
            } label: {
                saveLabel
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            Button {
                model.cancel()
            } label: {
                Text("Cancel")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)
            .controlSize(.large)
        }
        .disabled(model.isWorking)
        .padding()
    }

    @ViewBuilder private var saveLabel: some View {
        if model.isWorking {
            ProgressView()
        } else {
            Text("Save passkey")
        }
    }
}

// MARK: Turning TeaNode on

/// AutoFillConfigurationView is what the person sees after turning TeaNode on
/// in Settings, General, AutoFill & Passwords.
struct AutoFillConfigurationView: View {
    @ObservedObject var model: AutoFillModel

    var body: some View {
        AutoFillMessageScreen(buttonTitle: "Done", buttonAction: { model.finishConfiguration() }) {
            Image(systemName: "checkmark.circle")
                .font(.largeTitle)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            Text("TeaNode is on")
                .font(.title)
            Text("Safari, Chrome and other apps can now offer your TeaNode logins, passkeys and one-time codes.")
                .font(.body)
                .multilineTextAlignment(.center)
            setUpLine
        }
    }

    @ViewBuilder private var setUpLine: some View {
        if !model.hasSetUpPasswords {
            Text("Open TeaNode and set up Passwords to see your logins here.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
    }
}
