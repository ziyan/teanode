import Photos
import os
import SwiftUI
import TeaNodeKit
import UIKit

/// PhotoBackupRefusedFilesView is the list the photo backup status opens from
/// "N files could not be backed up": each file backup gave up on, with its
/// photo, which of the photo's files it is, when the photo was taken and why it
/// did not go up, and the files the person skipped below them. A file is tried
/// again or skipped with a swipe, its long-press menu, or a tap on its row.
struct PhotoBackupRefusedFilesView: View {
    private let engine = PhotoBackupEngine.shared
    @State private var files: [PhotoBackupRefusedFile] = []
    @State private var isLoaded = false

    var body: some View {
        PhotoBackupRefusedFilesContent(
            files: files, isLoaded: isLoaded,
            retry: { file in Task { await act { await engine.retry(file) } } },
            skip: { file in Task { await act { await engine.skip(file) } } })
            .task { await load() }
            // A pass that ends may have sent a file tried again, or given up on another.
            .onChange(of: engine.finishedWorkCount) { Task { await load() } }
    }

    private func load() async {
        files = await engine.refusedFiles()
        isLoaded = true
    }

    private func act(_ action: () async -> Void) async {
        await action()
        await load()
    }
}

/// PhotoBackupRefusedFilesContent is the list for the files handed in, so that
/// it can be previewed.
struct PhotoBackupRefusedFilesContent: View {
    let files: [PhotoBackupRefusedFile]
    let isLoaded: Bool
    let retry: (PhotoBackupRefusedFile) -> Void
    let skip: (PhotoBackupRefusedFile) -> Void

    private var failedFiles: [PhotoBackupRefusedFile] { files.filter { !$0.isSkipped } }
    private var skippedFiles: [PhotoBackupRefusedFile] { files.filter(\.isSkipped) }

    var body: some View {
        List {
            if !failedFiles.isEmpty {
                Section {
                    ForEach(failedFiles) { file in
                        PhotoBackupRefusedFileRow(file: file, retry: { retry(file) }, skip: { skip(file) })
                    }
                } header: {
                    Text("Could not be backed up")
                } footer: {
                    Text("Back up now tries these again. A file you skip stays out until you try it again.")
                }
            }
            if !skippedFiles.isEmpty {
                Section {
                    ForEach(skippedFiles) { file in
                        PhotoBackupRefusedFileRow(file: file, retry: { retry(file) }, skip: nil)
                    }
                } header: {
                    Text("Skipped")
                } footer: {
                    Text("Skipped files are not sent and do not count as problems.")
                }
            }
        }
        .overlay {
            if isLoaded && files.isEmpty {
                ContentUnavailableView("Nothing to try again", systemImage: "checkmark.icloud", description: Text("Every file that could be backed up has been."))
            }
        }
        .navigationTitle("Files not backed up")
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// PhotoBackupRefusedFileRow is one file: its photo's picture, which of its
/// files it is, the day the photo was taken, and why it did not go up. A tap
/// asks what to do with it; a swipe or the long-press menu does it at once.
/// `skip` is nil for a file already skipped, which can only be tried again.
struct PhotoBackupRefusedFileRow: View {
    let file: PhotoBackupRefusedFile
    let retry: () -> Void
    let skip: (() -> Void)?
    @State private var preview: PhotoBackupFilePreview?
    @State private var isAsking = false

    var body: some View {
        Button {
            isAsking = true
        } label: {
            HStack(alignment: .top, spacing: 12) {
                PhotoBackupFileThumbnail(image: preview?.image)
                VStack(alignment: .leading, spacing: 2) {
                    Text(roleText)
                        .foregroundStyle(.primary)
                    if let detailText {
                        Text(detailText)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    Text(reasonText)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityHint(skip == nil ? Text("Tries this file again") : Text("Tries this file again or skips it"))
        .confirmationDialog(roleText, isPresented: $isAsking, titleVisibility: .visible) {
            Button("Try again", action: retry)
            if let skip {
                Button("Skip this file", action: skip)
            }
        } message: {
            Text(reasonText)
        }
        // Try again at the edge, Skip beside it.
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            Button("Try again", systemImage: "arrow.clockwise", action: retry)
                .tint(.accentColor)
            if let skip {
                Button("Skip", systemImage: "forward", action: skip)
                    .tint(.gray)
            }
        }
        .contextMenu {
            Button("Try again", systemImage: "arrow.clockwise", action: retry)
            if let skip {
                Button("Skip this file", systemImage: "forward", action: skip)
            }
        }
        .task(id: file.localIdentifier) {
            preview = await PhotoBackupFilePreview.read(localIdentifier: file.localIdentifier)
        }
    }

    /// Which of the photo's files this is.
    private var roleText: String {
        switch file.role {
        case .original: String(localized: "Original photo")
        case .edited: String(localized: "Edited version")
        case .liveVideo: String(localized: "Live Photo video")
        }
    }

    /// When the photo was taken, or that it is no longer in the library; nil
    /// while the library has not been read.
    private var detailText: String? {
        guard let preview else { return nil }
        guard preview.isInLibrary else {
            // The reason says so already.
            return file.failure == .fileIsNotInLibrary ? nil : String(localized: "No longer in your photo library")
        }
        guard let takenAt = preview.takenAt else { return nil }
        return String(localized: "Taken \(takenAt.formatted(date: .abbreviated, time: .shortened))")
    }

    private var reasonText: String {
        file.failure?.localizedText ?? String(localized: "No reason was kept.")
    }
}

/// PhotoBackupFileThumbnail is a file's photo, small and square, or a symbol
/// when the library has no picture of it.
struct PhotoBackupFileThumbnail: View {
    let image: UIImage?
    @ScaledMetric(relativeTo: .body) private var sidePoints: CGFloat = 56

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFill()
            } else {
                Image(systemName: "photo")
                    .font(.title3)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Color(.secondarySystemBackground))
            }
        }
        .frame(width: sidePoints, height: sidePoints)
        .clipShape(.rect(cornerRadius: 8, style: .continuous))
        .accessibilityHidden(true)
    }
}

/// PhotoBackupFilePreview is what the photo library says of a file's photo: a
/// small picture, when the photo was taken, and whether it is still there.
struct PhotoBackupFilePreview {
    let image: UIImage?
    let takenAt: Date?
    let isInLibrary: Bool

    /// The square side asked of the library, in pixels.
    static let thumbnailPixels: CGFloat = 180

    /// Reads the photo from the library, without the network: a photo kept only
    /// in iCloud shows the symbol rather than costing a download.
    static func read(localIdentifier: String) async -> PhotoBackupFilePreview {
        #if DEBUG
            if DemoServer.isRequested { return demo(localIdentifier: localIdentifier) }
        #endif
        guard let asset = PHAsset.fetchAssets(withLocalIdentifiers: [localIdentifier], options: nil).firstObject else {
            return PhotoBackupFilePreview(image: nil, takenAt: nil, isInLibrary: false)
        }
        let options = PHImageRequestOptions()
        // One answer only, so the continuation is resumed once.
        options.deliveryMode = .highQualityFormat
        options.resizeMode = .fast
        options.isNetworkAccessAllowed = false
        let image = await withCheckedContinuation { (continuation: CheckedContinuation<UIImage?, Never>) in
            // Resumed by the first answer alone: a second resume would stop
            // the app, and PhotoKit does not promise a single answer in every
            // case.
            let answered = OSAllocatedUnfairLock(initialState: false)
            PHImageManager.default().requestImage(
                for: asset, targetSize: CGSize(width: thumbnailPixels, height: thumbnailPixels), contentMode: .aspectFill, options: options
            ) { image, _ in
                let isFirst = answered.withLock { isAnswered in
                    defer { isAnswered = true }
                    return !isAnswered
                }
                if isFirst { continuation.resume(returning: image) }
            }
        }
        return PhotoBackupFilePreview(image: image, takenAt: asset.creationDate, isInLibrary: true)
    }

    #if DEBUG
        /// The demo files' pictures, drawn: a sky and a hill in a color from the
        /// identifier. A file whose identifier ends in "gone" has left the library.
        private static func demo(localIdentifier: String) -> PhotoBackupFilePreview {
            guard !localIdentifier.hasSuffix("gone") else { return PhotoBackupFilePreview(image: nil, takenAt: nil, isInLibrary: false) }
            let hue = CGFloat(localIdentifier.utf8.reduce(0) { ($0 &* 31 &+ Int($1)) % 360 }) / 360
            let side = thumbnailPixels
            let format = UIGraphicsImageRendererFormat()
            format.scale = 1
            let image = UIGraphicsImageRenderer(size: CGSize(width: side, height: side), format: format).image { context in
                UIColor(hue: hue, saturation: 0.35, brightness: 0.95, alpha: 1).setFill()
                context.fill(CGRect(x: 0, y: 0, width: side, height: side))
                UIColor(white: 1, alpha: 0.85).setFill()
                context.cgContext.fillEllipse(in: CGRect(x: side * 0.58, y: side * 0.16, width: side * 0.22, height: side * 0.22))
                UIColor(hue: hue, saturation: 0.6, brightness: 0.4, alpha: 1).setFill()
                context.cgContext.fillEllipse(in: CGRect(x: -side * 0.2, y: side * 0.6, width: side * 1.4, height: side * 0.9))
            }
            let daysAgo = Double(localIdentifier.utf8.count % 9 + 3)
            return PhotoBackupFilePreview(image: image, takenAt: Date(timeIntervalSinceNow: -daysAgo * 86_400), isInLibrary: true)
        }
    #endif
}

// MARK: Previews

private let previewRefusedFiles = [
    PhotoBackupRefusedFile(
        localIdentifier: "preview-a", clientPhotoId: "local:preview-a", role: .liveVideo, failure: .fileIsEmpty,
        refusalCount: PhotoBackupPlanner.maximumRefusalCount),
    PhotoBackupRefusedFile(
        localIdentifier: "preview-b", clientPhotoId: "local:preview-b", role: .edited, failure: .fileRefused(serverReason: nil),
        refusalCount: PhotoBackupPlanner.maximumRefusalCount),
    PhotoBackupRefusedFile(
        localIdentifier: "preview-gone", clientPhotoId: "local:preview-gone", role: .original, failure: .fileIsNotInLibrary,
        refusalCount: PhotoBackupPlanner.maximumRefusalCount, skippedAt: Date(timeIntervalSinceNow: -86_400)),
]

#Preview("Files not backed up, light") {
    NavigationStack { PhotoBackupRefusedFilesContent(files: previewRefusedFiles, isLoaded: true, retry: { _ in }, skip: { _ in }) }
}
#Preview("Files not backed up, dark") {
    NavigationStack { PhotoBackupRefusedFilesContent(files: previewRefusedFiles, isLoaded: true, retry: { _ in }, skip: { _ in }) }
        .preferredColorScheme(.dark)
}
#Preview("Nothing left, light") {
    NavigationStack { PhotoBackupRefusedFilesContent(files: [], isLoaded: true, retry: { _ in }, skip: { _ in }) }
}
