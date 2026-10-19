import SwiftUI
import TeaNodeKit

/// WeekdayChecklist is the seven days of the week as rows, each with the
/// system's trailing checkmark when it is chosen, in the order the person's
/// calendar starts its week and named as their locale names them. It is the
/// weekly days of a custom repeat and the days of the morning brief, so the
/// two choose days the same way. Put it in a Section of a Form or List.
struct WeekdayChecklist: View {
    @Binding var dayNumbers: Set<Int>
    /// Whether the last chosen day can be unchosen. A repeat on no day is
    /// not a repeat, so the custom repeat keeps one chosen; the brief lets
    /// the person clear them all and says that it needs one.
    var isEmptyAllowed = true

    private var orderedDayNumbers: [Int] { ScheduleWeekday.weekOrder(firstWeekday: Calendar.current.firstWeekday) }

    var body: some View {
        ForEach(orderedDayNumbers, id: \.self) { dayNumber in
            dayRow(dayNumber)
        }
    }

    private func dayRow(_ dayNumber: Int) -> some View {
        let isChosen = dayNumbers.contains(dayNumber)
        return Button {
            toggle(dayNumber)
        } label: {
            HStack {
                Text(verbatim: ScheduleWeekday.name(dayNumber)).foregroundStyle(Color(.label))
                Spacer()
                if isChosen {
                    Image(systemName: "checkmark").foregroundStyle(.tint)
                }
            }
        }
        .accessibilityAddTraits(traits(isChosen: isChosen))
        .sensoryFeedback(.selection, trigger: isChosen)
    }

    private func traits(isChosen: Bool) -> AccessibilityTraits {
        isChosen ? [.isSelected] : []
    }

    private func toggle(_ dayNumber: Int) {
        if dayNumbers.contains(dayNumber) {
            if dayNumbers.count > 1 || isEmptyAllowed { dayNumbers.remove(dayNumber) }
        } else {
            dayNumbers.insert(dayNumber)
        }
    }
}
