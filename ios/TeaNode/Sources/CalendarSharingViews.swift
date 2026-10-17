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
                problemText = error.localizedDescription
            }
            isWorking = false
        }
    }
}

/// CalendarListRow is one calendar in the list: its color, the owner's face
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
        if calendar.isOwn { return nil }
        switch calendar.level {
        case .busy: return String(localized: "Busy times only")
        case .titles: return String(localized: "Titles")
        case .details: return String(localized: "Details")
        case .edit: return String(localized: "Can edit")
        case .owner: return nil
        }
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
    @State private var chosenPersonId: String?
    @State private var chosenCircleId: String?
    @State private var level: CalendarShareLevel = .titles
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
                    .contextMenu {
                        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) {
                            stop(share)
                        }
                    }
                }
            } header: {
                Text("Shared with")
            } footer: {
                if !shares.isEmpty {
                    Text("Touch and hold to stop sharing.")
                }
            }
            if isLoaded && people.isEmpty && circles.isEmpty {
                Section {
                    Text("You do not share a circle with anybody yet. Make or join a circle to share your calendar.")
                        .foregroundStyle(.secondary)
                }
            } else if isLoaded {
                Section {
                    Picker("Person", selection: $chosenPersonId) {
                        Text("None").tag(String?.none)
                        ForEach(people) { person in
                            Text(person.displayName).tag(Optional(person.userId))
                        }
                    }
                    Picker("Circle", selection: $chosenCircleId) {
                        Text("None").tag(String?.none)
                        ForEach(circles) { circle in
                            Text(circle.name).tag(Optional(circle.id))
                        }
                    }
                } header: {
                    Text("Share with")
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
                        .disabled(isWorking || (chosenPersonId == nil && chosenCircleId == nil))
                }
            }
        }
        .navigationTitle("Share my calendar")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if !isLoaded { ProgressView() }
        }
        // A person and a circle are not both chosen: the last choice wins.
        .onChange(of: chosenPersonId) { _, person in
            if person != nil { chosenCircleId = nil }
        }
        .onChange(of: chosenCircleId) { _, circle in
            if circle != nil { chosenPersonId = nil }
        }
        .task { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    private func load() async {
        do {
            async let readShares = model.connection.calendarShares(calendarId: calendar.id)
            async let readPeople = model.connection.calendarSharePeople()
            async let readCircles = model.connection.calendarShareCircles()
            (shares, people, circles) = try await (readShares, readPeople, readCircles)
            isLoaded = true
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func share() {
        isWorking = true
        Task {
            do {
                try await model.connection.shareCalendar(
                    calendarId: calendar.id, granteeUserId: chosenPersonId, granteeCircleId: chosenCircleId, level: level)
                doneText = String(localized: "Shared")
                chosenPersonId = nil
                chosenCircleId = nil
                shares = try await model.connection.calendarShares(calendarId: calendar.id)
            } catch {
                problemText = error.localizedDescription
            }
            isWorking = false
        }
    }

    private func stop(_ share: CalendarShare) {
        isWorking = true
        Task {
            do {
                try await model.connection.unshareCalendar(shareId: share.id)
                doneText = String(localized: "No longer shared")
                shares = try await model.connection.calendarShares(calendarId: calendar.id)
            } catch {
                problemText = error.localizedDescription
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
    let detail: CalendarEventDetail
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(detail.summary.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "(no title)"))
                        .font(.headline)
                    Text(whenText)
                    if let location = detail.location, !location.isEmpty {
                        Label(location, systemImage: "mappin.and.ellipse")
                    }
                }
                if let guests = detail.attendees, !guests.isEmpty {
                    Section("Invited") {
                        ForEach(guests, id: \.address) { guest in
                            Text(guest.name.flatMap { $0.isEmpty ? nil : $0 } ?? guest.address)
                        }
                    }
                }
                if let notes = detail.description, !notes.isEmpty {
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
        if detail.allDay {
            // The day it is on, read from the parts the server wrote and then
            // placed in this zone, so that it is the same day everywhere.
            guard let first = CalendarDates.day(ofAllDay: detail.startsAt), let date = Calendar.current.date(from: first) else {
                return String(localized: "All day")
            }
            return String(localized: "\(date.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())), all day")
        }
        guard let start = ServerTime.date(detail.startsAt) else { return "" }
        let end = ServerTime.date(detail.endsAt) ?? start
        return "\(start.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())) \((start..<max(start, end)).formatted(date: .omitted, time: .shortened))"
    }
}
