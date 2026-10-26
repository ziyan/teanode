import SwiftUI
import TeaNodeKit
import UIKit

// MARK: Asking for a title

/// AlbumTitleRequest is a question for an album's title: for a new album, one
/// made from a suggestion or from photos, and a rename. An alert on the screen
/// that raised it asks, and the model carries out the answer.
struct AlbumTitleRequest: Identifiable {
    enum Purpose {
        case new
        /// An empty album that belongs to a circle: every member adds to it.
        case newForCircle(PeopleCircle)
        case newWith([Photo])
        case suggestion(PhotoAlbumSuggestion)
        case rename(PhotoAlbum)
    }

    /// Which screen asks. A viewer opened over the library covers it, so an
    /// alert raised from the viewer is shown by the viewer only.
    enum Scope {
        case library, album, viewer
    }

    let id = UUID()
    let purpose: Purpose
    let scope: Scope

    var alertTitle: String {
        switch purpose {
        case .new, .newWith: String(localized: "New album")
        case .newForCircle(let circle): String(localized: "New album for \(circle.name)")
        case .suggestion: String(localized: "Create album")
        case .rename: String(localized: "Rename album")
        }
    }

    var buttonTitle: String {
        switch purpose {
        case .new, .newForCircle, .newWith, .suggestion: String(localized: "Create")
        case .rename: String(localized: "Rename")
        }
    }

    /// What the field starts with: the suggestion's proposed title, or the
    /// album's own.
    var initialTitle: String {
        switch purpose {
        case .new, .newForCircle, .newWith: ""
        case .suggestion(let suggestion): suggestion.albumTitle
        case .rename(let album): album.albumTitle
        }
    }
}

/// AlbumTitleAlert asks the model's pending title request, when it is the
/// screen's own, with a text field in an alert.
private struct AlbumTitleAlert: ViewModifier {
    let model: PhotosModel
    let scope: AlbumTitleRequest.Scope
    let isActive: Bool
    @State private var titleText = ""

    private var isPresented: Binding<Bool> {
        Binding(
            get: { isActive && model.albumTitleRequest?.scope == scope },
            set: { if !$0, model.albumTitleRequest?.scope == scope { model.albumTitleRequest = nil } }
        )
    }

    func body(content: Content) -> some View {
        content
            .alert(model.albumTitleRequest?.alertTitle ?? "", isPresented: isPresented) {
                TextField("Title", text: $titleText)
                Button(model.albumTitleRequest?.buttonTitle ?? String(localized: "Create")) {
                    guard let request = model.albumTitleRequest else { return }
                    let title = PhotoAlbumText.trimmedTitle(titleText)
                    model.albumTitleRequest = nil
                    guard !title.isEmpty else { return }
                    Task { await model.submit(request, title: title) }
                }
                .disabled(PhotoAlbumText.trimmedTitle(titleText).isEmpty)
                Button("Cancel", role: .cancel) { model.albumTitleRequest = nil }
            }
            .onChange(of: model.albumTitleRequest?.id) { _, _ in
                titleText = model.albumTitleRequest?.initialTitle ?? ""
            }
    }
}

extension View {
    /// Shows the alert that asks for an album's title when the screen is the
    /// one that asked.
    func albumTitleAlert(model: PhotosModel, scope: AlbumTitleRequest.Scope, isActive: Bool = true) -> some View {
        modifier(AlbumTitleAlert(model: model, scope: scope, isActive: isActive))
    }
}

// MARK: Adding to an album

/// AddToAlbumMenu is the menu that puts photos in an album: New Album first,
/// then the person's albums, the one changed most recently first. The album the
/// photos are being looked at in is left out, since they are in it.
struct AddToAlbumMenu: View {
    let model: PhotosModel
    let photos: [Photo]
    let scope: AlbumTitleRequest.Scope
    var hiddenAlbumId: String?
    var onAdded: () -> Void = {}

    var body: some View {
        Menu {
            Button("New album…", systemImage: "plus") {
                model.requestAlbumTitle(.newWith(photos), scope: scope)
                onAdded()
            }
            let choices = (model.albums ?? []).filter { $0.id != hiddenAlbumId && $0.canCallerContribute }
            if !choices.isEmpty {
                Divider()
                ForEach(choices) { album in
                    // Two albums of the same name are told apart by whose they are.
                    Button(album.isCircleAlbum ? "\(album.albumTitle) (\(album.circleName))" : album.albumTitle) {
                        let chosen = photos
                        Task { await model.add(chosen, to: album) }
                        onAdded()
                    }
                }
            }
        } label: {
            Label("Add to album", systemImage: "rectangle.stack.badge.plus")
        }
        .task { await model.loadAlbums() }
    }
}

/// PhotoSelectionToolbar is the bar under a list in Select mode: what can be
/// done to the photos chosen. Inside an album it also takes them out of it.
struct PhotoSelectionToolbar: ToolbarContent {
    let model: PhotosModel
    let selectedPhotos: [Photo]
    let listKey: PhotoListKey
    let scope: AlbumTitleRequest.Scope
    /// The album whose photos are listed, if the list is one; read by the
    /// screen that shows it, since the albums are the model's to say.
    var album: PhotoAlbum?
    @Binding var isConfirmingTrash: Bool
    let onEnd: () -> Void

    private var isEmpty: Bool { selectedPhotos.isEmpty }
    private var isEveryPhotoFavorite: Bool { !selectedPhotos.isEmpty && selectedPhotos.allSatisfy(\.isFavorite) }
    /// Whether the photos chosen are all the person's own. Somebody else's
    /// photo, seen through a shared album, can be kept or shared, not changed.
    private var isEveryPhotoOwn: Bool { selectedPhotos.allSatisfy(\.isOwnedByCaller) }
    /// Whether the album lets the person take any photo out of it: their own
    /// when they may contribute, others' when they manage it.
    private var isRemovalOffered: Bool { album.map { $0.canCallerContribute || $0.canCallerManage } ?? false }
    /// Whether every photo chosen is one the person may take out of the album.
    private var isEveryPhotoRemovable: Bool {
        guard let album, !selectedPhotos.isEmpty else { return false }
        return selectedPhotos.allSatisfy { album.canCallerRemove($0) }
    }

    var body: some ToolbarContent {
        if isEveryPhotoOwn {
            ownBar
        } else {
            othersBar
        }
    }

    /// Takes the photos chosen out of the album; they stay chosen if the
    /// server refused.
    @ToolbarContentBuilder
    private var removeFromAlbumItem: some ToolbarContent {
        if isRemovalOffered, let albumId = listKey.albumId {
            ToolbarItem(placement: .bottomBar) {
                Button("Remove from album", systemImage: "rectangle.stack.badge.minus") {
                    let chosen = selectedPhotos
                    Task {
                        if await model.remove(chosen, fromAlbum: albumId) { onEnd() }
                    }
                }
                .disabled(!isEveryPhotoRemovable)
            }
            ToolbarSpacer(.flexible, placement: .bottomBar)
        }
    }

    @ToolbarContentBuilder
    private var othersBar: some ToolbarContent {
        ToolbarItem(placement: .bottomBar) {
            Button("Save to my library", systemImage: "photo.badge.plus") {
                let chosen = selectedPhotos
                Task {
                    await model.saveToLibrary(chosen)
                    onEnd()
                }
            }
        }
        ToolbarSpacer(.flexible, placement: .bottomBar)
        removeFromAlbumItem
        ToolbarItem(placement: .bottomBar) {
            PhotosShareLink(photos: selectedPhotos, connection: model.connection)
        }
    }

    @ToolbarContentBuilder
    private var ownBar: some ToolbarContent {
        ToolbarItem(placement: .bottomBar) {
            AddToAlbumMenu(model: model, photos: selectedPhotos, scope: scope, hiddenAlbumId: listKey.albumId, onAdded: onEnd)
                .disabled(isEmpty)
        }
        ToolbarSpacer(.flexible, placement: .bottomBar)
        removeFromAlbumItem
        ToolbarItem(placement: .bottomBar) {
            Button(
                isEveryPhotoFavorite ? LocalizedStringKey("Remove from Favorites") : LocalizedStringKey("Add to Favorites"),
                systemImage: isEveryPhotoFavorite ? "heart.slash" : "heart"
            ) {
                let chosen = selectedPhotos
                let isFavorite = !isEveryPhotoFavorite
                Task {
                    await model.setFavorite(chosen, isFavorite: isFavorite, in: listKey)
                    onEnd()
                }
            }
            .disabled(isEmpty)
        }
        ToolbarSpacer(.flexible, placement: .bottomBar)
        ToolbarItem(placement: .bottomBar) {
            PhotoChangeLocationButton(model: model, photos: selectedPhotos, onChanged: onEnd)
                .disabled(isEmpty)
        }
        ToolbarSpacer(.flexible, placement: .bottomBar)
        ToolbarItem(placement: .bottomBar) {
            PhotosShareLink(photos: selectedPhotos, connection: model.connection)
                .disabled(isEmpty)
        }
        ToolbarSpacer(.flexible, placement: .bottomBar)
        ToolbarItem(placement: .bottomBar) {
            Button("Move to Trash", systemImage: "trash", role: .destructive) { isConfirmingTrash = true }
                .tint(.red)
                .disabled(isEmpty)
                .confirmationDialog("Move these photos to the Trash?", isPresented: $isConfirmingTrash, titleVisibility: .visible) {
                    Button("Move to Trash", role: .destructive) {
                        let chosen = selectedPhotos
                        Task {
                            if await model.moveToTrash(chosen) { onEnd() }
                        }
                    }
                }
        }
    }
}

// MARK: The albums

/// PhotoAlbumsView is the Albums section: the person's albums as covers with
/// their titles, then the albums shared with them and the albums of each circle
/// they are in, each under a heading, and the suggestions last.
struct PhotoAlbumsView: View {
    let model: PhotosModel

    var body: some View {
        Group {
            if let albums = model.albums {
                if albums.isEmpty, model.suggestions.isEmpty {
                    ScrollView {
                        ContentUnavailableView {
                            Label("No albums yet", systemImage: "rectangle.stack")
                        } description: {
                            Text("Choose photos in your library and add them to a new album, or make an empty one.")
                        } actions: {
                            Button("New album") { model.requestAlbumTitle(.new, scope: .library) }
                                .buttonStyle(.borderedProminent)
                        }
                        .containerRelativeFrame(.vertical)
                    }
                } else {
                    let sections = PhotoAlbumSections(albums)
                    ScrollView {
                        if !sections.own.isEmpty {
                            albumGrid(sections.own, title: sections.hasOthers ? String(localized: "Your albums") : nil)
                        }
                        if !sections.sharedWithYou.isEmpty {
                            albumGrid(sections.sharedWithYou, title: String(localized: "Albums shared with you"))
                        }
                        ForEach(sections.circles) { circleAlbums in
                            albumGrid(circleAlbums.albums, title: circleAlbums.circleName)
                        }
                        if !model.suggestions.isEmpty {
                            PhotoSuggestionsSection(model: model)
                        }
                    }
                }
            } else if let failureText = model.albumsFailureText {
                ScrollView {
                    PhotosFailedView(failureText: failureText) {
                        Task { await model.reloadAlbums() }
                    }
                    .containerRelativeFrame(.vertical)
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }

    private func albumGrid(_ albums: [PhotoAlbum], title: String?) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            if let title {
                Text(title)
                    .font(.headline)
                    .accessibilityAddTraits(.isHeader)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 150), spacing: 16)], spacing: 20) {
                ForEach(albums) { album in
                    NavigationLink {
                        PhotoAlbumScreen(model: model, albumId: album.id)
                    } label: {
                        PhotoAlbumCell(album: album, connection: model.connection)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .padding(.horizontal, PhotoAlbumLayout.horizontalPadding)
        .padding(.top, 12)
        .padding(.bottom, 8)
    }
}

/// PhotoAlbumRow is an album as a list row, on a person's card and on a
/// circle's page: its cover, its title and how many photos it holds, and, when
/// isSharerShown, who shared it if somebody shared it with the person.
struct PhotoAlbumRow: View {
    let album: PhotoAlbum
    let connection: Connection
    var isSharerShown = false
    @State private var picture: UIImage?

    private let side: CGFloat = 44

    var body: some View {
        HStack(spacing: 12) {
            Color(.secondarySystemBackground)
                .frame(width: side, height: side)
                .overlay {
                    if let picture {
                        Image(uiImage: picture)
                            .resizable()
                            .scaledToFill()
                    }
                }
                .clipShape(.rect(cornerRadius: 6))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: album.albumTitle)
                    .foregroundStyle(.primary)
                Text("\(album.photoCount) photos")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                if isSharerShown, album.isSharedWithCaller {
                    PhotoAlbumOwnerLine(album: album)
                }
            }
            Spacer(minLength: 0)
        }
        .contentShape(.rect)
        .task(id: album.coverPreviewUrl) {
            guard !album.coverPreviewUrl.isEmpty else {
                picture = nil
                return
            }
            if let loaded = await PhotoImages.picture(path: album.coverPreviewUrl, connection: connection) { picture = loaded }
        }
    }
}

/// PhotoAlbumLayout holds the one measure the album grids and the suggestion
/// cards share, so a card lines up with the edges of the grid under it.
enum PhotoAlbumLayout {
    /// The space at the left and right of the album sections.
    static let horizontalPadding: CGFloat = 16
}

/// PhotoAlbumCell is one album in the grid: its cover as a square, its title
/// under it, and in muted text how many photos it holds and the dates they
/// span.
struct PhotoAlbumCell: View {
    let album: PhotoAlbum
    let connection: Connection
    @State private var cover: UIImage?

    init(album: PhotoAlbum, connection: Connection) {
        self.album = album
        self.connection = connection
        _cover = State(initialValue: album.coverPreviewUrl.isEmpty ? nil : PhotoImages.cachedPicture(path: album.coverPreviewUrl, accountKey: connection.accountKey))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Color(.secondarySystemBackground)
                .aspectRatio(1, contentMode: .fit)
                .overlay {
                    if let cover {
                        Image(uiImage: cover)
                            .resizable()
                            .scaledToFill()
                    } else {
                        Image(systemName: "rectangle.stack")
                            .font(.largeTitle)
                            .foregroundStyle(.secondary)
                            .accessibilityHidden(true)
                    }
                }
                .clipShape(.rect(cornerRadius: 12))
                .padding(.bottom, 4)
            Text(album.albumTitle)
                .font(.subheadline.weight(.semibold))
                .lineLimit(1)
            Text(detailText)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            PhotoAlbumOwnerLine(album: album)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .task(id: album.coverPreviewUrl) {
            guard !album.coverPreviewUrl.isEmpty else {
                cover = nil
                return
            }
            if let loaded = await PhotoImages.picture(path: album.coverPreviewUrl, connection: connection) { cover = loaded }
        }
    }

    private var detailText: String {
        let count = PhotoAlbumWords.photoCount(album.photoCount)
        guard let range = album.dateRangeText() else { return count }
        return "\(count) · \(range)"
    }
}

/// PhotoAlbumWords writes the counts the album screens say.
enum PhotoAlbumWords {
    static func photoCount(_ count: Int) -> String {
        String(localized: "\(count) photos")
    }
}

/// PhotoSuggestionsSection is the trips and events the server found in photos
/// that are in no album, as cards to scroll across. Nothing is made until
/// the person chooses Create and names the album.
struct PhotoSuggestionsSection: View {
    let model: PhotosModel

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Suggested")
                .font(.headline)
                .padding(.horizontal, PhotoAlbumLayout.horizontalPadding)
                .accessibilityAddTraits(.isHeader)
            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 12) {
                    ForEach(model.suggestions) { suggestion in
                        // As wide as the album grids above it, so its edges line up with theirs;
                        // with several suggestions each one is a page to swipe to.
                        PhotoSuggestionCard(suggestion: suggestion, model: model)
                            .containerRelativeFrame(.horizontal) { containerWidth, _ in
                                containerWidth - 2 * PhotoAlbumLayout.horizontalPadding
                            }
                    }
                }
                .scrollTargetLayout()
                .padding(.horizontal, PhotoAlbumLayout.horizontalPadding)
            }
            .scrollIndicators(.hidden)
            .scrollTargetBehavior(.viewAligned)
        }
        .padding(.top, 8)
    }
}

/// PhotoSuggestionCard is one suggestion: a row of its pictures, its title, the
/// dates, places and count, and a button that makes the album. It is a card
/// that waits for the person, so it sits on the cards' shared surface.
struct PhotoSuggestionCard: View {
    let suggestion: PhotoAlbumSuggestion
    let model: PhotosModel

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 4) {
                ForEach(Array(suggestion.samplePhotoIds.prefix(PhotoAlbumSuggestion.shownSampleCount)), id: \.self) { photoId in
                    PhotoSuggestionSample(photo: model.suggestionSamples[photoId], connection: model.connection)
                }
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(suggestion.albumTitle)
                    .font(.headline)
                    .lineLimit(2)
                Text(detailText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                if !suggestion.placeNames.isEmpty, !suggestion.isTitleMadeOfPlaces {
                    Text(suggestion.placeNames.joined(separator: ", "))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
            // Full width, and the title wraps at the large text sizes instead of ending in an
            // ellipsis.
            Button {
                model.requestAlbumTitle(.suggestion(suggestion), scope: .library)
            } label: {
                Text("Create album")
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)
            .controlSize(.large)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18))
        .overlay { RoundedRectangle(cornerRadius: 18).strokeBorder(Color(.separator), lineWidth: 0.5) }
        .accessibilityElement(children: .contain)
    }

    private var detailText: String {
        let count = PhotoAlbumWords.photoCount(suggestion.photoCount)
        guard let range = suggestion.dateRangeText() else { return count }
        return "\(count) · \(range)"
    }
}

/// PhotoSuggestionSample is one small picture on a suggestion's card.
private struct PhotoSuggestionSample: View {
    let photo: Photo?
    let connection: Connection
    @State private var picture: UIImage?

    var body: some View {
        Color(.tertiarySystemBackground)
            .aspectRatio(1, contentMode: .fit)
            .overlay {
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFill()
                }
            }
            .clipShape(.rect(cornerRadius: 6))
            .accessibilityHidden(true)
            .task(id: photo?.gridPreviewPath) {
                guard let path = photo?.gridPreviewPath else { return }
                if let loaded = await PhotoImages.picture(path: path, connection: connection) { picture = loaded }
            }
    }
}

// MARK: One album

/// PhotoAlbumScreen is the photos of one album, in the library's grid and
/// viewer. Its menu renames the album or deletes it; the photos stay in the
/// library.
struct PhotoAlbumScreen: View {
    let model: PhotosModel
    let albumId: String
    @State private var opened: OpenedPhoto?
    @State private var isSelecting = false
    @State private var selectedPhotoIds: Set<String> = []
    @State private var isConfirmingTrashOfSelection = false
    @State private var isConfirmingDelete = false
    @State private var isSharing = false
    @State private var isAddingPhotos = false
    @Environment(\.dismiss) private var dismiss

    private var key: PhotoListKey { .album(albumId) }
    private var album: PhotoAlbum? { model.albums?.first { $0.id == albumId } }
    private var photos: [Photo] { model.photos(in: key) }
    private var selectedPhotos: [Photo] { photos.filter { selectedPhotoIds.contains($0.id) } }

    var body: some View {
        content
            .navigationTitle(isSelecting ? selectionTitle : (album?.albumTitle ?? ""))
            .navigationSubtitle(isSelecting ? "" : (album.map { PhotoAlbumWords.photoCount($0.photoCount) } ?? ""))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbarContent }
            .task { await model.load(key) }
            .task(id: model.hasWaitingPhotos(in: key)) {
                if model.hasWaitingPhotos(in: key) { await model.followWaitingPhotos(in: key) }
            }
            .refreshable {
                await model.reload(key)
                await model.reloadAlbums()
            }
            .fullScreenCover(item: $opened) { opened in
                PhotoViewer(model: model, listKey: key, openedPhotoId: opened.id)
            }
            .sheet(isPresented: $isSharing) {
                if let album { PhotoAlbumShareSheet(model: model, album: album) }
            }
            .sheet(isPresented: $isAddingPhotos) {
                if let album { PhotoPickerSheet(model: model, album: album) }
            }
            .albumTitleAlert(model: model, scope: .album, isActive: opened == nil)
            .onDisappear { model.dropPhotosNoLongerFavorite() }
            // "Photos taken near here" is answered by the library, so this screen goes away.
            .onChange(of: model.photoFilter.photoNearFilter) { _, photoNearFilter in
                if photoNearFilter != nil { dismiss() }
            }
            #if DEBUG
                // `-TeaNodeScreen photos-share` opens the share sheet over the first album, `photos-share-stop`
                // the same with the question asked on its first share, and
                // `photos-shared-viewer` the viewer over the first photo of a shared one.
                .task(id: album?.id) {
                    if DemoServer.isRequested, ["photos-share", "photos-share-stop"].contains(DemoServer.screen ?? ""), album?.canCallerShare == true { isSharing = true }
                }
                .task(id: photos.first?.id) {
                    if DemoServer.isRequested, ["photos-shared-viewer", "photos-shared-info"].contains(DemoServer.screen ?? ""), let first = photos.first, opened == nil {
                        opened = OpenedPhoto(id: first.id)
                    }
                }
            #endif
    }

    private var selectionTitle: String {
        selectedPhotoIds.isEmpty ? String(localized: "Select photos") : String(localized: "\(selectedPhotoIds.count) Selected")
    }

    private func endSelection() {
        isSelecting = false
        selectedPhotoIds = []
    }

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        if isSelecting {
            ToolbarItem(placement: .primaryAction) {
                Button("Done") { endSelection() }
            }
            PhotoSelectionToolbar(
                model: model, selectedPhotos: selectedPhotos, listKey: key, scope: .album, album: album, isConfirmingTrash: $isConfirmingTrashOfSelection,
                onEnd: endSelection)
        } else {
            if !photos.isEmpty {
                ToolbarItem(placement: .primaryAction) {
                    Button("Select", systemImage: "checkmark.circle") { isSelecting = true }
                }
            }
            if let album, album.canCallerContribute || album.canCallerManage {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        if album.canCallerContribute {
                            Button("Add photos", systemImage: "photo.badge.plus") { isAddingPhotos = true }
                        }
                        if album.canCallerShare {
                            Button("Share", systemImage: "person.badge.plus") { isSharing = true }
                        }
                        if album.canCallerManage {
                            Button("Rename album", systemImage: "pencil") {
                                model.requestAlbumTitle(.rename(album), scope: .album)
                            }
                            Button("Delete album", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
                                .tint(.red)
                        }
                    } label: {
                        Label("More", systemImage: "ellipsis")
                    }
                    .confirmationDialog("Delete this album?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                        Button("Delete album", role: .destructive) {
                            Task {
                                if await model.delete(album) { dismiss() }
                            }
                        }
                    } message: {
                        // Photos other people added stay in their owners' libraries, not in the person's.
                        if album.isCircleAlbum || photos.contains(where: { !$0.isOwnedByCaller }) {
                            Text("The photos stay in their owners' libraries.")
                        } else {
                            Text("The photos stay in your library.")
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var content: some View {
        if let listing = model.listing(key) {
            if listing.photos.isEmpty {
                ScrollView {
                    ContentUnavailableView {
                        Label("No photos in this album", systemImage: "rectangle.stack")
                    } description: {
                        if album?.canCallerContribute == true {
                            Text("Choose photos in your library and add them here.")
                        } else {
                            Text("Nothing has been added to this album yet.")
                        }
                    } actions: {
                        if album?.canCallerContribute == true {
                            Button("Add photos") { isAddingPhotos = true }
                                .buttonStyle(.borderedProminent)
                        }
                    }
                    .containerRelativeFrame(.vertical)
                }
            } else {
                ScrollView {
                    PhotoGrid(listing: listing, listKey: key, model: model, scope: .album, isSelecting: isSelecting, selectedPhotoIds: $selectedPhotoIds) { photo in
                        opened = OpenedPhoto(id: photo.id)
                    }
                }
            }
        } else if let failureText = model.failureText(key) {
            ScrollView {
                PhotosFailedView(failureText: failureText) {
                    Task { await model.reload(key) }
                }
                .containerRelativeFrame(.vertical)
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}
