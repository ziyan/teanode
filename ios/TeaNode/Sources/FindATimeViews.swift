import SwiftUI
import TeaNodeKit

/// FindATimeScreen is when several people are all free: the people who shared
/// their calendar with the person, a run of days and the hours of the day to
/// offer, and the stretches nobody has anything booked in. The dashboard's
/// Find a time dialog, the command line's `calendar free --with` and the
/// agent's free_together make the same kind of sum from the same busy times;
/// the days and hours here are the phone's zone, which is what the calendar
/// screen shows, and the command line and the agent count them in the
/// calendar's own zone, so across two zones the times offered differ.
struct FindATimeScreen: View {
    let connection: Connection
    /// The person's own account, whose busy times are always among them.
    let ownUserId: String
    @State private var people: [CalendarSharePerson] = []
    @State private var isAnyoneLeftOut = false
    @State private var chosen: Set<String> = []
    @State private var question = FreeTimeQuestion(
        firstDay: Calendar.current.startOfDay(for: Date()),
        lastDay: Calendar.current.date(byAdding: .day, value: 6, to: Calendar.current.startOfDay(for: Date())) ?? Date())
    @State private var earliest = FindATimeScreen.time(hour: 9)
    @State private var latest = FindATimeScreen.time(hour: 17)
    @State private var stretches: [FreeStretch]?
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        List {
            if isLoaded && people.isEmpty {
                Section {
                    Text("Nobody has shared their calendar with you yet. Ask the people you share a circle with to share theirs, at busy times or more.")
                        .foregroundStyle(.secondary)
                    OpenCirclesLink(connection: connection)
                }
            } else if isLoaded {
                Section {
                    ForEach(people) { person in
                        Toggle(person.pickerLabel, isOn: binding(for: person))
                    }
                } header: {
                    Text("With")
                } footer: {
                    if isAnyoneLeftOut {
                        Text("Some people in your circles have not shared their calendar with you, so they are not listed.")
                    }
                }
                Section {
                    // Both days bounded the same way: a picker with a range and
                    // one without were drawn in two date styles side by side.
                    // There is nothing to find before today.
                    DatePicker(
                        "First day", selection: $question.firstDay, in: Calendar.current.startOfDay(for: Date())...,
                        displayedComponents: .date)
                    DatePicker("Last day", selection: $question.lastDay, in: question.firstDay..., displayedComponents: .date)
                    DatePicker("Earliest hour", selection: $earliest, displayedComponents: .hourAndMinute)
                    DatePicker("Latest hour", selection: $latest, displayedComponents: .hourAndMinute)
                }
                Section {
                    Button("Find times") { Task { await find() } }
                        .disabled(isWorking || chosen.isEmpty)
                }
                if let stretches {
                    Section {
                        if stretches.isEmpty {
                            Text("There is no time in those days and hours when everyone is free.")
                                .foregroundStyle(.secondary)
                        }
                        ForEach(days(of: stretches)) { group in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(group.day.formatted(.dateTime.weekday(.wide).month(.wide).day()))
                                Text(group.stretches.map(Self.words).joined(separator: ", "))
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    } header: {
                        Text("When everyone is free")
                    }
                }
            }
        }
        .navigationTitle("Find a time")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if let loadProblemText, !isLoaded {
                LoadFailedView(problemText: loadProblemText) { await load() }
            } else if !isLoaded {
                ProgressView()
            }
        }
        .task { await load() }
        #if DEBUG
            // `-TeaNodeScreen calendar-find-time`: the first two people chosen and the answer shown.
            .task {
                guard DemoServer.isRequested, DemoServer.screen == "calendar-find-time" else { return }
                while !isLoaded { try? await Task.sleep(for: .milliseconds(100)) }
                chosen = Set(people.prefix(2).map(\.userId))
                await find()
            }
        #endif
        .toast(done: $doneText, problem: $problemText)
    }

    private func binding(for person: CalendarSharePerson) -> Binding<Bool> {
        Binding(
            get: { chosen.contains(person.userId) },
            set: { isOn in
                if isOn { chosen.insert(person.userId) } else { chosen.remove(person.userId) }
            })
    }

    private func load() async {
        do {
            let sharing = try await connection.peopleSharingTheirCalendar()
            people = sharing.people
            isAnyoneLeftOut = sharing.isAnyoneLeftOut
            isLoaded = true
            loadProblemText = nil
        } catch {
            loadProblemText = error.loadProblemText
        }
    }

    private func find() async {
        question.earliestMinutes = Self.minutes(of: earliest)
        question.latestMinutes = Self.minutes(of: latest)
        switch question.problem {
        case .lastDayBeforeFirst?:
            problemText = String(localized: "The last day comes before the first.")
            return
        case .dayEndsBeforeItBegins?:
            problemText = String(localized: "The day must end after it begins.")
            return
        case nil:
            break
        }
        isWorking = true
        defer { isWorking = false }
        do {
            let answer = try await connection.freeTime(ownUserId: ownUserId, with: people.filter { chosen.contains($0.userId) }, question: question)
            switch answer {
            case .free(let found):
                stretches = found
            case .notShared(let usernames):
                stretches = nil
                problemText = String(localized: "\(usernames.joined(separator: ", ")) stopped sharing a calendar with you, so when everyone is free cannot be said.")
            }
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }

    /// One day's free stretches.
    private struct FreeDay: Identifiable {
        let day: Date
        var stretches: [FreeStretch]
        var id: Date { day }
    }

    /// The stretches gathered under their days, in order.
    private func days(of stretches: [FreeStretch]) -> [FreeDay] {
        var groups: [FreeDay] = []
        for stretch in stretches {
            if let last = groups.last, last.day == stretch.day {
                groups[groups.count - 1].stretches.append(stretch)
            } else {
                groups.append(FreeDay(day: stretch.day, stretches: [stretch]))
            }
        }
        return groups
    }

    private static func words(_ stretch: FreeStretch) -> String {
        (stretch.startsAt..<stretch.endsAt).formatted(date: .omitted, time: .shortened)
    }

    private static func time(hour: Int) -> Date {
        Calendar.current.date(bySettingHour: hour, minute: 0, second: 0, of: Date()) ?? Date()
    }

    private static func minutes(of time: Date) -> Int {
        let parts = Calendar.current.dateComponents([.hour, .minute], from: time)
        return (parts.hour ?? 0) * 60 + (parts.minute ?? 0)
    }
}
