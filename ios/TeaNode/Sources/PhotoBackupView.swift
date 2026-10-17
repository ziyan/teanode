import SwiftUI
import TeaNodeKit
import UIKit

/// PhotoBackupView is the screen Settings and the Photos tab open to turn the
/// backup of this iPhone's photos on and off, and to see how it is going.
struct PhotoBackupView: View {
    let connection: Connection
    @Environment(\.scenePhase) private var scenePhase
    @State private var isConfirmingTurnOff = false
    private let engine = PhotoBackupEngine.shared

    var body: some View {
        PhotoBackupContent(
            status: engine.status, credentialName: engine.credentialName, accessProblem: engine.accessProblem, isWorking: engine.isWorking,
            isOn: Binding(
                get: { engine.isEnabled },
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
        .onChange(of: scenePhase) { _, phase in
            // Back from the system's Settings, where the person may have given
            // access to the whole library.
            if phase == .active { Task { await engine.refresh() } }
        }
    }
}

/// PhotoBackupContent is the Photo Backup screen for a status, with every
/// choice and button handed in, so that each state can be previewed.
struct PhotoBackupContent: View {
    let status: PhotoBackupStatus
    let credentialName: String?
    let accessProblem: PhotoBackupAccess?
    let isWorking: Bool
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
                    .disabled(isWorking)
                    .confirmationDialog("Turn off photo backup?", isPresented: $isConfirmingTurnOff, titleVisibility: .visible) {
                        Button("Turn off", role: .destructive, action: turnOff)
                    } message: {
                        Text("This iPhone stops backing up and its backup password is revoked. Photos already on TeaNode stay there.")
                    }
            } footer: {
                Text("Photos and videos on this iPhone are copied to your TeaNode server. Deleting a photo on this iPhone does not delete it from TeaNode.")
            }

            if let accessProblem {
                accessSection(accessProblem)
            }

            if status.isEnabled {
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
                        Text("This password can only add photos to your library. Turning backup off revokes it.")
                    }
                }
            }
        }
        .navigationTitle("Photo backup")
        .navigationBarTitleDisplayMode(.inline)
    }

    // MARK: Sections

    private func accessSection(_ access: PhotoBackupAccess) -> some View {
        Section {
            Label(accessText(access), systemImage: "exclamationmark.triangle")
            Button("Open Settings", systemImage: "gear") {
                if let address = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(address) }
            }
        } footer: {
            Text("Backup reads your whole library, so TeaNode needs Full Access. Open Settings, choose Photos, then Full Access.")
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
            if status.phase != .needsSignIn, let message = status.lastErrorMessage {
                VStack(alignment: .leading, spacing: 4) {
                    Label("The last problem", systemImage: "exclamationmark.triangle")
                    Text(message).font(.footnote).foregroundStyle(.secondary)
                    Text("Backup tries again the next time it runs.").font(.footnote).foregroundStyle(.secondary)
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
                : (status.libraryItemCount == 0 ? String(localized: "Ready to back up") : String(localized: "Waiting for the next pass"))
        case .listing: String(localized: "Reading your photo library")
        case .uploading: String(localized: "Backing up")
        case .waitingForNetwork: String(localized: "Waiting for Wi-Fi or iCloud")
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
    _ status: PhotoBackupStatus, credentialName: String? = "Robin's iPhone photo backup", accessProblem: PhotoBackupAccess? = nil
) -> some View {
    NavigationStack {
        PhotoBackupContent(
            status: status, credentialName: credentialName, accessProblem: accessProblem, isWorking: false,
            isOn: .constant(status.isEnabled), usesMobileData: .constant(false), includesHidden: .constant(false),
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
    previewContent(PhotoBackupStatus(phase: .needsSignIn, backedUpItemCount: 1284, libraryItemCount: 3912, lastErrorMessage: "not signed in"))
        .preferredColorScheme(.dark)
}
#Preview("Problem, light") {
    previewContent(
        PhotoBackupStatus(
            phase: .waitingForNetwork, backedUpItemCount: 1284, libraryItemCount: 3912, refusedFileCount: 2,
            lastErrorMessage: "the library is full"))
}
