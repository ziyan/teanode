import Foundation

// Receipts, paystubs and tax forms typed in by hand: the same arguments the
// CLI's record commands and the agent's finance tool send to RecordReceipt,
// PreviewRecordReceipt, RecordPaystub and RecordTaxForm, with the same
// words. A receipt, a paystub or a tax form is always recorded from a file
// the server holds (agentAttachmentId), so a form picks one first.

/// FinanceEntryKinds are the fixed vocabularies the record operations take.
public enum FinanceEntryKinds {
    public static let receiptLineKinds = ["item", "discount", "tax", "fee", "tip"]
    public static let paystubLineKinds = ["earning", "tax", "deduction", "reimbursement"]
    public static let paystubTaxKinds = [
        "federal_income", "social_security", "medicare", "medicare_additional", "state_income", "local_income",
        "state_disability", "other",
    ]
    public static let paystubDeductionKinds = [
        "retirement_traditional", "retirement_roth", "hsa", "health_premium", "fsa_health", "fsa_dependent_care", "commuter",
        "insurance", "other",
    ]
    public static let taxFormKinds = ["w2", "1099_int", "1099_div", "1099_b", "1098"]
}

/// FinanceEntryProblem is the first thing a form is missing or has wrong,
/// named by the label the form shows on the field, so a form can say why it
/// cannot go on.
public struct FinanceEntryProblem: Hashable, Sendable {
    public enum Reason: Hashable, Sendable {
        case missing
        case notAmount
        case notNumber
        case notDay
        case notCurrencyCode
        case notLastFourDigits
        case notYear
        case noLines
        case noBoxes
    }

    public let field: String
    public let reason: Reason
    /// The line or box it is on, counted from one, when it is on one.
    public var lineNumber: Int?

    public init(field: String, reason: Reason, lineNumber: Int? = nil) {
        self.field = field
        self.reason = reason
        self.lineNumber = lineNumber
    }

    func on(lineNumber: Int) -> FinanceEntryProblem {
        FinanceEntryProblem(field: field, reason: reason, lineNumber: lineNumber)
    }
}

/// FinanceEntryText checks what a person typed the way the server does, so a
/// form can say it is not ready before asking.
public enum FinanceEntryText {
    /// A day written 2026-10-02.
    public static func isDay(_ text: String) -> Bool {
        text.range(of: #"^\d{4}-\d{2}-\d{2}$"#, options: .regularExpression) != nil
    }

    /// A decimal amount, below zero only where the field allows it.
    public static func isAmount(_ text: String, isNegativeAllowed: Bool) -> Bool {
        let amount = normalized(text)
        return FinanceMoney.isDecimal(amount) && (isNegativeAllowed || !amount.hasPrefix("-"))
    }

    /// A quantity or hours: a decimal with up to eight places, never below zero.
    public static func isQuantity(_ text: String) -> Bool {
        normalized(text).range(of: #"^\d+(\.\d{1,8})?$"#, options: .regularExpression) != nil
    }

    /// A currency code such as USD.
    public static func isCurrencyCode(_ text: String) -> Bool {
        text.range(of: #"^[A-Za-z]{3}$"#, options: .regularExpression) != nil
    }

    /// The last four digits of a taxpayer number, never more.
    public static func isTaxIdMask(_ text: String) -> Bool {
        text.range(of: #"^\d{4}$"#, options: .regularExpression) != nil
    }

    static func trimmed(_ text: String) -> String {
        text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// A number as it is sent: trimmed, and a lone comma with no point read
    /// as the decimal point, as a keyboard of another region types it.
    static func normalized(_ text: String) -> String {
        let value = trimmed(text)
        let commaCount = value.filter { $0 == "," }.count
        if commaCount == 1, !value.contains(".") { return value.replacingOccurrences(of: ",", with: ".") }
        return value
    }

    /// Whether an optional field is empty or passes the check.
    static func isEmptyOr(_ text: String, _ check: (String) -> Bool) -> Bool {
        let value = trimmed(text)
        return value.isEmpty || check(value)
    }

    /// What is wrong with a field: nothing when it passes the check, or when
    /// it is empty and optional; missing when it is empty and required.
    static func problem(_ field: String, _ text: String, isRequired: Bool, reason: FinanceEntryProblem.Reason, _ check: (String) -> Bool)
        -> FinanceEntryProblem?
    {
        let value = trimmed(text)
        if value.isEmpty { return isRequired ? FinanceEntryProblem(field: field, reason: .missing) : nil }
        return check(value) ? nil : FinanceEntryProblem(field: field, reason: reason)
    }

    static func amountProblem(_ field: String, _ text: String, isRequired: Bool = false, isNegativeAllowed: Bool = false) -> FinanceEntryProblem? {
        problem(field, text, isRequired: isRequired, reason: .notAmount) { isAmount($0, isNegativeAllowed: isNegativeAllowed) }
    }

    static func quantityProblem(_ field: String, _ text: String) -> FinanceEntryProblem? {
        problem(field, text, isRequired: false, reason: .notNumber, isQuantity)
    }

    static func dayProblem(_ field: String, _ text: String, isRequired: Bool = false) -> FinanceEntryProblem? {
        problem(field, text, isRequired: isRequired, reason: .notDay, isDay)
    }

    static func lastFourDigitsProblem(_ field: String, _ text: String) -> FinanceEntryProblem? {
        problem(field, text, isRequired: false, reason: .notLastFourDigits, isTaxIdMask)
    }

    static func nameProblem(_ field: String, _ text: String) -> FinanceEntryProblem? {
        trimmed(text).isEmpty ? FinanceEntryProblem(field: field, reason: .missing) : nil
    }

    /// The first of the problems that is one.
    static func first(_ problems: [FinanceEntryProblem?]) -> FinanceEntryProblem? {
        problems.compactMap { $0 }.first
    }
}

extension Dictionary where Key == String, Value == JSONValue {
    /// Sets a text argument unless it is empty, as the server's nullable arguments are left out.
    fileprivate mutating func setText(_ key: String, _ text: String) {
        let value = FinanceEntryText.trimmed(text)
        if !value.isEmpty { self[key] = .string(value) }
    }

    /// Sets a number argument unless it is empty, as the server reads it.
    fileprivate mutating func setNumber(_ key: String, _ text: String) {
        let value = FinanceEntryText.normalized(text)
        if !value.isEmpty { self[key] = .string(value) }
    }
}

// MARK: Receipt

/// ReceiptLineDraft is one printed line of a receipt being typed in.
public struct ReceiptLineDraft: Identifiable, Hashable, Sendable {
    public let id = UUID()
    public var receiptLineKind = "item"
    public var description = ""
    public var quantity = ""
    public var unitPriceAmount = ""
    public var lineAmount = ""

    public init() {}

    /// A line nothing was typed on, which is left out rather than refused.
    public var isBlank: Bool {
        [description, quantity, unitPriceAmount, lineAmount].allSatisfy { FinanceEntryText.trimmed($0).isEmpty }
    }

    /// Its amount is a number, and the unit price and quantity are numbers when given.
    public var isValid: Bool { firstProblem == nil }

    /// The first field of the line that is not what the server takes.
    public var firstProblem: FinanceEntryProblem? {
        FinanceEntryText.first([
            FinanceEntryText.quantityProblem("Quantity", quantity),
            FinanceEntryText.amountProblem("Unit price", unitPriceAmount),
            FinanceEntryText.amountProblem("Amount", lineAmount, isRequired: true, isNegativeAllowed: true),
        ])
    }

    var argument: JSONValue {
        var fields: [String: JSONValue] = [
            "receiptLineKind": .string(receiptLineKind), "lineAmount": .string(FinanceEntryText.normalized(lineAmount)),
        ]
        fields.setText("description", description)
        fields.setNumber("quantity", quantity)
        fields.setNumber("unitPriceAmount", unitPriceAmount)
        return .object(fields)
    }
}

/// ReceiptDraft is a receipt being typed in: what RecordReceiptArguments take
/// besides the file it was read from.
public struct ReceiptDraft: Hashable, Sendable {
    public var merchantName = ""
    public var merchantReceiptNumber = ""
    public var purchasedOn = ""
    public var currencyCode = ""
    public var subtotalAmount = ""
    public var totalAmount = ""
    public var paymentAccountMask = ""
    public var receiptLines: [ReceiptLineDraft] = [ReceiptLineDraft()]
    public var isUnbalancedAccepted = false

    public init(currencyCode: String = "", purchasedOn: String = "") {
        self.currencyCode = currencyCode
        self.purchasedOn = purchasedOn
    }

    /// The lines that have something typed on them.
    public var filledLines: [ReceiptLineDraft] { receiptLines.filter { !$0.isBlank } }

    /// Whether the server could be asked: a merchant, a currency, a total and
    /// lines that are numbers, and a day when one is given.
    public var isReady: Bool { firstProblem == nil }

    /// The first field that is empty or not what the server takes, in the order the form shows them.
    public var firstProblem: FinanceEntryProblem? {
        let lineProblem = filledLines.enumerated().compactMap { index, line in line.firstProblem?.on(lineNumber: index + 1) }.first
        return FinanceEntryText.first([
            FinanceEntryText.nameProblem("Merchant", merchantName),
            FinanceEntryText.dayProblem("Bought on", purchasedOn),
            FinanceEntryText.problem("Currency", currencyCode, isRequired: true, reason: .notCurrencyCode, FinanceEntryText.isCurrencyCode),
            filledLines.isEmpty ? FinanceEntryProblem(field: "Line", reason: .noLines) : nil,
            lineProblem,
            FinanceEntryText.amountProblem("Subtotal", subtotalAmount),
            FinanceEntryText.amountProblem("Total", totalAmount, isRequired: true),
        ])
    }

    /// The arguments of PreviewRecordReceipt and RecordReceipt for the file
    /// it was read from. Only the recording says a receipt that does not add
    /// up is accepted.
    public func variables(attachmentId: String, isRecording: Bool) -> [String: JSONValue] {
        var variables: [String: JSONValue] = [
            "agentAttachmentId": .string(attachmentId),
            "merchantName": .string(FinanceEntryText.trimmed(merchantName)),
            "currencyCode": .string(FinanceEntryText.trimmed(currencyCode).uppercased()),
            "totalAmount": .string(FinanceEntryText.normalized(totalAmount)),
            "receiptLines": .array(filledLines.map(\.argument)),
        ]
        variables.setText("merchantReceiptNumber", merchantReceiptNumber)
        variables.setText("purchasedOn", purchasedOn)
        variables.setNumber("subtotalAmount", subtotalAmount)
        variables.setText("paymentAccountMask", paymentAccountMask)
        if isRecording, isUnbalancedAccepted { variables["isUnbalancedAccepted"] = .bool(true) }
        return variables
    }
}

/// ReceiptEntryCandidate is a charge a typed-in receipt could explain.
public struct ReceiptEntryCandidate: Codable, Hashable, Sendable, Identifiable {
    public let financeTransactionId: String
    public let financeTransaction: FinanceTransaction?
    public let matchedAmount: String?
    public let isExactAmount: Bool?
    public let isSameAccount: Bool?
    public let isMerchantNameShared: Bool?
    public let dayDistanceCount: Int?
    public let isAutomatic: Bool?
    public let matchConfidence: String?

    public var id: String { financeTransactionId }
}

/// ReceiptEntryPreview is what recording a receipt would do, which writes nothing.
public struct ReceiptEntryPreview: Codable, Hashable, Sendable {
    public let receiptCheckState: String?
    public let checkDifferenceAmount: String?
    public let receiptCheckSummary: String?
    public let receiptMatchCandidates: [ReceiptEntryCandidate?]?
    public let isReplacing: Bool?

    public var isBalanced: Bool { receiptCheckState == "balanced" }
    public var candidates: [ReceiptEntryCandidate] { (receiptMatchCandidates ?? []).compactMap { $0 } }
}

/// RecordedReceipt is what recording a receipt did.
public struct RecordedReceipt: Codable, Hashable, Sendable {
    public let financeReceipt: FinanceReceipt?
    public let receiptCheckSummary: String?
    public let receiptMatchCandidates: [ReceiptEntryCandidate?]?
    public let isReplaced: Bool?
    public let droppedReceiptMatchReasons: [String]?

    public var candidates: [ReceiptEntryCandidate] { (receiptMatchCandidates ?? []).compactMap { $0 } }
    /// Whether the matcher matched it to a charge on its own.
    public var isMatched: Bool { !((financeReceipt?.receiptMatches ?? []).compactMap { $0 }.isEmpty) }
}

// MARK: Paystub

/// PaystubLineDraft is one line of a paystub being typed in. What a line
/// takes depends on its kind: an earning its hours and rate, a tax which tax
/// and the wages it was figured on, a deduction what it pays for and the
/// employer's part.
public struct PaystubLineDraft: Identifiable, Hashable, Sendable {
    public let id = UUID()
    public var paystubLineKind = "earning"
    public var description = ""
    public var hours = ""
    public var rateAmount = ""
    public var currentAmount = ""
    public var yearToDateAmount = ""
    public var paystubTaxKind = "federal_income"
    public var taxJurisdictionCode = ""
    public var currentWagesAmount = ""
    public var yearToDateWagesAmount = ""
    public var paystubDeductionKind = "retirement_traditional"
    public var employerCurrentAmount = ""
    public var employerYearToDateAmount = ""

    public init() {}

    /// A line nothing was typed on, which is left out rather than refused.
    public var isBlank: Bool {
        [description, hours, rateAmount, currentAmount, yearToDateAmount, taxJurisdictionCode, currentWagesAmount,
         yearToDateWagesAmount, employerCurrentAmount, employerYearToDateAmount].allSatisfy { FinanceEntryText.trimmed($0).isEmpty }
    }

    /// An amount for the period or the year, every amount a number (the two
    /// the line itself carries may be below zero), and hours a decimal.
    public var isValid: Bool { firstProblem == nil }

    /// The first field of the line that is empty or not what the server takes.
    public var firstProblem: FinanceEntryProblem? {
        let hasAmount = [currentAmount, yearToDateAmount].contains { !FinanceEntryText.trimmed($0).isEmpty }
        var problems: [FinanceEntryProblem?] = []
        switch paystubLineKind {
        case "earning":
            problems = [
                FinanceEntryText.quantityProblem("Hours", hours),
                FinanceEntryText.amountProblem("Rate", rateAmount),
            ]
        case "tax":
            problems = [
                FinanceEntryText.amountProblem("Wages this period", currentWagesAmount),
                FinanceEntryText.amountProblem("Wages this year", yearToDateWagesAmount),
            ]
        case "deduction":
            problems = [
                FinanceEntryText.amountProblem("Employer adds this period", employerCurrentAmount),
                FinanceEntryText.amountProblem("Employer adds this year", employerYearToDateAmount),
            ]
        default:
            break
        }
        let missingAmount: FinanceEntryProblem? = hasAmount ? nil : FinanceEntryProblem(field: "Amount this period", reason: .missing)
        return FinanceEntryText.first(problems + [
            missingAmount,
            FinanceEntryText.amountProblem("Amount this period", currentAmount, isNegativeAllowed: true),
            FinanceEntryText.amountProblem("Amount this year", yearToDateAmount, isNegativeAllowed: true),
        ])
    }

    var argument: JSONValue {
        var fields: [String: JSONValue] = ["paystubLineKind": .string(paystubLineKind)]
        fields.setText("description", description)
        fields.setNumber("currentAmount", currentAmount)
        fields.setNumber("yearToDateAmount", yearToDateAmount)
        switch paystubLineKind {
        case "earning":
            fields.setNumber("hours", hours)
            fields.setNumber("rateAmount", rateAmount)
        case "tax":
            fields["paystubTaxKind"] = .string(paystubTaxKind)
            fields.setText("taxJurisdictionCode", taxJurisdictionCode)
            fields.setNumber("currentWagesAmount", currentWagesAmount)
            fields.setNumber("yearToDateWagesAmount", yearToDateWagesAmount)
        case "deduction":
            fields["paystubDeductionKind"] = .string(paystubDeductionKind)
            fields.setNumber("employerCurrentAmount", employerCurrentAmount)
            fields.setNumber("employerYearToDateAmount", employerYearToDateAmount)
        default:
            break
        }
        return .object(fields)
    }
}

/// PaystubDraft is a paystub being typed in: what RecordPaystubArguments take
/// besides the file it was read from.
public struct PaystubDraft: Hashable, Sendable {
    public var employerName = ""
    public var employerTaxId = ""
    public var employeeName = ""
    public var employeeTaxIdMask = ""
    public var payDate = ""
    public var periodStartOn = ""
    public var periodEndOn = ""
    public var currencyCode = ""
    public var grossAmount = ""
    public var yearToDateGrossAmount = ""
    public var totalTaxAmount = ""
    public var totalDeductionAmount = ""
    public var netAmount = ""
    public var paystubLines: [PaystubLineDraft] = [PaystubLineDraft()]
    public var isUnbalancedAccepted = false

    public init(currencyCode: String = "", payDate: String = "") {
        self.currencyCode = currencyCode
        self.payDate = payDate
    }

    /// The lines that have something typed on them.
    public var filledLines: [PaystubLineDraft] { paystubLines.filter { !$0.isBlank } }

    /// Whether the server could be asked: who paid whom, the pay date, gross
    /// and take-home pay, and lines with an amount each.
    public var isReady: Bool { firstProblem == nil }

    /// The first field that is empty or not what the server takes, in the order the form shows them.
    public var firstProblem: FinanceEntryProblem? {
        let lineProblem = filledLines.enumerated().compactMap { index, line in line.firstProblem?.on(lineNumber: index + 1) }.first
        return FinanceEntryText.first([
            FinanceEntryText.nameProblem("Employer", employerName),
            FinanceEntryText.nameProblem("Employee", employeeName),
            FinanceEntryText.lastFourDigitsProblem("Taxpayer number, last 4", employeeTaxIdMask),
            FinanceEntryText.dayProblem("Pay date", payDate, isRequired: true),
            FinanceEntryText.dayProblem("Period starts", periodStartOn),
            FinanceEntryText.dayProblem("Period ends", periodEndOn),
            FinanceEntryText.problem("Currency", currencyCode, isRequired: false, reason: .notCurrencyCode, FinanceEntryText.isCurrencyCode),
            FinanceEntryText.amountProblem("Gross pay", grossAmount, isRequired: true),
            FinanceEntryText.amountProblem("Gross pay this year", yearToDateGrossAmount),
            FinanceEntryText.amountProblem("Total taxes", totalTaxAmount),
            FinanceEntryText.amountProblem("Total deductions", totalDeductionAmount),
            FinanceEntryText.amountProblem("Take-home pay", netAmount, isRequired: true, isNegativeAllowed: true),
            filledLines.isEmpty ? FinanceEntryProblem(field: "Line", reason: .noLines) : nil,
            lineProblem,
        ])
    }

    /// The arguments of RecordPaystub for the file it was read from.
    public func variables(attachmentId: String) -> [String: JSONValue] {
        var variables: [String: JSONValue] = [
            "agentAttachmentId": .string(attachmentId),
            "employerName": .string(FinanceEntryText.trimmed(employerName)),
            "employeeName": .string(FinanceEntryText.trimmed(employeeName)),
            "payDate": .string(FinanceEntryText.trimmed(payDate)),
            "grossAmount": .string(FinanceEntryText.normalized(grossAmount)),
            "netAmount": .string(FinanceEntryText.normalized(netAmount)),
            "paystubLines": .array(filledLines.map(\.argument)),
        ]
        variables.setText("employerTaxId", employerTaxId)
        variables.setText("employeeTaxIdMask", employeeTaxIdMask)
        variables.setText("periodStartOn", periodStartOn)
        variables.setText("periodEndOn", periodEndOn)
        variables.setNumber("yearToDateGrossAmount", yearToDateGrossAmount)
        variables.setNumber("totalTaxAmount", totalTaxAmount)
        variables.setNumber("totalDeductionAmount", totalDeductionAmount)
        let code = FinanceEntryText.trimmed(currencyCode)
        if !code.isEmpty { variables["currencyCode"] = .string(code.uppercased()) }
        if isUnbalancedAccepted { variables["isUnbalancedAccepted"] = .bool(true) }
        return variables
    }
}

// MARK: Tax form

/// TaxFormEntryDraft is one box of a tax form being typed in: its number,
/// what tells boxes of one number apart, and its amount, text or mark.
public struct TaxFormEntryDraft: Identifiable, Hashable, Sendable {
    public let id = UUID()
    public var boxCode = ""
    public var boxQualifier = ""
    public var amount = ""
    public var textValue = ""
    public var isChecked = false

    public init() {}

    /// A box nothing was typed on, which is left out rather than refused.
    public var isBlank: Bool {
        [boxCode, boxQualifier, amount, textValue].allSatisfy { FinanceEntryText.trimmed($0).isEmpty } && !isChecked
    }

    /// A box number, and an amount that is a number when given.
    public var isValid: Bool { firstProblem == nil }

    /// The first field of the box that is empty or not what the server takes.
    public var firstProblem: FinanceEntryProblem? {
        FinanceEntryText.first([
            FinanceEntryText.nameProblem("Box", boxCode),
            FinanceEntryText.amountProblem("Amount", amount),
        ])
    }

    var argument: JSONValue {
        var fields: [String: JSONValue] = ["boxCode": .string(FinanceEntryText.trimmed(boxCode))]
        fields.setText("boxQualifier", boxQualifier)
        fields.setNumber("amount", amount)
        fields.setText("textValue", textValue)
        if isChecked { fields["isChecked"] = .bool(true) }
        return .object(fields)
    }
}

/// TaxFormDraft is a tax form being typed in: what RecordTaxFormArguments take
/// besides the file it was read from.
public struct TaxFormDraft: Hashable, Sendable {
    public var taxFormKind = "w2"
    public var taxYear: Int
    public var isCorrected = false
    public var payerName = ""
    public var payerTaxId = ""
    public var recipientName = ""
    public var recipientTaxIdMask = ""
    public var taxFormEntries: [TaxFormEntryDraft] = [TaxFormEntryDraft()]

    public init(taxYear: Int) {
        self.taxYear = taxYear
    }

    /// The boxes that have something typed on them.
    public var filledEntries: [TaxFormEntryDraft] { taxFormEntries.filter { !$0.isBlank } }

    /// Whether the server could be asked: who paid whom, and boxes that are numbers.
    public var isReady: Bool { firstProblem == nil }

    /// The first field that is empty or not what the server takes, in the order the form shows them.
    public var firstProblem: FinanceEntryProblem? {
        let entryProblem = filledEntries.enumerated().compactMap { index, entry in entry.firstProblem?.on(lineNumber: index + 1) }.first
        return FinanceEntryText.first([
            (1900...2999).contains(taxYear) ? nil : FinanceEntryProblem(field: "Tax year", reason: .notYear),
            FinanceEntryText.nameProblem("Payer", payerName),
            FinanceEntryText.nameProblem("Recipient", recipientName),
            FinanceEntryText.lastFourDigitsProblem("Taxpayer number, last 4", recipientTaxIdMask),
            filledEntries.isEmpty ? FinanceEntryProblem(field: "Box", reason: .noBoxes) : nil,
            entryProblem,
        ])
    }

    /// The arguments of RecordTaxForm for the file it was read from.
    public func variables(attachmentId: String) -> [String: JSONValue] {
        var variables: [String: JSONValue] = [
            "agentAttachmentId": .string(attachmentId),
            "taxFormKind": .string(taxFormKind),
            "taxYear": .number(Double(taxYear)),
            "payerName": .string(FinanceEntryText.trimmed(payerName)),
            "recipientName": .string(FinanceEntryText.trimmed(recipientName)),
            "taxFormEntries": .array(filledEntries.map(\.argument)),
        ]
        variables.setText("payerTaxId", payerTaxId)
        variables.setText("recipientTaxIdMask", recipientTaxIdMask)
        if isCorrected { variables["isCorrected"] = .bool(true) }
        return variables
    }
}

// MARK: Exchange rate

/// CurrencyRate is what one unit of a currency bought in another, on a day.
public struct CurrencyRate: Codable, Hashable, Sendable {
    public let fromCurrencyCode: String
    public let toCurrencyCode: String
    public let rate: String
    public let rateOn: String?
    public let rateSource: String?

    /// "1.0834": rounded to five significant digits, without trailing zeros.
    public var shownRate: String? {
        guard var value = FinanceMoney.decimal(rate), value > 0 else { return nil }
        let leadingDigitPlace = Int(floor(log10(NSDecimalNumber(decimal: value).doubleValue)))
        var rounded = Decimal()
        NSDecimalRound(&rounded, &value, 4 - leadingDigitPlace, .plain)
        return "\(rounded)"
    }
}

// MARK: Operations

extension Documents {
    private static let receiptVariableTypes = """
        $agentAttachmentId: String, $merchantName: String!, $merchantReceiptNumber: String, $purchasedOn: String,
        $currencyCode: String!, $subtotalAmount: String, $totalAmount: String!, $paymentAccountMask: String,
        $receiptLines: [ReceiptLineInput!]!, $isUnbalancedAccepted: Boolean
        """
    private static let receiptArguments = """
        agentAttachmentId: $agentAttachmentId, merchantName: $merchantName, merchantReceiptNumber: $merchantReceiptNumber,
        purchasedOn: $purchasedOn, currencyCode: $currencyCode, subtotalAmount: $subtotalAmount, totalAmount: $totalAmount,
        paymentAccountMask: $paymentAccountMask, receiptLines: $receiptLines, isUnbalancedAccepted: $isUnbalancedAccepted
        """
    static let previewRecordReceipt = """
        query (\(receiptVariableTypes)) {
          PreviewRecordReceipt(\(receiptArguments)) {
            receiptCheckState checkDifferenceAmount receiptCheckSummary
            receiptMatchCandidates { financeTransactionId financeTransaction { \(financeTransactionFields) } matchedAmount isExactAmount
              isSameAccount isMerchantNameShared dayDistanceCount isAutomatic matchConfidence } isReplacing }
        }
        """
    static let recordReceipt = """
        mutation (\(receiptVariableTypes)) {
          RecordReceipt(\(receiptArguments)) {
            financeReceipt { \(financeReceiptFields) } receiptCheckSummary
            receiptMatchCandidates { financeTransactionId financeTransaction { \(financeTransactionFields) } matchedAmount isExactAmount
              isSameAccount isMerchantNameShared dayDistanceCount isAutomatic matchConfidence } isReplaced droppedReceiptMatchReasons }
        }
        """
    static let recordPaystub = """
        mutation ($agentAttachmentId: String!, $employerName: String!, $employerTaxId: String, $employeeName: String!,
          $employeeTaxIdMask: String, $payDate: String!, $periodStartOn: String, $periodEndOn: String, $currencyCode: String,
          $grossAmount: String!, $yearToDateGrossAmount: String, $totalTaxAmount: String, $totalDeductionAmount: String,
          $netAmount: String!, $paystubLines: [PaystubLineInput!]!, $isUnbalancedAccepted: Boolean) {
          RecordPaystub(agentAttachmentId: $agentAttachmentId, employerName: $employerName, employerTaxId: $employerTaxId,
            employeeName: $employeeName, employeeTaxIdMask: $employeeTaxIdMask, payDate: $payDate, periodStartOn: $periodStartOn,
            periodEndOn: $periodEndOn, currencyCode: $currencyCode, grossAmount: $grossAmount,
            yearToDateGrossAmount: $yearToDateGrossAmount, totalTaxAmount: $totalTaxAmount,
            totalDeductionAmount: $totalDeductionAmount, netAmount: $netAmount, paystubLines: $paystubLines,
            isUnbalancedAccepted: $isUnbalancedAccepted) { \(financePaystubFields) }
        }
        """
    static let recordTaxForm = """
        mutation ($agentAttachmentId: String!, $taxFormKind: String, $taxYear: Int!, $isCorrected: Boolean,
          $payerName: String!, $payerTaxId: String, $recipientName: String!, $recipientTaxIdMask: String,
          $taxFormEntries: [TaxFormEntryInput!]!) {
          RecordTaxForm(agentAttachmentId: $agentAttachmentId, taxFormKind: $taxFormKind, taxYear: $taxYear,
            isCorrected: $isCorrected, payerName: $payerName, payerTaxId: $payerTaxId, recipientName: $recipientName,
            recipientTaxIdMask: $recipientTaxIdMask, taxFormEntries: $taxFormEntries) { \(taxFormViewFields) }
        }
        """
    static let exchangeRate = """
        query ($fromCurrencyCode: String!, $toCurrencyCode: String!) {
          ExchangeRate(fromCurrencyCode: $fromCurrencyCode, toCurrencyCode: $toCurrencyCode) {
            fromCurrencyCode toCurrencyCode rate rateOn rateSource }
        }
        """
}

extension Connection {
    /// What recording a typed-in receipt would do, writing nothing: whether
    /// its lines add up, the charges it could explain, and whether it
    /// replaces one read from the same file.
    public func previewRecordReceipt(_ draft: ReceiptDraft, attachmentId: String) async throws -> ReceiptEntryPreview? {
        struct Answer: Decodable, Sendable { let PreviewRecordReceipt: ReceiptEntryPreview? }
        return try await execute(
            Documents.previewRecordReceipt, variables: draft.variables(attachmentId: attachmentId, isRecording: false), as: Answer.self
        ).PreviewRecordReceipt
    }

    /// Records a typed-in receipt and matches it to the charge the matcher is sure of.
    public func recordReceipt(_ draft: ReceiptDraft, attachmentId: String) async throws -> RecordedReceipt? {
        struct Answer: Decodable, Sendable { let RecordReceipt: RecordedReceipt? }
        return try await execute(
            Documents.recordReceipt, variables: draft.variables(attachmentId: attachmentId, isRecording: true), as: Answer.self
        ).RecordReceipt
    }

    public func recordPaystub(_ draft: PaystubDraft, attachmentId: String) async throws -> FinancePaystub? {
        struct Answer: Decodable, Sendable { let RecordPaystub: FinancePaystub? }
        return try await execute(Documents.recordPaystub, variables: draft.variables(attachmentId: attachmentId), as: Answer.self).RecordPaystub
    }

    public func recordTaxForm(_ draft: TaxFormDraft, attachmentId: String) async throws -> FinanceTaxFormView? {
        struct Answer: Decodable, Sendable { let RecordTaxForm: FinanceTaxFormView? }
        return try await execute(Documents.recordTaxForm, variables: draft.variables(attachmentId: attachmentId), as: Answer.self).RecordTaxForm
    }

    /// Today's rate between two currencies.
    public func exchangeRate(from fromCurrencyCode: String, to toCurrencyCode: String) async throws -> CurrencyRate? {
        struct Answer: Decodable, Sendable { let ExchangeRate: CurrencyRate? }
        return try await execute(
            Documents.exchangeRate,
            variables: ["fromCurrencyCode": .string(fromCurrencyCode.uppercased()), "toCurrencyCode": .string(toCurrencyCode.uppercased())],
            as: Answer.self
        ).ExchangeRate
    }
}
