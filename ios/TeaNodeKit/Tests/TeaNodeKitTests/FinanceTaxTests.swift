import Foundation
import XCTest

@testable import TeaNodeKit

final class FinanceTaxTests: XCTestCase {
    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    func testATaxDocumentIsUploadedAsOne() async throws {
        let (connection, fake) = try connection(answering: #"{"attachments":[{"id":"attachment-1"}]}"#)
        let attachmentId = try await connection.upload(name: "stub.pdf", contentType: "application/pdf", data: Data("%PDF".utf8), isTaxDocument: true)
        XCTAssertEqual(attachmentId, "attachment-1")
        XCTAssertEqual(fake.sent.first?.url?.path, "/api/v1/agent/attachments")
        XCTAssertEqual(fake.sent.first?.url?.query, "isTaxDocument=true")
        _ = try await connection.upload(name: "picture.jpg", contentType: "image/jpeg", data: Data([0xFF]))
        XCTAssertNil(fake.sent.last?.url?.query)
    }

    func testATaxYearIsReadWithItsEmploymentsAndFindings() async throws {
        let figures = #"{"wagesAmount":"8000.00","federalIncomeTaxAmount":"900.00","socialSecurityWagesAmount":"8000.00","socialSecurityTaxAmount":"496.00","medicareWagesAmount":"8000.00","medicareTaxAmount":"116.00","dependentCareAmount":"0.00","retirementTraditionalAmount":"400.00","retirementRothAmount":"0.00","hsaAmount":"0.00","healthCoverageAmount":"0.00","localIncomeTaxAmount":"0.00","taxYearStateFigures":[{"stateCode":"ZZ","stateWagesAmount":"8000.00","stateIncomeTaxAmount":"0.00"}]}"#
        let projected = #"{"wagesAmount":"104000.00","federalIncomeTaxAmount":"11700.00","socialSecurityWagesAmount":"104000.00","socialSecurityTaxAmount":"6448.00","medicareWagesAmount":"104000.00","medicareTaxAmount":"1508.00","dependentCareAmount":"0.00","retirementTraditionalAmount":"5200.00","retirementRothAmount":"0.00","hsaAmount":"0.00","healthCoverageAmount":"0.00","localIncomeTaxAmount":"0.00","taxYearStateFigures":[{"stateCode":"ZZ","stateWagesAmount":"104000.00","stateIncomeTaxAmount":"0.00"}]}"#
        let body = """
            {"data":{"TaxYear":{"taxYear":2026,"socialSecurityWageBaseAmount":null,"isYearOver":false,"paystubCount":2,"taxFormCount":0,
            "figures":\(figures),"projectedFigures":\(projected),
            "taxYearEmployments":[{"employeeName":"Robin Example","employerName":"Example Payroll","employerTaxId":null,"taxYearFigureSource":"paystubs",
              "taxFormId":null,"latestPayDate":"2026-01-30","paystubCount":2,"payCadenceDayCount":14,"isPaystubYearComplete":false,"remainingPayCount":24,
              "figures":\(figures),"paystubFigures":\(figures),"projectedFigures":\(projected),"taxYearComparisons":[]}],
            "taxYearPeople":[],
            "taxYearFindings":[{"taxYearFindingCode":"pay_date_gap","employeeName":"Robin Example","employerName":"Example Payroll",
              "findingText":"A paystub between two pay dates is missing.","paystubIds":["paystub-1","paystub-2"]}]}}}
            """
        let (connection, _) = try connection(answering: body)
        let read = try await connection.taxYear(2026)
        let summary = try XCTUnwrap(read)
        XCTAssertEqual(summary.employments.count, 1)
        XCTAssertEqual(summary.employments.first?.isFromW2, false)
        XCTAssertEqual(summary.employments.first?.remainingPayCount, 24)
        XCTAssertEqual(summary.findings.first?.paystubIds, ["paystub-1", "paystub-2"])

        let rows = try XCTUnwrap(summary.figures).rows(projectedTo: summary.projectedFigures)
        XCTAssertEqual(rows.map(\.figureKind), [
            .wages, .federalIncomeTax, .socialSecurityWages, .socialSecurityTax, .medicareWages, .medicareTax,
            .retirementTraditional, .stateWages(stateCode: "ZZ"),
        ])
        XCTAssertEqual(rows.first?.projectedAmount, "104000.00")
    }

    func testZeroRowsAreLeftOutExceptWagesAndFederalTax() throws {
        let figures = try JSONDecoder().decode(TaxYearFigures.self, from: Data(#"{"wagesAmount":"0.00","federalIncomeTaxAmount":"0.00","medicareTaxAmount":"0.00"}"#.utf8))
        let rows = figures.rows()
        XCTAssertEqual(rows.map(\.figureKind), [.wages, .federalIncomeTax])
        XCTAssertNil(rows.first?.projectedAmount)
    }

    func testAW2CountsTheChecksThatDisagree() throws {
        let body = #"{"financeTaxForm":{"id":"form-1","taxYear":2025,"payerName":"Example Payroll","recipientName":"Robin Example","taxFormEntries":[{"id":"entry-2","entryNumber":2,"boxCode":"12","boxQualifier":"D","amount":"400.00"},{"id":"entry-1","entryNumber":1,"boxCode":"1","amount":"8000.00"}]},"taxFormFindings":[{"taxFormFindingCode":"social_security_rate","isAgreeing":true},{"taxFormFindingCode":"medicare_rate","isAgreeing":false}]}"#
        let view = try JSONDecoder().decode(FinanceTaxFormView.self, from: Data(body.utf8))
        XCTAssertEqual(view.disagreeingCount, 1)
        XCTAssertEqual(view.financeTaxForm?.entries.map(\.boxName), ["1", "12 D"])
    }
}
