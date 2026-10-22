import SwiftUI
import TeaNodeKit

// The sections of a person's card, one for each part of the app that has
// something of theirs. A section is left out when the server says it is
// unavailable; one that is not shared says so in a line, one found through a
// contact the person has not linked offers to link one, and one that failed
// says so while the rest still show. Each ends in a row that opens its part.

/// PersonCardStateRows is what a section says when it has no items: that the
/// account does not share this part, or that it could not be read. A section
/// waiting for a contact says nothing: the card asks for one contact, once.
struct PersonCardStateRows: View {
    let sectionState: PersonCardSectionState
    let notShared: Text

    var body: some View {
        switch sectionState {
        case .notShared:
            notShared.foregroundStyle(.secondary)
        case .failed:
            Text("This could not be loaded").foregroundStyle(.secondary)
        case .shown, .unavailable, .needsContact:
            EmptyView()
        }
    }
}

/// PersonCardShowAllRow is the last row of a section: it opens the part with
/// the person chosen, as Search's Show all opens its part with the words.
struct PersonCardShowAllRow: View {
    let title: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Label("Show all in \(title)", systemImage: "arrow.forward")
                .actionColor()
        }
    }
}

// MARK: - Location

/// PersonCardLocationView is where they are, as Location shows it: the place
/// they are at and since when, how fresh their position is, and today's visits
/// when they share where they have been, with a page of their earlier days and
/// places.
struct PersonCardLocationView: View {
    let card: PersonCard
    let connection: Connection
    let isInSheet: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        if let section = card.location, section.sectionState.isListed {
            Section {
                rows(of: section)
            } header: {
                Text(verbatim: AppSection.location.title)
            }
        }
    }

    @ViewBuilder
    private func rows(of section: PersonCardLocationSection) -> some View {
        if section.sectionState == .shown {
            shownRows(of: section)
        } else {
            PersonCardStateRows(
                sectionState: section.sectionState, notShared: Text("\(card.displayName) does not share where they are with you."))
        }
    }

    @ViewBuilder
    private func shownRows(of section: PersonCardLocationSection) -> some View {
        if let location = section.location {
            PersonCardWhereRow(location: location)
            if location.level == .current {
                Text("\(card.displayName) shares where they are now. Their visits and places are not shared with you.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            // The visit they are in is the row above, so it is not listed again.
            ForEach(otherVisits(of: section, location: location)) { visit in
                LocationVisitRow(visit: visit)
            }
            if location.level == .history {
                // Their earlier days and their places, which the card's few rows leave out.
                NavigationLink {
                    PersonVisitsScreen(connection: connection, userId: location.userId)
                } label: {
                    Label("Visits and places", systemImage: "clock")
                        .foregroundStyle(.primary)
                }
            }
            PersonCardShowAllRow(title: AppSection.location.title) { showOnMap(location.userId) }
        } else {
            Text("No position yet")
                .foregroundStyle(.secondary)
        }
    }

    private func otherVisits(of section: PersonCardLocationSection, location: PersonLocation) -> [PlaceVisit] {
        guard let openVisitId = location.openVisit?.id else { return section.todayVisits }
        return section.todayVisits.filter { $0.id != openVisitId }
    }

    /// Opens Location with them chosen, and the map centered on them.
    private func showOnMap(_ userId: String) {
        app.leavePersonCard(isInSheet: isInSheet) {
            app.openedLocationUserId = userId
            app.openedSection = .location
        }
    }
}

/// PersonCardWhereRow is the place somebody is at, since when, and how long
/// ago the position came.
struct PersonCardWhereRow: View {
    let location: PersonLocation

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: location.openVisit?.place.symbolName ?? "location")
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .frame(width: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: titleText)
                if let detailText {
                    Text(verbatim: detailText)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    /// The place they are at, else how long ago their position came.
    private var titleText: String {
        location.openVisit?.place.shownName ?? freshText
    }

    /// "Since 9:00 AM · 1 minute ago" while they are at a place.
    private var detailText: String? {
        guard let visit = location.openVisit else { return nil }
        let sinceText = LocationText.times(of: visit)
        return sinceText.isEmpty ? freshText : String(localized: "\(sinceText) · \(freshText)")
    }

    /// "3 minutes ago", for their latest position.
    private var freshText: String {
        guard let measured = location.latestPoint?.measuredDate else { return String(localized: "No position yet") }
        // A reading a moment ahead of this iPhone's clock is not in the future.
        return min(measured, Date().addingTimeInterval(-1)).formatted(.relative(presentation: .named, unitsStyle: .wide))
    }
}

// MARK: - Health

/// PersonCardHealthView is the health they share: each kind with its latest
/// day, steps first. A kind opens its chart in Health.
struct PersonCardHealthView: View {
    let card: PersonCard
    let isInSheet: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        if let section = card.health, section.sectionState.isListed {
            Section {
                rows(of: section)
            } header: {
                Text(verbatim: HealthWords.title)
            }
        }
    }

    @ViewBuilder
    private func rows(of section: PersonCardHealthSection) -> some View {
        if section.sectionState == .shown {
            shownRows(of: section)
        } else {
            PersonCardStateRows(
                sectionState: section.sectionState, notShared: Text("\(card.displayName) shares no Health data with you yet."))
        }
    }

    @ViewBuilder
    private func shownRows(of section: PersonCardHealthSection) -> some View {
        if section.healthTypes.isEmpty {
            Text("No Health data yet")
                .foregroundStyle(.secondary)
        } else {
            ForEach(section.healthTypes) { type in
                Button {
                    open(healthType: type.healthType)
                } label: {
                    PersonCardHealthRow(type: type)
                }
            }
            if section.hasMore {
                PersonCardShowAllRow(title: HealthWords.title) { open(healthType: nil) }
            }
        }
    }

    /// Opens Health on their data, on one kind's chart when one is named.
    private func open(healthType: String?) {
        app.leavePersonCard(isInSheet: isInSheet) {
            app.openedHealthLink = HealthLink(healthType: healthType, isSettings: false, userId: card.person?.userId)
            app.openedSection = .health
        }
    }
}

/// PersonCardHealthRow is one kind of data: its name, the day it is of, and
/// that day's value, as the Health list writes them.
struct PersonCardHealthRow: View {
    let type: HealthTypeView

    var body: some View {
        LabeledContent {
            Text(verbatim: HealthText.latestAmountText(of: type) ?? "")
                .monospacedDigit()
                .foregroundStyle(.primary)
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: HealthWords.typeName(healthType: type.healthType, serverName: type.typeName))
                    .foregroundStyle(.primary)
                if let dayText {
                    Text(verbatim: dayText)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .contentShape(.rect)
    }

    /// "Today" only when the latest day is today; else the day.
    private var dayText: String? {
        guard let summaryDay = type.latestDaySummary?.summaryDay else { return nil }
        if PersonCardHealthSection.isToday(type, today: Date(), calendar: .current) { return String(localized: "Today") }
        return HealthText.shortDayText(summaryDay)
    }
}

// MARK: - Finance

/// PersonCardFinanceView is the finance accounts they share, each as the level
/// it was shared at shows it. Show all opens Finance on them. The agent is
/// never shown this.
struct PersonCardFinanceView: View {
    let card: PersonCard
    let isInSheet: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        if let section = card.finance, section.sectionState.isListed {
            Section {
                rows(of: section)
            } header: {
                Text(verbatim: AppSection.finance.title)
            }
        }
    }

    @ViewBuilder
    private func rows(of section: PersonCardFinanceSection) -> some View {
        if section.sectionState == .shown {
            ForEach(section.financeAccounts) { account in
                SharedFinanceAccountRow(account: account)
            }
            PersonCardShowAllRow(title: AppSection.finance.title) { open() }
        } else {
            PersonCardStateRows(
                sectionState: section.sectionState, notShared: Text("\(card.displayName) shares no finance account with you yet."))
        }
    }

    /// Opens Finance on what they share.
    private func open() {
        app.leavePersonCard(isInSheet: isInSheet) {
            app.openedFinanceSharerId = card.person?.userId
            app.openedSection = .finance
        }
    }
}

// MARK: - Photos

/// PersonCardPhotosView is photos of them in the person's library, as a row of
/// pictures, and the albums they shared. Photos of a person are found through
/// the person's contact for them.
struct PersonCardPhotosView: View {
    let card: PersonCard
    let connection: Connection
    let isInSheet: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        if let section = card.photos, section.photosState.isListed || section.albumsState.isListed {
            Section {
                photoRows(of: section)
                albumRows(of: section)
                moreRows(of: section)
            } header: {
                Text(verbatim: AppSection.photos.title)
            }
        }
    }

    @ViewBuilder
    private func photoRows(of section: PersonCardPhotosSection) -> some View {
        if section.photosState == .shown {
            if !section.photos.isEmpty {
                PersonCardPhotoStrip(photos: section.photos, connection: connection, onOpen: stripAction)
                    .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
            } else if section.albums.isEmpty {
                Text("Nothing yet.")
                    .foregroundStyle(.secondary)
            }
        } else {
            PersonCardStateRows(sectionState: section.photosState, notShared: Text("Nothing yet."))
        }
    }

    @ViewBuilder
    private func albumRows(of section: PersonCardPhotosSection) -> some View {
        if section.albumsState == .shown {
            ForEach(section.albums) { album in
                Button {
                    openAlbums()
                } label: {
                    PersonCardAlbumRow(album: album, connection: connection)
                }
            }
        } else if section.albumsState == .failed {
            PersonCardStateRows(sectionState: .failed, notShared: Text("Nothing yet."))
        }
    }

    @ViewBuilder
    private func moreRows(of section: PersonCardPhotosSection) -> some View {
        if section.hasMorePhotos, let person = card.person {
            PersonCardShowAllRow(title: AppSection.photos.title) { openPhotos(of: person) }
        }
        if section.hasMoreAlbums {
            PersonCardShowAllRow(title: String(localized: "Albums")) { openAlbums() }
        }
    }

    /// Tapping a picture opens their photos, when they are an account the
    /// photos can be filtered by.
    private var stripAction: (() -> Void)? {
        guard let person = card.person else { return nil }
        return { openPhotos(of: person) }
    }

    private func openPhotos(of person: CircleMember) {
        app.leavePersonCard(isInSheet: isInSheet) {
            app.openedPhotoPerson = OpenedPhotoPerson(userId: person.userId, name: person.displayName)
        }
    }

    private func openAlbums() {
        app.leavePersonCard(isInSheet: isInSheet) {
            app.openedPhotoSection = .albums
        }
    }
}

/// PersonCardPhotoStrip is a few photos side by side, as small as a grid cell
/// is; the row scrolls sideways when they do not fit.
struct PersonCardPhotoStrip: View {
    let photos: [Photo]
    let connection: Connection
    /// What a tap on a picture does; nil leaves them as pictures only.
    let onOpen: (() -> Void)?

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(photos) { photo in
                    tile(photo)
                }
            }
        }
    }

    @ViewBuilder
    private func tile(_ photo: Photo) -> some View {
        if let onOpen {
            Button(action: onOpen) {
                PersonCardPhotoTile(photo: photo, connection: connection)
            }
            .buttonStyle(.plain)
        } else {
            PersonCardPhotoTile(photo: photo, connection: connection)
        }
    }
}

/// PersonCardPhotoTile is one photo's small preview in a square.
struct PersonCardPhotoTile: View {
    let photo: Photo
    let connection: Connection
    @State private var picture: UIImage?

    /// A picture, not text, so it keeps its size at the largest text size.
    private let side: CGFloat = 76

    init(photo: Photo, connection: Connection) {
        self.photo = photo
        self.connection = connection
        _picture = State(initialValue: photo.gridPreviewPath.flatMap { PhotoImages.cachedPicture(path: $0, accountKey: connection.accountKey) })
    }

    var body: some View {
        Color(.secondarySystemBackground)
            .frame(width: side, height: side)
            .overlay {
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFill()
                }
            }
            .clipShape(.rect(cornerRadius: 8))
            .accessibilityLabel(photo.placeName.isEmpty ? String(localized: "Photo") : photo.placeName)
            .task(id: photo.gridPreviewPath) {
                guard let path = photo.gridPreviewPath else {
                    picture = nil
                    return
                }
                if let loaded = await PhotoImages.picture(path: path, connection: connection) { picture = loaded }
            }
    }
}

/// PersonCardAlbumRow is an album they shared: its cover, its title and how
/// many photos it holds.
struct PersonCardAlbumRow: View {
    let album: PhotoAlbum
    let connection: Connection
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

// MARK: - Mail

/// PersonCardMailView is the person's recent mail with them, from or to any
/// address on their contact. A mail opens as a Search result for it does.
struct PersonCardMailView: View {
    let card: PersonCard
    let isInSheet: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        if let section = card.mail, section.sectionState.isListed {
            Section {
                rows(of: section)
            } header: {
                Text(verbatim: AppSection.mail.title)
            }
        }
    }

    @ViewBuilder
    private func rows(of section: PersonCardMailSection) -> some View {
        if section.sectionState == .shown {
            if section.mails.isEmpty {
                Text("Nothing yet.")
                    .foregroundStyle(.secondary)
            }
            ForEach(section.mails) { mail in
                Button {
                    app.leavePersonCard(isInSheet: isInSheet) { app.openedMailItemId = mail.mailboxItemId }
                } label: {
                    SearchResultRow(result: result(of: mail))
                }
            }
            if section.hasMore {
                PersonCardShowAllRow(title: AppSection.mail.title) { showAll() }
            }
        } else {
            PersonCardStateRows(sectionState: section.sectionState, notShared: Text("Nothing yet."))
        }
    }

    /// The mail as a line of a search: its subject, who sent it, and when. The
    /// agent's one line about it follows the sender once it has read the mail.
    private func result(of mail: PersonCardMail) -> SearchResult {
        let detailText = mail.snippet.isEmpty ? mail.senderText : String(localized: "\(mail.senderText) · \(mail.snippet)")
        return SearchResult(
            searchArea: SearchArea.mail.rawValue, targetId: mail.mailboxItemId,
            titleText: mail.subject.isEmpty ? String(localized: "(no subject)") : mail.subject, detailText: detailText,
            happenedAt: mail.receivedAt)
    }

    /// Opens Mail searched for their address, or their name when the contact
    /// has none.
    private func showAll() {
        let words = card.mailAddress ?? card.displayName
        app.leavePersonCard(isInSheet: isInSheet) {
            app.openedPartSearch = PartSearch(area: .mail, words: words)
            app.openedSection = .mail
        }
    }
}

// MARK: - Recordings

/// PersonCardRecordingsView is the recordings they speak in. A recording opens
/// where Search opens one.
struct PersonCardRecordingsView: View {
    let card: PersonCard
    let isInSheet: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        if let section = card.recordings, section.sectionState.isListed {
            Section {
                rows(of: section)
            } header: {
                Text(verbatim: AppSection.recordings.title)
            }
        }
    }

    @ViewBuilder
    private func rows(of section: PersonCardRecordingSection) -> some View {
        if section.sectionState == .shown {
            if section.recordings.isEmpty {
                Text("Nothing yet.")
                    .foregroundStyle(.secondary)
            }
            ForEach(section.recordings) { recording in
                Button {
                    app.leavePersonCard(isInSheet: isInSheet) { app.show(path: "/recordings/\(recording.recordingId)") }
                } label: {
                    SearchResultRow(result: result(of: recording))
                }
            }
            if section.hasMore {
                PersonCardShowAllRow(title: AppSection.recordings.title) { showAll() }
            }
        } else {
            PersonCardStateRows(sectionState: section.sectionState, notShared: Text("Nothing yet."))
        }
    }

    private func result(of recording: PersonCardRecording) -> SearchResult {
        let passageCount = recording.spokenPassageCount
        let detailText = passageCount == 1 ? String(localized: "Speaks in 1 passage") : String(localized: "Speaks in \(passageCount) passages")
        return SearchResult(
            searchArea: SearchArea.recordings.rawValue, targetId: recording.recordingId, titleText: recording.title, detailText: detailText,
            happenedAt: recording.recordedAt)
    }

    /// Opens Recordings searched for their name.
    private func showAll() {
        let words = card.displayName
        app.leavePersonCard(isInSheet: isInSheet) {
            app.openedPartSearch = PartSearch(area: .recordings, words: words)
            app.openedSection = .recordings
        }
    }
}

// MARK: - Memory

/// PersonCardMemoryView is what memory knows about them: their own page, then
/// the pages linked to it. A page opens in Knowledge.
struct PersonCardMemoryView: View {
    let card: PersonCard
    let isInSheet: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        if let section = card.memory, section.sectionState.isListed {
            Section {
                rows(of: section)
            } header: {
                Text("In memory")
            }
        }
    }

    @ViewBuilder
    private func rows(of section: PersonCardMemorySection) -> some View {
        if section.sectionState == .shown {
            if section.pages.isEmpty {
                Text("Nothing yet.")
                    .foregroundStyle(.secondary)
            }
            ForEach(section.pages) { page in
                Button {
                    app.leavePersonCard(isInSheet: isInSheet) { app.openedKnowledgePath = page.nodePath }
                } label: {
                    SearchResultRow(result: result(of: page))
                }
            }
            if section.hasMore {
                PersonCardShowAllRow(title: AppSection.knowledge.title) { showAll() }
            }
        } else {
            PersonCardStateRows(sectionState: section.sectionState, notShared: Text("Nothing yet."))
        }
    }

    private func result(of page: PersonCardMemoryPage) -> SearchResult {
        SearchResult(searchArea: SearchArea.memory.rawValue, targetId: page.nodePath, titleText: page.nodeName, detailText: detailText(of: page))
    }

    /// The page's first lines, and for a page not tied to the contact that it
    /// was found by the contact's name.
    private func detailText(of page: PersonCardMemoryPage) -> String {
        guard page.isMatchedByName == true else { return page.summary }
        let matchedText = String(localized: "Found by name")
        return page.summary.isEmpty ? matchedText : "\(page.summary) · \(matchedText)"
    }

    /// Opens Knowledge searched for their name.
    private func showAll() {
        let words = card.displayName
        app.leavePersonCard(isInSheet: isInSheet) {
            app.openedPartSearch = PartSearch(area: .memory, words: words)
            app.openedSection = .knowledge
        }
    }
}

// MARK: - Contact details

/// PersonCardDetailsView is the person's contact for them as the contact
/// screen draws it: the addresses to write to, the numbers to call, the places,
/// and the notes, each tapped to do what it says.
struct PersonCardDetailsView: View {
    let card: PersonCard
    let isInSheet: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        if let contact = card.contact {
            if let emails = contact.emails, !emails.isEmpty {
                Section("Email") {
                    ForEach(emails, id: \.self) { email in
                        // Written from TeaNode's own mail, as the dashboard would.
                        Button {
                            app.writeFromPersonCard(to: email, isInSheet: isInSheet)
                        } label: {
                            Label(email, systemImage: "envelope")
                                .actionColor()
                        }
                        .contextMenu {
                            Button("Copy", systemImage: "doc.on.doc") { UIPasteboard.general.string = email }
                        }
                    }
                }
            }
            if let phones = contact.phones, !phones.isEmpty {
                Section("Phone") {
                    ForEach(phones, id: \.self) { phone in
                        ReachRow(
                            text: phone, systemImage: "phone",
                            address: URL(string: "tel:\(phone.filter { $0.isNumber || $0 == "+" })"))
                    }
                }
            }
            if let addresses = contact.addresses, !addresses.isEmpty {
                Section("Address") {
                    ForEach(Array(addresses.enumerated()), id: \.offset) { _, address in
                        let written = ContactWords.written(address)
                        ReachRow(
                            text: written, systemImage: "mappin.and.ellipse",
                            address: written.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed)
                                .flatMap { URL(string: "maps://?q=\($0)") })
                    }
                }
            }
            if let note = contact.note, !note.isEmpty {
                Section("Notes") {
                    Text(note).textSelection(.enabled)
                }
            }
        }
    }
}
