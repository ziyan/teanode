import Charts
import PhotosUI
import SwiftUI
import TeaNodeKit

/// FinanceModel is what every finance screen shares: the person's time
/// zone, the categories and the accounts, read once and again after a
/// change.
@MainActor
@Observable
final class FinanceModel {
    let connection: Connection
    private(set) var dates = Calendar.current
    private(set) var categories: [SpendingCategory] = []
    private(set) var accounts: [FinanceAccount] = []
    private(set) var reportingCurrency = ""
    private(set) var isShown: Bool?
    var doneText: String?
    var problemText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    func load() async {
        do {
            isShown = try await connection.isFinanceShown()
            var zoned = Calendar(identifier: .gregorian)
            zoned.timeZone = try await connection.financeZone()
            dates = zoned
            categories = try await connection.spendingCategories()
            accounts = try await connection.financeAccounts()
            reportingCurrency = (try? await connection.reportingCurrency().code) ?? ""
        } catch {
            problemText = error.localizedDescription
        }
    }

    func category(_ id: String?) -> SpendingCategory? {
        guard let id, !id.isEmpty else { return nil }
        return categories.first { $0.id == id }
    }

    func categoryName(_ id: String?) -> String {
        guard let category = category(id) else { return String(localized: "Uncategorized") }
        return label(category)
    }

    /// A category as shown: in the reader's words when it is one of the
    /// built-in ones, under its parent.
    func label(_ category: SpendingCategory) -> String {
        FinanceCategories.label(category, in: categories, named: FinanceCategoryNames.shown)
    }

    /// The categories to choose from, in the order they are shown.
    func pickable(keeping chosen: String?) -> [SpendingCategory] {
        FinanceCategories.pickable(categories, keeping: chosen, named: FinanceCategoryNames.shown)
    }

    func account(_ id: String?) -> FinanceAccount? {
        accounts.first { $0.id == id }
    }

    /// Today as the person's calendar has it: "2026-10-10".
    var today: String { day(Date()) }

    func day(_ date: Date) -> String {
        let parts = dates.dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", parts.year ?? 1970, parts.month ?? 1, parts.day ?? 1)
    }

    /// A day written as the server takes it, read back in the person's zone.
    func date(ofDay text: String) -> Date? {
        let parts = text.split(separator: "-").compactMap { Int($0) }
        guard parts.count == 3 else { return nil }
        return dates.date(from: DateComponents(year: parts[0], month: parts[1], day: parts[2]))
    }

    func month(_ date: Date) -> String {
        let parts = dates.dateComponents([.year, .month], from: date)
        return String(format: "%04d-%02d", parts.year ?? 1970, parts.month ?? 1)
    }

    /// Runs a change and says what it did.
    func act(saying said: String, _ work: @MainActor () async throws -> Void) async -> Bool {
        do {
            try await work()
            doneText = said
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }
}

/// FinancePart is one section of the dashboard's Finance page. The side
/// menu lists them under Finance, so each is one tap away.
enum FinancePart: String, CaseIterable, Identifiable, Hashable {
    case spending, transactions, accounts, budgets, netWorth, savingsTargets, receipts, taxes, setup
    var id: String { rawValue }

    var title: String {
        switch self {
        case .spending: String(localized: "Spending")
        case .transactions: String(localized: "Transactions")
        case .accounts: String(localized: "Accounts")
        case .budgets: String(localized: "Budgets")
        case .netWorth: String(localized: "Net worth")
        case .savingsTargets: String(localized: "Savings targets")
        case .receipts: String(localized: "Receipts")
        case .taxes: String(localized: "Taxes")
        case .setup: String(localized: "Banks and currency")
        }
    }

    var systemImage: String {
        switch self {
        case .spending: "chart.pie"
        case .transactions: "list.bullet.rectangle"
        case .accounts: "building.columns"
        case .budgets: "gauge.with.dots.needle.33percent"
        case .netWorth: "chart.line.uptrend.xyaxis"
        case .savingsTargets: "target"
        case .receipts: "receipt"
        case .taxes: "doc.text"
        case .setup: "link"
        }
    }
}

/// FinanceScreen is one section of the dashboard's Finance page, chosen in
/// the side menu, with the button that opens the menu at its top left.
struct FinanceScreen: View {
    let finance: FinanceModel
    let part: FinancePart

    var body: some View {
        NavigationStack {
            Group {
                switch part {
                case .spending: SpendingView()
                case .transactions: TransactionsView(filter: FinanceTransactionFilter())
                case .accounts: AccountsView()
                case .budgets: BudgetsView()
                case .netWorth: NetWorthView()
                case .savingsTargets: SavingsTargetsView()
                case .receipts: ReceiptsView()
                case .taxes: TaxesView()
                case .setup: FinanceSetupView()
                }
            }
            .id(part)
            .toolbar { MenuButton() }
            .withoutSwipeBack()
            .navigationDestination(for: TransactionLink.self) { link in
                TransactionsView(filter: link.filter)
            }
            .overlay {
                if finance.isShown == false, part != .setup {
                    ContentUnavailableView("Finance is not set up", systemImage: "building.columns", description: Text("Link a bank from your dashboard: Settings, then Agent, then Finance."))
                }
            }
        }
        .environment(finance)
        .task { if finance.isShown == nil { await finance.load() } }
        .toast(done: Bindable(finance).doneText, problem: Bindable(finance).problemText)
    }
}

/// TransactionLink opens the transactions narrowed to what was tapped.
struct TransactionLink: Hashable {
    var filter: FinanceTransactionFilter
}

/// MonthStepper moves between months: back, forward, never past this one.
struct MonthStepper: View {
    @Binding var month: Date
    let dates: Calendar

    var body: some View {
        HStack {
            Button {
                month = dates.date(byAdding: .month, value: -1, to: month) ?? month
            } label: {
                Image(systemName: "chevron.backward").frame(minWidth: 44, minHeight: 44)
            }
            .accessibilityLabel("Earlier")
            Spacer()
            Text(month.formatted(.dateTime.month(.wide).year()))
                .font(.headline)
                .contentTransition(.numericText())
            Spacer()
            Button {
                month = dates.date(byAdding: .month, value: 1, to: month) ?? month
            } label: {
                Image(systemName: "chevron.forward").frame(minWidth: 44, minHeight: 44)
            }
            .accessibilityLabel("Later")
            .disabled(dates.isDate(month, equalTo: Date(), toGranularity: .month))
        }
        // In a list row every button fires on any tap in the row unless
        // the buttons are borderless.
        .buttonStyle(.borderless)
    }
}

// MARK: Spending

/// SpendingLine is one row of "Where it went".
struct SpendingLine: Identifiable {
    let id: String
    let label: String
    let amount: Decimal
    let count: Int
}

/// SpendingSlice is one slice of the "Where it went" donut: a group of its
/// own, or the groups beyond the palette together, and the color its rows
/// share.
struct SpendingSlice: Identifiable {
    /// The slice the groups beyond the palette share.
    static let restId = "\u{0}rest"

    let id: String
    let label: String
    let amount: Decimal
    let color: Color

    var amountValue: Double { NSDecimalNumber(decimal: amount).doubleValue }
}

/// SpendingView is a month as the dashboard's Spending tab shows it: the
/// year's cash flow, the month's saving, its budgets and where it went.
struct SpendingView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var month = Date()
    @State private var cashFlow: CashFlow?
    @State private var saving: SavingSummary?
    @State private var budgets: BudgetStatus?
    @State private var summary: SpendingSummary?
    @State private var groupBy = "spendingCategory"
    /// The month touched on the cash flow chart, as "2026-03".
    @State private var selectedCashFlowMonth: String?
    /// Where a touch on the donut is, as Swift Charts reports it: a running
    /// total from the start of the first slice.
    @State private var selectedRunningTotal: Double?
    /// The slice chosen on the donut or by its row; it stays chosen when the
    /// finger lifts.
    @State private var selectedSliceId: String?
    /// The transactions a row opens.
    @State private var openedLink: TransactionLink?

    var body: some View {
        ScrollViewReader { scrollProxy in
            spendingList
                #if DEBUG
                    .task(id: summary == nil) { await showDemoScreen(scrollProxy: scrollProxy) }
                #endif
        }
        .navigationTitle("Spending")
        .navigationDestination(item: $openedLink) { link in
            TransactionsView(filter: link.filter)
        }
        .task(id: "\(finance.month(month))-\(groupBy)") {
            selectedSliceId = nil
            await load()
        }
        .refreshable { await load() }
        .onChange(of: selectedSliceId) { _, sliceId in
            announce(sliceId: sliceId)
        }
    }

    private var spendingList: some View {
        List {
            Section {
                MonthStepper(month: $month, dates: finance.dates)
            }
            if let months = cashFlow?.cashFlowMonths?.compactMap({ $0 }), !months.isEmpty {
                Section("Cash flow") {
                    cashFlowChart(months)
                }
            }
            if let saving {
                Section("Saving") {
                    MoneyRow(title: String(localized: "Income"), amount: saving.incomeAmount, of: saving.expectedIncomeAmount, currency: saving.reportingCurrencyCode)
                    MoneyRow(title: String(localized: "Spending"), amount: saving.spendingAmount, of: saving.expectedSpendingAmount, currency: saving.reportingCurrencyCode)
                    MoneyRow(title: String(localized: "Saved"), amount: saving.savingAmount, of: saving.expectedSavingAmount, currency: saving.reportingCurrencyCode)
                    if let pace = saving.savingPace, !pace.isEmpty {
                        PaceTag(pace: pace)
                    }
                }
            }
            if let rows = budgets?.spendingCategories?.compactMap({ $0 }), !rows.isEmpty {
                Section("Budgets") {
                    ForEach(rows, id: \.spendingCategoryId) { row in
                        NavigationLink(value: TransactionLink(filter: filter(category: row.spendingCategoryId))) {
                            BudgetRow(
                                name: finance.category(row.spendingCategoryId) != nil ? finance.categoryName(row.spendingCategoryId) : FinanceCategoryNames.shown(row.spendingCategoryName ?? ""), spent: row.spendingAmount,
                                budget: row.budgetAmount, projected: row.projectedAmount, pace: row.budgetPace, currency: row.currencyCode)
                        }
                    }
                }
            }
            Section {
                Picker("Group by", selection: $groupBy) {
                    Text("Category").tag("spendingCategory")
                    Text("Merchant").tag("merchant")
                    Text("Account").tag("financeAccount")
                }
                .pickerStyle(.segmented)
                .id(Self.whereItWentId)
                let lines = spendingLines
                if lines.isEmpty {
                    Text("No spending this month.").foregroundStyle(.secondary)
                } else {
                    let slices = spendingSlices(lines)
                    let total = lines.reduce(Decimal(0)) { $0 + $1.amount }
                    if !slices.isEmpty {
                        spendingDonut(slices, total: total)
                    }
                    ForEach(Array(lines.enumerated()), id: \.element.id) { rank, line in
                        let lineSlice = drawnSlice(ofRank: rank, line: line, in: slices)
                        Button {
                            selectedSliceId = lineSlice?.id
                            openedLink = TransactionLink(filter: filter(groupKey: line.id))
                        } label: {
                            SpendingLineRow(line: line, color: lineSlice?.color, currency: currency, shareText: shareText(line.amount, of: total))
                        }
                        .tint(Color.primary)
                        .accessibilityAddTraits(lineSlice != nil && lineSlice?.id == selectedSliceId ? .isSelected : [])
                        .accessibilityHint(Text("Shows its transactions"))
                    }
                }
            } header: {
                Text("Where it went")
            }
        }
    }

    /// The year's income and spending by month, side by side in the two
    /// colors its legend names. Touching a month shows its numbers.
    private func cashFlowChart(_ months: [CashFlow.Month]) -> some View {
        let incomeWord = String(localized: "Income")
        let spendingWord = String(localized: "Spending")
        let incomeColor = ChartPalette.seriesColor(at: 0)
        let spendingColor = ChartPalette.seriesColor(at: 1)
        let currencyCode = cashFlow?.reportingCurrencyCode.flatMap { $0.isEmpty ? nil : $0 } ?? finance.reportingCurrency
        let selectedFlow = months.first { $0.cashFlowMonth == selectedCashFlowMonth }
        return VStack(alignment: .leading, spacing: 8) {
            // The legend's line is tall enough for the callout of a touched
            // month to take its place rather than cover the row above.
            ChartKey(entries: [ChartKeyEntry(label: incomeWord, color: incomeColor), ChartKeyEntry(label: spendingWord, color: spendingColor)])
                .frame(minHeight: 44, alignment: .bottomLeading)
                .opacity(selectedFlow == nil ? 1 : 0)
            Chart {
                ForEach(months, id: \.cashFlowMonth) { flow in
                    let opacity = ChartPalette.markOpacity(isChosen: flow.cashFlowMonth == selectedFlow?.cashFlowMonth, isAnyChosen: selectedFlow != nil)
                    BarMark(x: .value("Month", flow.cashFlowMonth), y: .value("Amount", amount(flow.incomeAmount)))
                        .foregroundStyle(by: .value("Kind", incomeWord))
                        .position(by: .value("Kind", incomeWord))
                        .opacity(opacity)
                        .accessibilityLabel(Text(verbatim: "\(monthTitle(flow.cashFlowMonth)), \(incomeWord)"))
                        .accessibilityValue(Text(verbatim: FinanceMoney.shown(flow.incomeAmount, currencyCode: currencyCode)))
                    BarMark(x: .value("Month", flow.cashFlowMonth), y: .value("Amount", amount(flow.spendingAmount)))
                        .foregroundStyle(by: .value("Kind", spendingWord))
                        .position(by: .value("Kind", spendingWord))
                        .opacity(opacity)
                        .accessibilityLabel(Text(verbatim: "\(monthTitle(flow.cashFlowMonth)), \(spendingWord)"))
                        .accessibilityValue(Text(verbatim: FinanceMoney.shown(flow.spendingAmount, currencyCode: currencyCode)))
                }
                if let selectedFlow {
                    ChartSelectionRule(value: selectedFlow.cashFlowMonth) {
                        ChartCallout(
                            title: monthTitle(selectedFlow.cashFlowMonth),
                            figures: [
                                ChartFigure(label: incomeWord, valueText: FinanceMoney.shown(selectedFlow.incomeAmount, currencyCode: currencyCode), color: incomeColor),
                                ChartFigure(label: spendingWord, valueText: FinanceMoney.shown(selectedFlow.spendingAmount, currencyCode: currencyCode), color: spendingColor),
                                ChartFigure(label: String(localized: "Net"), valueText: FinanceMoney.shown(netAmountText(selectedFlow), currencyCode: currencyCode)),
                            ])
                    }
                }
            }
            .chartForegroundStyleScale(domain: [incomeWord, spendingWord], range: [incomeColor, spendingColor])
            .chartLegend(.hidden)
            .chartXSelection(value: $selectedCashFlowMonth)
            .keepsSidePanelClosed()
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

    /// The month's groups as a donut, each in its color, the rest in gray.
    /// Touching a slice chooses it: it stays full, the others step back,
    /// and the middle says what it is. With none chosen the middle says the
    /// total.
    private func spendingDonut(_ slices: [SpendingSlice], total: Decimal) -> some View {
        let selectedSlice = slices.first { $0.id == selectedSliceId }
        return Chart(slices) { slice in
            let isChosen = slice.id == selectedSlice?.id
            SectorMark(
                angle: .value("Amount", slice.amountValue),
                innerRadius: .ratio(0.62),
                outerRadius: .ratio(isChosen || selectedSlice == nil ? 1 : 0.94),
                angularInset: 1
            )
            .foregroundStyle(slice.color)
            .opacity(ChartPalette.markOpacity(isChosen: isChosen, isAnyChosen: selectedSlice != nil))
            .accessibilityLabel(Text(verbatim: slice.label))
            .accessibilityValue(Text(verbatim: "\(moneyText(slice.amount)), \(shareText(slice.amount, of: total))"))
        }
        .chartLegend(.hidden)
        .chartAngleSelection(value: $selectedRunningTotal)
        .onChange(of: selectedRunningTotal) { previousTotal, runningTotal in
            guard let runningTotal, let index = ChartShares.sliceIndex(atRunningTotal: runningTotal, amounts: slices.map(\.amountValue)) else { return }
            let touchedId = slices[index].id
            // A new touch on the chosen slice lets it go; a drag across
            // the donut chooses whatever is under the finger.
            selectedSliceId = previousTotal == nil && touchedId == selectedSliceId ? nil : touchedId
        }
        .chartBackground { chartProxy in
            GeometryReader { geometry in
                if let plotFrame = chartProxy.plotFrame {
                    let frame = geometry[plotFrame]
                    let holeWidth = min(frame.width, frame.height) * 0.62
                    VStack(spacing: 2) {
                        Text(verbatim: selectedSlice?.label ?? String(localized: "Total"))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                        Text(verbatim: moneyText(selectedSlice?.amount ?? total))
                            .font(.headline)
                            .monospacedDigit()
                            .lineLimit(1)
                            .contentTransition(.numericText())
                        if let selectedSlice {
                            Text(verbatim: shareText(selectedSlice.amount, of: total))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                        }
                    }
                    .multilineTextAlignment(.center)
                    .minimumScaleFactor(0.4)
                    .frame(width: holeWidth * 0.82)
                    .position(x: frame.midX, y: frame.midY)
                    .accessibilityHidden(true)
                }
            }
        }
        .frame(height: 220)
        .padding(.vertical, 4)
    }

    /// The donut's slices: the largest groups each in a color of the
    /// palette, then the rest together in gray. Only money out is drawn; a
    /// group that took in more than it spent has a row but no slice.
    private func spendingSlices(_ lines: [SpendingLine]) -> [SpendingSlice] {
        let drawn = lines.filter { $0.amount > 0 }
        let sliceAmounts = ChartShares.sliceAmounts(drawn.map(\.amount))
        return sliceAmounts.enumerated().map { index, sliceAmount in
            if index < ChartShares.ownSliceLimit {
                let line = drawn[index]
                return SpendingSlice(id: line.id, label: line.label, amount: sliceAmount, color: ChartPalette.seriesColor(at: index))
            }
            return SpendingSlice(id: SpendingSlice.restId, label: String(localized: "Everything else"), amount: sliceAmount, color: ChartPalette.restColor)
        }
    }

    /// The slice a row's group is drawn in, by the row's rank; none for a
    /// group that took in more than it spent.
    private func drawnSlice(ofRank rank: Int, line: SpendingLine, in slices: [SpendingSlice]) -> SpendingSlice? {
        guard line.amount > 0 else { return nil }
        let index = ChartShares.sliceIndex(ofRank: rank)
        return slices.indices.contains(index) ? slices[index] : nil
    }

    private func moneyText(_ amount: Decimal) -> String {
        FinanceMoney.shown(NSDecimalNumber(decimal: amount).stringValue, currencyCode: currency)
    }

    private func shareText(_ amount: Decimal, of total: Decimal) -> String {
        FinanceMoney.percent(total > 0 ? NSDecimalNumber(decimal: amount / total).doubleValue : 0)
    }

    /// Tells VoiceOver which slice was chosen, since the change is drawn.
    private func announce(sliceId: String?) {
        let lines = spendingLines
        let total = lines.reduce(Decimal(0)) { $0 + $1.amount }
        guard let sliceId, let slice = spendingSlices(lines).first(where: { $0.id == sliceId }) else { return }
        let spokenText = "\(slice.label), \(moneyText(slice.amount)), \(shareText(slice.amount, of: total))"
        AccessibilityNotification.Announcement(spokenText).post()
    }

    /// A cash flow month, "2026-03", in the middle of that month on the
    /// phone's calendar, so no time zone moves it into another.
    private func cashFlowDate(_ cashFlowMonth: String) -> Date? {
        let parts = cashFlowMonth.split(separator: "-").compactMap { Int($0) }
        guard parts.count == 2 else { return nil }
        return Calendar.current.date(from: DateComponents(year: parts[0], month: parts[1], day: 15))
    }

    /// A month on the axis: its first letter, as Health labels a year.
    private func monthLetter(_ cashFlowMonth: String) -> String {
        cashFlowDate(cashFlowMonth)?.formatted(.dateTime.month(.narrow)) ?? String(cashFlowMonth.suffix(2))
    }

    private func monthTitle(_ cashFlowMonth: String) -> String {
        cashFlowDate(cashFlowMonth)?.formatted(.dateTime.month(.wide).year()) ?? cashFlowMonth
    }

    private func netAmountText(_ flow: CashFlow.Month) -> String? {
        if let netAmount = flow.netAmount, !netAmount.isEmpty { return netAmount }
        guard let income = FinanceMoney.decimal(flow.incomeAmount), let spending = FinanceMoney.decimal(flow.spendingAmount) else { return nil }
        return NSDecimalNumber(decimal: income - spending).stringValue
    }

    private static let whereItWentId = "whereItWent"

    #if DEBUG
        /// The state a screenshot names (`-TeaNodeScreen`): the donut
        /// scrolled into view, a slice chosen, or a month touched.
        private func showDemoScreen(scrollProxy: ScrollViewProxy) async {
            guard summary != nil, let screen = DemoServer.screen else { return }
            // The rows are laid out a moment after the answer arrives.
            try? await Task.sleep(for: .milliseconds(500))
            switch screen {
            case "spending-groups", "spending-chosen":
                scrollProxy.scrollTo(Self.whereItWentId, anchor: .top)
                if screen == "spending-chosen" { selectedSliceId = spendingSlices(spendingLines).first?.id }
            case "cash-flow-chosen":
                let months = (cashFlow?.cashFlowMonths ?? []).compactMap { $0 }
                if months.count > 3 { selectedCashFlowMonth = months[months.count - 4].cashFlowMonth }
            default:
                break
            }
        }
    #endif

    private var currency: String {
        summary?.reportingCurrencyCode.flatMap { $0.isEmpty ? nil : $0 } ?? finance.reportingCurrency
    }

    private func amount(_ text: String?) -> Double {
        FinanceMoney.decimal(text).map { NSDecimalNumber(decimal: $0).doubleValue } ?? 0
    }

    /// The days the month covers: its first, and today or its last.
    private var range: (from: String, to: String) {
        let dates = finance.dates
        let start = dates.date(from: dates.dateComponents([.year, .month], from: month)) ?? month
        let end = dates.date(byAdding: DateComponents(month: 1, day: -1), to: start) ?? start
        return (finance.day(start), finance.day(min(end, Date())))
    }

    private func load() async {
        let connection = finance.connection
        let monthText = finance.month(month)
        let span = range
        let grouping = groupBy
        let toMonth = finance.month(Date())
        let fromMonth = finance.month(finance.dates.date(byAdding: .month, value: -11, to: Date()) ?? Date())
        async let flow = connection.cashFlow(fromMonth: fromMonth, toMonth: toMonth)
        async let saved = connection.savingSummary(month: monthText)
        async let status = connection.budgetStatus(month: monthText)
        async let spent = connection.spendingSummary(from: span.from, to: span.to, groupBy: grouping)
        cashFlow = try? await flow
        saving = try? await saved
        budgets = try? await status
        summary = try? await spent
    }

    /// The rows as the dashboard counts them: income left out; Other and
    /// uncategorized by money out alone, the rest by money out less money
    /// in; nothing left at zero, and merchants and accounts only above it.
    private var spendingLines: [SpendingLine] {
        guard let summary else { return [] }
        let isConverted = !(summary.reportingCurrencyCode ?? "").isEmpty
        struct Raw {
            let key: String
            let label: String
            let moneyOut: Decimal
            let moneyIn: Decimal
            let count: Int
        }
        let raws: [Raw] = isConverted
            ? (summary.convertedSpendingSummaryRows ?? []).compactMap { $0 }.map { row in
                let count = (summary.spendingSummaryRows ?? []).compactMap { $0 }.filter { $0.groupKey == row.groupKey }
                    .reduce(0) { $0 + ($1.financeTransactionCount ?? 0) }
                return Raw(key: row.groupKey, label: row.groupLabel ?? "", moneyOut: FinanceMoney.decimal(row.moneyOut) ?? 0, moneyIn: FinanceMoney.decimal(row.moneyIn) ?? 0, count: count)
            }
            : (summary.spendingSummaryRows ?? []).compactMap { $0 }.map { row in
                Raw(key: row.groupKey, label: row.groupLabel ?? "", moneyOut: FinanceMoney.decimal(row.moneyOut) ?? 0, moneyIn: FinanceMoney.decimal(row.moneyIn) ?? 0, count: row.financeTransactionCount ?? 0)
            }
        return raws.compactMap { raw in
            let category = groupBy == "spendingCategory" ? finance.category(raw.key) : nil
            if category?.isIncome == true { return nil }
            let isOutOnly = groupBy == "spendingCategory" && (raw.key.isEmpty || category?.isOther == true)
            let amount = isOutOnly ? raw.moneyOut : raw.moneyOut - raw.moneyIn
            if amount == 0 || (groupBy != "spendingCategory" && amount <= 0) { return nil }
            let label = groupBy == "spendingCategory" ? finance.categoryName(raw.key) : (raw.label.isEmpty ? String(localized: "Unknown") : raw.label)
            return SpendingLine(id: raw.key, label: label, amount: amount, count: raw.count)
        }
        .sorted { $0.amount > $1.amount }
    }

    private func filter(category: String) -> FinanceTransactionFilter {
        var filter = FinanceTransactionFilter()
        filter.from = range.from
        filter.to = range.to
        filter.spendingCategoryId = category
        return filter
    }

    private func filter(groupKey: String) -> FinanceTransactionFilter {
        var filter = FinanceTransactionFilter()
        filter.from = range.from
        filter.to = range.to
        switch groupBy {
        case "spendingCategory":
            if groupKey.isEmpty { filter.isUncategorized = true } else { filter.spendingCategoryId = groupKey }
        case "financeAccount":
            filter.financeAccountId = groupKey
        default:
            filter.text = spendingLines.first { $0.id == groupKey }?.label ?? groupKey
        }
        return filter
    }
}

/// SpendingLineRow is one group of "Where it went": its color on the
/// donut, its name and count, and its amount and share, opening its
/// transactions.
struct SpendingLineRow: View {
    let line: SpendingLine
    /// The color of the slice it is drawn in; none for a group that took
    /// in more than it spent, which the donut leaves out.
    let color: Color?
    let currency: String
    let shareText: String
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        HStack(spacing: 12) {
            ChartSwatch(color: color ?? .clear)
            // Side by side, or one under the other at the accessibility
            // sizes, where side by side breaks the words apart.
            let layout = dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4)) : AnyLayout(HStackLayout())
            layout {
                VStack(alignment: .leading) {
                    Text(line.label).foregroundStyle(.primary)
                    Text("\(line.count) transactions").font(.caption).foregroundStyle(.secondary)
                }
                Spacer(minLength: 0)
                VStack(alignment: dynamicTypeSize.isAccessibilitySize ? .leading : .trailing) {
                    Text(FinanceMoney.shown(NSDecimalNumber(decimal: line.amount).stringValue, currencyCode: currency))
                        .monospacedDigit()
                        .foregroundStyle(.primary)
                    Text(shareText)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            // A row opens a screen, so it carries the chevron a navigation
            // row would; it is a button so that the tap chooses its slice too.
            Image(systemName: "chevron.forward")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.tertiary)
                .accessibilityHidden(true)
        }
        .contentShape(.rect)
    }
}

/// MoneyRow is an amount so far, and what was expected, side by side.
struct MoneyRow: View {
    let title: String
    let amount: String?
    let of: String?
    let currency: String?

    var body: some View {
        LabeledContent(title) {
            VStack(alignment: .trailing) {
                Text(FinanceMoney.shown(amount, currencyCode: currency)).monospacedDigit()
                if let of, !of.isEmpty {
                    Text("of \(FinanceMoney.shown(of, currencyCode: currency))").font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }
}

/// PaceTag is how a budget is going, in words and color.
struct PaceTag: View {
    let pace: String

    var body: some View {
        let (text, isBad, isWarn): (String, Bool, Bool) = switch pace {
        case "under", "on_track", "ahead": (String(localized: "On track"), false, false)
        case "at_risk", "behind": (String(localized: "At risk"), false, true)
        case "over": (String(localized: "Over"), true, false)
        default: (pace, false, false)
        }
        Text(text)
            .font(.caption.weight(.medium))
            .foregroundStyle(isBad ? AnyShapeStyle(.red) : isWarn ? AnyShapeStyle(.orange) : AnyShapeStyle(.green))
    }
}

/// BudgetRow is a category's spending against its budget.
struct BudgetRow: View {
    let name: String
    let spent: String?
    let budget: String?
    let projected: String?
    let pace: String?
    let currency: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(name)
                Spacer()
                if let pace, !pace.isEmpty { PaceTag(pace: pace) }
            }
            let spentAmount = FinanceMoney.decimal(spent) ?? 0
            let budgetAmount = FinanceMoney.decimal(budget) ?? 0
            if budgetAmount > 0 {
                ProgressView(value: min(1, NSDecimalNumber(decimal: spentAmount / budgetAmount).doubleValue))
                    .tint(pace == "over" ? .red : pace == "at_risk" ? .orange : .accentColor)
            }
            HStack {
                Text("\(FinanceMoney.shown(spent, currencyCode: currency)) of \(FinanceMoney.shown(budget, currencyCode: currency))")
                    .monospacedDigit()
                Spacer()
                if let projected, !projected.isEmpty {
                    Text("heading for \(FinanceMoney.shown(projected, currencyCode: currency))")
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
    }
}

// MARK: Transactions

/// TransactionsView is the transactions, narrowed, fifty at a time, each
/// filed under a category from a menu and opened on a tap.
struct TransactionsView: View {
    @Environment(FinanceModel.self) private var finance
    @State var filter: FinanceTransactionFilter
    @State private var transactions: [FinanceTransaction] = []
    @State private var total = 0
    @State private var leftOutCount = 0
    @State private var isLoaded = false
    @State private var isNarrowing = false

    var body: some View {
        List {
            if total > 0 {
                Text(leftOutCount > 0 ? String(localized: "\(total) transactions · \(leftOutCount) duplicates left out") : String(localized: "\(total) transactions"))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            ForEach(transactions) { transaction in
                NavigationLink {
                    TransactionDetail(transactionId: transaction.id).environment(finance)
                } label: {
                    TransactionRow(transaction: transaction)
                }
                .contextMenu { categoryMenu(transaction) }
                .onAppear {
                    if transaction.id == transactions.last?.id, transactions.count < total {
                        Task { await load(isMore: true) }
                    }
                }
            }
        }
        .listStyle(.plain)
        .navigationTitle("Transactions")
        .searchable(text: $filter.text, prompt: "Merchant or description")
        .task(id: filter) {
            if !filter.text.isEmpty {
                try? await Task.sleep(for: .milliseconds(400))
                guard !Task.isCancelled else { return }
            }
            await load(isMore: false)
        }
        .refreshable { await load(isMore: false) }
        .overlay {
            if isLoaded, transactions.isEmpty {
                ContentUnavailableView("No transactions", systemImage: "list.bullet.rectangle")
            } else if !isLoaded {
                ProgressView()
            }
        }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    isNarrowing = true
                } label: {
                    Label("Filter", systemImage: filter.isNarrowing ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
                }
            }
        }
        .sheet(isPresented: $isNarrowing) {
            TransactionFilterSheet(filter: $filter).environment(finance)
        }
    }

    @ViewBuilder
    private func categoryMenu(_ transaction: FinanceTransaction) -> some View {
        Menu("Category") {
            ForEach(finance.pickable(keeping: transaction.spendingCategoryId)) { category in
                Button(finance.label(category)) {
                    Task { await categorize(transaction, category) }
                }
            }
        }
    }

    private func categorize(_ transaction: FinanceTransaction, _ category: SpendingCategory) async {
        _ = await finance.act(saying: String(localized: "Filed under \(finance.label(category))")) {
            _ = try await finance.connection.categorize(transaction.id, as: category.id, makingRule: false)
        }
        await load(isMore: false)
    }

    private func load(isMore: Bool) async {
        do {
            let page = try await finance.connection.financeTransactions(filter, offset: isMore ? transactions.count : 0)
            let fresh = page.financeTransactions.compactMap { $0 }
            transactions = isMore ? transactions + fresh.filter { row in !transactions.contains { $0.id == row.id } } : fresh
            total = page.totalCount ?? transactions.count
            leftOutCount = page.leftOutDuplicateCount ?? 0
            isLoaded = true
        } catch {
            finance.problemText = error.localizedDescription
        }
    }
}

/// TransactionRow is a transaction: when, who, its category and amount.
struct TransactionRow: View {
    @Environment(FinanceModel.self) private var finance
    let transaction: FinanceTransaction

    var body: some View {
        // The amount is centered on the row, level with the disclosure
        // chevron, rather than on the first line.
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    Text(transaction.shownName).lineLimit(1)
                    if (transaction.receiptCount ?? 0) > 0 {
                        Image(systemName: "receipt").font(.caption).foregroundStyle(.secondary).accessibilityLabel("Has a receipt")
                    }
                }
                HStack(spacing: 4) {
                    Text(transaction.postedOn ?? "")
                    Text(finance.categoryName(transaction.spendingCategoryId))
                    if transaction.isPending == true { Text("Pending") }
                    if transaction.isDuplicate { Text("Duplicate") }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                if let annotation = transaction.annotation, !annotation.isEmpty {
                    Text(annotation).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                }
            }
            Spacer()
            Text(FinanceMoney.shown(transaction.amount, currencyCode: transaction.currencyCode))
                .monospacedDigit()
                .strikethrough(transaction.isDuplicate)
                .foregroundStyle((FinanceMoney.decimal(transaction.amount) ?? 0) > 0 ? AnyShapeStyle(.green) : AnyShapeStyle(.primary))
        }
    }
}

/// TransactionFilterSheet narrows the transactions: dates, account,
/// category, uncategorized only, and whether duplicates show.
struct TransactionFilterSheet: View {
    @Environment(FinanceModel.self) private var finance
    @Binding var filter: FinanceTransactionFilter
    @Environment(\.dismiss) private var dismiss
    @State private var draft = FinanceTransactionFilter()

    /// A day kept as text for the server, picked as a date.
    private func dayBinding(_ text: Binding<String>) -> Binding<Date?> {
        Binding(
            get: { finance.date(ofDay: text.wrappedValue) },
            set: { text.wrappedValue = $0.map { finance.day($0) } ?? "" })
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    OptionalDatePicker(title: String(localized: "From"), date: dayBinding($draft.from))
                    OptionalDatePicker(title: String(localized: "To"), date: dayBinding($draft.to))
                }
                Section {
                    Picker("Account", selection: $draft.financeAccountId) {
                        Text("All accounts").tag("")
                        ForEach(finance.accounts) { account in
                            Text(account.shownName).tag(account.id)
                        }
                    }
                    Picker("Category", selection: $draft.spendingCategoryId) {
                        Text("All categories").tag("")
                        ForEach(finance.pickable(keeping: draft.spendingCategoryId)) { category in
                            Text(finance.label(category)).tag(category.id)
                        }
                    }
                    .disabled(draft.isUncategorized)
                    Toggle("Only uncategorized", isOn: $draft.isUncategorized)
                    Toggle("Show duplicates", isOn: $draft.isDuplicateIncluded)
                }
                Section {
                    Button("Clear", role: .destructive) { draft = FinanceTransactionFilter() }
                }
            }
            .navigationTitle("Filter")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Apply") {
                        if draft.isUncategorized { draft.spendingCategoryId = "" }
                        filter = draft
                        dismiss()
                    }
                }
            }
            .onAppear { draft = filter }
        }
        .presentationDetents([.medium, .large])
    }
}

/// TransactionDetail is one transaction whole: its category, note,
/// duplicates and receipts, and asking the agent about it.
struct TransactionDetail: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(AppModel.self) private var app
    let transactionId: String
    @State private var transaction: FinanceTransaction?
    @State private var duplicates: [FinanceTransaction] = []
    @State private var receipts: [FinanceReceipt] = []
    @State private var annotation = ""
    @State private var categoryId = ""
    @State private var madeRuleFor: String?
    @State private var readingText: String?
    @State private var pickedItems: [PhotosPickerItem] = []
    @State private var isTakingPicture = false
    @State private var memory = InMemoryModel()

    var body: some View {
        List {
            if let transaction {
                Section {
                    LabeledContent("Amount") {
                        Text(FinanceMoney.shown(transaction.amount, currencyCode: transaction.currencyCode)).monospacedDigit()
                    }
                    LabeledContent("Posted", value: transaction.postedOn ?? "")
                    if let account = finance.account(transaction.financeAccountId) {
                        LabeledContent("Account", value: account.shownName)
                    }
                    if let description = transaction.description, !description.isEmpty {
                        Text(description).font(.footnote).foregroundStyle(.secondary).textSelection(.enabled)
                    }
                    if transaction.isPending == true {
                        Label("Pending", systemImage: "clock").font(.footnote)
                    }
                }
                Section {
                    Picker("Category", selection: $categoryId) {
                        Text("Uncategorized").tag("")
                        ForEach(finance.pickable(keeping: categoryId)) { category in
                            Text(finance.label(category)).tag(category.id)
                        }
                    }
                    .onChange(of: categoryId) { previous, chosen in
                        guard chosen != (transaction.spendingCategoryId ?? ""), !chosen.isEmpty else { return }
                        Task { await categorize(chosen, makingRule: false) }
                    }
                    if let by = transaction.categorizedBy, !by.isEmpty, by != "person" {
                        Text(FinanceCategoryNames.filedBy(by)).font(.caption).foregroundStyle(.secondary)
                    }
                    if !categoryId.isEmpty, madeRuleFor == nil {
                        Button("Always for this merchant") {
                            Task { await categorize(categoryId, makingRule: true) }
                        }
                    }
                    if let madeRuleFor {
                        Text("A rule now files “\(madeRuleFor)” here.").font(.caption).foregroundStyle(.secondary)
                    }
                    if transaction.isDuplicate {
                        Text("A duplicate's category does not count.").font(.caption).foregroundStyle(.secondary)
                    }
                }
                Section("Note") {
                    TextField("A note about this charge", text: $annotation, axis: .vertical)
                    if annotation.trimmed != (transaction.annotation ?? "") {
                        Button("Save note") { Task { await saveNote(annotation.trimmed) } }
                    }
                    if !(transaction.annotation ?? "").isEmpty {
                        Button("Clear note", role: .destructive) { Task { await saveNote("") } }
                    }
                }
                duplicatesSection(transaction)
                receiptsSection(transaction)
                InMemorySection(model: memory)
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .navigationTitle(transaction?.shownName ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    guard let transaction else { return }
                    app.askingAbout = AgentReference(
                        transactionId: transaction.id, postedOn: transaction.postedOn, amount: transaction.amount,
                        currencyCode: transaction.currencyCode, merchantName: transaction.merchantName, description: transaction.description)
                } label: {
                    Label("Ask \(app.agentTitle)", systemImage: "bubble.left")
                }
            }
        }
        .inMemory(memory, recordKind: "finance_transaction", recordId: transactionId)
        .task { await load() }
        .cameraPicker(isPresented: $isTakingPicture) { picture in
            if let file = PendingFile(picture: picture, index: 0) { Task { await readReceipt(file) } }
        }
    }

    @ViewBuilder
    private func duplicatesSection(_ transaction: FinanceTransaction) -> some View {
        if transaction.isDuplicate {
            Section {
                Text("Not counted: it is a duplicate of another charge.").font(.footnote)
                Button("Count this one") { Task { await count(isUndone: false) } }
                if transaction.duplicateDecidedBy == "person" {
                    Button("Let detection decide") { Task { await count(isUndone: true) } }
                }
            }
        } else if !duplicates.isEmpty {
            Section("Duplicates not counted") {
                ForEach(duplicates) { duplicate in
                    NavigationLink {
                        TransactionDetail(transactionId: duplicate.id).environment(finance)
                    } label: {
                        TransactionRow(transaction: duplicate)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func receiptsSection(_ transaction: FinanceTransaction) -> some View {
        Section {
            ForEach(receipts) { receipt in
                ReceiptCard(receipt: receipt, transactionId: transaction.id) {
                    Task { await load() }
                }
            }
            if let readingText {
                HStack {
                    ProgressView()
                    Text(readingText).font(.footnote).foregroundStyle(.secondary)
                }
            }
            Menu {
                PhotosPicker(selection: $pickedItems, maxSelectionCount: 1, matching: .images) {
                    Label("Photo library", systemImage: "photo.on.rectangle")
                }
                if UIImagePickerController.isSourceTypeAvailable(.camera) {
                    Button("Camera", systemImage: "camera") { isTakingPicture = true }
                }
            } label: {
                Label("Add a receipt", systemImage: "receipt")
            }
            .disabled((FinanceMoney.decimal(transaction.amount) ?? 0) > 0)
        } header: {
            Text("Receipts")
        } footer: {
            if (FinanceMoney.decimal(transaction.amount) ?? 0) > 0 {
                Text("Receipts are for money going out.")
            }
        }
        .onChange(of: pickedItems) { _, items in
            guard let item = items.first else { return }
            pickedItems = []
            Task {
                if let data = try? await item.loadTransferable(type: Data.self), let file = PendingFile(pictureData: data, index: 0) {
                    await readReceipt(file)
                }
            }
        }
    }

    private func load() async {
        let connection = finance.connection
        guard let read = try? await connection.financeTransactions(FinanceTransactionFilter(), ids: [transactionId], limit: 1).financeTransactions.first ?? nil
        else { return }
        transaction = read
        annotation = read.annotation ?? ""
        categoryId = read.spendingCategoryId ?? ""
        if !read.isDuplicate {
            duplicates = (try? await connection.financeTransactions(FinanceTransactionFilter(), duplicatesOf: read.id, limit: 200).financeTransactions.compactMap { $0 }) ?? []
        }
        receipts = (try? await connection.financeReceipts(transactionId: read.id, limit: 200).financeReceipts.compactMap { $0 }) ?? []
    }

    private func categorize(_ chosen: String, makingRule: Bool) async {
        var rule: String?
        let isDone = await finance.act(saying: makingRule ? String(localized: "Rule made") : String(localized: "Filed under \(finance.categoryName(chosen))")) {
            rule = try await finance.connection.categorize(transactionId, as: chosen, makingRule: makingRule)
        }
        if isDone, makingRule { madeRuleFor = rule ?? "" }
        await load()
    }

    private func saveNote(_ text: String) async {
        _ = await finance.act(saying: text.isEmpty ? String(localized: "Note cleared") : String(localized: "Note saved")) {
            try await finance.connection.annotate(transactionId, with: text)
        }
        await load()
    }

    private func count(isUndone: Bool) async {
        _ = await finance.act(saying: isUndone ? String(localized: "Detection decides again") : String(localized: "Counted")) {
            try await finance.connection.count(transactionId, isUndone: isUndone)
        }
        await load()
    }

    /// Uploads a picture of a receipt and has the agent read it against
    /// this charge, then waits for it: every four seconds, two minutes at
    /// most, as the dashboard waits.
    private func readReceipt(_ file: PendingFile) async {
        readingText = String(localized: "Reading the receipt…")
        do {
            let attachmentId = try await finance.connection.upload(name: file.name, contentType: file.contentType, data: file.data)
            try await finance.connection.readReceipt(attachmentId: attachmentId, transactionId: transactionId)
            for _ in 0..<30 {
                try? await Task.sleep(for: .seconds(4))
                let mine = (try? await finance.connection.financeReceipts(transactionId: transactionId, limit: 200).financeReceipts.compactMap { $0 }) ?? []
                if mine.contains(where: { $0.agentAttachmentId == attachmentId }) {
                    receipts = mine
                    readingText = nil
                    finance.doneText = String(localized: "Receipt read")
                    return
                }
                let unmatched = (try? await finance.connection.financeReceipts(isUnmatched: true, limit: 200).financeReceipts.compactMap { $0 }) ?? []
                if unmatched.contains(where: { $0.agentAttachmentId == attachmentId }) {
                    readingText = nil
                    finance.doneText = String(localized: "Read, but it did not match this charge")
                    return
                }
            }
            readingText = String(localized: "Still reading. It will appear here when done.")
        } catch {
            readingText = nil
            finance.problemText = error.localizedDescription
        }
    }
}

/// ReceiptCard is a receipt: where it is from, whether its lines add up,
/// the lines, and taking it off this charge or deleting it.
struct ReceiptCard: View {
    let receipt: FinanceReceipt
    let transactionId: String
    let onChange: () -> Void

    var body: some View {
        NavigationLink {
            ReceiptDetailView(receipt: receipt, transactionId: transactionId, onChange: onChange)
        } label: {
            ReceiptRow(receipt: receipt)
        }
    }
}

// MARK: Accounts

/// AccountsView is the credit cards' usage and every account's balance.
struct AccountsView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var usage: CreditUsage?
    @State private var accounts: [FinanceAccount] = []
    @State private var statementImport: StatementImport?
    @State private var isRegeneratingStatementAddress = false

    var body: some View {
        List {
            if let usage, let cards = usage.creditCards?.compactMap({ $0 }), !cards.isEmpty {
                Section("Credit used") {
                    HStack(spacing: 16) {
                        Gauge(value: min(1, usage.usageShare ?? 0)) {
                            EmptyView()
                        } currentValueLabel: {
                            Text(FinanceMoney.percent(usage.usageShare ?? 0))
                        }
                        .gaugeStyle(.accessoryCircularCapacity)
                        .tint(CreditWords.color(usage.usageShare ?? 0))
                        VStack(alignment: .leading, spacing: 2) {
                            Text("\(FinanceMoney.shown(usage.totalOwedAmount, currencyCode: usage.reportingCurrencyCode)) owed")
                            Text("of \(FinanceMoney.shown(usage.totalCreditLimitAmount, currencyCode: usage.reportingCurrencyCode))")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                    ForEach(cards, id: \.financeAccountId) { card in
                        LabeledContent {
                            VStack(alignment: .trailing) {
                                Text(FinanceMoney.shown(card.owedAmount, currencyCode: card.currencyCode)).monospacedDigit()
                                Text(card.usageShare.map { FinanceMoney.percent($0) } ?? String(localized: "limit unknown"))
                                    .font(.caption)
                                    .foregroundStyle(card.usageShare.map { CreditWords.color($0) } ?? Color.secondary)
                            }
                        } label: {
                            Text([card.accountName, card.accountMask.map { "··\($0)" }].compactMap { $0 }.joined(separator: " "))
                        }
                    }
                    if (usage.leftOutCardCount ?? 0) > 0 {
                        Text("\(usage.leftOutCardCount ?? 0) cards left out: their limits are not known.").font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            let byInstitution = Dictionary(grouping: accounts) { $0.institutionName ?? "" }
            ForEach(byInstitution.keys.sorted(), id: \.self) { institution in
                Section(institution.isEmpty ? String(localized: "Accounts") : institution) {
                    ForEach(byInstitution[institution] ?? []) { account in
                        NavigationLink(value: TransactionLink(filter: { var filter = FinanceTransactionFilter(); filter.financeAccountId = account.id; return filter }())) {
                            LabeledContent {
                                VStack(alignment: .trailing) {
                                    Text(FinanceMoney.shown(account.currentBalance, currencyCode: account.currencyCode)).monospacedDigit()
                                    if let available = account.availableBalance, !available.isEmpty {
                                        Text("\(FinanceMoney.shown(available, currencyCode: account.currencyCode)) available").font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                            } label: {
                                VStack(alignment: .leading) {
                                    Text(account.shownName)
                                    HStack(spacing: 4) {
                                        Text(account.accountKind ?? "")
                                        if account.isFromStatements { Text("From statements") }
                                        if account.isSignInRequired == true { Text("Sign-in required").foregroundStyle(.orange) }
                                    }
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                }
            }
            statementImportSection
        }
        .navigationTitle("Accounts")
        .task {
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(10))
            }
        }
        .task { await loadStatementImport() }
        .refreshable {
            await load()
            await loadStatementImport()
        }
    }

    /// The statement import, as the dashboard's Accounts tab shows it: the
    /// address to mail OFX statements to, with copying it and making a new
    /// one, whether importing is on, and what the last import did.
    @ViewBuilder
    private var statementImportSection: some View {
        if let statementImport {
            Section {
                if let address = statementImport.importAddress, !address.isEmpty {
                    Text(verbatim: address)
                        .font(.callout.monospaced())
                        .textSelection(.enabled)
                    Button("Copy address", systemImage: "doc.on.doc") {
                        UIPasteboard.general.string = address
                        finance.doneText = String(localized: "Address copied")
                    }
                } else {
                    Text("You have no mailbox address to receive statements at.")
                        .foregroundStyle(.secondary)
                }
                Button("New address", systemImage: "arrow.clockwise") { isRegeneratingStatementAddress = true }
                .confirmationDialog("New import address?", isPresented: $isRegeneratingStatementAddress, titleVisibility: .visible) {
                    Button("Make a new address", role: .destructive) {
                        Task {
                            var renewed: StatementImport?
                            if await finance.act(saying: String(localized: "Import address changed. The old one no longer takes mail."), { renewed = try await finance.connection.regenerateStatementImportAddress() }) {
                                self.statementImport = renewed
                            }
                        }
                    }
                } message: {
                    Text("You get a new import address, and mail to the old one is refused from now on. What was already imported stays.")
                }
                if statementImport.isEnabled == false {
                    Label("Importing is off. Turn the Imported statements source on to import again.", systemImage: "pause.circle")
                        .foregroundStyle(.secondary)
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text("Last import").foregroundStyle(.secondary).font(.subheadline)
                    Text(StatementImportWords.summary(statementImport.lastStatementImport))
                    if let day = StatementImportWords.dayText(statementImport.lastStatementImport) {
                        Text(day).font(.caption).foregroundStyle(.secondary)
                    }
                }
            } header: {
                Text("Import statements")
            } footer: {
                Text("For an account no provider reaches. From a phone's wallet: open the card's statement, choose Export Transactions, pick OFX, then share the file to Mail and send it to this address. A transaction imported twice is kept once, and your agent tells you what was imported.")
            }
        }
    }

    /// Reads the statement import; a failure says so and keeps what is
    /// shown.
    private func loadStatementImport() async {
        do {
            if let read = try await finance.connection.statementImport() { statementImport = read }
        } catch {
            finance.problemText = error.localizedDescription
        }
    }

    private func load() async {
        usage = try? await finance.connection.creditUsage()
        if let read = try? await finance.connection.financeAccounts() { accounts = read }
    }
}

enum CreditWords {
    /// Below 30% is good, to 50% a warning, above it bad, judged at the
    /// whole percent shown.
    static func color(_ share: Double) -> Color {
        let percent = (share * 100).rounded()
        if percent < 30 { return .green }
        if percent <= 50 { return .orange }
        return .red
    }
}
