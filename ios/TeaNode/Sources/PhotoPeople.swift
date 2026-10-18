import SwiftUI
import TeaNodeKit
import UIKit

// The people found in the person's photos: a round picture for each, the name
// of the contact the person gave it, the person's own screen of photos, and
// what can be done to a person. Nothing here names anybody by itself: a group
// of faces is nobody until the person chooses a contact.

// MARK: What the model does with people

extension PhotosModel {
    /// Reads the groups of faces, unless they have been read.
    func loadFaceGroups() async {
        guard faceGroups == nil else { return }
        await reloadFaceGroups()
    }

    /// Reads the groups of faces again. Quietly, a failure is not told: it is
    /// how a server that finds no faces is asked whether it has people from
    /// before.
    func reloadFaceGroups(isQuiet: Bool = false) async {
        do {
            faceGroups = try await connection.photoFaceGroups(isHiddenIncluded: isHiddenPeopleShown)
            faceGroupsFailureText = nil
        } catch {
            guard !Task.isCancelled, !isQuiet else { return }
            if faceGroups == nil { faceGroupsFailureText = describe(error) }
            problemText = describe(error)
        }
    }

    /// Puts a group as the server now has it in place of the one held, and
    /// forgets the photos listed for it, which may have changed.
    private func replaceFaceGroup(_ group: PhotoFaceGroup) {
        faceGroups = faceGroups?.map { $0.id == group.id ? group : $0 }
        forgetListing(.faceGroup(group.id))
    }

    /// Names a group by a contact the person chose; the group as it now is,
    /// or nil when that failed. A contact names one person, so a contact that
    /// names another group already merges the two: the group that is left is
    /// the one answered, and the one that was named is gone from the people.
    /// A contact with no picture is given a face of the person, and the toast
    /// offers to change it or take it off.
    func name(_ group: PhotoFaceGroup, contactId: String) async -> PhotoFaceGroup? {
        do {
            let answer = try await connection.namePhotoFaceGroup(group.id, contactId: contactId)
            let named = answer.photoFaceGroup
            if answer.isMerged {
                faceGroups?.removeAll { $0.id == answer.mergedPhotoFaceGroupId }
                forgetListing(.faceGroup(answer.mergedPhotoFaceGroupId))
            }
            replaceFaceGroup(named)
            var sentences = [
                answer.isMerged
                    ? String(localized: "Merged with “\(named.trimmedContactName)”")
                    : String(localized: "Named “\(named.trimmedContactName)”")
            ]
            if answer.isContactPictureSet {
                sentences.append(String(localized: "Used this face as their picture"))
            }
            showDone(sentences.joined(separator: " "), buttons: answer.isContactPictureSet ? contactPictureButtons(contactId: contactId) : [])
            return named
        } catch {
            problemText = describe(error)
            return nil
        }
    }

    /// Change opens the contact whose picture was just made; Remove takes the
    /// picture off again.
    private func contactPictureButtons(contactId: String) -> [ToastButton] {
        [
            ToastButton(title: String(localized: "Change")) {
                AppModel.shared.openedContactId = contactId
            },
            ToastButton(title: String(localized: "Remove")) { [weak self] in
                guard let self else { return }
                do {
                    try await self.connection.removeContactPicture(contactId)
                    self.doneText = String(localized: "Removed their picture")
                } catch {
                    self.problemText = self.describe(error)
                }
            },
        ]
    }

    /// Makes a contact with a name the address book does not have, and names
    /// the group by it.
    func nameWithNewContact(_ group: PhotoFaceGroup, contactName: String) async -> PhotoFaceGroup? {
        do {
            let contact = try await connection.createNamedContact(PhotoNaming.trimmedName(contactName))
            return await name(group, contactId: contact.id)
        } catch {
            problemText = describe(error)
            return nil
        }
    }

    func removeName(of group: PhotoFaceGroup) async -> PhotoFaceGroup? {
        do {
            let unnamed = try await connection.unnamePhotoFaceGroup(group.id)
            replaceFaceGroup(unnamed)
            doneText = String(localized: "Removed the name")
            return unnamed
        } catch {
            problemText = describe(error)
            return nil
        }
    }

    /// Puts another group into this one, which stays; the group as it now is.
    func merge(_ group: PhotoFaceGroup, with other: PhotoFaceGroup) async -> PhotoFaceGroup? {
        do {
            let merged = try await connection.mergePhotoFaceGroups([group.id, other.id])
            faceGroups?.removeAll { $0.id == other.id }
            forgetListing(.faceGroup(other.id))
            replaceFaceGroup(merged)
            doneText = String(localized: "Merged the two people")
            return merged
        } catch {
            problemText = describe(error)
            return nil
        }
    }

    func hide(_ group: PhotoFaceGroup, isHidden: Bool) async -> PhotoFaceGroup? {
        do {
            let changed = try await connection.hidePhotoFaceGroup(group.id, isHidden: isHidden)
            replaceFaceGroup(changed)
            if isHidden, !isHiddenPeopleShown { faceGroups?.removeAll { $0.id == changed.id } }
            doneText = isHidden ? String(localized: "Hidden from People") : String(localized: "Shown in People again")
            return changed
        } catch {
            problemText = describe(error)
            return nil
        }
    }

    /// "This is not them": takes a face out of its group. True when it went.
    func removeFace(_ face: PhotoFace) async -> Bool {
        do {
            _ = try await connection.removePhotoFaceFromGroup(face.id)
            forgetListing(.faceGroup(face.photoFaceGroupId))
            await reloadFaceGroups(isQuiet: true)
            doneText = String(localized: "Took the face out of this person")
            return true
        } catch {
            problemText = describe(error)
            return false
        }
    }

    /// Switches the finding of faces on or off. Off deletes every face and
    /// group, so the people shown are gone with them.
    func setFaceRecognition(isEnabled: Bool) async -> Bool {
        do {
            try await connection.setPhotoFaceRecognition(isEnabled: isEnabled)
            if !isEnabled {
                faceGroups = []
                forgetListings { $0.photoFaceGroupId != nil }
            }
            await loadLibrary()
            doneText = isEnabled ? String(localized: "Finding faces in your photos") : String(localized: "Stopped finding faces")
            return true
        } catch {
            problemText = describe(error)
            return false
        }
    }
}

// MARK: A face

/// FacePictureView is the round picture of a face. The server cuts it from the
/// photo and it needs the app's sign-in, so it is read through the app's
/// connection and its preview cache, never by an address alone. An initial
/// holds the place until it arrives, or when there is none.
struct FacePictureView: View {
    let facePictureUrl: String
    let name: String
    let connection: Connection
    @State private var picture: UIImage?

    init(facePictureUrl: String, name: String, connection: Connection) {
        self.facePictureUrl = facePictureUrl
        self.name = name
        self.connection = connection
        _picture = State(initialValue: facePictureUrl.isEmpty ? nil : PhotoImages.cachedPicture(path: facePictureUrl, accountKey: connection.accountKey))
    }

    var body: some View {
        Color(.secondarySystemBackground)
            .aspectRatio(1, contentMode: .fit)
            .overlay {
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFill()
                } else {
                    Text(initial)
                        .font(.title2.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .minimumScaleFactor(0.5)
                }
            }
            .clipShape(.circle)
            .accessibilityHidden(true)
            .task(id: facePictureUrl) {
                guard !facePictureUrl.isEmpty else {
                    picture = nil
                    return
                }
                if let loaded = await PhotoImages.picture(path: facePictureUrl, connection: connection) { picture = loaded }
            }
    }

    private var initial: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines).first.map { String($0).uppercased() } ?? "?"
    }
}

/// PhotoPersonWords writes what the people screens say.
enum PhotoPersonWords {
    /// A group's name, or what an unnamed one is called.
    static func name(of group: PhotoFaceGroup) -> String {
        PhotoNaming.shownName(of: group) ?? String(localized: "Unnamed person")
    }

    /// What goes under a person's name on their own screen: how many photos
    /// they are in, after "You" when the person is the one looking.
    static func subtitle(of group: PhotoFaceGroup) -> String {
        let photoCountText = PhotoAlbumWords.photoCount(group.photoCount)
        return group.isOwnContact ? String(localized: "You") + " · " + photoCountText : photoCountText
    }
}

/// PhotoPersonNameText is a person's name and, after it in secondary text,
/// "You" when the person is the one looking. The caller sets the font and the
/// color of the name; the name gives way before "You" when the line is short.
struct PhotoPersonNameText: View {
    let name: String
    let isOwnContact: Bool
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// At the accessibility sizes a name wraps instead of being cut short,
    /// since a name cut after two letters no longer says who it is.
    private var nameLineLimit: Int {
        dynamicTypeSize.isAccessibilitySize ? 3 : 1
    }

    var body: some View {
        HStack(spacing: 4) {
            Text(name)
                .lineLimit(nameLineLimit)
            if isOwnContact {
                Text("You")
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .layoutPriority(1)
            }
        }
    }
}

// MARK: The people

/// PhotoPeopleView is the People section: a round picture for each person with
/// their name under it, and what to do when there is nobody to show.
struct PhotoPeopleView: View {
    let model: PhotosModel
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// At the accessibility sizes the columns are two, wide enough for a name
    /// and a count to be read whole, and each face grows with its column.
    private var gridColumns: [GridItem] {
        if dynamicTypeSize.isAccessibilitySize {
            return [GridItem(.flexible(), spacing: 16), GridItem(.flexible(), spacing: 16)]
        }
        return [GridItem(.adaptive(minimum: 104), spacing: 16)]
    }

    var body: some View {
        Group {
            if let groups = model.faceGroups {
                if groups.isEmpty {
                    ScrollView {
                        emptyView
                            .containerRelativeFrame(.vertical)
                    }
                } else {
                    ScrollView {
                        LazyVGrid(columns: gridColumns, spacing: 20) {
                            ForEach(groups) { group in
                                NavigationLink {
                                    PhotoPersonScreen(model: model, photoFaceGroupId: group.id)
                                } label: {
                                    PhotoPersonCell(group: group, connection: model.connection)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding()
                    }
                }
            } else if let failureText = model.faceGroupsFailureText {
                ScrollView {
                    PhotosFailedView(failureText: failureText) {
                        Task { await model.reloadFaceGroups() }
                    }
                    .containerRelativeFrame(.vertical)
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .onChange(of: model.isHiddenPeopleShown) { _, _ in
            Task { await model.reloadFaceGroups() }
        }
    }

    @ViewBuilder
    private var emptyView: some View {
        if let library = model.library, !library.isFaceRecognitionAvailable {
            ContentUnavailableView {
                Label("Faces are not found here", systemImage: "person.crop.circle.badge.exclamationmark")
            } description: {
                Text(library.faceRecognitionUnavailableMessage.isEmpty ? String(localized: "This server cannot find faces in photos.") : library.faceRecognitionUnavailableMessage.asReasonSentence)
            }
        } else if let library = model.library, !library.isPhotoFaceRecognitionEnabled {
            ContentUnavailableView {
                Label("Finding faces is off", systemImage: "person.crop.circle")
            } description: {
                Text("Turn it on to see the people in your photos.")
            } actions: {
                Button("Find faces in my photos") {
                    Task { _ = await model.setFaceRecognition(isEnabled: true) }
                }
                .buttonStyle(.borderedProminent)
            }
        } else {
            ContentUnavailableView {
                Label("No people yet", systemImage: "person.2")
            } description: {
                Text("A person appears here once their face has been found in at least three photos.")
            }
        }
    }
}

/// PhotoPersonCell is one person in the grid: the round picture, the name
/// under it, and in muted text how many photos they are in.
struct PhotoPersonCell: View {
    let group: PhotoFaceGroup
    let connection: Connection
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// A count such as "6 photos" wraps rather than ending in an ellipsis at
    /// the accessibility sizes.
    private var countLineLimit: Int {
        dynamicTypeSize.isAccessibilitySize ? 2 : 1
    }

    var body: some View {
        VStack(spacing: 4) {
            FacePictureView(facePictureUrl: group.facePictureUrl, name: PhotoPersonWords.name(of: group), connection: connection)
                .padding(.bottom, 4)
            PhotoPersonNameText(name: PhotoPersonWords.name(of: group), isOwnContact: group.isOwnContact)
                .font(.subheadline.weight(group.isNamed ? .semibold : .regular))
                .foregroundStyle(group.isNamed ? .primary : .secondary)
            Text(PhotoAlbumWords.photoCount(group.photoCount))
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(countLineLimit)
            // A hidden person is dimmed, and says so in words too: dimming alone
            // is not seen by everybody.
            if group.isHidden {
                Label("Hidden", systemImage: "eye.slash")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(countLineLimit)
            }
        }
        .multilineTextAlignment(.center)
        .opacity(group.isHidden ? 0.5 : 1)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

// MARK: One person

/// PhotoPersonScreen is the photos of one person, in the library's grid and
/// viewer. Its menu names the person or changes the name, takes the name away,
/// merges them with another, or hides them from People.
struct PhotoPersonScreen: View {
    let model: PhotosModel
    /// The group shown. It is the one that was opened until naming it puts it
    /// into a group the contact named already, which is shown from then on.
    @State private var photoFaceGroupId: String
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var group: PhotoFaceGroup?
    @State private var opened: OpenedPhoto?
    @State private var isSelecting = false
    @State private var selectedPhotoIds: Set<String> = []
    @State private var isConfirmingTrashOfSelection = false
    @State private var isNaming = false
    @State private var isMerging = false
    @State private var isExplainingContacts = false

    init(model: PhotosModel, photoFaceGroupId: String) {
        self.model = model
        _photoFaceGroupId = State(initialValue: photoFaceGroupId)
    }

    private var key: PhotoListKey { .faceGroup(photoFaceGroupId) }
    private var photos: [Photo] { model.photos(in: key) }
    private var selectedPhotos: [Photo] { photos.filter { selectedPhotoIds.contains($0.id) } }
    /// The group as the model has it, which is newer than the one read here
    /// once the person has changed something from another screen.
    private var current: PhotoFaceGroup? { model.faceGroups?.first { $0.id == photoFaceGroupId } ?? group }

    var body: some View {
        content
            .navigationTitle(isSelecting ? selectionTitle : (current.map { PhotoPersonWords.name(of: $0) } ?? ""))
            .navigationSubtitle(isSelecting ? "" : (current.map { PhotoPersonWords.subtitle(of: $0) } ?? ""))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbarContent }
            .task(id: photoFaceGroupId) { await model.load(key) }
            .task(id: photoFaceGroupId) { await readGroup() }
            .task(id: model.hasWaitingPhotos(in: key)) {
                if model.hasWaitingPhotos(in: key) { await model.followWaitingPhotos(in: key) }
            }
            .refreshable {
                await model.reload(key)
                await readGroup()
            }
            .fullScreenCover(item: $opened) { opened in
                PhotoViewer(model: model, listKey: key, openedPhotoId: opened.id)
            }
            .albumTitleAlert(model: model, scope: .album, isActive: opened == nil)
            .sheet(isPresented: $isNaming) {
                if let current {
                    NamePersonSheet(model: model, group: current) { named in
                        // Merged into the group the contact named already, that group is this person now.
                        group = named
                        photoFaceGroupId = named.id
                    }
                }
            }
            .sheet(isPresented: $isMerging) {
                if let current {
                    MergePersonSheet(model: model, group: current) { group = $0 }
                }
            }
            .alert("Name this person", isPresented: $isExplainingContacts) {
                Button("OK", role: .cancel) {}
            } message: {
                Text("Naming people means choosing a contact from your address book, and Contacts is not turned on for you. Ask whoever runs this server to turn on Contacts.")
            }
            .onDisappear { model.dropPhotosNoLongerFavorite() }
            // "Photos taken near here" is answered by the library, so this screen goes away.
            .onChange(of: model.photoFilter.photoNearFilter) { _, photoNearFilter in
                if photoNearFilter != nil { dismiss() }
            }
    }

    private var selectionTitle: String {
        selectedPhotoIds.isEmpty ? String(localized: "Select photos") : String(localized: "\(selectedPhotoIds.count) Selected")
    }

    private func endSelection() {
        isSelecting = false
        selectedPhotoIds = []
    }

    /// Reads the person. The photos below have their own failure text; this is
    /// the person's name and picture, and a failure leaves what was known and
    /// tells by the toast.
    private func readGroup() async {
        do {
            group = try await model.connection.photoFaceGroup(photoFaceGroupId)
        } catch {
            guard !Task.isCancelled else { return }
            model.problemText = model.describe(error)
        }
    }

    private func startNaming() {
        if app.isSectionAvailable(.contacts) { isNaming = true } else { isExplainingContacts = true }
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
            if let current {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button(current.isNamed ? LocalizedStringKey("Change name") : LocalizedStringKey("Name this person"), systemImage: "pencil") { startNaming() }
                        if current.isNamed {
                            Button("Remove name", systemImage: "person.crop.circle.badge.minus") {
                                Task { if let changed = await model.removeName(of: current) { group = changed } }
                            }
                        }
                        Button("Merge with…", systemImage: "person.2") { isMerging = true }
                        Button(
                            current.isHidden ? LocalizedStringKey("Show in People") : LocalizedStringKey("Hide from People"),
                            systemImage: current.isHidden ? "eye" : "eye.slash"
                        ) {
                            Task { if let changed = await model.hide(current, isHidden: !current.isHidden) { group = changed } }
                        }
                    } label: {
                        Label("More", systemImage: "ellipsis")
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
                        Label("No photos of this person", systemImage: "person.crop.circle")
                    } description: {
                        Text("Their photos are gone from your library, or were moved to the Trash.")
                    }
                    .containerRelativeFrame(.vertical)
                }
            } else {
                ScrollView {
                    personHeader
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

    /// The person's face, large, and an invitation to name them while they are
    /// nobody's.
    @ViewBuilder
    private var personHeader: some View {
        if let current {
            VStack(spacing: 12) {
                FacePictureView(facePictureUrl: current.facePictureUrl, name: PhotoPersonWords.name(of: current), connection: model.connection)
                    .frame(width: 96, height: 96)
                if current.isHidden {
                    Label("Hidden from People", systemImage: "eye.slash")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                if !current.isNamed {
                    Button("Name this person", systemImage: "person.crop.circle.badge.plus") { startNaming() }
                        .buttonStyle(.bordered)
                        .controlSize(.large)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 16)
        }
    }
}

// MARK: Naming

/// NamePersonSheet names a person by choosing a contact from the address book,
/// as the server asks, or by typing a name the address book does not have yet:
/// that makes a contact with it first.
struct NamePersonSheet: View {
    let model: PhotosModel
    let group: PhotoFaceGroup
    let onNamed: (PhotoFaceGroup) -> Void
    @State private var searchText = ""
    @State private var contacts: [ContactSummary] = []
    @State private var hasReadContacts = false
    @State private var isNaming = false
    /// Why the name was not saved, or the contacts not read: shown in the
    /// sheet, since the toast of the screen under it cannot be seen.
    @State private var failureText: String?
    @Environment(\.dismiss) private var dismiss

    /// The contacts that name some other person: choosing one merges the two.
    private var linkedContactIds: Set<String> {
        Set((model.faceGroups ?? []).filter { $0.id != group.id && !$0.contactId.isEmpty }.map(\.contactId))
    }

    var body: some View {
        NavigationStack {
            List {
                if let failureText {
                    Section {
                        Label(failureText, systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.red)
                    }
                }
                Section {
                    if PhotoNaming.offersNewContact(typed: searchText, contacts: contacts) {
                        Button {
                            choose { await model.nameWithNewContact(group, contactName: searchText) }
                        } label: {
                            Label("New contact “\(PhotoNaming.trimmedName(searchText))”", systemImage: "person.badge.plus")
                        }
                        .fullWidthRowSeparator()
                    }
                    ForEach(contacts) { contact in
                        Button {
                            choose { await model.name(group, contactId: contact.id) }
                        } label: {
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(contact.shownName)
                                        .foregroundStyle(.primary)
                                    if let email = contact.emails?.first, email != contact.shownName {
                                        Text(email)
                                            .font(.footnote)
                                            .foregroundStyle(.secondary)
                                    }
                                    if linkedContactIds.contains(contact.id) {
                                        Text("Already names another person; choosing it merges the two")
                                            .font(.footnote)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                                Spacer()
                                if contact.id == group.contactId {
                                    Image(systemName: "checkmark")
                                        .foregroundStyle(.tint)
                                        .accessibilityLabel("Current name")
                                }
                            }
                        }
                    }
                    if hasReadContacts, contacts.isEmpty, searchText.isEmpty {
                        Text("No contacts yet. Type a name to make one.")
                            .foregroundStyle(.secondary)
                    }
                } footer: {
                    Text("Choose the contact who is in these photos. The name comes from your address book.")
                }
            }
            .navigationTitle(group.isNamed ? "Change name" : "Name this person")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $searchText, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search or type a new name")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .disabled(isNaming)
            .overlay {
                if isNaming { ProgressView() }
            }
            .task(id: searchText) {
                // A pause in typing is waited for; the first read is at once.
                if !searchText.isEmpty { try? await Task.sleep(for: .milliseconds(250)) }
                guard !Task.isCancelled else { return }
                do {
                    contacts = try await model.connection.photoNamingContacts(matching: searchText)
                    hasReadContacts = true
                    failureText = nil
                } catch {
                    guard !Task.isCancelled else { return }
                    failureText = model.describe(error)
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func choose(_ name: @escaping () async -> PhotoFaceGroup?) {
        guard !isNaming else { return }
        isNaming = true
        failureText = nil
        Task {
            let named = await name()
            isNaming = false
            if let named {
                onNamed(named)
                dismiss()
            } else {
                // The model told the screen under this sheet; say it here.
                failureText = model.problemText ?? String(localized: "The name could not be saved. Try again.")
                model.problemText = nil
            }
        }
    }
}

// MARK: Merging

/// MergePersonSheet picks the person who is the same as this one. This person
/// stays and takes the other's faces, and the other's name too when it has
/// none. It asks again before it merges, since it cannot be undone.
struct MergePersonSheet: View {
    let model: PhotosModel
    let group: PhotoFaceGroup
    let onMerged: (PhotoFaceGroup) -> Void
    @State private var others: [PhotoFaceGroup]?
    @State private var chosenId: String?
    @State private var isConfirming = false
    @State private var isMerging = false
    /// Why the people could not be read, or the merge did not happen: shown in
    /// the sheet, since the toast of the screen under it cannot be seen.
    @State private var failureText: String?
    @Environment(\.dismiss) private var dismiss

    private var chosen: PhotoFaceGroup? { others?.first { $0.id == chosenId } }

    var body: some View {
        NavigationStack {
            Group {
                if others == nil, let failureText {
                    ContentUnavailableView {
                        Label("The people could not be read", systemImage: "wifi.exclamationmark")
                    } description: {
                        Text(failureText)
                    } actions: {
                        Button("Try again") { Task { await loadOthers() } }
                    }
                } else if let others {
                    if others.isEmpty {
                        ContentUnavailableView {
                            Label("Nobody to merge with", systemImage: "person.2")
                        } description: {
                            Text("There is nobody else to merge with yet.")
                        }
                    } else {
                        List {
                            if let failureText {
                                Section {
                                    Label(failureText, systemImage: "exclamationmark.triangle")
                                        .foregroundStyle(.red)
                                }
                            }
                            Section {
                                ForEach(others) { other in
                                    Button {
                                        chosenId = other.id
                                    } label: {
                                        HStack(spacing: 12) {
                                            FacePictureView(facePictureUrl: other.facePictureUrl, name: PhotoPersonWords.name(of: other), connection: model.connection)
                                                .frame(width: 44, height: 44)
                                            VStack(alignment: .leading, spacing: 2) {
                                                Text(PhotoPersonWords.name(of: other))
                                                    .foregroundStyle(.primary)
                                                Text(PhotoAlbumWords.photoCount(other.photoCount))
                                                    .font(.footnote)
                                                    .foregroundStyle(.secondary)
                                            }
                                            Spacer()
                                            if other.id == chosenId {
                                                Image(systemName: "checkmark")
                                                    .foregroundStyle(.tint)
                                                    .accessibilityLabel("Chosen")
                                            }
                                        }
                                    }
                                    .accessibilityAddTraits(other.id == chosenId ? .isSelected : [])
                                }
                            } footer: {
                                Text("Choose the person who is the same as this one. Their photos are shown together as one person.")
                            }
                        }
                    }
                } else {
                    ProgressView()
                }
            }
            .navigationTitle("Merge with…")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Merge") { isConfirming = true }
                        .disabled(chosen == nil || isMerging)
                        .confirmationDialog(
                            "Merge \(chosen.map { PhotoPersonWords.name(of: $0) } ?? "") into \(PhotoPersonWords.name(of: group))?", isPresented: $isConfirming,
                            titleVisibility: .visible
                        ) {
                            Button("Merge", role: .destructive) { merge() }
                        } message: {
                            Text("This cannot be undone.")
                        }
                }
            }
            .task { await loadOthers() }
        }
        .presentationDetents([.medium, .large])
    }

    private func loadOthers() async {
        do {
            let all = try await model.connection.photoFaceGroups(isHiddenIncluded: true)
            others = all.filter { $0.id != group.id }
            failureText = nil
        } catch {
            guard !Task.isCancelled else { return }
            failureText = model.describe(error)
        }
    }

    private func merge() {
        guard let chosen, !isMerging else { return }
        isMerging = true
        failureText = nil
        Task {
            let merged = await model.merge(group, with: chosen)
            isMerging = false
            if let merged {
                onMerged(merged)
                dismiss()
            } else {
                // The model told the screen under this sheet; say it here.
                failureText = model.problemText ?? String(localized: "The people could not be merged. Try again.")
                model.problemText = nil
            }
        }
    }
}

// MARK: The switch

/// PhotoFacesSettingsView is the person's own switch for finding faces in their
/// photos. Turning it off deletes the faces found so far and the names given to
/// them, so that asks first; turning it on has the photos read again.
struct PhotoFacesSettingsView: View {
    let connection: Connection
    @State private var library: PhotoLibrary?
    @State private var isBusy = false
    @State private var isConfirmingTurnOff = false
    @State private var doneText: String?
    @State private var problemText: String?
    /// Why the switch could not be read; nil when it could.
    @State private var libraryFailureText: String?

    var body: some View {
        Form {
            if let libraryFailureText {
                Section {
                    // A problem is orange with its symbol, never the green of the accent.
                    Label {
                        Text("This setting could not be read.")
                    } icon: {
                        Image(systemName: "exclamationmark.triangle").foregroundStyle(.orange)
                    }
                    .fullWidthRowSeparator()
                    Text(libraryFailureText)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Button("Try again", systemImage: "arrow.clockwise") { Task { await readLibrary() } }
                }
            }
            Section {
                Toggle(
                    "Find faces in my photos",
                    isOn: Binding(
                        get: { library?.isPhotoFaceRecognitionEnabled ?? false },
                        set: { isOn in
                            if isOn {
                                Task { await change(isEnabled: true) }
                            } else {
                                isConfirmingTurnOff = true
                            }
                        })
                )
                .disabled(library == nil || library?.isFaceRecognitionAvailable != true || isBusy)
                .confirmationDialog("Stop finding faces?", isPresented: $isConfirmingTurnOff, titleVisibility: .visible) {
                    Button("Turn off and delete", role: .destructive) { Task { await change(isEnabled: false) } }
                } message: {
                    Text("This deletes every face found in your photos and the names you gave to people. Your photos and your contacts stay. Turning it on again finds the faces again, but not the names.")
                }
            } footer: {
                Text(footerText)
            }
        }
        .navigationTitle("People in Photos")
        .navigationBarTitleDisplayMode(.inline)
        .toast(done: $doneText, problem: $problemText)
        .task { await readLibrary() }
    }

    private var footerText: String {
        guard let library else { return "" }
        if !library.isFaceRecognitionAvailable {
            return library.faceRecognitionUnavailableMessage.isEmpty ? String(localized: "This server cannot find faces in photos.") : library.faceRecognitionUnavailableMessage.asReasonSentence
        }
        return String(localized: "Finds the people in your photos and groups them, so you can browse by person. Turning this off deletes the faces found so far and the names given to them. Your photos are not changed.")
    }

    private func readLibrary() async {
        do {
            library = try await connection.photoLibrary()
            libraryFailureText = nil
        } catch {
            guard !Task.isCancelled else { return }
            libraryFailureText = AppModel.shared.describe(error)
        }
    }

    private func change(isEnabled: Bool) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await connection.setPhotoFaceRecognition(isEnabled: isEnabled)
            await readLibrary()
            doneText = isEnabled ? String(localized: "Finding faces in your photos") : String(localized: "Stopped finding faces")
        } catch {
            problemText = AppModel.shared.describe(error)
        }
    }
}
