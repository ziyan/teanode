import AVFoundation
import AVKit
import MapKit
import Photos
import SwiftUI
import TeaNodeKit
import UniformTypeIdentifiers

/// PhotoViewer shows the photos of a list one at a time, full screen on black,
/// and pages between them with a swipe: a picture can be zoomed, a video plays.
/// The date and time of the photo are at the top, a strip of the neighboring
/// photos and a bar with what can be done to the photo on screen are at the
/// bottom, a tap on the picture hides all of them, and a drag down on a picture
/// that is not zoomed closes the viewer.
struct PhotoViewer: View {
    let model: PhotosModel
    /// The list the photos paged through come from: a list of the library, or
    /// an album.
    let listKey: PhotoListKey
    /// Whether Info offers "Photos taken near here". It shows the Photos tab
    /// narrowed to the place, which only a viewer opened from that tab can
    /// do: the tab keeps its own model, so a viewer opened from a
    /// conversation does not offer it.
    let canShowNearby: Bool
    @State private var currentPhotoId: String?
    @State private var isZoomed = false
    @State private var isShowingInfo = false
    @State private var isConfirmingTrash = false
    @State private var isConfirmingDelete = false
    /// Whether a tap on the picture has hidden the bars and the strip.
    @State private var isChromeHidden = false
    /// How far the picture has been pulled down on its way to closing the viewer.
    @State private var closeDragOffsetPoints: CGFloat = 0
    /// Whether the drag in progress is the one that closes the viewer: it began
    /// going down, not sideways, and the picture was not zoomed.
    @State private var isDraggingToClose = false
    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var isReduceMotionEnabled
    @Environment(AppModel.self) private var app

    /// A drag down this far, or one this fast, closes the viewer; less lets the
    /// picture spring back.
    private static let closeDistancePoints: CGFloat = 140
    private static let closePredictedDistancePoints: CGFloat = 400

    init(model: PhotosModel, listKey: PhotoListKey, openedPhotoId: String, canShowNearby: Bool = true) {
        self.model = model
        self.listKey = listKey
        self.canShowNearby = canShowNearby
        _currentPhotoId = State(initialValue: openedPhotoId)
        #if DEBUG
            // `-TeaNodeScreen photos-viewer-described`, `photos-viewer-info`, `photos-viewer-failed` and `photos-shared-info` open Info over the viewer.
            if DemoServer.isRequested,
                DemoServer.screen == "photos-viewer-described" || DemoServer.screen == "photos-viewer-info" || DemoServer.screen == "photos-viewer-failed"
                    || DemoServer.screen == "photos-change-date" || DemoServer.screen == "photos-location" || DemoServer.screen == "photos-shared-info"
            {
                _isShowingInfo = State(initialValue: true)
            }
            // `photos-viewer-hidden` is the viewer after a tap on the picture: the bars and the strip are gone.
            if DemoServer.isRequested, DemoServer.screen == "photos-viewer-hidden" {
                _isChromeHidden = State(initialValue: true)
            }
        #endif
    }

    private var photos: [Photo] { model.photos(in: listKey) }
    private var current: Photo? { photos.first { $0.id == currentPhotoId } }

    /// Whether the bars show: not after a tap that hid them, and not while the
    /// picture is being pulled down.
    private var areBarsVisible: Bool { !isChromeHidden && closeDragOffsetPoints == 0 }

    /// "Photos taken near here", when this viewer can show the library it
    /// filters; spelled out with its type, which the compiler could not work out
    /// from a conditional inside the sheet's builder.
    private var showNearbyAction: ((PhotoNearFilter) -> Void)? {
        guard canShowNearby else { return nil }
        return { photoNearFilter in showNearby(photoNearFilter) }
    }

    var body: some View {
        NavigationStack {
            viewerContent
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { toolbarContent }
                .toolbarVisibility(areBarsVisible ? .visible : .hidden, for: .navigationBar, .bottomBar)
                // The picture is on black, as in the Photos app, so the bars are dark over it
                // whatever the appearance of the phone is.
                .toolbarColorScheme(.dark, for: .navigationBar, .bottomBar)
                .statusBarHidden(isChromeHidden)
                .sheet(isPresented: $isShowingInfo) {
                    if let current { PhotoInfoView(photo: current, model: model, listKey: listKey, onAskAgent: askAgent, onShowNearby: showNearbyAction) }
                }
                .albumTitleAlert(model: model, scope: .viewer)
        }
        // Clear, so that the black behind the picture can fade away while it is pulled
        // down and show the library under the viewer.
        .presentationBackground(.clear)
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText, buttons: Bindable(model).doneButtons)
        .onChange(of: currentPhotoId) { _, photoId in
            isZoomed = false
            guard let photoId else { return }
            Task { await model.loadMoreIfNeeded(showing: photoId, in: listKey) }
        }
        .onDisappear { model.dropPhotosNoLongerFavorite() }
    }

    /// The picture on black, with the strip of neighbors under it. Black is not
    /// one of the semantic colors: a photograph is looked at on black in both
    /// appearances, as the Photos app does.
    private var viewerContent: some View {
        VStack(spacing: 0) {
            pager
                .offset(y: closeDragOffsetPoints)
                .scaleEffect(closeDragScale)
            if !isChromeHidden {
                PhotoThumbnailStrip(photos: photos, currentPhotoId: $currentPhotoId, model: model)
                    .opacity(closeDragOffsetPoints == 0 ? 1 : 0)
                    .transition(.opacity)
            }
        }
        .background {
            Color.black
                .opacity(closeDragBackgroundOpacity)
                .ignoresSafeArea()
        }
        .simultaneousGesture(closeDrag)
        // VoiceOver closes the viewer with the escape gesture, as it does a sheet.
        .accessibilityAction(.escape) { dismiss() }
    }

    private var pager: some View {
        GeometryReader { proxy in
            ScrollView(.horizontal) {
                LazyHStack(spacing: 0) {
                    ForEach(photos) { photo in
                        PhotoPage(
                            photo: photo, model: model, isCurrent: photo.id == currentPhotoId, isZoomed: $isZoomed, isChromeHidden: isChromeHidden,
                            onSingleTap: { toggleChrome() }, onSetChromeHidden: { isHidden in setChromeHidden(isHidden) })
                            // A page is as tall as the room the pager has, not taller. A horizontal scroll view is
                            // the container only along its scrolling direction, so `containerRelativeFrame(.vertical)`
                            // measured the screen instead: each page ran about a hundred points past the pager's
                            // top and bottom edges, under the bars and the strip, where the scroll view clips it. What
                            // a page draws at its foot, such as a video's controls, was cut off there.
                            .containerRelativeFrame(.horizontal)
                            .frame(height: proxy.size.height)
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
        }
    }

    /// The animation of the bars going and coming; none when Reduce Motion is on.
    private var barsAnimation: Animation? {
        isReduceMotionEnabled ? nil : Animation.default
    }

    private var springBackAnimation: Animation? {
        isReduceMotionEnabled ? nil : Animation.spring(duration: 0.3)
    }

    private func toggleChrome() {
        withAnimation(barsAnimation) { isChromeHidden.toggle() }
    }

    /// Hides or shows the bars and the strip, for a video that hides them by
    /// itself while it plays and shows them again when it ends.
    private func setChromeHidden(_ isHidden: Bool) {
        guard isChromeHidden != isHidden else { return }
        withAnimation(barsAnimation) { isChromeHidden = isHidden }
    }

    // MARK: Drag down to close

    /// The picture follows the finger down, shrinks a little and lets the black
    /// fade. Let go past a distance, or fast, and the viewer closes; otherwise
    /// the picture springs back. It does nothing while the picture is zoomed in,
    /// where the same drag moves around the picture, and a drag that begins
    /// mostly sideways is left to the pager.
    private var closeDrag: some Gesture {
        DragGesture(minimumDistance: 16)
            .onChanged { value in
                guard !isZoomed else { return }
                if !isDraggingToClose {
                    guard value.translation.height > abs(value.translation.width) else { return }
                    isDraggingToClose = true
                }
                closeDragOffsetPoints = max(0, value.translation.height)
            }
            .onEnded { value in
                guard isDraggingToClose else { return }
                isDraggingToClose = false
                if value.translation.height > Self.closeDistancePoints || value.predictedEndTranslation.height > Self.closePredictedDistancePoints {
                    dismiss()
                } else {
                    withAnimation(springBackAnimation) { closeDragOffsetPoints = 0 }
                }
            }
    }

    private var closeDragScale: CGFloat { max(0.7, 1 - closeDragOffsetPoints / 1000) }
    private var closeDragBackgroundOpacity: Double { max(0, 1 - Double(closeDragOffsetPoints) / 400) }

    // MARK: The bars

    /// The day over the time, in a capsule at the top. It is the zone the photo
    /// was taken in that the clock is read in.
    private func dateCapsule(dayText: String, timeText: String?) -> some View {
        VStack(spacing: 0) {
            Text(dayText)
                .font(.subheadline.weight(.semibold))
            if let timeText {
                Text(timeText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 6)
        .glassEffect(.regular, in: .capsule)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button("Close", systemImage: "chevron.backward") { dismiss() }
        }
        if let current, let dayText = PhotoTimeline.viewerDayText(of: current) {
            ToolbarItem(placement: .principal) {
                dateCapsule(dayText: dayText, timeText: PhotoTimeline.viewerTimeText(of: current))
            }
            .sharedBackgroundVisibility(.hidden)
        }
        if let current, !current.isTrashed, moreMenuHasItems(for: current) {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    if current.isOwnedByCaller {
                        saveToPhoneMenuItems(for: current)
                    }
                    if let albumId = listKey.albumId, let album = model.album(in: listKey) {
                        if album.canCallerManage {
                            Button("Use as album cover", systemImage: "rectangle.stack.badge.person.crop") {
                                Task { await model.setCover(of: album, to: current) }
                            }
                        }
                        // The person's own photo comes out when they may contribute to the album,
                        // somebody else's only when they manage it.
                        if album.canCallerRemove(current) {
                            Button("Remove from album", systemImage: "rectangle.stack.badge.minus") {
                                let next = neighbor(of: current)
                                Task {
                                    if await model.remove([current], fromAlbum: albumId) { leave(movingTo: next) }
                                }
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
                        .tint(.red)
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
            } else if !current.isOwnedByCaller {
                // Somebody else's photo, seen through a shared album: share it at the left,
                // save it to the phone and see its Info in the capsule, keep it at the right.
                // Favorites and the Trash are its owner's.
                ToolbarItem(placement: .bottomBar) {
                    PhotoShareLink(photo: current, connection: model.connection)
                        .labelStyle(.iconOnly)
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItemGroup(placement: .bottomBar) {
                    Button("Info", systemImage: "info.circle") { isShowingInfo = true }
                    saveToPhoneControl(for: current)
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    if model.savingPhotoIds.contains(current.id) {
                        ProgressView()
                            .accessibilityLabel("Saving to my library")
                    } else {
                        Button("Save to my library", systemImage: "photo.badge.plus") {
                            Task { await model.saveToLibrary([current]) }
                        }
                    }
                }
            } else {
                // The person's own photo, as the Photos app lays it out: Share at the left, one
                // capsule with Favorite, Info and Add to album, the Trash at the right. Saving to
                // the phone, the album cover and taking it out of an album are under More.
                ToolbarItem(placement: .bottomBar) {
                    PhotoShareLink(photo: current, connection: model.connection)
                        .labelStyle(.iconOnly)
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItemGroup(placement: .bottomBar) {
                    Button(
                        current.isFavorite ? LocalizedStringKey("Remove from Favorites") : LocalizedStringKey("Add to Favorites"),
                        systemImage: current.isFavorite ? "heart.fill" : "heart"
                    ) {
                        Task { await model.toggleFavorite(current, in: listKey) }
                    }
                    .sensoryFeedback(.selection, trigger: current.isFavorite)
                    Button("Info", systemImage: "info.circle") { isShowingInfo = true }
                    AddToAlbumMenu(model: model, photos: [current], scope: .viewer, hiddenAlbumId: listKey.albumId)
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) {
                    Button("Move to Trash", systemImage: "trash", role: .destructive) { isConfirmingTrash = true }
                        .tint(.red)
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

    /// Whether the More menu has anything for the photo: the person's own photo
    /// can be saved to the phone, and an album has its cover and taking the
    /// photo out of it.
    private func moreMenuHasItems(for photo: Photo) -> Bool {
        if photo.isOwnedByCaller { return true }
        guard let album = model.album(in: listKey) else { return false }
        return album.canCallerManage || album.canCallerRemove(photo)
    }

    /// What of the photo can be saved to the phone: the original, the edited
    /// version, a Live Photo's video together with its picture.
    @ViewBuilder
    private func saveChoices(for photo: Photo) -> some View {
        Button("Save original", systemImage: "photo") { Task { await model.saveToPhotos(photo) } }
        if photo.hasEditedFile {
            Button("Save edited version", systemImage: "slider.horizontal.3") { Task { await model.saveToPhotos(photo, saving: .edited) } }
        }
        if photo.hasLiveVideoFile {
            Button("Save as Live Photo", systemImage: "livephoto") { Task { await model.saveToPhotos(photo, saving: .livePhoto) } }
        }
    }

    /// Adds the photo to the phone's own Photos app, for the More menu: one
    /// item, or a submenu when the photo has an edited version or a Live Photo's
    /// video to choose from.
    @ViewBuilder
    private func saveToPhoneMenuItems(for photo: Photo) -> some View {
        if model.savingPhotoIds.contains(photo.id) {
            Button("Saving to the Photos app", systemImage: "square.and.arrow.down") {}
                .disabled(true)
        } else if photo.hasEditedFile || photo.hasLiveVideoFile {
            Menu {
                saveChoices(for: photo)
            } label: {
                Label("Save to the Photos app", systemImage: "square.and.arrow.down")
            }
        } else {
            Button("Save to the Photos app", systemImage: "square.and.arrow.down") {
                Task { await model.saveToPhotos(photo) }
            }
        }
    }

    /// Adds the photo to the phone's own Photos app: one button, or a menu when
    /// the photo has an edited version or a Live Photo's video to choose from.
    @ViewBuilder
    private func saveToPhoneControl(for photo: Photo) -> some View {
        if model.savingPhotoIds.contains(photo.id) {
            ProgressView()
                .accessibilityLabel("Saving to the Photos app")
        } else if photo.hasEditedFile || photo.hasLiveVideoFile {
            Menu {
                saveChoices(for: photo)
            } label: {
                Label("Save to the Photos app", systemImage: "square.and.arrow.down")
            }
        } else {
            Button("Save to the Photos app", systemImage: "square.and.arrow.down") {
                Task { await model.saveToPhotos(photo) }
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

    /// Shows the library narrowed to the photos taken near the place of the
    /// photo on screen: the viewer and Info close, and the library under them
    /// shows the filter.
    private func showNearby(_ photoNearFilter: PhotoNearFilter) {
        isShowingInfo = false
        model.showPhotos(near: photoNearFilter)
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

/// PhotoThumbnailStrip is the row of small squares under the picture: the
/// photos around the one on screen, which is a little larger. A tap shows that
/// photo, and the strip keeps the one on screen in the middle as the pager
/// moves.
struct PhotoThumbnailStrip: View {
    let photos: [Photo]
    @Binding var currentPhotoId: String?
    let model: PhotosModel
    @Environment(\.accessibilityReduceMotion) private var isReduceMotionEnabled

    private static let thumbnailSidePoints: CGFloat = 44
    private static let currentThumbnailSidePoints: CGFloat = 56

    private var scrollAnimation: Animation? {
        isReduceMotionEnabled ? nil : Animation.default
    }

    private var growAnimation: Animation? {
        isReduceMotionEnabled ? nil : Animation.snappy
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                LazyHStack(spacing: 2) {
                    ForEach(photos) { photo in
                        thumbnailButton(for: photo)
                            .id(photo.id)
                    }
                }
                .padding(.horizontal)
            }
            .scrollIndicators(.hidden)
            .frame(height: Self.currentThumbnailSidePoints + 8)
            .onAppear {
                guard let currentPhotoId else { return }
                proxy.scrollTo(currentPhotoId, anchor: .center)
            }
            .onChange(of: currentPhotoId) { _, photoId in
                guard let photoId else { return }
                withAnimation(scrollAnimation) { proxy.scrollTo(photoId, anchor: .center) }
            }
        }
    }

    private func thumbnailButton(for photo: Photo) -> some View {
        let isCurrent = photo.id == currentPhotoId
        let side = isCurrent ? Self.currentThumbnailSidePoints : Self.thumbnailSidePoints
        return Button {
            currentPhotoId = photo.id
        } label: {
            PhotoThumbnailImage(photo: photo, model: model)
                .frame(width: side, height: side)
                .clipShape(.rect(cornerRadius: 3))
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .animation(growAnimation, value: isCurrent)
        .accessibilityLabel(accessibilityText(of: photo))
        .accessibilityAddTraits(isCurrent ? .isSelected : [])
    }

    private func accessibilityText(of photo: Photo) -> String {
        let kind = PhotoWords.kind(of: photo)
        guard let taken = PhotoTimeline.takenText(of: photo) else { return kind }
        return "\(kind), \(taken)"
    }
}

/// PhotoPositionMap is a small map with one pin where a photo was taken. It
/// does not take the scroll of the details it is in; pressing it opens the
/// position in Apple Maps.
struct PhotoPositionMap: View {
    let coordinate: CLLocationCoordinate2D
    let title: String

    private var region: MKCoordinateRegion {
        MKCoordinateRegion(center: coordinate, latitudinalMeters: 1200, longitudinalMeters: 1200)
    }

    var body: some View {
        Button {
            let item = MKMapItem(location: CLLocation(latitude: coordinate.latitude, longitude: coordinate.longitude), address: nil)
            item.name = title.isEmpty ? nil : title
            item.openInMaps()
        } label: {
            Map(initialPosition: .region(region), interactionModes: []) {
                Marker(title, coordinate: coordinate)
            }
            .allowsHitTesting(false)
            .frame(height: 180)
            .clipShape(.rect(cornerRadius: 12))
        }
        .buttonStyle(.plain)
        .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
        .accessibilityLabel(String(localized: "Map of where the photo was taken"))
        .accessibilityHint(String(localized: "Opens in Maps"))
    }
}

/// PhotoThumbnailImage is the preview of a photo, filling the space it is
/// given: the small one for a thumbnail in the viewer's strip, and with
/// `isLarge` the large one for a picture of a month or a year, which shows the
/// small one until the large one has been read.
struct PhotoThumbnailImage: View {
    let photo: Photo
    let model: PhotosModel
    var isLarge = false
    @State private var picture: UIImage?

    init(photo: Photo, model: PhotosModel, isLarge: Bool = false) {
        self.photo = photo
        self.model = model
        self.isLarge = isLarge
        _picture = State(initialValue: photo.gridPreviewPath.flatMap { PhotoImages.cachedPicture(path: $0, accountKey: model.connection.accountKey) })
    }

    /// The addresses the picture is read from, the small one and then the large
    /// one: what the loading starts again for when a photo is given other previews.
    private var previewPaths: [String?] { [photo.gridPreviewPath, isLarge ? photo.viewerPreviewPath : nil] }

    var body: some View {
        Color(.tertiarySystemFill)
            .overlay {
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFill()
                }
            }
            .clipped()
            .task(id: previewPaths) {
                let paths = previewPaths
                guard let smallPath = paths[0] else {
                    picture = nil
                    return
                }
                if picture == nil, let small = await PhotoImages.picture(path: smallPath, connection: model.connection) {
                    picture = small
                }
                if let largePath = paths[1], let large = await PhotoImages.picture(path: largePath, connection: model.connection) {
                    picture = large
                }
            }
    }
}

/// PhotoPage is one photo in the viewer.
struct PhotoPage: View {
    let photo: Photo
    let model: PhotosModel
    let isCurrent: Bool
    @Binding var isZoomed: Bool
    /// Whether the bars around the picture are hidden. A video's controls go
    /// and come with them.
    var isChromeHidden = false
    /// What a tap on the picture does, such as hiding the bars around it.
    var onSingleTap: (() -> Void)?
    /// Hides or shows the bars around the picture, which a video does by itself:
    /// it hides them while it plays untouched, and shows them when it ends.
    var onSetChromeHidden: ((Bool) -> Void)?
    @State private var picture: UIImage?
    /// The player of a Live Photo's video. A video has its own, in `playback`.
    @State private var player: AVPlayer?
    @State private var playback: VideoPlayback?
    /// How many times the controls have been used. A new count restarts the
    /// wait before they hide.
    @State private var controlUseCount = 0
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
                // A page that is not the one on screen goes back to its whole size.
                ZoomablePicture(picture: picture, isZoomed: $isZoomed, isCurrent: isCurrent, onSingleTap: onSingleTap)
                    .id(ObjectIdentifier(picture))
                    .accessibilityLabel(accessibilityText)
                    .accessibilityAddTraits(.isImage)
            } else if photo.isFailed {
                ContentUnavailableView {
                    Label("Cannot show this photo", systemImage: "exclamationmark.circle")
                } description: {
                    Text(PhotoWords.failure(of: photo))
                } actions: {
                    if photo.isOwnedByCaller {
                        Button("Try again") { Task { await model.retryProcessing([photo]) } }
                    }
                }
            } else {
                // A photo still being made ready says so, rather than spinning unexplained.
                VStack(spacing: 12) {
                    ProgressView()
                    if photo.isWaiting {
                        Text("Getting this photo ready")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
        .task(id: photo.viewerPreviewPath) {
            await loadPicture()
        }
        .task(id: isCurrent) {
            await loadVideo()
        }
        .task(id: hideTimerKey) {
            await hideControlsWhenLeftAlone()
        }
        .onDisappear { stopVideo() }
        .overlay(alignment: .topLeading) {
            // Only a Live Photo whose video the reader can have: a photo shared without
            // places comes with its picture only.
            if photo.isLivePhoto, photo.hasLiveVideoFile, isCurrent, !isZoomed {
                liveButton
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .AVPlayerItemDidPlayToEndTime)) { notification in
            let endedItem = notification.object as? AVPlayerItem
            if isPlayingLiveVideo, endedItem === player?.currentItem {
                stopLiveVideo()
            } else if let playback, endedItem === playback.player.currentItem {
                // The video stays on its last frame with the controls and the bars shown.
                playback.didReachEnd()
                onSetChromeHidden?(false)
            }
        }
        .accessibilityElement(children: .contain)
    }

    private var accessibilityText: String {
        let kind = PhotoWords.kind(of: photo)
        guard let taken = PhotoTimeline.takenText(of: photo) else { return kind }
        return "\(kind), \(taken)"
    }

    @ViewBuilder
    private var videoBody: some View {
        if let playback {
            ZStack {
                // The poster shows until the first frame is ready, so there is no flash of black.
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFit()
                }
                VideoLayerView(player: playback.player)
            }
            // The whole page answers a tap, the black beside the video too.
            .contentShape(.rect)
            .onTapGesture { onSingleTap?() }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(accessibilityText)
            .accessibilityAddTraits(.isImage)
            .overlay(alignment: .bottom) {
                VideoControlsBar(playback: playback, onUse: { controlUseCount += 1 })
                    .padding(.horizontal, 16)
                    .padding(.bottom, 8)
                    .opacity(isChromeHidden ? 0 : 1)
                    .allowsHitTesting(!isChromeHidden)
                    .accessibilityHidden(isChromeHidden)
            }
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
                        Text(PhotoWords.failure(of: photo))
                            .font(.callout)
                            .foregroundStyle(.secondary)
                        if photo.isOwnedByCaller {
                            Button("Try again") { Task { await model.retryProcessing([photo]) } }
                                .buttonStyle(.bordered)
                        }
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
            stopVideo()
            return
        }
        guard photo.isVideo, photo.videoPath != nil else { return }
        do {
            let address = try await model.connection.photoPlaybackAddress(photo.id, source: .preview("video"))
            guard !Task.isCancelled else { return }
            videoProblemText = nil
            stopVideo()
            // The video starts as soon as its page is the one on screen, as in the Photos app.
            let startedPlayback = VideoPlayback(url: address, fallbackDurationSeconds: Double(photo.durationMS) / 1000)
            playback = startedPlayback
            startedPlayback.play()
            #if DEBUG
                // `-TeaNodeScreen photos-video-viewer` is a video stopped a third of the way in, so that the
                // controls, which hide three seconds into playing, are on screen for the screenshot.
                if DemoServer.isRequested, DemoServer.screen == "photos-video-viewer" {
                    startedPlayback.pause(atSeconds: 2)
                }
            #endif
        } catch {
            if !Task.isCancelled { videoProblemText = model.describe(error) }
        }
    }

    /// Pauses the video and lets go of its player, when its page stops being
    /// the one on screen.
    private func stopVideo() {
        playback?.stop()
        playback = nil
    }

    // MARK: The controls hiding by themselves

    /// What the wait before the controls hide starts again for: the bars
    /// coming back, the video starting, pausing or ending, a finger on the
    /// progress bar, and any use of the controls.
    private struct HideTimerKey: Hashable {
        let isCurrent: Bool
        let isChromeHidden: Bool
        let isPlaying: Bool
        let hasEnded: Bool
        let isScrubbing: Bool
        let controlUseCount: Int
    }

    private var hideTimerKey: HideTimerKey {
        HideTimerKey(
            isCurrent: isCurrent, isChromeHidden: isChromeHidden, isPlaying: playback?.isPlaying ?? false, hasEnded: playback?.hasEnded ?? false,
            isScrubbing: playback?.isScrubbing ?? false, controlUseCount: controlUseCount)
    }

    /// Hides the controls, the bars and the strip together once a playing video
    /// has been left alone for a few seconds. The wait ends, and starts again,
    /// whenever the key changes; leaving the page ends it.
    private func hideControlsWhenLeftAlone() async {
        guard isCurrent, !isChromeHidden, let playback else { return }
        guard
            VideoControls.shouldHide(
                isPlaying: playback.isPlaying, hasEnded: playback.hasEnded, isScrubbing: playback.isScrubbing,
                isVoiceOverRunning: UIAccessibility.isVoiceOverRunning)
        else { return }
        try? await Task.sleep(for: .seconds(VideoControls.hideDelaySeconds))
        guard !Task.isCancelled else { return }
        onSetChromeHidden?(true)
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
    /// The list the viewer pages through, which a changed date reads again.
    let listKey: PhotoListKey
    let onAskAgent: (Photo) -> Void
    /// Shows the library narrowed to the photos taken near this photo's place;
    /// nil where the viewer cannot, and then the action is not offered.
    let onShowNearby: ((PhotoNearFilter) -> Void)?
    @State private var isChangingDate = false
    @State private var isChangingLocation = false
    /// The unnamed person being named from here, over the photo.
    @State private var namingGroup: PhotoFaceGroup?
    @State private var isExplainingContacts = false
    @State private var detail: PhotoDetail?
    /// Where the photo was taken by the person's own places and the street,
    /// read again with the details; nil when it has no position or none is known.
    @State private var positionPlace: PhotoPositionPlace?
    /// Why the photo's details could not be read; nil when they could, or have
    /// not been asked for yet.
    @State private var detailFailureText: String?
    @State private var isAskingToDescribe = false
    /// Whether the description has been waited for long enough to say it is taking
    /// longer than usual. It is still waited for.
    @State private var isDescriptionSlow = false
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var app

    private var connection: Connection { model.connection }
    /// The photo as the details last read it, which is newer than the one the
    /// viewer opened this with once it was asked to be made ready again.
    private var shownPhoto: Photo { detail?.photo ?? photo }

    /// Where the photo was taken, as numbers, for a position no city is near.
    private var coordinatesText: String? {
        guard let latitude = shownPhoto.latitude, let longitude = shownPhoto.longitude else { return nil }
        return String(format: "%.4f, %.4f", latitude, longitude)
    }
    /// The name of the place the photo was taken at, when the server knows a
    /// better one than the city: the person's own place, else the street.
    private var positionName: String? {
        if let locationPlace = positionPlace?.locationPlace { return locationPlace.shownName }
        guard let addressText = positionPlace?.addressText, !addressText.isEmpty else { return nil }
        return addressText
    }
    private var description: PhotoDescription { detail?.photoDescription ?? PhotoDescription() }
    private var isDescriptionAvailable: Bool { model.library?.isDescriptionAvailable == true }
    private var people: [PhotoFace] { (detail?.faces ?? []).filter(\.isGrouped) }
    /// The names of the people in somebody else's photo, each once: all that
    /// is theirs to see, since the people are the owner's to open and correct.
    private var namesOfPeopleInSharedPhoto: [String] {
        var names: [String] = []
        for face in detail?.faces ?? [] {
            let name = face.contactName.trimmingCharacters(in: .whitespacesAndNewlines)
            if !name.isEmpty, !names.contains(name) { names.append(name) }
        }
        return names
    }

    var body: some View {
        NavigationStack {
            List {
                if shownPhoto.isFailed {
                    failureSection
                }
                if let detailFailureText {
                    detailFailureSection(detailFailureText)
                }
                if shownPhoto.isWaiting {
                    Label("Getting this photo ready", systemImage: "hourglass")
                        .foregroundStyle(.secondary)
                        .fullWidthRowSeparator()
                }
                if !shownPhoto.isOwnedByCaller, !shownPhoto.ownerName.isEmpty {
                    LabeledContent("Shared by") { Text(shownPhoto.ownerName) }
                }
                LabeledContent("Date") {
                    Text(PhotoTimeline.takenText(of: shownPhoto) ?? String(localized: "Unknown"))
                }
                if shownPhoto.isOwnedByCaller, !shownPhoto.isTrashed {
                    Button("Change date", systemImage: "calendar") { isChangingDate = true }
                        .actionColor()
                        .fullWidthRowSeparator()
                }
                // The person's own place, else the street, else the city near it.
                if let positionName {
                    LabeledContent("Place") { Text(verbatim: positionName) }
                } else if !shownPhoto.placeName.isEmpty {
                    LabeledContent("Place") { Text(shownPhoto.placeName) }
                } else if let coordinatesText {
                    // A position with no city near it is shown as it is.
                    LabeledContent("Place") { Text(verbatim: coordinatesText) }
                }
                if let latitude = shownPhoto.latitude, let longitude = shownPhoto.longitude {
                    PhotoPositionMap(
                        coordinate: CLLocationCoordinate2D(latitude: latitude, longitude: longitude), title: positionName ?? shownPhoto.placeName)
                }
                if shownPhoto.isOwnedByCaller, !shownPhoto.isTrashed {
                    Button("Change location", systemImage: "mappin") { isChangingLocation = true }
                        .actionColor()
                        .fullWidthRowSeparator()
                }
                // A photo shared without places has no coordinates, so it has nothing to be near.
                if let onShowNearby, !shownPhoto.isTrashed, let photoNearFilter = PhotoNearFilter(near: shownPhoto) {
                    Button("Photos taken near here", systemImage: "mappin.and.ellipse") { onShowNearby(photoNearFilter) }
                        .actionColor()
                        .fullWidthRowSeparator()
                }
                if !cameraText.isEmpty {
                    LabeledContent("Camera") { Text(cameraText) }
                }
                LabeledContent("Size") {
                    Text(sizeText)
                }
                if !filename.isEmpty {
                    LabeledContent("File name") { Text(filename).monospaced() }
                }
                if let albums = detail?.albums, !albums.isEmpty {
                    Section("Albums") {
                        ForEach(albums) { album in
                            NavigationLink {
                                PhotoAlbumScreen(model: model, albumId: album.id)
                            } label: {
                                Label(album.albumTitle, systemImage: "rectangle.stack")
                                    .foregroundStyle(.primary)
                            }
                        }
                    }
                }
                if shownPhoto.isOwnedByCaller, !people.isEmpty {
                    peopleSection
                } else if !shownPhoto.isOwnedByCaller, !namesOfPeopleInSharedPhoto.isEmpty {
                    sharedPeopleSection
                }
                if shownPhoto.isOwnedByCaller, !shownPhoto.isFailed, !shownPhoto.isWaiting, !shownPhoto.isTrashed {
                    makeThumbnailsSection
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
            .sheet(isPresented: $isChangingDate) {
                PhotoDateChangeSheet(model: model, photo: shownPhoto, listKey: listKey)
            }
            .sheet(isPresented: $isChangingLocation) {
                PhotoLocationSheet(model: model, photos: [shownPhoto])
            }
            .onChange(of: isChangingDate) { _, isStillChanging in
                if !isStillChanging { Task { await loadDetail() } }
            }
            .onChange(of: isChangingLocation) { _, isStillChanging in
                if !isStillChanging { Task { await loadDetail() } }
            }
            #if DEBUG
                // `-TeaNodeScreen photos-change-date` opens the date over Info.
                .task {
                    guard DemoServer.isRequested, DemoServer.screen == "photos-change-date" else { return }
                    try? await Task.sleep(for: .seconds(1))
                    isChangingDate = true
                }
                // `-TeaNodeScreen photos-location` opens the places over Info.
                .task {
                    guard DemoServer.isRequested, DemoServer.screen == "photos-location" else { return }
                    try? await Task.sleep(for: .seconds(1))
                    isChangingLocation = true
                }
            #endif
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
            return
        }
        await loadPositionPlace()
    }

    /// Reads where the photo was taken. A photo moved or without a position
    /// has none; a failure leaves the city and the numbers to say it.
    private func loadPositionPlace() async {
        guard shownPhoto.latitude != nil, shownPhoto.longitude != nil else {
            positionPlace = nil
            return
        }
        positionPlace = try? await connection.readPhotoPlace(photo.id)
    }

    // MARK: When the details cannot be read, or the photo failed

    private func detailFailureSection(_ failureText: String) -> some View {
        Section {
            // A problem is orange with its symbol, never the green of the accent.
            Label {
                Text("The details of this photo could not be read.")
            } icon: {
                Image(systemName: "exclamationmark.triangle").foregroundStyle(.orange)
            }
            .fullWidthRowSeparator()
            Text(failureText)
                .font(.footnote)
                .foregroundStyle(.secondary)
            Button("Try again", systemImage: "arrow.clockwise") { Task { await loadDetail() } }
                .actionColor()
        }
    }

    /// Why the picture could not be made, in the server's plain words, and a
    /// way to ask for it to be made again.
    private var failureSection: some View {
        Section {
            // The picture was not made: a failure, so red with its symbol.
            Label {
                Text(PhotoWords.failure(of: shownPhoto))
            } icon: {
                Image(systemName: "exclamationmark.circle").foregroundStyle(.red)
            }
            if shownPhoto.isOwnedByCaller {
                Button("Try again", systemImage: "arrow.clockwise") {
                    Task {
                        if await model.retryProcessing([shownPhoto]) { await loadDetail() }
                    }
                }
                .actionColor()
            }
        }
    }

    /// For a photo that is ready: have its thumbnails made again, as the
    /// dashboard offers, for a picture that came out wrong.
    private var makeThumbnailsSection: some View {
        Section {
            Button("Make thumbnails again", systemImage: "arrow.clockwise") {
                Task {
                    if await model.retryProcessing([shownPhoto]) { await loadDetail() }
                }
            }
            .actionColor()
        }
    }

    // MARK: People

    /// The people in the photo, each with their face, opening that person. A
    /// person who is not who the server says is taken out of the group with a
    /// touch and hold.
    private var peopleSection: some View {
        Section {
            ForEach(people) { face in
                personRow(face)
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
                    .tint(.red)
                }
            }
        } header: {
            Text("photos.people")
        } footer: {
            Text("Touch and hold a person if it is not them.")
        }
        .sheet(item: $namingGroup) { group in
            NamePersonSheet(model: model, group: group) { _ in
                Task { await loadDetail() }
            }
        }
        .alert("Name this person", isPresented: $isExplainingContacts) {
            Button("OK", role: .cancel) {}
        } message: {
            Text("Naming people means choosing a contact from your address book, and Contacts is not turned on for you. Ask whoever runs this server to turn on Contacts.")
        }
    }

    /// A named person opens their photos. An unnamed one is named here, over
    /// the photo: their page is not in People yet, and leaving the photo to
    /// name them loses it.
    @ViewBuilder
    private func personRow(_ face: PhotoFace) -> some View {
        if face.contactName.isEmpty {
            Button {
                if app.isSectionAvailable(.contacts) {
                    namingGroup = PhotoFaceGroup(id: face.photoFaceGroupId, facePictureUrl: face.facePictureUrl)
                } else {
                    isExplainingContacts = true
                }
            } label: {
                personLabel(face)
            }
            .accessibilityHint(Text("Name this person"))
        } else {
            NavigationLink {
                PhotoPersonScreen(model: model, photoFaceGroupId: face.photoFaceGroupId)
            } label: {
                personLabel(face)
            }
        }
    }

    private func personLabel(_ face: PhotoFace) -> some View {
        HStack(spacing: 12) {
            FacePictureView(facePictureUrl: face.facePictureUrl, name: shownName(of: face), connection: connection)
                .frame(width: 40, height: 40)
            PhotoPersonNameText(name: shownName(of: face), isOwnContact: face.isOwnContact)
                .foregroundStyle(face.contactName.isEmpty ? .secondary : .primary)
        }
        .accessibilityElement(children: .combine)
    }

    /// The people in somebody else's photo: their names as plain text. The
    /// person cannot open or correct them, so nothing here is pressed.
    private var sharedPeopleSection: some View {
        Section {
            ForEach(namesOfPeopleInSharedPhoto, id: \.self) { name in
                Text(name)
            }
        } header: {
            Text("photos.people")
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
            } else if shownPhoto.isOwnedByCaller {
                // Describing is the owner's: the server refuses it for a photo shared with the person.
                if description.isDescribed {
                    if isDescriptionAvailable {
                        Button("Describe again", systemImage: "arrow.clockwise") { Task { await describe(isRedescribed: true) } }
                            .actionColor()
                            .fullWidthRowSeparator()
                    }
                } else if isDescriptionAvailable {
                    Button(shownPhoto.isVideo ? LocalizedStringKey("Describe this video") : "Describe this photo", systemImage: "text.viewfinder") {
                        Task { await describe(isRedescribed: false) }
                    }
                    .actionColor()
                    .fullWidthRowSeparator()
                } else if let library = model.library {
                    Text(library.descriptionUnavailableMessage.isEmpty ? String(localized: "Describing photos is not available.") : library.descriptionUnavailableMessage.asReasonSentence)
                        .foregroundStyle(.secondary)
                }
            }
            Button(askAgentText, systemImage: "bubble.left") { onAskAgent(photo) }
                .actionColor()
        } header: {
            Text("Description")
        } footer: {
            if shownPhoto.isOwnedByCaller, !description.isDescribed, !description.isDescriptionRequested, isDescriptionAvailable {
                Text(describeHintText)
            }
        }
    }

    // The assistant is named as the conversation names it, and a video is a
    // video here, not a photo: what the assistant looks at for one is its
    // poster picture. These are the dashboard's words.
    private var askAgentText: String {
        let agentName = app.agentName
        if shownPhoto.isVideo {
            return agentName.isEmpty ? String(localized: "Ask the assistant about this video") : String(localized: "Ask \(agentName) about this video")
        }
        return agentName.isEmpty ? String(localized: "Ask the assistant about this photo") : String(localized: "Ask \(agentName) about this photo")
    }

    private var describeHintText: String {
        let agentName = app.agentName
        if shownPhoto.isVideo {
            return agentName.isEmpty
                ? String(localized: "The assistant looks at the video's poster and writes what it shows, so a search can find it.")
                : String(localized: "\(agentName) looks at the video's poster and writes what it shows, so a search can find it.")
        }
        return agentName.isEmpty
            ? String(localized: "The assistant looks at the picture and writes what it shows, so a search can find it.")
            : String(localized: "\(agentName) looks at the picture and writes what it shows, so a search can find it.")
    }

    private var waitingText: String {
        if isDescriptionSlow { return String(localized: "This is taking longer than usual. It keeps going in the background, so look again in a few minutes.") }
        return description.isDescribed ? String(localized: "Describing again… this can take a minute.") : String(localized: "Describing… this can take a minute.")
    }

    private func describe(isRedescribed: Bool) async {
        isAskingToDescribe = true
        isDescriptionSlow = false
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
    /// seconds, as the dashboard does: every 3 seconds, then every 15 once two
    /// minutes have passed, when it also says the wait is longer than usual. It
    /// does not give up while the sheet is open, so the description is there
    /// when it arrives.
    private func followDescription() async {
        var waitedSeconds = 0
        while !Task.isCancelled, description.isDescriptionRequested {
            isDescriptionSlow = PhotoDescriptionPolling.isSlow(afterWaiting: waitedSeconds)
            let pauseSeconds = PhotoDescriptionPolling.secondsBeforeAsking(afterWaiting: waitedSeconds)
            try? await Task.sleep(for: .seconds(pauseSeconds))
            waitedSeconds += pauseSeconds
            guard !Task.isCancelled else { return }
            await loadDetail()
        }
        isDescriptionSlow = false
    }

    // MARK: Details

    private var cameraText: String {
        [shownPhoto.cameraMake, shownPhoto.cameraModel].filter { !$0.isEmpty }.joined(separator: " ")
    }

    private var filename: String {
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
                    ShareLink(item: PhotoShareFile(connection: connection, photo: photo, role: "edited"), preview: SharePreview(photo.filename)) {
                        Label("Share edited version", systemImage: "slider.horizontal.3")
                    }
                }
                if photo.hasLiveVideoFile {
                    ShareLink(item: PhotoShareFile(connection: connection, photo: photo, role: "liveVideo"), preview: SharePreview(photo.filename)) {
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
            ShareLink(item: PhotoShareMovie(connection: connection, photo: photo), preview: SharePreview(photo.filename)) {
                Label(label, systemImage: "square.and.arrow.up")
            }
        } else {
            ShareLink(item: PhotoShareImage(connection: connection, photo: photo), preview: SharePreview(photo.filename)) {
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
            preview: { SharePreview($0.photo.filename) }
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
            return PhotoShareFile.hand(over: download)
        }
    }

    /// The file for the share sheet, which copies it. The folder it is in is
    /// removed after a while, once the sheet has had its time.
    static func hand(over download: PhotoDownload) -> SentTransferredFile {
        PhotoTemporaryFiles.removeLater(download)
        return SentTransferredFile(download.fileURL, allowAccessingOriginalFile: false)
    }
}

/// PhotoShareImage is a picture handed to the share sheet as a file.
struct PhotoShareImage: Transferable, Sendable {
    let connection: Connection
    let photo: Photo

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(exportedContentType: .image) { item in
            let download = try await item.connection.downloadPhotoFile(item.photo)
            return PhotoShareFile.hand(over: download)
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
            return PhotoShareFile.hand(over: download)
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

    /// Adds a picture with its video as one Live Photo.
    static func saveLivePhoto(pictureURL: URL, videoURL: URL) async throws {
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            throw TeaNodeError.refused(String(localized: "TeaNode is not allowed to add to your Photos. Allow it in Settings, under Photos."))
        }
        try await PHPhotoLibrary.shared().performChanges {
            let request = PHAssetCreationRequest.forAsset()
            request.addResource(with: .photo, fileURL: pictureURL, options: nil)
            request.addResource(with: .pairedVideo, fileURL: videoURL, options: nil)
        }
    }
}
