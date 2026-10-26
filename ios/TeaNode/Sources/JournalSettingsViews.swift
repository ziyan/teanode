import SwiftUI
import TeaNodeKit

// The Journal in Settings: the weather, the question asked each evening,
// who reads the journal, and erasing it.

/// JournalSettingsRow is the row in Settings that opens the Journal settings.
struct JournalSettingsRow: View {
    var body: some View {
        Label {
            Text(verbatim: JournalText.title)
        } icon: {
            Image(systemName: AppSection.journal.systemImage)
        }
        .foregroundStyle(.primary)
    }
}

/// JournalSettingsScreen is what the person decides about their journal.
struct JournalSettingsScreen: View {
    @State private var model: JournalSettingsModel
    /// The time shown in the picker; saved a moment after it stops changing.
    @State private var eveningTime = Date()
    @State private var isConfirmingErase = false

    init(connection: Connection) {
        _model = State(initialValue: JournalSettingsModel(connection: connection))
    }

    var body: some View {
        Form {
            if let loadFailedText = model.loadFailedText, model.settings == nil {
                Section {
                    Text(verbatim: loadFailedText.asReasonSentence)
                    Button("Try again") { Task { await model.load() } }
                        .actionColor()
                }
            }
            weatherSection
            eveningSection
            readingSection
            eraseSection
        }
        .navigationTitle(JournalText.title)
        .navigationBarTitleDisplayMode(.inline)
        .task { await model.load() }
        .refreshable { await model.load() }
        .toast(done: $model.doneText, problem: $model.problemText)
        .onChange(of: model.settings?.eveningQuestionTime, initial: true) { _, timeText in
            eveningTime = Self.date(ofTime: timeText ?? JournalEveningTime.defaultText)
        }
        // A moment after the picker stops moving, so a turn of the wheel is one
        // change and not one for every hour it passes.
        .task(id: eveningTime) {
            try? await Task.sleep(for: .milliseconds(800))
            guard !Task.isCancelled, let saved = model.settings?.eveningQuestionTime else { return }
            let timeText = Self.timeText(of: eveningTime)
            if timeText != saved { await model.setEveningQuestionTime(timeText) }
        }
    }

    // MARK: Weather

    private var weatherSection: some View {
        Section {
            Toggle(
                "Weather",
                isOn: Binding(
                    get: { model.settings?.isWeatherOn ?? true },
                    set: { isOn in Task { await model.setWeather(isOn: isOn) } })
            )
            .disabled(model.settings == nil || model.isBusy)
        } footer: {
            Text("Adds each day's weather to it, from Open-Meteo, for the place you spent most of the day. Only that place, rounded to about 10 kilometers, is sent. A day with no place has no weather.")
        }
    }

    // MARK: The evening question

    private var eveningSection: some View {
        Section {
            Toggle(
                "Ask how the day was",
                isOn: Binding(
                    get: { model.settings?.isEveningQuestionOn ?? true },
                    set: { isOn in Task { await model.setEveningQuestion(isOn: isOn) } })
            )
            .disabled(model.settings == nil || model.isBusy)
            if model.settings?.isEveningQuestionOn ?? true {
                DatePicker("Time", selection: $eveningTime, displayedComponents: .hourAndMinute)
                    .disabled(model.settings == nil)
            }
        } header: {
            Text("The evening question")
        } footer: {
            Text("Each evening TeaNode sends a notification that asks how the day was. You answer with one tap, without opening the app.")
        }
    }

    // MARK: Who reads it

    private var readingSection: some View {
        Section {
            Text(verbatim: readingText)
        } header: {
            Text("Who reads it")
        }
    }

    /// Where what TeaNode reads of the journal goes: to the provider of its
    /// model.
    private var readingText: String {
        let providerName = model.settings?.modelProviderName ?? ""
        if providerName.isEmpty {
            return String(localized: "TeaNode reads your journal, your words included, to write the story of each day. What it reads goes to the provider of its model.")
        }
        return String(localized: "TeaNode reads your journal, your words included, to write the story of each day. What it reads goes to \(providerName).")
    }

    // MARK: Erasing

    private var eraseSection: some View {
        Section {
            Button(role: .destructive) {
                isConfirmingErase = true
            } label: {
                // The icon takes the tint unless it is colored too.
                Label("Erase the journal", systemImage: "trash").foregroundStyle(.red)
            }
            .tint(.red)
            .actionColor(isDestructive: true)
            .disabled(model.isBusy)
            .confirmationDialog("Erase the journal?", isPresented: $isConfirmingErase, titleVisibility: .visible) {
                Button("Erase", role: .destructive) {
                    Task { _ = await model.erase() }
                }
            } message: {
                Text(
                    "Erases every word you wrote, every mood, and every story TeaNode wrote, for all days, weeks, months and years. Your photos, places, mail and everything else the days link to stay where they are. This cannot be undone."
                )
            }
        }
    }

    // MARK: The time of day

    /// Today at the time written HH:MM, for the picker.
    private static func date(ofTime text: String) -> Date {
        let parts = JournalEveningTime.parts(of: text) ?? (hour: 21, minute: 0)
        return Calendar.current.date(bySettingHour: parts.hour, minute: parts.minute, second: 0, of: Date()) ?? Date()
    }

    private static func timeText(of date: Date) -> String {
        let parts = Calendar.current.dateComponents([.hour, .minute], from: date)
        return JournalEveningTime.text(hour: parts.hour ?? 21, minute: parts.minute ?? 0)
    }
}
