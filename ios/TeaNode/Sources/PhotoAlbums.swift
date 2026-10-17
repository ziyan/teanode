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
        case .new, .newWith: String(localized: "New Album")
        case .suggestion: String(localized: "Create Album")
        case .rename: String(localized: "Rename Album")
        }
    }

    var buttonTitle: String {
        switch purpose {
        case .new, .newWith, .suggestion: String(localized: "Create")
        case .rename: String(localized: "Rename")
        }
    }

    /// What the field starts with: the suggestion's proposed title, or the
    /// album's own.
    var initialTitle: String {
        switch purpose {
        case .new, .newWith: ""
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
            Button("New Album…", systemImage: "plus") {
                model.requestAlbumTitle(.newWith(photos), scope: scope)
                onAdded()
            }
            let choices = (model.albums ?? []).filter { $0.id != hiddenAlbumId }
            if !choices.isEmpty {
                Divider()
                ForEach(choices) { album in
                    Button(album.albumTitle) {
                        let chosen = photos
                        Task { await model.add(chosen, to: album) }
                        onAdded()
                    }
                }
            }
        } label: {
            Label("Add to Album", systemImage: "rectangle.stack.badge.plus")
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
    @Binding var isConfirmingTrash: Bool
    let onEnd: () -> Void

    private var isEmpty: Bool { selectedPhotos.isEmpty }
    private var areAllFavorites: Bool { !selectedPhotos.isEmpty && selectedPhotos.allSatisfy(\.isFavorite) }

    var body: some ToolbarContent {
        ToolbarItem(placement: .bottomBar) {
            AddToAlbumMenu(model: model, photos: selectedPhotos, scope: scope, hiddenAlbumId: listKey.albumId, onAdded: onEnd)
                .disabled(isEmpty)
        }
        ToolbarSpacer(.flexible, placement: .bottomBar)
        if let albumId = listKey.albumId {
            ToolbarItem(placement: .bottomBar) {
                Button("Remove from Album", systemImage: "rectangle.stack.badge.minus") {
                    let chosen = selectedPhotos
                    Task {
                        await model.remove(chosen, fromAlbum: albumId)
                        onEnd()
                    }
                }
                .disabled(isEmpty)
            }
            ToolbarSpacer(.flexible, placement: .bottomBar)
        }
        ToolbarItem(placement: .bottomBar) {
            Button(
                areAllFavorites ? LocalizedStringKey("Remove from Favorites") : LocalizedStringKey("Favorite"),
                systemImage: areAllFavorites ? "heart.slash" : "heart"
            ) {
                let chosen = selectedPhotos
                let isFavorite = !areAllFavorites
                Task {
                    await model.setFavorite(chosen, isFavorite: isFavorite, in: listKey)
                    onEnd()
                }
            }
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
                .disabled(isEmpty)
                .confirmationDialog("Move these photos to the Trash?", isPresented: $isConfirmingTrash, titleVisibility: .visible) {
                    Button("Move to Trash", role: .destructive) {
                        let chosen = selectedPhotos
                        Task {
                            await model.moveToTrash(chosen)
                            onEnd()
                        }
                    }
                }
        }
    }
}

// MARK: The albums

/// PhotoAlbumsView is the Albums section: the suggestions across the top, then
/// the albums as covers with their titles.
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
                            Button("New Album") { model.requestAlbumTitle(.new, scope: .library) }
                                .buttonStyle(.borderedProminent)
                        }
                        .containerRelativeFrame(.vertical)
                    }
                } else {
                    ScrollView {
                        if !model.suggestions.isEmpty {
                            PhotoSuggestionsSection(model: model)
                        }
                        if !albums.isEmpty {
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
                            .padding(.horizontal)
                            .padding(.top, model.suggestions.isEmpty ? 8 : 20)
                            .padding(.bottom)
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
                .padding(.horizontal)
                .accessibilityAddTraits(.isHeader)
            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 12) {
                    ForEach(model.suggestions) { suggestion in
                        PhotoSuggestionCard(suggestion: suggestion, model: model)
                    }
                }
                .scrollTargetLayout()
                .padding(.horizontal)
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
            Button("Create Album") {
                model.requestAlbumTitle(.suggestion(suggestion), scope: .library)
            }
            .buttonStyle(.bordered)
            .controlSize(.large)
        }
        .padding(14)
        .frame(width: 300, alignment: .leading)
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
            .albumTitleAlert(model: model, scope: .album, isActive: opened == nil)
            .onDisappear { model.dropPhotosNoLongerFavorite() }
    }

    private var selectionTitle: String {
        selectedPhotoIds.isEmpty ? String(localized: "Select Items") : String(localized: "\(selectedPhotoIds.count) Selected")
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
                model: model, selectedPhotos: selectedPhotos, listKey: key, scope: .album, isConfirmingTrash: $isConfirmingTrashOfSelection, onEnd: endSelection)
        } else {
            if !photos.isEmpty {
                ToolbarItem(placement: .primaryAction) {
                    Button("Select") { isSelecting = true }
                }
            }
            if let album {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button("Rename Album", systemImage: "pencil") {
                            model.requestAlbumTitle(.rename(album), scope: .album)
                        }
                        Button("Delete Album", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
                    } label: {
                        Label("More", systemImage: "ellipsis")
                    }
                    .confirmationDialog("Delete this album?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                        Button("Delete Album", role: .destructive) {
                            Task {
                                if await model.delete(album) { dismiss() }
                            }
                        }
                    } message: {
                        Text("The photos stay in your library.")
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
                        Text("Choose photos in your library and add them here.")
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
