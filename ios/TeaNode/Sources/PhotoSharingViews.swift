import SwiftUI
import TeaNodeKit

// Sharing an album, adding to an album that is shared, the filters on the
// library and the date of a photo: the screens that go beyond the person's own
// photos in their own albums. The words are the dashboard's where it has them.

// MARK: Words

/// PhotoWords writes what these screens say about kinds, counts and levels.
enum PhotoWords {
    /// What a photo is: a Picture for a still, as the dashboard calls it.
    static func kind(of photo: Photo) -> String {
        if photo.isVideo { return String(localized: "Video") }
        if photo.isLivePhoto { return String(localized: "Live Photo") }
        return String(localized: "photos.kindPicture", defaultValue: "Picture")
    }

    static func title(of photoKindFilter: PhotoKindFilter) -> String {
        switch photoKindFilter {
        case .all: String(localized: "All kinds")
        case .picture: String(localized: "Pictures")
        case .livePhoto: String(localized: "Live Photos")
        case .video: String(localized: "Videos")
        }
    }

    /// Why a photo has no picture, as the dashboard says it.
    static func failure(of photo: Photo) -> String {
        let reason = photo.processingErrorMessage.trimmingCharacters(in: .whitespacesAndNewlines)
        return reason.isEmpty
            ? String(localized: "This photo could not be read.")
            : String(localized: "This photo could not be read: \(reason)").asReasonSentence
    }

    static func videoCount(_ count: Int) -> String { String(localized: "\(count) videos") }
    static func favoriteCount(_ count: Int) -> String { String(localized: "\(count) favorites") }
    static func trashedCount(_ count: Int) -> String { String(localized: "\(count) in the Trash") }

    static func title(of photoShareLevel: PhotoShareLevel) -> String {
        switch photoShareLevel {
        case .view: String(localized: "photos.levelView", defaultValue: "View")
        case .contribute: String(localized: "photos.levelContribute", defaultValue: "Contribute")
        }
    }

    static func explanation(of photoShareLevel: PhotoShareLevel) -> String {
        switch photoShareLevel {
        case .view: String(localized: "See the photos and save them.")
        case .contribute: String(localized: "Also add photos of their own, and take out the ones they added.")
        }
    }

    /// A share's line under who it is with: the level, and whether the place
    /// of each photo comes with it.
    static func line(of share: PhotoAlbumShare) -> String {
        let level = title(of: share.knownShareLevel)
        return share.isLocationIncluded ? String(localized: "\(level), with places") : level
    }
}

// MARK: What the library holds

/// PhotoLibrarySummary is what the library holds, under its last photo: the
/// photos with the videos among them, the favorites and what is in the Trash,
/// as the dashboard counts them.
struct PhotoLibrarySummary: View {
    let library: PhotoLibrary

    private var contentsText: String {
        // The videos are among the photos, as the dashboard, the command line and the agent count them.
        let photos = PhotoAlbumWords.photoCount(library.itemCount)
        guard library.videoCount > 0 else { return photos }
        return String(localized: "\(photos), including \(PhotoWords.videoCount(library.videoCount))")
    }

    private var otherText: String {
        var parts: [String] = []
        if library.favoriteCount > 0 { parts.append(PhotoWords.favoriteCount(library.favoriteCount)) }
        if library.trashedCount > 0 { parts.append(PhotoWords.trashedCount(library.trashedCount)) }
        return parts.joined(separator: " · ")
    }

    var body: some View {
        VStack(spacing: 2) {
            Text(contentsText)
            if !otherText.isEmpty { Text(otherText) }
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .multilineTextAlignment(.center)
        .frame(maxWidth: .infinity)
        .padding()
        .accessibilityElement(children: .combine)
    }
}

// MARK: Filters

/// PhotoFilterMenu narrows the library by kind and by days. The icon is filled
/// while a filter is on, so the narrowed list is never mistaken for the whole.
struct PhotoFilterMenu: View {
    let model: PhotosModel
    @Binding var isChoosingDates: Bool

    var body: some View {
        Menu {
            Picker("Kind", selection: Bindable(model).photoFilter.photoKindFilter) {
                ForEach(PhotoKindFilter.allCases) { photoKindFilter in
                    Text(PhotoWords.title(of: photoKindFilter)).tag(photoKindFilter)
                }
            }
            .pickerStyle(.inline)
            Button("Choose dates…", systemImage: "calendar") { isChoosingDates = true }
            if model.photoFilter.isActive {
                Button("Clear filters", systemImage: "xmark.circle", role: .destructive) { model.photoFilter = PhotoFilter() }
            }
        } label: {
            Label("Filter", systemImage: model.photoFilter.isActive ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
        }
    }
}

/// PhotoNearChip says the library is narrowed to the photos taken near a
/// place, and takes that off when pressed.
struct PhotoNearChip: View {
    let photoNearFilter: PhotoNearFilter
    let onRemove: () -> Void

    private var title: String {
        photoNearFilter.placeName.isEmpty ? String(localized: "Near here") : String(localized: "Near \(photoNearFilter.placeName)")
    }

    var body: some View {
        Button(action: onRemove) {
            HStack(spacing: 6) {
                Image(systemName: "mappin.and.ellipse")
                Text(title)
                    .lineLimit(1)
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(.secondary)
            }
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.capsule)
        .controlSize(.large)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityLabel(title)
        .accessibilityHint(String(localized: "Removes this filter"))
    }
}

/// PhotoDateFilterSheet chooses the first and last day the library is narrowed
/// to. Each end is optional.
struct PhotoDateFilterSheet: View {
    let model: PhotosModel
    @Environment(\.dismiss) private var dismiss

    private var firstDayBinding: Binding<Date> {
        Binding(get: { model.photoFilter.firstDay ?? Date() }, set: { model.photoFilter.firstDay = $0 })
    }

    private var lastDayBinding: Binding<Date> {
        Binding(get: { model.photoFilter.lastDay ?? Date() }, set: { model.photoFilter.lastDay = $0 })
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Toggle("photos.filterFrom", isOn: Binding(get: { model.photoFilter.firstDay != nil }, set: { model.photoFilter.firstDay = $0 ? Date() : nil }))
                    if model.photoFilter.firstDay != nil {
                        DatePicker("First day", selection: firstDayBinding, displayedComponents: .date)
                    }
                    Toggle("photos.filterTo", isOn: Binding(get: { model.photoFilter.lastDay != nil }, set: { model.photoFilter.lastDay = $0 ? Date() : nil }))
                    if model.photoFilter.lastDay != nil {
                        DatePicker("Last day", selection: lastDayBinding, displayedComponents: .date)
                    }
                } footer: {
                    if model.photoFilter.isRangeBackwards {
                        Text("The first day is after the last day, so no photo matches.")
                    } else {
                        Text("Days are counted on this iPhone's clock, so a photo taken in another time zone can fall on a neighboring day.")
                    }
                }
                if model.photoFilter.firstDay != nil || model.photoFilter.lastDay != nil {
                    Section {
                        Button("Clear dates", role: .destructive) {
                            model.photoFilter.firstDay = nil
                            model.photoFilter.lastDay = nil
                        }
                    }
                }
            }
            .navigationTitle("Dates")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium])
    }
}

// MARK: A new album

/// NewAlbumMenu starts an album: the person's own, or, when they are in
/// circles, one for a circle that every member adds to.
struct NewAlbumMenu: View {
    let model: PhotosModel

    var body: some View {
        if model.circles.isEmpty {
            Button("New album", systemImage: "plus") { model.requestAlbumTitle(.new, scope: .library) }
        } else {
            Menu {
                Button("New album", systemImage: "plus") { model.requestAlbumTitle(.new, scope: .library) }
                ForEach(model.circles) { circle in
                    Button("For \(circle.name)", systemImage: "person.2") { model.requestAlbumTitle(.newForCircle(circle), scope: .library) }
                }
            } label: {
                Label("New album", systemImage: "plus")
            }
        }
    }
}

// MARK: Whose album it is

/// PhotoAlbumOwnerLine says whose a shared album is under its title: the
/// owner's face and name, or the circle's name.
struct PhotoAlbumOwnerLine: View {
    let album: PhotoAlbum

    var body: some View {
        if album.isCircleAlbum {
            Label(album.circleName, systemImage: "person.2")
                .labelStyle(.titleAndIcon)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        } else if album.isSharedWithCaller {
            HStack(spacing: 4) {
                AvatarView(name: album.ownerDisplayName, avatarUrl: album.ownerAvatarUrl, colorIndex: album.ownerAvatarColorIndex, size: 16)
                Text(album.ownerDisplayName)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
    }
}

// MARK: Sharing

/// PhotoAlbumShareSheet is who an album is shared with, at what level, with a
/// way to change or stop each share, and a form to share it with a circle or a
/// person from a circle the person shares.
struct PhotoAlbumShareSheet: View {
    let model: PhotosModel
    let album: PhotoAlbum
    @Environment(\.dismiss) private var dismiss
    @State private var shares: [PhotoAlbumShare] = []
    @State private var people: [ChatPerson] = []
    @State private var circles: [PeopleCircle] = []
    @State private var isLoaded = false
    @State private var draft = PhotoShareDraft()
    @State private var isWorking = false
    @State private var doneText: String?
    @State private var problemText: String?
    /// The share the person pressed Stop sharing on, to be asked about.
    @State private var shareToStop: PhotoAlbumShare?

    private var connection: Connection { model.connection }

    /// The circles and people not shared with yet.
    private var availableCircles: [PeopleCircle] {
        let taken = Set(shares.compactMap(\.granteeCircleId))
        return circles.filter { !taken.contains($0.id) }
    }

    private var availablePeople: [ChatPerson] {
        let taken = Set(shares.compactMap(\.granteeUserId))
        return people.filter { !taken.contains($0.userId) }
    }

    var body: some View {
        NavigationStack {
            List {
                sharesSection
                if isLoaded {
                    if availableCircles.isEmpty, availablePeople.isEmpty {
                        Section {
                            Group {
                                if shares.isEmpty {
                                    Text("You do not share a circle with anybody yet. Make or join a circle to share an album.")
                                } else {
                                    Text("The album is shared with everybody you share a circle with.")
                                }
                            }
                            .foregroundStyle(.secondary)
                        }
                    } else {
                        chooseSections
                    }
                }
            }
            .navigationTitle("Share album")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .overlay {
                if !isLoaded { ProgressView() }
            }
            .task { await load() }
            #if DEBUG
                // `-TeaNodeScreen photos-share-stop` asks the question before the first share is stopped.
                .task(id: isLoaded) {
                    guard DemoServer.isRequested, DemoServer.screen == "photos-share-stop", isLoaded, let first = shares.first else { return }
                    try? await Task.sleep(for: .seconds(1))
                    shareToStop = first
                }
            #endif
            .toast(done: $doneText, problem: $problemText)
        }
        .presentationDetents([.large])
    }

    // MARK: Who it is shared with

    private var sharesSection: some View {
        Section {
            if isLoaded, shares.isEmpty {
                Text("This album is not shared with anybody yet.")
                    .foregroundStyle(.secondary)
            }
            ForEach(shares) { share in
                HStack(spacing: 12) {
                    if share.isWithCircle {
                        Image(systemName: "person.2.circle.fill")
                            .font(.title)
                            .foregroundStyle(.secondary)
                            .frame(width: 32, height: 32)
                            .accessibilityHidden(true)
                    } else {
                        AvatarView(name: share.granteeDisplayName, avatarUrl: share.granteeAvatarUrl, colorIndex: share.granteeAvatarColorIndex, size: 32)
                    }
                    VStack(alignment: .leading, spacing: 2) {
                        Text(share.granteeDisplayName)
                        Text(PhotoWords.line(of: share))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 0)
                    Menu {
                        shareOptions(share)
                    } label: {
                        Image(systemName: "ellipsis.circle")
                            .font(.title3)
                            .frame(minWidth: 44, minHeight: 44)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel("Options for \(share.granteeDisplayName)")
                    // Attached to the menu, not to the row or the list: it points at this button,
                    // and a touch and hold on the row asks the same question.
                    .confirmationDialog(
                        "Stop sharing with \(share.granteeDisplayName)?",
                        isPresented: Binding(get: { shareToStop?.id == share.id }, set: { if !$0, shareToStop?.id == share.id { shareToStop = nil } }),
                        titleVisibility: .visible
                    ) {
                        Button("Stop sharing", role: .destructive) { Task { await stop(share) } }
                    } message: {
                        Text("\(share.granteeDisplayName) can no longer see “\(album.albumTitle)”, and the photos they added to it leave the album.")
                    }
                }
                .accessibilityElement(children: .contain)
                .contextMenu { shareOptions(share) }
            }
        } header: {
            Text("Shared with")
        }
    }

    @ViewBuilder
    private func shareOptions(_ share: PhotoAlbumShare) -> some View {
        Picker(
            "Level",
            selection: Binding(
                get: { share.knownShareLevel },
                set: { level in Task { await update(share, photoShareLevel: level, isLocationIncluded: share.isLocationIncluded) } })
        ) {
            ForEach(PhotoShareLevel.allCases) { photoShareLevel in
                Text(PhotoWords.title(of: photoShareLevel)).tag(photoShareLevel)
            }
        }
        .pickerStyle(.inline)
        Toggle(
            "Include where photos were taken",
            isOn: Binding(
                get: { share.isLocationIncluded },
                set: { isOn in Task { await update(share, photoShareLevel: share.knownShareLevel, isLocationIncluded: isOn) } }))
        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { shareToStop = share }
    }

    // MARK: Sharing it with someone

    @ViewBuilder
    private var chooseSections: some View {
        if !availableCircles.isEmpty {
            Section("Share with a circle") {
                ForEach(availableCircles) { circle in
                    chooseRow(isChosen: draft.target == .circle(circleId: circle.id)) {
                        draft.target = .circle(circleId: circle.id)
                    } label: {
                        Label(circle.name, systemImage: "person.2")
                    }
                }
            }
        }
        if !availablePeople.isEmpty {
            Section("Share with a person") {
                ForEach(availablePeople) { person in
                    chooseRow(isChosen: draft.target == .person(userId: person.userId)) {
                        draft.target = .person(userId: person.userId)
                    } label: {
                        HStack(spacing: 12) {
                            AvatarView(name: person.displayName, avatarUrl: person.avatarUrl, colorIndex: person.avatarColorIndex, size: 28)
                            Text(person.displayName)
                        }
                    }
                }
            }
        }
        Section {
            Picker("What they can do", selection: $draft.photoShareLevel) {
                ForEach(PhotoShareLevel.allCases) { photoShareLevel in
                    Text(PhotoWords.title(of: photoShareLevel)).tag(photoShareLevel)
                }
            }
            .pickerStyle(.inline)
            .labelsHidden()
        } header: {
            Text("What they can do")
        } footer: {
            Text(PhotoWords.explanation(of: draft.photoShareLevel))
        }
        Section {
            Toggle("Include where photos were taken", isOn: $draft.isLocationIncluded)
        } footer: {
            VStack(alignment: .leading, spacing: 8) {
                Text("Without it, they see each picture without its place, and cannot get the original files.")
                Text("People in the photos are shown with the names you gave them, with or without places.")
            }
        }
        Section {
            Button("Share") { Task { await share() } }
                .disabled(!draft.canSubmit || isWorking)
        }
    }

    /// A row that chooses one of the people or circles, with a check on the
    /// chosen. Plain, like the other rows of a form that choose.
    private func chooseRow<RowLabel: View>(isChosen: Bool, action: @escaping () -> Void, @ViewBuilder label: () -> RowLabel) -> some View {
        Button(action: action) {
            HStack {
                label()
                Spacer(minLength: 8)
                if isChosen {
                    Image(systemName: "checkmark")
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                }
            }
            .contentShape(.rect)
        }
        .foregroundStyle(.primary)
        .accessibilityAddTraits(isChosen ? .isSelected : [])
    }

    // MARK: Doing it

    private func load() async {
        do {
            async let readShares = connection.photoAlbumShares(album.id)
            async let readPeople = connection.chatPeople()
            async let readCircles = connection.circles()
            (shares, people, circles) = try await (readShares, readPeople, readCircles)
            isLoaded = true
        } catch {
            guard !Task.isCancelled else { return }
            problemText = model.describe(error)
        }
    }

    private func share() async {
        isWorking = true
        defer { isWorking = false }
        do {
            let saved = try await connection.sharePhotoAlbum(album.id, draft: draft)
            shares.removeAll { $0.id == saved.id }
            shares.append(saved)
            draft = PhotoShareDraft()
            doneText = String(localized: "Shared with \(saved.granteeDisplayName)")
        } catch {
            problemText = model.describe(error)
        }
    }

    private func update(_ share: PhotoAlbumShare, photoShareLevel: PhotoShareLevel, isLocationIncluded: Bool) async {
        do {
            let changed = try await connection.updatePhotoAlbumShare(share.id, photoShareLevel: photoShareLevel, isLocationIncluded: isLocationIncluded)
            shares = shares.map { $0.id == changed.id ? changed : $0 }
        } catch {
            problemText = model.describe(error)
        }
    }

    private func stop(_ share: PhotoAlbumShare) async {
        do {
            try await connection.removePhotoAlbumShare(share.id)
            shares.removeAll { $0.id == share.id }
            doneText = String(localized: "No longer shared with \(share.granteeDisplayName)")
        } catch {
            problemText = model.describe(error)
        }
    }
}

// MARK: Adding photos to a shared album

/// PhotoPickerSheet chooses photos of the person's own library to add to an
/// album they contribute to: the library's grid, in selection.
struct PhotoPickerSheet: View {
    let model: PhotosModel
    let album: PhotoAlbum
    @Environment(\.dismiss) private var dismiss
    @State private var selectedPhotoIds: Set<String> = []
    @State private var isAdding = false

    private let key = PhotoListKey.library

    private var selectedPhotos: [Photo] { model.photos(in: key).filter { selectedPhotoIds.contains($0.id) } }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(selectedPhotoIds.isEmpty ? String(localized: "Select photos") : String(localized: "\(selectedPhotoIds.count) Selected"))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        if isAdding {
                            ProgressView()
                        } else {
                            Button("Add") { Task { await add() } }
                                .disabled(selectedPhotoIds.isEmpty)
                        }
                    }
                }
                .task { await model.load(key) }
        }
    }

    @ViewBuilder
    private var content: some View {
        if let listing = model.listing(key) {
            if listing.photos.isEmpty {
                ScrollView {
                    ContentUnavailableView {
                        Label("No photos yet", systemImage: "photo.on.rectangle")
                    } description: {
                        Text("Photos you add to your library can be added to the album.")
                    }
                    .containerRelativeFrame(.vertical)
                }
            } else {
                ScrollView {
                    PhotoGrid(listing: listing, listKey: key, model: model, scope: .library, isSelecting: true, selectedPhotoIds: $selectedPhotoIds, isMenuAvailable: false) { _ in }
                }
            }
        } else if let failureText = model.failureText(key) {
            ScrollView {
                PhotosFailedView(failureText: failureText) { Task { await model.reload(key) } }
                    .containerRelativeFrame(.vertical)
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func add() async {
        isAdding = true
        defer { isAdding = false }
        let chosen = selectedPhotos
        await model.add(chosen, to: album)
        // The album screen under this sheet shows the photos just added.
        await model.reload(.album(album.id))
        dismiss()
    }
}

// MARK: The date of a photo

/// PhotoDateChangeSheet places a photo at another date and time, read on the
/// clock of the place it was taken when the photo says where that was.
struct PhotoDateChangeSheet: View {
    let model: PhotosModel
    let photo: Photo
    let listKey: PhotoListKey
    @Environment(\.dismiss) private var dismiss
    @State private var date: Date
    @State private var isSaving = false

    init(model: PhotosModel, photo: Photo, listKey: PhotoListKey) {
        self.model = model
        self.photo = photo
        self.listKey = listKey
        _date = State(initialValue: PhotoTimeline.moment(of: photo)?.date ?? Date())
    }

    private var originalDate: Date? { PhotoTimeline.moment(of: photo)?.date }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    DatePicker("Date and time", selection: $date, displayedComponents: [.date, .hourAndMinute])
                        .datePickerStyle(.graphical)
                        .environment(\.timeZone, PhotoDateChange.timeZone(of: photo))
                } footer: {
                    Text("Shown on the clock of the place the photo was taken.")
                }
            }
            .navigationTitle("Change date")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isSaving {
                        ProgressView()
                    } else {
                        Button("Save") { Task { await save() } }
                            .disabled(date == originalDate)
                    }
                }
            }
        }
        .presentationDetents([.large])
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        if await model.changeDate(of: photo, to: date, in: listKey) { dismiss() }
    }
}

#if DEBUG
    #Preview("Library summary, light") {
        PhotoLibrarySummary(library: PhotoLibrary(photoCount: 3_800, videoCount: 112, favoriteCount: 90, trashedCount: 4, usedBytes: 0, trashRetentionDays: 30))
            .preferredColorScheme(.light)
    }
    #Preview("Library summary, dark") {
        PhotoLibrarySummary(library: PhotoLibrary(photoCount: 3_800, videoCount: 112, favoriteCount: 90, trashedCount: 4, usedBytes: 0, trashRetentionDays: 30))
            .preferredColorScheme(.dark)
    }
    #Preview("Near chip, light") {
        PhotoNearChip(photoNearFilter: PhotoNearFilter(latitudeDegrees: 10.5, longitudeDegrees: 20.5, placeName: "Riverside Park")) {}
            .padding()
            .preferredColorScheme(.light)
    }
    #Preview("Near chip, dark, no place name") {
        PhotoNearChip(photoNearFilter: PhotoNearFilter(latitudeDegrees: 10.5, longitudeDegrees: 20.5)) {}
            .padding()
            .preferredColorScheme(.dark)
    }
    #Preview("Library summary, no videos") {
        PhotoLibrarySummary(library: PhotoLibrary(photoCount: 12, videoCount: 0, favoriteCount: 0, trashedCount: 0, usedBytes: 0, trashRetentionDays: 30))
    }
#endif
