import SwiftUI
import TeaNodeKit
import UIKit

/// PhotoBackupView is the screen Settings and the Photos tab open to turn the
/// backup of this iPhone's photos on and off, and to see how it is going.
struct PhotoBackupView: View {
    let connection: Connection
    @Environment(\.scenePhase) private var scenePhase
    @Environment(AppModel.self) private var app
    @State private var isConfirmingTurnOff = false
    /// The library's size and limits, read when the screen opens.
    @State private var library: PhotoLibrary?
    @State private var libraryFailureText: String?
    private let engine = PhotoBackupEngine.shared

    /// Backup is on for another account than the one signed in now.
    private var isOnForOtherAccount: Bool {
        engine.isEnabled && engine.enabledAccountKey != connection.accountKey
    }

    var body: some View {
        PhotoBackupContent(
            status: engine.status, credentialName: engine.credentialName, accessProblem: engine.accessProblem, isWorking: engine.isWorking,
            isPermitted: app.isPhotosOffered, enabledAccountName: engine.enabledAccountName, isOnForOtherAccount: isOnForOtherAccount,
            library: library, libraryFailureText: libraryFailureText, readLibrary: { Task { await loadLibrary() } },
            isOn: Binding(
                get: { engine.isEnabled && !isOnForOtherAccount },
                set: { isOn in
                    if isOn {
                        Task { await engine.turnOn(connection: connection) }
                    } else {
                        isConfirmingTurnOff = true
                    }
                }),
            usesMobileData: Binding(get: { engine.usesMobileData }, set: { engine.usesMobileData = $0 }),
            includesHidden: Binding(get: { engine.includesHidden }, set: { engine.includesHidden = $0 }),
            isConfirmingTurnOff: $isConfirmingTurnOff,
            backUpNow: { Task { await engine.backUpNow() } },
            signInAgain: { Task { await engine.signInAgain(connection: connection) } },
            turnOff: { Task { await engine.turnOff(connection: connection) } }
        )
        .toast(
            done: Binding(get: { engine.doneText }, set: { engine.doneText = $0 }),
            problem: Binding(get: { engine.problemText }, set: { engine.problemText = $0 })
        )
        .sensoryFeedback(.success, trigger: engine.doneText) { _, text in text != nil }
        .sensoryFeedback(.warning, trigger: engine.problemText) { _, text in text != nil }
        .task { await engine.refresh() }
        .task { await loadLibrary() }
        .onChange(of: scenePhase) { _, phase in
            // Back from the system's Settings, where the person may have given
            // access to the whole library.
            if phase == .active { Task { await engine.refresh() } }
        }
    }
}

extension PhotoBackupView {
    /// Reads the size of the library and its limits; a failure is shown with a
    /// way to try again, and what was read before stays.
    fileprivate func loadLibrary() async {
        do {
            library = try await connection.photoLibrary()
            libraryFailureText = nil
        } catch {
            guard !Task.isCancelled else { return }
            libraryFailureText = app.describe(error)
        }
    }
}

/// PhotoBackupContent is the Photo backup screen for a status, with every
/// choice and button handed in, so that each state can be previewed.
struct PhotoBackupContent: View {
    let status: PhotoBackupStatus
    let credentialName: String?
    let accessProblem: PhotoBackupAccess?
    let isWorking: Bool
    /// Whether the person may use Photos on this server (`photos:use`); the
    /// switch is off limits to someone who may not, but can still turn it off.
    let isPermitted: Bool
    /// The account backup is on for, such as "robin on photos.example.com";
    /// nil when it is off.
    let enabledAccountName: String?
    /// Backup is on, for an account other than the one signed in now: its
    /// numbers are not shown here, and it is not turned off or on from here.
    let isOnForOtherAccount: Bool
    let library: PhotoLibrary?
    let libraryFailureText: String?
    let readLibrary: () -> Void
    @Binding var isOn: Bool
    @Binding var usesMobileData: Bool
    @Binding var includesHidden: Bool
    @Binding var isConfirmingTurnOff: Bool
    let backUpNow: () -> Void
    let signInAgain: () -> Void
    let turnOff: () -> Void

    var body: some View {
        Form {
            Section {
                Toggle("Back up this iPhone", isOn: $isOn)
                    .disabled(isWorking || isOnForOtherAccount || (!isPermitted && !isOn))
                    .confirmationDialog("Turn off photo backup?", isPresented: $isConfirmingTurnOff, titleVisibility: .visible) {
                        Button("Turn off", role: .destructive, action: turnOff)
                    } message: {
                        Text("This iPhone stops backing up and its backup password is revoked. Photos already on TeaNode stay there.")
                    }
            } footer: {
                if isOnForOtherAccount {
                    Text("This iPhone's photos are backed up to a different account. Sign in as that account to turn it off.")
                } else if isPermitted || isOn {
                    Text("Photos and videos on this iPhone are copied to your TeaNode server. Deleting a photo on this iPhone does not delete it from TeaNode.")
                } else {
                    Text("Your account may not keep photos on this server. Ask whoever runs it to allow it.")
                }
            }

            if let accessProblem {
                accessSection(accessProblem)
            }

            if isOnForOtherAccount, let enabledAccountName {
                Section {
                    Label("Photo backup is on for \(enabledAccountName).", systemImage: "person.crop.circle")
                } footer: {
                    Text("Its progress shows when you are signed in as that account.")
                }
            } else if status.isEnabled {
                statusSection
                if status.phase == .needsSignIn {
                    signInSection
                }
                Section {
                    Toggle("Use mobile data", isOn: $usesMobileData)
                    Toggle("Include the Hidden album", isOn: $includesHidden)
                } footer: {
                    Text("Photos and videos go up over Wi-Fi unless you allow mobile data. The Hidden album is left out unless you include it.")
                }
                Section {
                    Button("Back up now", systemImage: "icloud.and.arrow.up", action: backUpNow)
                        .disabled(isWorking || status.phase == .needsSignIn)
                }
                if let credentialName {
                    Section {
                        LabeledContent("Password name", value: credentialName)
                    } header: {
                        Text("Device password")
                    } footer: {
                        Text("This password can only add photos to your library. Turning photo backup off revokes it.")
                    }
                }
            }

            if library != nil || libraryFailureText != nil {
                librarySection
            }
        }
        .navigationTitle("Photo backup")
        .navigationBarTitleDisplayMode(.inline)
    }

    // MARK: Sections

    /// How much of the server's room the library takes, and the largest file
    /// it takes, as the server says.
    private var librarySection: some View {
        Section {
            if let library {
                LabeledContent("Space used", value: spaceUsedText(of: library))
                LabeledContent("Largest file", value: library.maximumFileBytes > 0 ? library.maximumFileBytes.formatted(.byteCount(style: .file)) : String(localized: "No limit"))
            } else if let libraryFailureText {
                Label("The size of your library could not be read.", systemImage: "exclamationmark.triangle")
                Text(libraryFailureText)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Button("Try again", systemImage: "arrow.clockwise", action: readLibrary)
            }
        } header: {
            Text("Your library")
        } footer: {
            if library != nil {
                Text("A file over the largest size is not backed up, and nothing more is taken once the space is used.")
            }
        }
    }

    private func spaceUsedText(of library: PhotoLibrary) -> String {
        let used = library.usedBytes.formatted(.byteCount(style: .file))
        guard library.maximumAccountBytes > 0 else { return used }
        return String(localized: "\(used) of \(library.maximumAccountBytes.formatted(.byteCount(style: .file)))")
    }

    private func accessSection(_ access: PhotoBackupAccess) -> some View {
        Section {
            Label(accessText(access), systemImage: "exclamationmark.triangle")
            Button("Open Settings", systemImage: "gear") {
                if let address = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(address) }
            }
        } footer: {
            Text("Photo backup reads your whole library, so TeaNode needs Full Access. Open Settings, choose Photos, then Full Access.")
        }
    }

    private func accessText(_ access: PhotoBackupAccess) -> String {
        switch access {
        case .limited: String(localized: "TeaNode can only see some of your photos.")
        case .denied: String(localized: "TeaNode cannot see your photos.")
        case .notDetermined, .full: String(localized: "TeaNode needs access to your photos.")
        }
    }

    private var statusSection: some View {
        Section("Status") {
            if let enabledAccountName {
                LabeledContent("Account", value: enabledAccountName)
            }
            VStack(alignment: .leading, spacing: 8) {
                Text("\(status.backedUpItemCount) of \(status.libraryItemCount) backed up")
                    .contentTransition(.numericText())
                if let fraction = status.fractionBackedUp {
                    ProgressView(value: fraction)
                        .accessibilityHidden(true)
                }
            }
            .accessibilityElement(children: .combine)
            Label(stateText, systemImage: stateSymbol)
                .foregroundStyle(.secondary)
            if status.phase == .waitingForNetwork, status.leftForLaterFileCount > 0 {
                Label("\(status.leftForLaterFileCount) files will be sent when the network is back.", systemImage: "clock.arrow.circlepath")
                    .foregroundStyle(.secondary)
            }
            if status.phase == .uploading, let filename = status.currentFilename {
                LabeledContent("Sending") {
                    Text(filename).monospaced()
                }
            }
            if let lastPassFinishedAt = status.lastPassFinishedAt {
                LabeledContent("Last finished", value: lastPassFinishedAt.formatted(.relative(presentation: .named)))
            }
            if status.refusedFileCount > 0 {
                Label("\(status.refusedFileCount) files could not be backed up. Back up now tries them again.", systemImage: "exclamationmark.triangle")
            }
            if status.phase != .needsSignIn, let failure = status.lastFailure {
                VStack(alignment: .leading, spacing: 4) {
                    Label("The last problem", systemImage: "exclamationmark.triangle")
                    Text(failure.localizedText).font(.footnote).foregroundStyle(.secondary)
                    Text("Photo backup tries again when you open TeaNode, when the network is back, or while the phone charges.").font(.footnote).foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    private var signInSection: some View {
        Section {
            Label("The server did not accept this iPhone's backup password.", systemImage: "exclamationmark.triangle")
            Button("Sign in again", systemImage: "key", action: signInAgain)
                .disabled(isWorking)
        } footer: {
            Text("This makes a new backup password. Photos already backed up are not sent again.")
        }
    }

    private var isEverythingBackedUp: Bool {
        status.libraryItemCount > 0 && status.backedUpItemCount >= status.libraryItemCount
    }

    private var stateText: String {
        switch status.phase {
        case .off: String(localized: "Off")
        case .idle:
            isEverythingBackedUp
                ? String(localized: "Everything is backed up")
                : (status.libraryItemCount == 0 ? String(localized: "Ready to back up") : String(localized: "Waiting to continue"))
        case .listing: String(localized: "Reading your photo library")
        case .uploading: String(localized: "Backing up")
        case .waitingForNetwork: usesMobileData ? String(localized: "Waiting for the network") : String(localized: "Waiting for Wi-Fi")
        case .needsAccess: String(localized: "Needs access to all your photos")
        case .needsSignIn: String(localized: "Needs you to sign in again")
        }
    }

    private var stateSymbol: String {
        switch status.phase {
        case .off: "icloud.slash"
        case .idle: isEverythingBackedUp ? "checkmark.icloud" : "icloud"
        case .listing: "photo.stack"
        case .uploading: "icloud.and.arrow.up"
        case .waitingForNetwork: "wifi"
        case .needsAccess, .needsSignIn: "exclamationmark.triangle"
        }
    }
}

// MARK: Previews

private func previewContent(
    _ status: PhotoBackupStatus, credentialName: String? = "Robin's iPhone photo backup", accessProblem: PhotoBackupAccess? = nil, isPermitted: Bool = true,
    usesMobileData: Bool = false, isOnForOtherAccount: Bool = false
) -> some View {
    NavigationStack {
        PhotoBackupContent(
            status: status, credentialName: credentialName, accessProblem: accessProblem, isWorking: false, isPermitted: isPermitted,
            enabledAccountName: status.isEnabled ? "robin on demo.example.com" : nil, isOnForOtherAccount: isOnForOtherAccount,
            library: PhotoLibrary(photoCount: 3_800, videoCount: 112, favoriteCount: 90, trashedCount: 4, usedBytes: 21_400_000_000, trashRetentionDays: 30, maximumFileBytes: 2_000_000_000, maximumAccountBytes: 50_000_000_000),
            libraryFailureText: nil, readLibrary: {},
            isOn: .constant(status.isEnabled && !isOnForOtherAccount), usesMobileData: .constant(usesMobileData), includesHidden: .constant(false),
            isConfirmingTurnOff: .constant(false), backUpNow: {}, signInAgain: {}, turnOff: {})
    }
}

private let previewUploading = PhotoBackupStatus(
    phase: .uploading, backedUpItemCount: 1284, libraryItemCount: 3912, plannedFileCount: 2891, finishedFileCount: 1284, queuedFileCount: 4,
    currentFilename: "IMG_2041.MOV", lastPassFinishedAt: Date(timeIntervalSinceNow: -86_400))

#Preview("Off, light") { previewContent(PhotoBackupStatus(phase: .off), credentialName: nil) }
#Preview("Off, dark") { previewContent(PhotoBackupStatus(phase: .off), credentialName: nil).preferredColorScheme(.dark) }
#Preview("Backing up, light") { previewContent(previewUploading) }
#Preview("Backing up, dark") { previewContent(previewUploading).preferredColorScheme(.dark) }
#Preview("Up to date, light") {
    previewContent(PhotoBackupStatus(phase: .idle, backedUpItemCount: 3912, libraryItemCount: 3912, lastPassFinishedAt: Date(timeIntervalSinceNow: -600)))
}
#Preview("Limited access, light") {
    previewContent(PhotoBackupStatus(phase: .off), credentialName: nil, accessProblem: .limited)
}
#Preview("Sign in again, dark") {
    previewContent(PhotoBackupStatus(phase: .needsSignIn, backedUpItemCount: 1284, libraryItemCount: 3912, lastFailure: .signInRefused))
        .preferredColorScheme(.dark)
}
#Preview("Waiting for the network, light") {
    previewContent(
        PhotoBackupStatus(
            phase: .waitingForNetwork, backedUpItemCount: 1284, libraryItemCount: 3912, refusedFileCount: 2, leftForLaterFileCount: 6,
            lastFailure: .serverReason("photo storage is unavailable")))
}
#Preview("On for another account, light") {
    previewContent(previewUploading, isOnForOtherAccount: true)
}
#Preview("Not allowed, light") {
    previewContent(PhotoBackupStatus(phase: .off), credentialName: nil, isPermitted: false)
}
