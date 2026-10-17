import SwiftUI
import TeaNodeKit

/// CalendarListSheet is the calendars the person reaches: their own, their
/// circles' and the ones shared with them, each with its color and whose it
/// is, and a switch to show or hide it. What is hidden is hidden on this
/// phone only. Below the list are the ways to share their own calendar and
/// to make a circle's.
struct CalendarListSheet: View {
    let model: CalendarModel
    @Environment(\.dismiss) private var dismiss
    @State private var circlesWithoutCalendar: [PeopleCircle] = []
    @State private var problemText: String?
    @State private var doneText: String?
    @State private var isWorking = false
    #if DEBUG
        /// Whether the share screen is pushed for a screenshot.
        @State private var isShowingDemoShare = false
        @State private var isShowingDemoFindTime = false
    #endif

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(model.calendars) { calendar in
                        Toggle(
                            isOn: Binding(
                                get: { !model.hiddenIds.contains(calendar.id) },
                                set: { isOn in Task { await model.setShown(calendar.id, isShown: isOn) } })
                        ) {
                            CalendarListRow(model: model, calendar: calendar)
                        }
                    }
                } footer: {
                    Text("Calendars you hide here stay hidden on this phone only.")
                }
                if let own = model.calendar {
                    Section {
                        NavigationLink {
                            CalendarShareScreen(model: model, calendar: own)
                        } label: {
                            Label("Share my calendar", systemImage: "person.2")
                        }
                        if let ownUserId = own.ownerUserId {
                            NavigationLink {
                                FindATimeScreen(connection: model.connection, ownUserId: ownUserId)
                            } label: {
                                Label("Find a time", systemImage: "calendar.badge.clock")
                            }
                        }
                    } footer: {
                        Text("Choose who can see your calendar, and how much. An event you mark private is only ever shown to others as busy.")
                    }
                }
                if !circlesWithoutCalendar.isEmpty {
                    Section {
                        ForEach(circlesWithoutCalendar) { circle in
                            Button {
                                make(for: circle)
                            } label: {
                                Label(
                                    circle.knownCircleKind == .family
                                        ? String(localized: "Make a family calendar for \(circle.name)")
                                        : String(localized: "Make a shared calendar for \(circle.name)"),
                                    systemImage: "calendar.badge.plus")
                            }
                            .disabled(isWorking)
                        }
                    } footer: {
                        Text("A calendar of the circle’s own, which everyone in it reads and writes. Events in it cannot have guests.")
                    }
                }
            }
            .navigationTitle("Calendars")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .task { await findCirclesWithoutCalendar() }
            #if DEBUG
                // `-TeaNodeScreen calendar-share-sheet`: Share my calendar, open in the sheet.
                .navigationDestination(isPresented: $isShowingDemoShare) {
                    if let own = model.calendar { CalendarShareScreen(model: model, calendar: own) }
                }
                .task {
                    guard DemoServer.isRequested, DemoServer.screen == "calendar-share-sheet" else { return }
                    try? await Task.sleep(for: .seconds(1))
                    isShowingDemoShare = true
                }
                // `-TeaNodeScreen calendar-find-time`: Find a time, open with its answer.
                .navigationDestination(isPresented: $isShowingDemoFindTime) {
                    if let ownUserId = model.calendar?.ownerUserId { FindATimeScreen(connection: model.connection, ownUserId: ownUserId) }
                }
                .task {
                    guard DemoServer.isRequested, DemoServer.screen == "calendar-find-time" else { return }
                    try? await Task.sleep(for: .seconds(1))
                    isShowingDemoFindTime = true
                }
            #endif
            .toast(done: $doneText, problem: $problemText)
        }
    }

    /// The circles the person is an admin of that have no calendar yet.
    private func findCirclesWithoutCalendar() async {
        guard let circles = try? await model.connection.circles() else { return }
        let withCalendar = Set(model.calendars.compactMap(\.circleId))
        circlesWithoutCalendar = circles.filter { $0.isAdmin && !withCalendar.contains($0.id) }
    }

    private func make(for circle: PeopleCircle) {
        isWorking = true
        Task {
            do {
                try await model.connection.createCircleCalendar(circleId: circle.id)
                doneText = String(localized: "Calendar made")
                await model.reloadCalendars()
                await findCirclesWithoutCalendar()
            } catch {
                problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
            }
            isWorking = false
        }
    }
}

/// CalendarListRow is one calendar in the list: its color, the owner's picture
/// when it is another person's, its name and how much of it the person may
/// use.
struct CalendarListRow: View {
    let model: CalendarModel
    let calendar: AgentCalendar

    var body: some View {
        HStack(spacing: 10) {
            Circle()
                .fill(model.color(of: calendar))
                .frame(width: 12, height: 12)
                .accessibilityHidden(true)
            if !calendar.isOwn, calendar.isCircleCalendar != true {
                AvatarView(name: calendar.ownerName ?? "", avatarUrl: calendar.ownerAvatarUrl, colorIndex: calendar.ownerAvatarColorIndex, size: 24)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(model.displayName(of: calendar))
                    .font(.body)
                if let note = levelNote {
                    Text(note)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    /// What the person may do with it, for a calendar that is not their own.
    private var levelNote: String? {
        // The same names the pickers use for the levels.
        calendar.isOwn ? nil : calendar.level.title
    }
}

extension CalendarModel {
    /// What a list calls a calendar: its own name for the person's and a
    /// circle's, and whose it is for another person's, since everybody's is
    /// called "Calendar".
    func displayName(of calendar: AgentCalendar) -> String {
        if calendar.isNamedForOwner, let owner = calendar.ownerName {
            return String(localized: "\(owner)’s calendar")
        }
        return calendar.name
    }
}

extension CalendarModel {
    /// Whose the calendar an event is in is, to be said beside it: the owner's
    /// name for a calendar shared with the person, the circle's for a circle's,
    /// and nothing for their own.
    func ownerName(of event: CalendarEvent) -> String? {
        calendar(withId: event.calendarId)?.whoseName
    }

    /// An event's title as a list shows it. Time somebody else is busy says
    /// whose it is, so that several people's gray blocks can be told apart.
    func shownTitle(of event: CalendarEvent) -> String {
        if event.isBusy, let owner = ownerName(of: event) { return String(localized: "Busy · \(owner)") }
        return event.shownTitle
    }
}

extension CalendarShareLevel {
    /// What a share at this level is called.
    var title: String {
        switch self {
        case .busy: String(localized: "Busy times only")
        case .titles: String(localized: "Titles")
        case .details: String(localized: "Details")
        case .edit: String(localized: "Details, and can edit")
        case .owner: String(localized: "Owner")
        }
    }

    /// What somebody a calendar is shared at this level can do.
    var explanation: String {
        switch self {
        case .busy: String(localized: "They see when you are busy, and nothing else.")
        case .titles: String(localized: "They see the time, title and place of each event.")
        case .details: String(localized: "They see everything in each event, including its notes and guests.")
        case .edit: String(localized: "They see everything, and can add, change and remove events that have no guests.")
        case .owner: ""
        }
    }
}

/// ShareTargetPicker is the one picker for who a calendar or a page is shared
/// with: the people in one section and the circles in another, so that a person
/// and a circle are never both chosen.
struct ShareTargetPicker: View {
    let people: [CalendarSharePerson]
    let circles: [CalendarShareCircle]
    @Binding var chosen: ShareTarget?

    var body: some View {
        Picker("Share with", selection: $chosen) {
            Text("Choose a person or a circle").tag(ShareTarget?.none)
            if !people.isEmpty {
                Section("People") {
                    ForEach(people) { person in
                        Text(verbatim: person.pickerLabel).tag(Optional(ShareTarget.person(person.userId)))
                    }
                }
            }
            if !circles.isEmpty {
                Section("Circles") {
                    ForEach(circles) { circle in
                        // The size too, so two circles with one name can be told apart.
                        Text("Everyone in \(circle.name) (\(CircleWords.memberCount(circle.memberCount)))").tag(Optional(ShareTarget.circle(circle.id)))
                    }
                }
            }
        }
    }

    /// What the choice is called in a sentence, such as "Shared with Ben".
    static func name(of target: ShareTarget?, people: [CalendarSharePerson], circles: [CalendarShareCircle]) -> String {
        switch target {
        case .person(let userId)?:
            return people.first(where: { $0.userId == userId })?.displayName ?? ""
        case .circle(let circleId)?:
            let circleName = circles.first(where: { $0.id == circleId })?.name ?? ""
            return String(localized: "Everyone in \(circleName)")
        case nil:
            return ""
        }
    }
}

/// CalendarShareScreen is who the person's calendar is shared with, at what
/// level, with a way to stop, and a form to share it with someone else: one
/// person in a circle they share, or a whole circle.
struct CalendarShareScreen: View {
    let model: CalendarModel
    let calendar: AgentCalendar
    @State private var shares: [CalendarShare] = []
    @State private var people: [CalendarSharePerson] = []
    @State private var circles: [CalendarShareCircle] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var chosen: ShareTarget?
    @State private var level: CalendarShareLevel = .titles
    @State private var stopping: CalendarShare?
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        List {
            Section {
                if isLoaded && shares.isEmpty {
                    Text("Your calendar is not shared with anybody yet.")
                        .foregroundStyle(.secondary)
                }
                ForEach(shares) { share in
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(share.isWithCircle ? String(localized: "Everyone in \(share.granteeDisplayName)") : share.granteeDisplayName)
                            Text(share.level?.title ?? share.shareLevel)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        Spacer(minLength: 0)
                    }
                    .confirmationDialog(
                        "Stop sharing", isPresented: Binding(get: { stopping?.id == share.id }, set: { if !$0 { stopping = nil } }),
                        titleVisibility: .visible, presenting: stopping
                    ) { share in
                        Button("Stop sharing", role: .destructive) { stop(share) }
                    } message: { share in
                        Text("\(Self.name(of: share)) will no longer see your calendar.")
                    }
                    .swipeActions {
                        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                    }
                    .contextMenu {
                        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                    }
                }
            } header: {
                Text("Shared with")
            } footer: {
                if !shares.isEmpty {
                    Text("Swipe left, or touch and hold, to stop sharing.")
                }
            }
            if isLoaded && people.isEmpty && circles.isEmpty {
                Section {
                    Text("You do not share a circle with anybody yet. Make or join a circle to share your calendar.")
                        .foregroundStyle(.secondary)
                    OpenCirclesLink(connection: model.connection)
                }
            } else if isLoaded {
                Section {
                    ShareTargetPicker(people: people, circles: circles, chosen: $chosen)
                } footer: {
                    Text("Choose a person, or a whole circle.")
                }
                Section {
                    Picker("How much", selection: $level) {
                        ForEach(CalendarShareLevel.grantable, id: \.self) { each in
                            Text(each.title).tag(each)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                } header: {
                    Text("How much")
                } footer: {
                    Text(level.explanation)
                }
                Section {
                    Button("Share") { share() }
                        .disabled(isWorking || chosen == nil)
                }
            }
        }
        .navigationTitle("Share my calendar")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if let loadProblemText, !isLoaded {
                LoadFailedView(problemText: loadProblemText) { await load() }
            } else if !isLoaded {
                ProgressView()
            }
        }
        .refreshable { await load() }
        .task { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    private static func name(of share: CalendarShare) -> String {
        share.isWithCircle ? String(localized: "Everyone in \(share.granteeDisplayName)") : share.granteeDisplayName
    }

    private func load() async {
        do {
            async let readShares = model.connection.calendarShares(calendarId: calendar.id)
            async let readPeople = model.connection.calendarSharePeople()
            async let readCircles = model.connection.calendarShareCircles()
            (shares, people, circles) = try await (readShares, readPeople, readCircles)
            isLoaded = true
            loadProblemText = nil
        } catch {
            // With the screen already drawn a failed refresh leaves it be and
            // says so.
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }

    private func share() {
        guard let target = chosen else { return }
        let name = ShareTargetPicker.name(of: target, people: people, circles: circles)
        isWorking = true
        Task {
            do {
                try await model.connection.shareCalendar(
                    calendarId: calendar.id, granteeUserId: target.granteeUserId, granteeCircleId: target.granteeCircleId, level: level)
                doneText = String(localized: "Shared with \(name)")
                chosen = nil
                level = .titles
                shares = try await model.connection.calendarShares(calendarId: calendar.id)
            } catch {
                problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
            }
            isWorking = false
        }
    }

    private func stop(_ share: CalendarShare) {
        let name = Self.name(of: share)
        isWorking = true
        Task {
            do {
                try await model.connection.unshareCalendar(shareId: share.id)
                doneText = String(localized: "No longer shared with \(name)")
                shares = try await model.connection.calendarShares(calendarId: calendar.id)
            } catch {
                problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
            }
            isWorking = false
        }
    }
}

/// EventReader is an event in a calendar shared below edit: what the person
/// is shown of it, to read and not to change.
struct EventReader: View {
    let model: CalendarModel
    let calendar: AgentCalendar?
    let eventDetail: CalendarEventDetail
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(eventDetail.summary.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "(no title)"))
                        .font(.headline)
                    Text(whenText)
                    if let location = eventDetail.location, !location.isEmpty {
                        Label(location, systemImage: "mappin.and.ellipse")
                    }
                }
                if let guests = eventDetail.attendees, !guests.isEmpty {
                    Section("Invited") {
                        ForEach(guests, id: \.address) { guest in
                            Text(guest.name.flatMap { $0.isEmpty ? nil : $0 } ?? guest.address)
                        }
                    }
                }
                if let notes = eventDetail.description, !notes.isEmpty {
                    Section("Notes") {
                        Text(notes)
                    }
                }
                if let calendar {
                    Section {
                    } footer: {
                        Text("Shared with you in \(model.displayName(of: calendar)). You can read it but not change it.")
                    }
                }
            }
            .navigationTitle("Event")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }

    private var whenText: String {
        if eventDetail.allDay {
            // The day it is on, read from the parts the server wrote and then
            // placed in this zone, so that it is the same day everywhere.
            guard let first = CalendarDates.day(ofAllDay: eventDetail.startsAt), let date = Calendar.current.date(from: first) else {
                return String(localized: "All day")
            }
            return String(localized: "\(date.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())), all day")
        }
        guard let start = ServerTime.date(eventDetail.startsAt) else { return "" }
        let end = ServerTime.date(eventDetail.endsAt) ?? start
        return "\(start.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())) \((start..<max(start, end)).formatted(date: .omitted, time: .shortened))"
    }
}

/// CircleCalendarOfferSheet is the question asked once, as a person joins a
/// family or a group of colleagues: share their calendar with everyone in it,
/// at a level they choose, or not now. For a family the same sheet also asks
/// about where they are: now and where they have been, only now, or not now,
/// so they answer once. The dashboard asks the same on its Circles page, and
/// the Share my calendar screen and Location's Sharing have the choices
/// whenever they want them.
struct CircleCalendarOfferSheet: View {
    let connection: Connection
    let offer: CircleCalendarOffer
    /// Said on the screen behind the sheet once something is shared.
    let onShared: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    /// nil is "Not now", offered for the calendar only beside the location.
    /// With no calendar to share it starts as nil: nothing is chosen there.
    @State private var calendarLevel: CalendarShareLevel?
    @State private var locationLevel: LocationShareLevel?
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?

    init(connection: Connection, offer: CircleCalendarOffer, onShared: @escaping (String) -> Void) {
        self.connection = connection
        self.offer = offer
        self.onShared = onShared
        _calendarLevel = State(initialValue: offer.isCalendarOffered ? .busy : nil)
    }

    var body: some View {
        NavigationStack {
            Form {
                if offer.isCalendarOffered {
                    Section {
                        Text("Everyone in \(offer.circleName) could see your calendar, as much as you choose. An event you mark private is only ever shown as busy.")
                    }
                    Section {
                        Picker("How much", selection: $calendarLevel) {
                            if offer.isLocationOffered {
                                Text("Not now").tag(CalendarShareLevel?.none)
                            }
                            ForEach(CircleCalendarOffer.levels, id: \.self) { each in
                                Text(each.title).tag(Optional(each))
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    } header: {
                        Text("Your calendar")
                    } footer: {
                        if let calendarLevel { Text(calendarLevel.explanation) }
                    }
                }
                if offer.isLocationOffered {
                    Section {
                        Picker("Share where I am", selection: $locationLevel) {
                            Text("Not now").tag(LocationShareLevel?.none)
                            ForEach(CircleCalendarOffer.locationLevels, id: \.self) { each in
                                Text(each.title).tag(Optional(each))
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    } header: {
                        Text("Where you are")
                    } footer: {
                        Text("Everyone in \(offer.circleName) could see it. You can change this under Sharing in Location.")
                    }
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Not now") { dismiss() }
                        .disabled(isWorking)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isWorking {
                        ProgressView()
                    } else {
                        Button("Share") { Task { await share() } }
                            .disabled((offer.isCalendarOffered ? calendarLevel : nil) == nil && locationLevel == nil)
                    }
                }
            }
            .toast(done: $doneText, problem: $problemText)
        }
        .presentationDetents([.medium, .large])
    }

    private var title: String {
        switch (offer.isCalendarOffered, offer.isLocationOffered) {
        case (true, true): String(localized: "Share with \(offer.circleName)?")
        case (false, _): String(localized: "Share where you are with \(offer.circleName)?")
        default: String(localized: "Share your calendar with \(offer.circleName)?")
        }
    }

    private func share() async {
        isWorking = true
        defer { isWorking = false }
        do {
            if let calendarId = offer.calendarId, let calendarLevel {
                try await connection.shareCalendar(calendarId: calendarId, granteeUserId: nil, granteeCircleId: offer.circleId, level: calendarLevel)
            }
            if let locationLevel {
                try await connection.shareLocation(granteeUserId: nil, granteeCircleId: offer.circleId, level: locationLevel)
            }
            let name = String(localized: "Everyone in \(offer.circleName)")
            onShared(String(localized: "Shared with \(name)"))
            dismiss()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }
}
