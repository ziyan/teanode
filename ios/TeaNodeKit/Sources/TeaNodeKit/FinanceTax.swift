import Foundation

// Paystubs and W-2s, read from uploads and checked, and a tax year put
// together from them. Amounts are decimal strings, positive as printed.

public struct FinancePaystub: Codable, Hashable, Sendable, Identifiable {
    public struct Line: Codable, Hashable, Sendable {
        public let id: String
        public let lineNumber: Int?
        public let paystubLineKind: String?
        public let description: String?
        public let hours: String?
        public let rateAmount: String?
        public let currentAmount: String?
        public let yearToDateAmount: String?
        public let paystubTaxKind: String?
        public let taxJurisdictionCode: String?
        public let currentWagesAmount: String?
        public let yearToDateWagesAmount: String?
        public let paystubDeductionKind: String?
        public let employerCurrentAmount: String?
        public let employerYearToDateAmount: String?
    }

    public let id: String
    public let agentAttachmentId: String?
    public let employerName: String?
    public let employerTaxId: String?
    public let employeeName: String?
    public let employeeTaxIdMask: String?
    public let payDate: String?
    public let periodStartOn: String?
    public let periodEndOn: String?
    public let currencyCode: String?
    public let grossAmount: String?
    public let yearToDateGrossAmount: String?
    public let totalTaxAmount: String?
    public let totalDeductionAmount: String?
    public let netAmount: String?
    public let paystubCheckState: String?
    public let checkDifferenceAmount: String?
    public let paystubLines: [Line?]?

    /// Whether its lines add up to its printed totals.
    public var isBalanced: Bool { paystubCheckState == "balanced" }

    /// Its lines of one kind ("earning", "tax", "deduction"), in printed order.
    public func lines(ofKind lineKind: String) -> [Line] {
        (paystubLines ?? []).compactMap { $0 }.filter { $0.paystubLineKind == lineKind }
            .sorted { ($0.lineNumber ?? 0) < ($1.lineNumber ?? 0) }
    }
}

public struct FinancePaystubPage: Codable, Hashable, Sendable {
    public let financePaystubs: [FinancePaystub?]
    public let hasMore: Bool?
    public let totalCount: Int?
}

public struct FinanceTaxForm: Codable, Hashable, Sendable, Identifiable {
    public struct Entry: Codable, Hashable, Sendable {
        public let id: String
        public let entryNumber: Int?
        public let boxCode: String?
        public let boxQualifier: String?
        public let amount: String?
        public let textValue: String?
        public let isChecked: Bool?

        /// "12 D", or "1" for a box that is one.
        public var boxName: String {
            guard let boxQualifier, !boxQualifier.isEmpty else { return boxCode ?? "" }
            return "\(boxCode ?? "") \(boxQualifier)"
        }
    }

    public let id: String
    public let agentAttachmentId: String?
    public let taxFormKind: String?
    public let taxYear: Int?
    public let isCorrected: Bool?
    public let payerName: String?
    public let payerTaxId: String?
    public let recipientName: String?
    public let recipientTaxIdMask: String?
    public let taxFormEntries: [Entry?]?

    public var entries: [Entry] {
        (taxFormEntries ?? []).compactMap { $0 }.sorted { ($0.entryNumber ?? 0) < ($1.entryNumber ?? 0) }
    }
}

/// FinanceTaxFormView is a tax form with what it says about itself.
public struct FinanceTaxFormView: Codable, Hashable, Sendable, Identifiable {
    public struct Finding: Codable, Hashable, Sendable {
        public let taxFormFindingCode: String?
        public let isAgreeing: Bool?
        public let expectedAmount: String?
        public let printedAmount: String?
        public let findingText: String?
    }

    public let financeTaxForm: FinanceTaxForm?
    public let taxFormFindings: [Finding?]?

    public var id: String { financeTaxForm?.id ?? "" }
    public var findings: [Finding] { (taxFormFindings ?? []).compactMap { $0 } }
    /// How many of its checks disagree; none when it agrees with itself.
    public var disagreeingCount: Int { findings.filter { $0.isAgreeing == false }.count }
}

public struct FinanceTaxFormPage: Codable, Hashable, Sendable {
    public let taxForms: [FinanceTaxFormView?]
    public let hasMore: Bool?
    public let totalCount: Int?
}

/// TaxYearFigures are a year's wages and withholding as a W-2 names them.
public struct TaxYearFigures: Codable, Hashable, Sendable {
    public struct State: Codable, Hashable, Sendable {
        public let stateCode: String
        public let stateWagesAmount: String?
        public let stateIncomeTaxAmount: String?
    }

    public let wagesAmount: String?
    public let federalIncomeTaxAmount: String?
    public let socialSecurityWagesAmount: String?
    public let socialSecurityTaxAmount: String?
    public let medicareWagesAmount: String?
    public let medicareTaxAmount: String?
    public let dependentCareAmount: String?
    public let retirementTraditionalAmount: String?
    public let retirementRothAmount: String?
    public let hsaAmount: String?
    public let healthCoverageAmount: String?
    public let localIncomeTaxAmount: String?
    public let taxYearStateFigures: [State?]?
}

/// TaxYearFigureKind is which figure of a year a row shows.
public enum TaxYearFigureKind: Hashable, Sendable {
    case wages, federalIncomeTax, socialSecurityWages, socialSecurityTax, medicareWages, medicareTax
    case retirementTraditional, retirementRoth, hsa, dependentCare
    case stateWages(stateCode: String), stateIncomeTax(stateCode: String)
    case localIncomeTax
}

/// TaxYearFigureRow is one figure of a year, and the same figure projected
/// to December 31 when there is a projection.
public struct TaxYearFigureRow: Hashable, Sendable, Identifiable {
    public let figureKind: TaxYearFigureKind
    public let amount: String?
    public let projectedAmount: String?

    public var id: TaxYearFigureKind { figureKind }
}

extension TaxYearFigures {
    /// The figures in the order a W-2 prints them, each with its projection
    /// when one is given. Rows at zero are left out, except wages and
    /// federal income tax, which always show.
    public func rows(projectedTo projected: TaxYearFigures? = nil) -> [TaxYearFigureRow] {
        func row(_ figureKind: TaxYearFigureKind, _ amount: String?, _ projectedAmount: String?, isAlwaysShown: Bool = false) -> TaxYearFigureRow? {
            let isZero = (FinanceMoney.decimal(amount) ?? 0) == 0 && (FinanceMoney.decimal(projectedAmount) ?? 0) == 0
            if isZero, !isAlwaysShown { return nil }
            return TaxYearFigureRow(figureKind: figureKind, amount: amount, projectedAmount: projected == nil ? nil : projectedAmount)
        }
        var rows: [TaxYearFigureRow?] = [
            row(.wages, wagesAmount, projected?.wagesAmount, isAlwaysShown: true),
            row(.federalIncomeTax, federalIncomeTaxAmount, projected?.federalIncomeTaxAmount, isAlwaysShown: true),
            row(.socialSecurityWages, socialSecurityWagesAmount, projected?.socialSecurityWagesAmount),
            row(.socialSecurityTax, socialSecurityTaxAmount, projected?.socialSecurityTaxAmount),
            row(.medicareWages, medicareWagesAmount, projected?.medicareWagesAmount),
            row(.medicareTax, medicareTaxAmount, projected?.medicareTaxAmount),
            row(.retirementTraditional, retirementTraditionalAmount, projected?.retirementTraditionalAmount),
            row(.retirementRoth, retirementRothAmount, projected?.retirementRothAmount),
            row(.hsa, hsaAmount, projected?.hsaAmount),
            row(.dependentCare, dependentCareAmount, projected?.dependentCareAmount),
        ]
        let states = (taxYearStateFigures ?? []).compactMap { $0 }
        let projectedStates = (projected?.taxYearStateFigures ?? []).compactMap { $0 }
        var stateCodes = states.map(\.stateCode)
        for state in projectedStates where !stateCodes.contains(state.stateCode) { stateCodes.append(state.stateCode) }
        for stateCode in stateCodes {
            let state = states.first { $0.stateCode == stateCode }
            let projectedState = projectedStates.first { $0.stateCode == stateCode }
            rows.append(row(.stateWages(stateCode: stateCode), state?.stateWagesAmount, projectedState?.stateWagesAmount))
            rows.append(row(.stateIncomeTax(stateCode: stateCode), state?.stateIncomeTaxAmount, projectedState?.stateIncomeTaxAmount))
        }
        rows.append(row(.localIncomeTax, localIncomeTaxAmount, projected?.localIncomeTaxAmount))
        return rows.compactMap { $0 }
    }
}

/// TaxYearSummary is a tax year put together from paystubs and W-2s.
public struct TaxYearSummary: Codable, Hashable, Sendable {
    public struct Comparison: Codable, Hashable, Sendable {
        public let boxCode: String?
        public let boxQualifier: String?
        public let boxLabel: String?
        public let w2Amount: String?
        public let paystubAmount: String?
        public let differenceAmount: String?
        public let isMatching: Bool?

        /// "12 D", or "1" for a box that is one.
        public var boxName: String {
            guard let boxQualifier, !boxQualifier.isEmpty else { return boxCode ?? "" }
            return "\(boxCode ?? "") \(boxQualifier)"
        }
    }

    public struct Employment: Codable, Hashable, Sendable {
        public let employeeName: String?
        public let employerName: String?
        public let employerTaxId: String?
        public let taxYearFigureSource: String?
        public let taxFormId: String?
        public let latestPayDate: String?
        public let paystubCount: Int?
        public let payCadenceDayCount: Int?
        public let isPaystubYearComplete: Bool?
        public let remainingPayCount: Int?
        /// Whether the projection stops retirement contributions at the
        /// year's limit, the rest counted as wages.
        public let isRetirementDeferralCapped: Bool?
        public let figures: TaxYearFigures?
        public let paystubFigures: TaxYearFigures?
        public let projectedFigures: TaxYearFigures?
        public let taxYearComparisons: [Comparison?]?

        /// Whether its figures are the W-2's rather than the paystubs'.
        public var isFromW2: Bool { taxYearFigureSource == "w2" }
        public var comparisons: [Comparison] { (taxYearComparisons ?? []).compactMap { $0 } }
    }

    public struct Person: Codable, Hashable, Sendable {
        public let employeeName: String?
        public let excessSocialSecurityTaxAmount: String?
        public let figures: TaxYearFigures?
        public let projectedFigures: TaxYearFigures?
    }

    public struct Finding: Codable, Hashable, Sendable {
        public let taxYearFindingCode: String?
        public let employeeName: String?
        public let employerName: String?
        public let findingText: String?
        public let paystubIds: [String]?
    }

    /// Estimate is the year's tax on its wages beside what was withheld,
    /// the household's, to December 31 when the year is projected. Paid,
    /// upcoming withheld, owing and remaining add up to pay; a refund is
    /// inside what was withheld.
    public struct Estimate: Codable, Hashable, Sendable {
        public let taxFilingStatus: String?
        /// The person has not said how they file, and this one is assumed.
        public let isTaxFilingStatusAssumed: Bool?
        public let isProjected: Bool?
        public let payAmount: String?
        public let standardDeductionAmount: String?
        public let taxableIncomeAmount: String?
        public let federalIncomeTaxAmount: String?
        public let socialSecurityTaxAmount: String?
        public let medicareTaxAmount: String?
        public let stateIncomeTaxAmount: String?
        public let localIncomeTaxAmount: String?
        public let totalTaxAmount: String?
        public let paidTaxAmount: String?
        public let upcomingWithheldTaxAmount: String?
        public let owingTaxAmount: String?
        public let refundAmount: String?
        public let remainingIncomeAmount: String?

        /// The ring's slices in the order they are drawn, the ones of
        /// nothing left out.
        public var slices: [TaxEstimateSlice] {
            [
                TaxEstimateSlice(sliceKind: .paid, amount: paidTaxAmount),
                TaxEstimateSlice(sliceKind: .upcomingWithheld, amount: upcomingWithheldTaxAmount),
                TaxEstimateSlice(sliceKind: .owing, amount: owingTaxAmount),
                TaxEstimateSlice(sliceKind: .remainingIncome, amount: remainingIncomeAmount),
            ].filter { $0.amountValue > 0 }
        }

        public var filingStatus: TaxFilingStatus? { taxFilingStatus.flatMap(TaxFilingStatus.init(rawValue:)) }

        /// An amount's share of pay, 0.191 for 19.1%; nil without pay.
        public func shareOfPay(_ amount: String?) -> Double? {
            guard let pay = Double(payAmount ?? ""), pay > 0 else { return nil }
            return (Double(amount ?? "") ?? 0) / pay
        }
    }

    public let taxYear: Int?
    public let socialSecurityWageBaseAmount: String?
    /// The year's limit on a person's retirement plan contributions from
    /// pay before catch-up, when TeaNode knows it.
    public let retirementDeferralLimitAmount: String?
    public let isYearOver: Bool?
    public let paystubCount: Int?
    public let taxFormCount: Int?
    public let figures: TaxYearFigures?
    public let projectedFigures: TaxYearFigures?
    public let taxYearEmployments: [Employment?]?
    public let taxYearPeople: [Person?]?
    public let taxYearFindings: [Finding?]?
    public let taxYearEstimate: Estimate?

    public var employments: [Employment] { (taxYearEmployments ?? []).compactMap { $0 } }
    public var findings: [Finding] { (taxYearFindings ?? []).compactMap { $0 } }
}

/// TaxDocumentAddress is the address paystubs and W-2s are mailed to,
/// empty without a mailbox address, and whether the server reads PDFs.
public struct TaxDocumentAddress: Codable, Hashable, Sendable {
    public let taxDocumentAddress: String?
    public let canReadPdfs: Bool?
}

/// TaxFilingStatus is how a return is filed, which the estimate's federal
/// tax depends on.
public enum TaxFilingStatus: String, CaseIterable, Hashable, Sendable, Identifiable {
    case single
    case marriedFilingJointly = "married_filing_jointly"
    case marriedFilingSeparately = "married_filing_separately"
    case headOfHousehold = "head_of_household"

    public var id: String { rawValue }
}

/// TaxEstimateSliceKind is which part of a year's pay a slice of the tax
/// ring is.
public enum TaxEstimateSliceKind: String, Hashable, Sendable {
    case paid, upcomingWithheld, owing, remainingIncome
}

/// TaxEstimateSlice is one slice of the tax ring.
public struct TaxEstimateSlice: Hashable, Sendable, Identifiable {
    public let sliceKind: TaxEstimateSliceKind
    public let amount: String?

    public var id: TaxEstimateSliceKind { sliceKind }
    public var amountValue: Double { Double(amount ?? "") ?? 0 }
}

/// TaxDocumentReading is the uploads queued to be read as paystubs or tax
/// forms, and the job reading each.
public struct TaxDocumentReading: Codable, Hashable, Sendable {
    public let agentAttachmentIds: [String]?
    public let agentJobIds: [String]?
}

extension Documents {
    static let financePaystubFields = """
        id agentAttachmentId employerName employerTaxId employeeName employeeTaxIdMask payDate periodStartOn periodEndOn
        currencyCode grossAmount yearToDateGrossAmount totalTaxAmount totalDeductionAmount netAmount paystubCheckState
        checkDifferenceAmount createdAt modifiedAt
        paystubLines { id lineNumber paystubLineKind description hours rateAmount currentAmount yearToDateAmount paystubTaxKind
          taxJurisdictionCode currentWagesAmount yearToDateWagesAmount paystubDeductionKind employerCurrentAmount
          employerYearToDateAmount }
        """
    static let taxFormViewFields = """
        financeTaxForm { id agentAttachmentId taxFormKind taxYear isCorrected payerName payerTaxId recipientName
          recipientTaxIdMask createdAt modifiedAt
          taxFormEntries { id entryNumber boxCode boxQualifier amount textValue isChecked } }
        taxFormFindings { taxFormFindingCode isAgreeing expectedAmount printedAmount findingText }
        """
    static let taxYearFiguresFields = """
        wagesAmount federalIncomeTaxAmount socialSecurityWagesAmount socialSecurityTaxAmount medicareWagesAmount
        medicareTaxAmount dependentCareAmount retirementTraditionalAmount retirementRothAmount hsaAmount healthCoverageAmount
        localIncomeTaxAmount taxYearStateFigures { stateCode stateWagesAmount stateIncomeTaxAmount }
        """
    public static let financePaystubs = """
        query ($taxYear: Int, $from: String, $to: String, $limit: Int, $offset: Int) {
          FinancePaystubs(taxYear: $taxYear, from: $from, to: $to, limit: $limit, offset: $offset) {
            financePaystubs { \(financePaystubFields) } hasMore totalCount }
        }
        """
    public static let financePaystub = """
        query ($paystubId: String!) { FinancePaystub(paystubId: $paystubId) { \(financePaystubFields) } }
        """
    public static let financeTaxForms = """
        query ($taxYear: Int, $limit: Int, $offset: Int) {
          FinanceTaxForms(taxYear: $taxYear, limit: $limit, offset: $offset) { taxForms { \(taxFormViewFields) } hasMore totalCount }
        }
        """
    public static let financeTaxForm = """
        query ($taxFormId: String!) { FinanceTaxForm(taxFormId: $taxFormId) { \(taxFormViewFields) } }
        """
    public static let taxYear = """
        query ($taxYear: Int!, $taxFilingStatus: String) {
          TaxYear(taxYear: $taxYear, taxFilingStatus: $taxFilingStatus) {
            taxYear socialSecurityWageBaseAmount retirementDeferralLimitAmount isYearOver paystubCount taxFormCount
            figures { \(taxYearFiguresFields) } projectedFigures { \(taxYearFiguresFields) }
            taxYearEmployments { employeeName employerName employerTaxId taxYearFigureSource taxFormId latestPayDate paystubCount
              payCadenceDayCount isPaystubYearComplete remainingPayCount isRetirementDeferralCapped
              figures { \(taxYearFiguresFields) } paystubFigures { \(taxYearFiguresFields) } projectedFigures { \(taxYearFiguresFields) }
              taxYearComparisons { boxCode boxQualifier boxLabel w2Amount paystubAmount differenceAmount isMatching } }
            taxYearPeople { employeeName excessSocialSecurityTaxAmount figures { \(taxYearFiguresFields) }
              projectedFigures { \(taxYearFiguresFields) } }
            taxYearFindings { taxYearFindingCode employeeName employerName findingText paystubIds }
            taxYearEstimate { taxFilingStatus isTaxFilingStatusAssumed isProjected payAmount standardDeductionAmount taxableIncomeAmount
              federalIncomeTaxAmount socialSecurityTaxAmount medicareTaxAmount stateIncomeTaxAmount localIncomeTaxAmount
              totalTaxAmount paidTaxAmount upcomingWithheldTaxAmount owingTaxAmount refundAmount remainingIncomeAmount }
          }
        }
        """
    public static let taxDocumentAddress = """
        query { TaxDocumentAddress { taxDocumentAddress canReadPdfs } }
        """
    public static let regenerateTaxDocumentAddress = """
        mutation { RegenerateTaxDocumentAddress { taxDocumentAddress canReadPdfs } }
        """
    public static let setTaxFilingStatus = """
        mutation ($taxFilingStatus: String!) { SetTaxFilingStatus(taxFilingStatus: $taxFilingStatus) }
        """
    public static let readTaxDocument = """
        mutation ($agentAttachmentId: String, $mailboxItemId: String) {
          ReadTaxDocument(agentAttachmentId: $agentAttachmentId, mailboxItemId: $mailboxItemId) { agentAttachmentIds agentJobIds }
        }
        """
    public static let deletePaystub = """
        mutation ($paystubId: String!) { DeletePaystub(paystubId: $paystubId) }
        """
    public static let deleteTaxForm = """
        mutation ($taxFormId: String!) { DeleteTaxForm(taxFormId: $taxFormId) }
        """
}

extension Connection {
    /// A tax year, its tax estimated as filed with filingStatus, or as the
    /// server guesses from the number of people when it is nil.
    public func taxYear(_ year: Int, filingStatus: TaxFilingStatus? = nil) async throws -> TaxYearSummary? {
        struct Answer: Decodable, Sendable { let TaxYear: TaxYearSummary? }
        var variables: [String: JSONValue] = ["taxYear": .number(Double(year))]
        if let filingStatus { variables["taxFilingStatus"] = .string(filingStatus.rawValue) }
        return try await execute(Documents.taxYear, variables: variables, as: Answer.self).TaxYear
    }

    /// The address to mail paystubs and W-2s to, made the first time.
    public func taxDocumentAddress() async throws -> TaxDocumentAddress? {
        struct Answer: Decodable, Sendable { let TaxDocumentAddress: TaxDocumentAddress? }
        return try await execute(Documents.taxDocumentAddress, as: Answer.self).TaxDocumentAddress
    }

    /// A new tax document address; the old one stops taking mail at once.
    public func regenerateTaxDocumentAddress() async throws -> TaxDocumentAddress? {
        struct Answer: Decodable, Sendable { let RegenerateTaxDocumentAddress: TaxDocumentAddress? }
        return try await execute(Documents.regenerateTaxDocumentAddress, as: Answer.self).RegenerateTaxDocumentAddress
    }

    /// Keeps how the person files, which every tax year's estimate is
    /// worked out for; nil goes back to assuming one.
    public func setTaxFilingStatus(_ filingStatus: TaxFilingStatus?) async throws {
        struct Answer: Decodable, Sendable { let SetTaxFilingStatus: JSONValue? }
        _ = try await execute(Documents.setTaxFilingStatus, variables: ["taxFilingStatus": .string(filingStatus?.rawValue ?? "")], as: Answer.self)
    }

    public func financePaystubs(taxYear year: Int, limit: Int = 200, offset: Int = 0) async throws -> FinancePaystubPage {
        struct Answer: Decodable, Sendable { let FinancePaystubs: FinancePaystubPage }
        return try await execute(Documents.financePaystubs, variables: [
            "taxYear": .number(Double(year)), "limit": .number(Double(limit)), "offset": .number(Double(offset)),
        ], as: Answer.self).FinancePaystubs
    }

    public func financePaystub(_ paystubId: String) async throws -> FinancePaystub? {
        struct Answer: Decodable, Sendable { let FinancePaystub: FinancePaystub? }
        return try await execute(Documents.financePaystub, variables: ["paystubId": .string(paystubId)], as: Answer.self).FinancePaystub
    }

    public func financeTaxForms(taxYear year: Int, limit: Int = 200, offset: Int = 0) async throws -> FinanceTaxFormPage {
        struct Answer: Decodable, Sendable { let FinanceTaxForms: FinanceTaxFormPage }
        return try await execute(Documents.financeTaxForms, variables: [
            "taxYear": .number(Double(year)), "limit": .number(Double(limit)), "offset": .number(Double(offset)),
        ], as: Answer.self).FinanceTaxForms
    }

    public func financeTaxForm(_ taxFormId: String) async throws -> FinanceTaxFormView? {
        struct Answer: Decodable, Sendable { let FinanceTaxForm: FinanceTaxFormView? }
        return try await execute(Documents.financeTaxForm, variables: ["taxFormId": .string(taxFormId)], as: Answer.self).FinanceTaxForm
    }

    /// Has the agent read an upload as a paystub or a tax form. The upload
    /// must have been handed in as a tax document.
    public func readTaxDocument(attachmentId: String) async throws -> TaxDocumentReading? {
        struct Answer: Decodable, Sendable { let ReadTaxDocument: TaxDocumentReading? }
        return try await execute(Documents.readTaxDocument, variables: ["agentAttachmentId": .string(attachmentId)], as: Answer.self)
            .ReadTaxDocument
    }

    /// Has the agent read each PDF and picture a message carries as a
    /// paystub or a tax form.
    public func readTaxDocument(mailboxItemId: String) async throws -> TaxDocumentReading? {
        struct Answer: Decodable, Sendable { let ReadTaxDocument: TaxDocumentReading? }
        return try await execute(Documents.readTaxDocument, variables: ["mailboxItemId": .string(mailboxItemId)], as: Answer.self)
            .ReadTaxDocument
    }

    public func deletePaystub(_ paystubId: String) async throws {
        struct Answer: Decodable, Sendable { let DeletePaystub: JSONValue? }
        _ = try await execute(Documents.deletePaystub, variables: ["paystubId": .string(paystubId)], as: Answer.self)
    }

    public func deleteTaxForm(_ taxFormId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteTaxForm: JSONValue? }
        _ = try await execute(Documents.deleteTaxForm, variables: ["taxFormId": .string(taxFormId)], as: Answer.self)
    }
}
