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
    @Environment(AppModel.self) private var app

    init(model: PhotosModel, listKey: PhotoListKey, openedPhotoId: String) {
        self.model = model
        self.listKey = listKey
        _currentPhotoId = State(initialValue: openedPhotoId)
        #if DEBUG
            // `-TeaNodeScreen photos-viewer-described` and `photos-viewer-failed` open Info over the viewer.
            if DemoServer.isRequested, DemoServer.screen == "photos-viewer-described" || DemoServer.screen == "photos-viewer-failed" {
                _isShowingInfo = State(initialValue: true)
            }
        #endif
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
                    if let current { PhotoInfoView(photo: current, model: model, onAskAgent: askAgent) }
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
                        Button("Use as album cover", systemImage: "rectangle.stack.badge.person.crop") {
                            Task { await model.setCover(of: album, to: current) }
                        }
                        Button("Remove from album", systemImage: "rectangle.stack.badge.minus") {
                            let next = neighbor(of: current)
                            Task {
                                if await model.remove([current], fromAlbum: albumId) { leave(movingTo: next) }
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
                            if await model.restore(current) { leave(movingTo: next) }
                        }
                    }
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    Button("Info", systemImage: "info.circle") { isShowingInfo = true }
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    Button("Delete forever", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
                        .confirmationDialog("Delete this photo forever?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                            Button("Delete forever", role: .destructive) {
                                let next = neighbor(of: current)
                                Task {
                                    if await model.deleteForever(current) { leave(movingTo: next) }
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
                                    if await model.moveToTrash(current) { leave(movingTo: next) }
                                }
                            }
                        }
                }
            }
        }
    }

    /// Opens the conversation pointed at the photo: a chip above the box says
    /// which photo, and the person writes their own question.
    private func askAgent(about photo: Photo) {
        app.askingAbout = AgentReference(photoId: photo.id, photoTitle: PhotoTimeline.referenceTitle(of: photo))
        isShowingInfo = false
        dismiss()
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
    @State private var videoProblemText: String?
    @State private var isPlayingLiveVideo = false

    var body: some View {
        ZStack {
            if photo.isVideo {
                videoBody
            } else if isPlayingLiveVideo, let player {
                VideoPlayer(player: player)
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
                    Text(photo.processingErrorMessage.isEmpty ? String(localized: "No picture could be made of it.") : photo.processingErrorMessage)
                } actions: {
                    Button("Try again") { Task { await model.retryProcessing([photo]) } }
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
        .overlay(alignment: .topLeading) {
            if photo.isLivePhoto, isCurrent, !isZoomed {
                liveButton
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .AVPlayerItemDidPlayToEndTime)) { notification in
            if isPlayingLiveVideo, (notification.object as? AVPlayerItem) === player?.currentItem { stopLiveVideo() }
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
                    VStack(spacing: 12) {
                        Label("Cannot play this video", systemImage: "exclamationmark.circle")
                        if !photo.processingErrorMessage.isEmpty {
                            Text(photo.processingErrorMessage)
                                .font(.callout)
                                .foregroundStyle(.secondary)
                        }
                        Button("Try again") { Task { await model.retryProcessing([photo]) } }
                            .buttonStyle(.bordered)
                    }
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

    /// Floats over the picture of a Live Photo: plays the video it came with.
    private var liveButton: some View {
        Button {
            if isPlayingLiveVideo { stopLiveVideo() } else { Task { await playLiveVideo() } }
        } label: {
            Label(
                isPlayingLiveVideo ? LocalizedStringKey("Stop") : LocalizedStringKey("Live"),
                systemImage: isPlayingLiveVideo ? "livephoto.slash" : "livephoto"
            )
            .font(.subheadline.bold())
        }
        .buttonStyle(.glass)
        .controlSize(.regular)
        .accessibilityLabel(isPlayingLiveVideo ? LocalizedStringKey("Stop Live Photo") : LocalizedStringKey("Play Live Photo"))
        .padding()
    }

    /// Plays the video while its page is the one on screen, and lets go of it
    /// when the person pages away. A player reads from a fresh address that
    /// opens without the app's sign-in and serves byte ranges, so the video
    /// starts at once and only what is watched is read.
    private func loadVideo() async {
        guard isCurrent else {
            player?.pause()
            player = nil
            isPlayingLiveVideo = false
            return
        }
        guard photo.isVideo, photo.videoPath != nil else { return }
        do {
            let address = try await model.connection.photoPlaybackAddress(photo.id, source: .preview("video"))
            guard !Task.isCancelled else { return }
            videoProblemText = nil
            player = AVPlayer(url: address)
        } catch {
            if !Task.isCancelled { videoProblemText = model.describe(error) }
        }
    }

    /// Plays the video of a Live Photo in place of its picture.
    private func playLiveVideo() async {
        do {
            let address = try await model.connection.photoPlaybackAddress(photo.id, source: .file("liveVideo"))
            guard isCurrent else { return }
            let livePlayer = AVPlayer(url: address)
            player = livePlayer
            isPlayingLiveVideo = true
            livePlayer.play()
        } catch {
            model.problemText = model.describe(error)
        }
    }

    private func stopLiveVideo() {
        player?.pause()
        player = nil
        isPlayingLiveVideo = false
    }
}

/// PhotoInfoView is what is known of one photo: when and where it was taken,
/// with what, how large it is and what it is called, why it could not be made
/// ready if that failed, the albums it is in and the people in it (each opens),
/// and what a model wrote about the picture.
struct PhotoInfoView: View {
    let photo: Photo
    let model: PhotosModel
    let onAskAgent: (Photo) -> Void
    @State private var detail: PhotoDetail?
    /// Why the photo's details could not be read; nil when they could, or have
    /// not been asked for yet.
    @State private var detailFailureText: String?
    @State private var isAskingToDescribe = false
    @State private var hasGivenUpWaiting = false
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var app

    private var connection: Connection { model.connection }
    /// The photo as the details last read it, which is newer than the one the
    /// viewer opened this with once it was asked to be made ready again.
    private var shownPhoto: Photo { detail?.photo ?? photo }
    private var description: PhotoDescription { detail?.photoDescription ?? PhotoDescription() }
    private var isDescriptionAvailable: Bool { model.library?.isDescriptionAvailable == true }
    private var people: [PhotoFace] { (detail?.faces ?? []).filter(\.isGrouped) }

    var body: some View {
        NavigationStack {
            List {
                if shownPhoto.isFailed {
                    failureSection
                }
                if let detailFailureText {
                    detailFailureSection(detailFailureText)
                }
                LabeledContent("Date") {
                    Text(PhotoTimeline.takenText(of: shownPhoto) ?? String(localized: "Unknown"))
                }
                if !shownPhoto.placeName.isEmpty {
                    LabeledContent("Place") { Text(shownPhoto.placeName) }
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
                            NavigationLink {
                                PhotoAlbumScreen(model: model, albumId: album.id)
                            } label: {
                                Label(album.albumTitle, systemImage: "rectangle.stack")
                            }
                        }
                    }
                }
                if !people.isEmpty {
                    peopleSection
                }
                if detail != nil {
                    descriptionSection
                }
            }
            .navigationTitle("Info")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            // The viewer's toasts are behind this sheet, so it shows its own.
            .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
        }
        .presentationDetents([.medium, .large])
        .task { await loadDetail() }
        .task(id: description.isDescriptionRequested) { await followDescription() }
        .task { if model.library == nil { await model.loadLibrary() } }
    }

    /// Reads the photo's details. A failure is kept to be shown with a way to
    /// try again, and what was read before stays.
    private func loadDetail() async {
        do {
            detail = try await connection.photoDetail(photo.id)
            detailFailureText = nil
        } catch {
            guard !Task.isCancelled else { return }
            detailFailureText = model.describe(error)
        }
    }

    // MARK: When the details cannot be read, or the photo failed

    private func detailFailureSection(_ failureText: String) -> some View {
        Section {
            Label("The details of this photo could not be read.", systemImage: "exclamationmark.triangle")
            Text(failureText)
                .font(.footnote)
                .foregroundStyle(.secondary)
            Button("Try again", systemImage: "arrow.clockwise") { Task { await loadDetail() } }
        }
    }

    /// Why the picture could not be made, in the server's plain words, and a
    /// way to ask for it to be made again.
    private var failureSection: some View {
        Section {
            Label("This photo could not be made ready.", systemImage: "exclamationmark.circle")
            if !shownPhoto.processingErrorMessage.isEmpty {
                Text(shownPhoto.processingErrorMessage)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Button("Try again", systemImage: "arrow.clockwise") {
                Task {
                    if await model.retryProcessing([shownPhoto]) { await loadDetail() }
                }
            }
        }
    }

    // MARK: People

    /// The people in the photo, each with their face, opening that person. A
    /// person who is not who the server says is taken out of the group with a
    /// touch and hold.
    private var peopleSection: some View {
        Section {
            ForEach(people) { face in
                NavigationLink {
                    PhotoPersonScreen(model: model, photoFaceGroupId: face.photoFaceGroupId)
                } label: {
                    HStack(spacing: 12) {
                        FacePictureView(facePictureUrl: face.facePictureUrl, name: shownName(of: face), connection: connection)
                            .frame(width: 40, height: 40)
                        Text(shownName(of: face))
                            .foregroundStyle(face.contactName.isEmpty ? .secondary : .primary)
                    }
                    .accessibilityElement(children: .combine)
                }
                .contextMenu {
                    Button(role: .destructive) {
                        Task {
                            if await model.removeFace(face) { await loadDetail() }
                        }
                    } label: {
                        if face.contactName.isEmpty {
                            Label("Not this person", systemImage: "person.crop.circle.badge.xmark")
                        } else {
                            Label("Not \(face.contactName)", systemImage: "person.crop.circle.badge.xmark")
                        }
                    }
                }
            }
        } header: {
            Text("photos.people")
        } footer: {
            Text("Touch and hold a person if it is not them.")
        }
    }

    private func shownName(of face: PhotoFace) -> String {
        let name = face.contactName.trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? String(localized: "Unnamed person") : name
    }

    // MARK: Description

    /// What a model wrote about the picture, Describe when nothing has, a
    /// progress view while it is on its way, and the reason when describing is
    /// not available. The words are the model's, shown and never obeyed.
    private var descriptionSection: some View {
        Section {
            if description.isDescribed {
                Text(description.photoDescription)
                if !description.descriptionVisibleText.isEmpty {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Text in the picture")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        Text(description.descriptionVisibleText)
                    }
                    .accessibilityElement(children: .combine)
                }
                if let describedOn = description.describedOnText() {
                    Text("Described on \(describedOn)")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            if description.isDescriptionRequested || isAskingToDescribe {
                HStack(spacing: 12) {
                    ProgressView()
                    Text(waitingText)
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            } else if description.isDescribed {
                if isDescriptionAvailable {
                    Button("Describe again", systemImage: "arrow.clockwise") { Task { await describe(isRedescribed: true) } }
                }
            } else if isDescriptionAvailable {
                Button("Describe", systemImage: "text.viewfinder") { Task { await describe(isRedescribed: false) } }
            } else if let library = model.library {
                Text(library.descriptionUnavailableMessage.isEmpty ? String(localized: "Describing photos is not available.") : library.descriptionUnavailableMessage)
                    .foregroundStyle(.secondary)
            }
            Button(askAgentText, systemImage: "bubble.left") { onAskAgent(photo) }
        } header: {
            Text("Description")
        } footer: {
            if !description.isDescribed, !description.isDescriptionRequested, isDescriptionAvailable {
                Text("\(app.agentTitle) looks at the picture and writes what it shows, so a search can find it.")
            }
        }
    }

    /// The assistant is named as the conversation names it.
    private var askAgentText: String {
        app.agentName.isEmpty ? String(localized: "Ask your agent") : String(localized: "Ask \(app.agentName)")
    }

    private var waitingText: String {
        if hasGivenUpWaiting { return String(localized: "This is taking longer than usual. It keeps going in the background, so look again in a few minutes.") }
        return description.isDescribed ? String(localized: "Describing again…") : String(localized: "Describing…")
    }

    private func describe(isRedescribed: Bool) async {
        isAskingToDescribe = true
        hasGivenUpWaiting = false
        defer { isAskingToDescribe = false }
        do {
            let asked = try await connection.describePhoto(photo.id, isRedescribed: isRedescribed)
            if let held = detail {
                detail = PhotoDetail(
                    photo: held.photo, photoFiles: held.photoFiles, photoPreviews: held.photoPreviews, albums: held.albums, photoDescription: asked, faces: held.faces)
            } else {
                await loadDetail()
            }
        } catch {
            model.problemText = model.describe(error)
        }
    }

    /// While a description is on its way, asks for the photo again every few
    /// seconds, and not for longer than two minutes: after that the job goes
    /// on without the screen, and the description is there when the person
    /// comes back.
    private func followDescription() async {
        var waitedSeconds = 0
        while !Task.isCancelled, description.isDescriptionRequested {
            guard PhotoDescriptionPolling.shouldAskAgain(afterWaiting: waitedSeconds) else {
                hasGivenUpWaiting = true
                return
            }
            try? await Task.sleep(for: .seconds(PhotoDescriptionPolling.intervalSeconds))
            waitedSeconds += PhotoDescriptionPolling.intervalSeconds
            guard !Task.isCancelled else { return }
            await loadDetail()
        }
    }

    // MARK: Details

    private var cameraText: String {
        [shownPhoto.cameraMake, shownPhoto.cameraModel].filter { !$0.isEmpty }.joined(separator: " ")
    }

    private var fileName: String {
        if let name = detail?.originalFile?.originalFilename, !name.isEmpty { return name }
        return shownPhoto.originalFilename
    }

    /// The pixels, and the bytes of the original once they are known.
    private var sizeText: String {
        var parts: [String] = []
        if shownPhoto.widthPixels > 0, shownPhoto.heightPixels > 0 {
            parts.append("\(shownPhoto.widthPixels) × \(shownPhoto.heightPixels)")
        }
        if let bytes = detail?.originalFile?.fileSizeBytes {
            parts.append(bytes.formatted(.byteCount(style: .file)))
        }
        return parts.isEmpty ? String(localized: "Unknown") : parts.joined(separator: " · ")
    }
}

// MARK: Sharing and saving

/// PhotoShareLink shares the photo's file. The file is read from the server
/// only when the person picks where to send it. A photo that was edited, or
/// is a Live Photo, has a menu instead, to share its original, its edited
/// version or its video.
struct PhotoShareLink: View {
    let photo: Photo
    let connection: Connection

    var body: some View {
        if photo.hasEditedFile || photo.hasLiveVideoFile {
            Menu {
                originalLink(label: "Share original")
                if photo.hasEditedFile {
                    ShareLink(item: PhotoShareFile(connection: connection, photo: photo, role: "edited"), preview: SharePreview(photo.fileName)) {
                        Label("Share edited version", systemImage: "slider.horizontal.3")
                    }
                }
                if photo.hasLiveVideoFile {
                    ShareLink(item: PhotoShareFile(connection: connection, photo: photo, role: "liveVideo"), preview: SharePreview(photo.fileName)) {
                        Label("Share Live Photo video", systemImage: "livephoto")
                    }
                }
            } label: {
                Label("Share", systemImage: "square.and.arrow.up")
            }
        } else {
            originalLink(label: "Share")
        }
    }

    @ViewBuilder
    private func originalLink(label: LocalizedStringKey) -> some View {
        if photo.isVideo {
            ShareLink(item: PhotoShareMovie(connection: connection, photo: photo), preview: SharePreview(photo.fileName)) {
                Label(label, systemImage: "square.and.arrow.up")
            }
        } else {
            ShareLink(item: PhotoShareImage(connection: connection, photo: photo), preview: SharePreview(photo.fileName)) {
                Label(label, systemImage: "square.and.arrow.up")
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
/// sheet: the original (and the preview when that cannot be read) or, when a
/// role is given, "edited" or "liveVideo" and nothing else.
struct PhotoShareFile: Transferable, Sendable {
    let connection: Connection
    let photo: Photo
    let role: String?

    init(connection: Connection, photo: Photo) {
        self.connection = connection
        self.photo = photo
        role = nil
    }

    init(connection: Connection, photo: Photo, role: String) {
        self.connection = connection
        self.photo = photo
        self.role = role
    }

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(exportedContentType: .item) { item in
            let download: PhotoDownload
            if let role = item.role {
                download = try await item.connection.downloadPhotoFile(item.photo, role: role)
            } else {
                download = try await item.connection.downloadPhotoFile(item.photo)
            }
            return SentTransferredFile(download.fileURL)
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
            return SentTransferredFile(download.fileURL)
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
            return SentTransferredFile(download.fileURL)
        }
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
