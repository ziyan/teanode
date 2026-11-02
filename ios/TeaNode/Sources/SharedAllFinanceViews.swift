import Charts
import SwiftUI
import TeaNodeKit

// What somebody who shares all of their finance shows the person: one
// combined view across all their accounts, like the person's own Spending
// page, and at the transactions level their transactions too. It names no
// account and shows no balance; those stay with the accounts shared one at a
// time, which are listed below it.

/// SharedAllFinanceView is the finance somebody shares as a whole: a month or
/// a year of money in and out by spending category, with the owner's own
/// category names, and a Transactions row at the transactions level.
struct SharedAllFinanceView: View {
    @Environment(FinanceModel.self) private var finance
    let sharer: FinanceSharer
    let wholeShare: SharedAllFinance
    @State private var rows: [SharedFinanceSpendingRow] = []
    @State private var hasLoaded = false
    @State private var loadFailedText: String?
    @State private var periodKind = SpendingPeriodKind.month
    @State private var month = Date()
    @State private var year = Calendar.current.component(.year, from: Date())
    @State private var chosenCurrencyCode = ""
    /// The slice chosen on the donut or by its row.
    @State private var selectedSliceId: String?
    /// Whether the month shown has been moved to the latest one with
    /// spending, once, when the rows first arrive.
    @State private var hasPlacedMonth = false

    var body: some View {
        List {
            if wholeShare.isSharingTransactions {
                Section {
                    NavigationLink {
                        SharedFinanceTransactionsScreen(sharer: sharer)
                    } label: {
                        Label("Transactions", systemImage: "list.bullet.rectangle").foregroundStyle(.primary)
                    }
                }
            }
            periodSection
            if currencyCodes.count > 1 {
                Section {
                    Picker("Currency", selection: Binding(get: { currencyCode }, set: { chosen in chosenCurrencyCode = chosen })) {
                        ForEach(currencyCodes, id: \.self) { code in
                            Text(verbatim: code).tag(code)
                        }
                    }
                }
            }
            if hasLoaded {
                if rows.isEmpty {
                    Section {
                        Text("Nothing spent or received yet").foregroundStyle(.secondary)
                    }
                } else {
                    Section {
                        cashFlowChart(chartMonths)
                    } header: {
                        Text("Cash flow")
                    }
                    totalsSection
                    whereItWentSection
                }
            }
            if !finance.accountsShared(by: sharer).isEmpty {
                SharedFinanceAccountsSection(sharer: sharer)
            }
        }
        .navigationTitle(sharer.displayName)
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(for: SharedFinanceAccount.self) { account in
            SharedFinanceAccountScreen(account: account)
        }
        .overlay {
            if let loadFailedText, !hasLoaded {
                LoadFailedView(problemText: loadFailedText) { await load() }
            } else if !hasLoaded {
                ProgressView()
            }
        }
        .onChange(of: periodText) {
            selectedSliceId = nil
        }
        .task { await load() }
        .refreshable {
            await finance.loadShared()
            await load()
        }
    }

    // MARK: Sections

    private var periodSection: some View {
        Section {
            Picker("Show a month or a year", selection: $periodKind) {
                Text("Month").tag(SpendingPeriodKind.month)
                Text("Year").tag(SpendingPeriodKind.year)
            }
            .pickerStyle(.segmented)
            if periodKind == .month {
                MonthStepper(month: $month, dates: finance.dates)
            } else {
                YearStepper(year: $year, firstYear: firstYear, currentYear: currentYear)
            }
        }
    }

    private var totalsSection: some View {
        let monthsOfCurrency = SharedSpendingSummary.months(in: rows, currencyCode: currencyCode)
        let period = SharedSpendingSummary.total(of: monthsOfCurrency, period: periodText)
        return Section {
            LabeledContent("Money in") { Text(verbatim: moneyText(period.moneyIn)).monospacedDigit() }
            LabeledContent("Money out") { Text(verbatim: moneyText(period.moneyOut)).monospacedDigit() }
            LabeledContent("Net") { Text(verbatim: moneyText(period.moneyIn - period.moneyOut)).monospacedDigit() }
        } footer: {
            Text("All of \(sharer.displayName)'s accounts together, in \(currencyCode). No account names or balances.")
        }
    }

    private var whereItWentSection: some View {
        let lines = SharedSpendingSummary.lines(in: rows, currencyCode: currencyCode, period: periodText)
        return Section {
            if lines.isEmpty {
                Group {
                    if periodKind == .month { Text("No spending this month.") } else { Text("No spending this year.") }
                }
                .foregroundStyle(.secondary)
            } else {
                let total = lines.reduce(Decimal(0)) { $0 + $1.spentAmount }
                let slices = spendingSlices(lines)
                if !slices.isEmpty {
                    SpendingDonutChart(slices: slices, total: total, currency: currencyCode, selectedSliceId: $selectedSliceId)
                }
                ForEach(Array(lines.enumerated()), id: \.element.id) { rank, line in
                    let lineSlice = drawnSlice(ofRank: rank, in: slices)
                    Button {
                        chooseSlice(lineSlice)
                    } label: {
                        SpendingLineRow(
                            line: SpendingLine(id: line.id, label: categoryLabel(line), amount: line.spentAmount, count: line.financeTransactionCount),
                            color: lineSlice?.color, currency: currencyCode, shareText: shareText(line.spentAmount, of: total), isOpening: false)
                    }
                    .tint(Color.primary)
                    .accessibilityAddTraits(lineSlice != nil && lineSlice?.id == selectedSliceId ? .isSelected : [])
                }
            }
        } header: {
            Text("Where it went")
        }
    }

    // MARK: Cash flow chart

    /// The twelve months to the one shown, or the twelve of the year shown,
    /// with a month that has no rows at zero.
    private var chartMonths: [SharedSpendingMonth] {
        let monthsOfCurrency = SharedSpendingSummary.months(in: rows, currencyCode: currencyCode)
        let monthTexts = periodKind == .month ? monthsEnding(at: month) : (1...12).map { String(format: "%04d-%02d", year, $0) }
        return monthTexts.map { monthText in
            monthsOfCurrency.first { $0.spendingMonth == monthText } ?? SharedSpendingMonth(spendingMonth: monthText, moneyOut: 0, moneyIn: 0)
        }
    }

    private func monthsEnding(at lastMonth: Date) -> [String] {
        (0..<12).reversed().map { monthsBack in
            finance.month(finance.dates.date(byAdding: .month, value: -monthsBack, to: lastMonth) ?? lastMonth)
        }
    }

    private func cashFlowChart(_ flows: [SharedSpendingMonth]) -> some View {
        let moneyInWord = String(localized: "Money in")
        let moneyOutWord = String(localized: "Money out")
        let moneyInColor = ChartPalette.seriesColor(at: 0)
        let moneyOutColor = ChartPalette.seriesColor(at: 1)
        return VStack(alignment: .leading, spacing: 8) {
            ChartKey(entries: [ChartKeyEntry(label: moneyInWord, color: moneyInColor), ChartKeyEntry(label: moneyOutWord, color: moneyOutColor)])
            Chart {
                ForEach(flows) { flow in
                    let opacity = markOpacity(ofMonth: flow.spendingMonth)
                    BarMark(x: .value("Month", flow.spendingMonth), y: .value("Amount", doubleValue(flow.moneyIn)))
                        .foregroundStyle(by: .value("Kind", moneyInWord))
                        .position(by: .value("Kind", moneyInWord))
                        .opacity(opacity)
                        .accessibilityLabel(Text(verbatim: "\(FinanceShareWords.monthTitle(flow.spendingMonth)), \(moneyInWord)"))
                        .accessibilityValue(Text(verbatim: moneyText(flow.moneyIn)))
                    BarMark(x: .value("Month", flow.spendingMonth), y: .value("Amount", doubleValue(flow.moneyOut)))
                        .foregroundStyle(by: .value("Kind", moneyOutWord))
                        .position(by: .value("Kind", moneyOutWord))
                        .opacity(opacity)
                        .accessibilityLabel(Text(verbatim: "\(FinanceShareWords.monthTitle(flow.spendingMonth)), \(moneyOutWord)"))
                        .accessibilityValue(Text(verbatim: moneyText(flow.moneyOut)))
                }
            }
            .chartForegroundStyleScale(domain: [moneyInWord, moneyOutWord], range: [moneyInColor, moneyOutColor])
            .chartLegend(.hidden)
            .chartXAxis {
                AxisMarks { value in
                    AxisValueLabel {
                        if let text = value.as(String.self) { Text(verbatim: monthLetter(text)) }
                    }
                }
            }
            .chartYAxis {
                AxisMarks { value in
                    AxisGridLine()
                    AxisValueLabel {
                        if let number = value.as(Double.self) { Text(verbatim: FinanceMoney.axisText(number, currencyCode: currencyCode)) }
                    }
                }
            }
            .frame(height: 180)
        }
    }

    /// The month shown stands out among the twelve; a year shows all of its
    /// months in full.
    private func markOpacity(ofMonth monthText: String) -> Double {
        if periodKind == .year { return 1 }
        return ChartPalette.markOpacity(isChosen: monthText == periodText, isAnyChosen: true)
    }

    /// A month on the axis: its first letter.
    private func monthLetter(_ monthText: String) -> String {
        let parts = monthText.split(separator: "-").compactMap { Int($0) }
        guard parts.count == 2, let date = Calendar.current.date(from: DateComponents(year: parts[0], month: parts[1], day: 15)) else {
            return String(monthText.suffix(2))
        }
        return date.formatted(.dateTime.month(.narrow))
    }

    // MARK: Donut

    private func categoryLabel(_ line: SharedSpendingLine) -> String {
        line.spendingCategoryName.isEmpty ? String(localized: "Uncategorized") : line.spendingCategoryName
    }

    /// The donut's slices: the largest categories each in a color of the
    /// palette, then the rest together in gray.
    private func spendingSlices(_ lines: [SharedSpendingLine]) -> [SpendingSlice] {
        let sliceAmounts = ChartShares.sliceAmounts(lines.map(\.spentAmount))
        return sliceAmounts.enumerated().map { index, sliceAmount in
            if index < ChartShares.ownSliceLimit {
                let line = lines[index]
                return SpendingSlice(id: line.id, label: categoryLabel(line), amount: sliceAmount, color: ChartPalette.seriesColor(at: index))
            }
            return SpendingSlice(id: SpendingSlice.restId, label: String(localized: "Everything else"), amount: sliceAmount, color: ChartPalette.restColor)
        }
    }

    /// The slice a row's category is drawn in, by the row's rank.
    private func drawnSlice(ofRank rank: Int, in slices: [SpendingSlice]) -> SpendingSlice? {
        let index = ChartShares.sliceIndex(ofRank: rank)
        return slices.indices.contains(index) ? slices[index] : nil
    }

    /// Chooses a row's slice, or lets it go when it is the one chosen.
    private func chooseSlice(_ slice: SpendingSlice?) {
        guard let slice else { return }
        if selectedSliceId == slice.id {
            selectedSliceId = nil
        } else {
            selectedSliceId = slice.id
        }
    }

    private func shareText(_ amount: Decimal, of total: Decimal) -> String {
        FinanceMoney.percent(total > 0 ? NSDecimalNumber(decimal: amount / total).doubleValue : 0)
    }

    // MARK: Period and money

    /// The period shown as the rows write it: "2026-09" or "2026".
    private var periodText: String {
        periodKind == .month ? finance.month(month) : String(format: "%04d", year)
    }

    private var currentYear: Int { finance.dates.component(.year, from: Date()) }

    private var firstYear: Int {
        min(SharedSpendingSummary.firstYear(in: rows) ?? currentYear, currentYear)
    }

    private var currencyCodes: [String] { SharedSpendingSummary.currencyCodes(in: rows) }

    /// The currency shown: the one chosen, else the one with the most money out.
    private var currencyCode: String {
        currencyCodes.contains(chosenCurrencyCode) ? chosenCurrencyCode : (currencyCodes.first ?? "")
    }

    private func moneyText(_ amount: Decimal) -> String {
        FinanceMoney.shown(NSDecimalNumber(decimal: amount).stringValue, currencyCode: currencyCode)
    }

    private func doubleValue(_ amount: Decimal) -> Double {
        NSDecimalNumber(decimal: amount).doubleValue
    }

    private func load() async {
        do {
            rows = try await finance.connection.sharedFinanceSpending(ownerUserId: sharer.userId)
            hasLoaded = true
            loadFailedText = nil
            placeMonth()
        } catch {
            loadFailedText = error.loadProblemText
        }
    }

    /// Shows the latest month that has rows when the current one has none, so
    /// the page does not open empty.
    private func placeMonth() {
        guard !hasPlacedMonth, let latestMonth = SharedSpendingSummary.latestMonth(in: rows) else { return }
        hasPlacedMonth = true
        let isCurrentMonthEmpty = !rows.contains { $0.spendingMonth == finance.month(month) }
        if isCurrentMonthEmpty, let latestDate = finance.date(ofDay: "\(latestMonth)-15") {
            month = latestDate
            year = finance.dates.component(.year, from: latestDate)
        }
    }
}
