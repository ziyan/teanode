import AVKit
import Photos
import SwiftUI
import TeaNodeKit
import UniformTypeIdentifiers

/// PhotoViewer shows the photos of a list one at a time, full screen, and
/// pages between them with a swipe: a picture can be zoomed, a video plays.
/// The bar under it has what can be done to the photo on screen.
struct PhotoViewer: View {
    let model: PhotosModel
    /// The list the photos paged through come from: a list of the library, or
    /// an album.
    let listKey: PhotoListKey
    @State private var currentPhotoId: String?
    @State private var isZoomed = false
    @State private var isShowingInfo = false
    @State private var isConfirmingTrash = false
    @State private var isConfirmingDelete = false
    @Environment(\.dismiss) private var dismiss

    init(model: PhotosModel, listKey: PhotoListKey, openedPhotoId: String) {
        self.model = model
        self.listKey = listKey
        _currentPhotoId = State(initialValue: openedPhotoId)
    }

    private var photos: [Photo] { model.photos(in: listKey) }
    private var current: Photo? { photos.first { $0.id == currentPhotoId } }

    var body: some View {
        NavigationStack {
            pager
                .background(Color(.systemBackground))
                .navigationTitle(titleText)
                .navigationSubtitle(subtitleText)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { toolbarContent }
                .sheet(isPresented: $isShowingInfo) {
                    if let current { PhotoInfoView(photo: current, connection: model.connection) }
                }
                .albumTitleAlert(model: model, scope: .viewer)
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
        .onChange(of: currentPhotoId) { _, photoId in
            isZoomed = false
            guard let photoId else { return }
            Task { await model.loadMoreIfNeeded(showing: photoId, in: listKey) }
        }
        .onDisappear { model.dropPhotosNoLongerFavorite() }
    }

    private var pager: some View {
        ScrollView(.horizontal) {
            LazyHStack(spacing: 0) {
                ForEach(photos) { photo in
                    PhotoPage(photo: photo, model: model, isCurrent: photo.id == currentPhotoId, isZoomed: $isZoomed)
                        .containerRelativeFrame([.horizontal, .vertical])
                        .id(photo.id)
                }
            }
            .scrollTargetLayout()
        }
        .scrollTargetBehavior(.paging)
        .scrollPosition(id: $currentPhotoId)
        .scrollIndicators(.hidden)
        // A zoomed picture is panned with the same drag that pages.
        .scrollDisabled(isZoomed)
        .ignoresSafeArea(edges: .bottom)
    }

    private var titleText: String {
        guard let current, let moment = PhotoTimeline.moment(of: current) else { return "" }
        return moment.date.formatted(Date.FormatStyle(date: .abbreviated, time: .omitted, timeZone: moment.timeZone))
    }

    private var subtitleText: String {
        guard let current, let moment = PhotoTimeline.moment(of: current) else { return "" }
        return moment.date.formatted(Date.FormatStyle(date: .omitted, time: .shortened, timeZone: moment.timeZone))
    }

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button("Close", systemImage: "xmark") { dismiss() }
        }
        if let current, !current.isTrashed {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    AddToAlbumMenu(model: model, photos: [current], scope: .viewer, hiddenAlbumId: listKey.albumId)
                    if let albumId = listKey.albumId, let album = model.albums?.first(where: { $0.id == albumId }) {
                        Button("Use as Album Cover", systemImage: "rectangle.stack.badge.person.crop") {
                            Task { await model.setCover(of: album, to: current) }
                        }
                        Button("Remove from Album", systemImage: "rectangle.stack.badge.minus") {
                            let next = neighbor(of: current)
                            Task {
                                await model.remove([current], fromAlbum: albumId)
                                leave(movingTo: next)
                            }
                        }
                    }
                } label: {
                    Label("More", systemImage: "ellipsis")
                }
            }
        }
        if let current {
            if current.isTrashed {
                ToolbarItem(placement: .bottomBar) {
                    Button("Restore", systemImage: "arrow.uturn.backward") {
                        let next = neighbor(of: current)
                        Task {
                            await model.restore(current)
                            leave(movingTo: next)
                        }
                    }
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    Button("Info", systemImage: "info.circle") { isShowingInfo = true }
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    Button("Delete Forever", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
                        .confirmationDialog("Delete this photo for good?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                            Button("Delete Forever", role: .destructive) {
                                let next = neighbor(of: current)
                                Task {
                                    await model.deleteForever(current)
                                    leave(movingTo: next)
                                }
                            }
                        } message: {
                            Text("This cannot be undone.")
                        }
                }
            } else {
                ToolbarItem(placement: .bottomBar) {
                    Button(
                        current.isFavorite ? LocalizedStringKey("Remove from Favorites") : LocalizedStringKey("Favorite"),
                        systemImage: current.isFavorite ? "heart.fill" : "heart"
                    ) {
                        Task { await model.toggleFavorite(current, in: listKey) }
                    }
                    .sensoryFeedback(.selection, trigger: current.isFavorite)
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    PhotoShareLink(photo: current, connection: model.connection)
                        .labelStyle(.iconOnly)
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    if model.savingPhotoIds.contains(current.id) {
                        ProgressView()
                            .accessibilityLabel("Saving to Photos")
                    } else {
                        Button("Save to Photos", systemImage: "square.and.arrow.down") {
                            Task { await model.saveToPhotos(current) }
                        }
                    }
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    Button("Info", systemImage: "info.circle") { isShowingInfo = true }
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    Button("Move to Trash", systemImage: "trash", role: .destructive) { isConfirmingTrash = true }
                        .confirmationDialog("Move this photo to the Trash?", isPresented: $isConfirmingTrash, titleVisibility: .visible) {
                            Button("Move to Trash", role: .destructive) {
                                let next = neighbor(of: current)
                                Task {
                                    await model.moveToTrash(current)
                                    leave(movingTo: next)
                                }
                            }
                        }
                }
            }
        }
    }

    /// The photo that takes the place of one about to leave the list: the one
    /// after it, or else the one before.
    private func neighbor(of photo: Photo) -> String? {
        let listed = photos
        guard let index = listed.firstIndex(where: { $0.id == photo.id }) else { return nil }
        if index + 1 < listed.count { return listed[index + 1].id }
        return index > 0 ? listed[index - 1].id : nil
    }

    /// Goes on to the photo that took the place of the one that left, or
    /// closes the viewer when it was the last.
    private func leave(movingTo photoId: String?) {
        if let photoId, photos.contains(where: { $0.id == photoId }) {
            currentPhotoId = photoId
        } else {
            dismiss()
        }
    }
}

/// PhotoPage is one photo in the viewer.
struct PhotoPage: View {
    let photo: Photo
    let model: PhotosModel
    let isCurrent: Bool
    @Binding var isZoomed: Bool
    @State private var picture: UIImage?
    @State private var player: AVPlayer?
    @State private var videoFileURL: URL?
    @State private var videoProblemText: String?

    var body: some View {
        ZStack {
            if photo.isVideo {
                videoBody
            } else if let picture {
                // A new picture, the large one in place of the small, is a new
                // zooming view: the old one keeps showing what it was made with.
                ZoomablePicture(picture: picture, isZoomed: isCurrent ? $isZoomed : .constant(false))
                    .id(ObjectIdentifier(picture))
                    .accessibilityLabel(accessibilityText)
                    .accessibilityAddTraits(.isImage)
            } else if photo.isFailed {
                ContentUnavailableView {
                    Label("Cannot show this photo", systemImage: "exclamationmark.circle")
                } description: {
                    Text("TeaNode could not make a picture of it.")
                }
            } else {
                ProgressView()
            }
        }
        .task(id: photo.viewerPreviewPath) {
            await loadPicture()
        }
        .task(id: isCurrent) {
            await loadVideo()
        }
        .accessibilityElement(children: .contain)
    }

    private var accessibilityText: String {
        let kind = photo.isLivePhoto ? String(localized: "Live Photo") : String(localized: "Photo")
        guard let taken = PhotoTimeline.takenText(of: photo) else { return kind }
        return "\(kind), \(taken)"
    }

    @ViewBuilder
    private var videoBody: some View {
        if let player {
            VideoPlayer(player: player)
        } else {
            ZStack {
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFit()
                }
                if let videoProblemText {
                    Text(videoProblemText)
                        .font(.callout)
                        .padding()
                        .background(.regularMaterial, in: .rect(cornerRadius: 12))
                        .padding()
                } else if photo.isFailed {
                    Label("Cannot play this video", systemImage: "exclamationmark.circle")
                        .padding()
                        .background(.regularMaterial, in: .rect(cornerRadius: 12))
                } else {
                    ProgressView()
                }
            }
        }
    }

    /// The small preview first, which the grid already has, then the large
    /// one.
    private func loadPicture() async {
        if picture == nil, let small = photo.gridPreviewPath,
            let smallPicture = await PhotoImages.picture(path: small, connection: model.connection)
        {
            picture = smallPicture
        }
        if let path = photo.viewerPreviewPath, let loaded = await PhotoImages.picture(path: path, connection: model.connection) {
            picture = loaded
        }
    }

    /// Plays the video while its page is the one on screen, and lets go of it
    /// when the person pages away. The video is read once into a file of the
    /// app's own, since a player cannot send the app's sign-in.
    private func loadVideo() async {
        guard photo.isVideo else { return }
        guard isCurrent else {
            player?.pause()
            player = nil
            if let videoFileURL { try? FileManager.default.removeItem(at: videoFileURL.deletingLastPathComponent()) }
            videoFileURL = nil
            return
        }
        guard let path = photo.videoPath else { return }
        do {
            let data = try await model.connection.download(path)
            guard !Task.isCancelled else { return }
            let fileURL = try PhotoTemporaryFile.write(data: data, fileName: "video.mp4")
            videoFileURL = fileURL
            videoProblemText = nil
            player = AVPlayer(url: fileURL)
        } catch {
            if !Task.isCancelled { videoProblemText = error.localizedDescription }
        }
    }
}

/// PhotoInfoView is what is known of one photo: when and where it was taken,
/// with what, how large it is and what it is called.
struct PhotoInfoView: View {
    let photo: Photo
    let connection: Connection
    @State private var detail: PhotoDetail?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                LabeledContent("Date") {
                    Text(PhotoTimeline.takenText(of: photo) ?? String(localized: "Unknown"))
                }
                if !photo.placeName.isEmpty {
                    LabeledContent("Place") { Text(photo.placeName) }
                }
                if !cameraText.isEmpty {
                    LabeledContent("Camera") { Text(cameraText) }
                }
                LabeledContent("Size") {
                    Text(sizeText)
                }
                if !fileName.isEmpty {
                    LabeledContent("File name") { Text(fileName).monospaced() }
                }
                if let albums = detail?.albums, !albums.isEmpty {
                    Section("Albums") {
                        ForEach(albums) { album in
                            Label(album.albumTitle, systemImage: "rectangle.stack")
                        }
                    }
                }
            }
            .navigationTitle("Info")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .task { detail = try? await connection.photoDetail(photo.id) }
    }

    private var cameraText: String {
        [photo.cameraMake, photo.cameraModel].filter { !$0.isEmpty }.joined(separator: " ")
    }

    private var fileName: String {
        if let name = detail?.originalFile?.originalFilename, !name.isEmpty { return name }
        return photo.originalFilename
    }

    /// The pixels, and the bytes of the original once they are known.
    private var sizeText: String {
        var parts: [String] = []
        if photo.widthPixels > 0, photo.heightPixels > 0 {
            parts.append("\(photo.widthPixels) × \(photo.heightPixels)")
        }
        if let bytes = detail?.originalFile?.fileSizeBytes {
            parts.append(bytes.formatted(.byteCount(style: .file)))
        }
        return parts.isEmpty ? String(localized: "Unknown") : parts.joined(separator: " · ")
    }
}

// MARK: Sharing and saving

/// PhotoShareLink shares the photo's file. The file is read from the server
/// only when the person picks where to send it.
struct PhotoShareLink: View {
    let photo: Photo
    let connection: Connection

    var body: some View {
        if photo.isVideo {
            ShareLink(item: PhotoShareMovie(connection: connection, photo: photo), preview: SharePreview(photo.fileName)) {
                Label("Share", systemImage: "square.and.arrow.up")
            }
        } else {
            ShareLink(item: PhotoShareImage(connection: connection, photo: photo), preview: SharePreview(photo.fileName)) {
                Label("Share", systemImage: "square.and.arrow.up")
            }
        }
    }
}

/// PhotosShareLink shares the files of several photos at once. Pictures and
/// videos go together, so each is handed on as a file of any kind.
struct PhotosShareLink: View {
    let photos: [Photo]
    let connection: Connection

    var body: some View {
        ShareLink(
            items: photos.map { PhotoShareFile(connection: connection, photo: $0) },
            preview: { SharePreview($0.photo.fileName) }
        ) {
            Label("Share", systemImage: "square.and.arrow.up")
        }
    }
}

/// PhotoShareFile is one photo's file, of whatever kind, handed to the share
/// sheet.
struct PhotoShareFile: Transferable, Sendable {
    let connection: Connection
    let photo: Photo

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(exportedContentType: .item) { item in
            let download = try await item.connection.downloadPhotoFile(item.photo)
            return SentTransferredFile(try PhotoTemporaryFile.write(download))
        }
    }
}

/// PhotoShareImage is a picture handed to the share sheet as a file.
struct PhotoShareImage: Transferable, Sendable {
    let connection: Connection
    let photo: Photo

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(exportedContentType: .image) { item in
            let download = try await item.connection.downloadPhotoFile(item.photo)
            return SentTransferredFile(try PhotoTemporaryFile.write(download))
        }
    }
}

/// PhotoShareMovie is a video handed to the share sheet as a file.
struct PhotoShareMovie: Transferable, Sendable {
    let connection: Connection
    let photo: Photo

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(exportedContentType: .movie) { item in
            let download = try await item.connection.downloadPhotoFile(item.photo)
            return SentTransferredFile(try PhotoTemporaryFile.write(download))
        }
    }
}

/// PhotoTemporaryFile writes what was read from the server to a file of the
/// app's own, in a folder of its own so that the file can keep its name.
enum PhotoTemporaryFile {
    static func write(_ download: PhotoDownload) throws -> URL {
        try write(data: download.data, fileName: download.fileName)
    }

    static func write(data: Data, fileName: String) throws -> URL {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let safeName = fileName.isEmpty ? "photo" : fileName.replacingOccurrences(of: "/", with: "-")
        let fileURL = folder.appendingPathComponent(safeName)
        try data.write(to: fileURL, options: .atomic)
        return fileURL
    }
}

/// PhotoLibrarySaver adds a file to the phone's Photos. It asks only to add,
/// never to read the library.
enum PhotoLibrarySaver {
    static func save(fileURL: URL, isVideo: Bool) async throws {
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            throw TeaNodeError.refused(String(localized: "TeaNode is not allowed to add to your Photos. Allow it in Settings, under Photos."))
        }
        try await PHPhotoLibrary.shared().performChanges {
            let request = PHAssetCreationRequest.forAsset()
            request.addResource(with: isVideo ? .video : .photo, fileURL: fileURL, options: nil)
        }
    }
}
