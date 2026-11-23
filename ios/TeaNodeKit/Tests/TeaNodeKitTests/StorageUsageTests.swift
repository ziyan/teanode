import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// StorageUsageTests cover the operator's storage screen: reading the one
/// answer, the disks that fill soon, and the columns the chart is drawn from.
final class StorageUsageTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "operator")
        return (Connection(account: account, transport: fake), fake)
    }

    private let answer = #"""
        {"data":{"StorageUsage":{
          "breakdownBy":"area","databaseSizeBytes":6000,"fileSizeBytes":4000,"totalSizeBytes":10000,
          "usageRows":[{"rowKey":"search","rowLabel":"search","rowSizeBytes":3000,"rowTableCount":7,"rowFileSizes":[]},
                       {"rowKey":"mail","rowLabel":"mail","rowSizeBytes":500,"rowTableCount":18,"rowFileSizes":[]}],
          "historyDays":[
            {"sampledOn":"2030-05-01","databaseSizeBytes":5900,"fileSizeBytes":4000,"totalSizeBytes":9900,
             "breakdownSizes":[{"sizeKey":"a","sizeBytes":70},{"sizeKey":"b","sizeBytes":60},{"sizeKey":"c","sizeBytes":50},{"sizeKey":"d","sizeBytes":40},{"sizeKey":"e","sizeBytes":30},{"sizeKey":"f","sizeBytes":20},{"sizeKey":"g","sizeBytes":10}]},
            {"sampledOn":"2030-05-02","databaseSizeBytes":6000,"fileSizeBytes":4000,"totalSizeBytes":10000,
             "breakdownSizes":[{"sizeKey":"a","sizeBytes":71},{"sizeKey":"b","sizeBytes":61},{"sizeKey":"c","sizeBytes":51},{"sizeKey":"d","sizeBytes":41},{"sizeKey":"e","sizeBytes":31},{"sizeKey":"f","sizeBytes":21},{"sizeKey":"g","sizeBytes":11}]}],
          "disks":[
            {"diskLabel":"dataDirectory","isDiskMeasured":true,"diskTotalBytes":20000,"diskFreeBytes":2000,"diskUsedBytes":18000,"projectedFullOn":"2030-05-21","daysUntilFull":20,"isProjectedFull":true},
            {"diskLabel":"fileDirectory","isDiskMeasured":true,"diskTotalBytes":20000,"diskFreeBytes":2000,"diskUsedBytes":18000,"projectedFullOn":"2030-05-11","daysUntilFull":10,"isProjectedFull":true},
            {"diskLabel":"objectStore","isDiskMeasured":false,"diskTotalBytes":null,"diskFreeBytes":null,"diskUsedBytes":null,"projectedFullOn":null,"daysUntilFull":null,"isProjectedFull":false}],
          "growth":{"growthSampleCount":12,"isGrowthProjectable":true,"growthBytesPerDay":100.5,"currentSizeBytes":10000,
                    "projectedSize30DaysBytes":13000,"projectedSize90DaysBytes":19000,"projectedSize365DaysBytes":46000,"areFilesCounted":true}}}}
        """#

    func testReadsTheSizesTheDisksAndTheGrowthInOneCall() async throws {
        let (connection, fake) = connection(answering: answer)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let day = calendar.date(from: DateComponents(year: 2030, month: 5, day: 1))!
        let usage = try await connection.storageUsage(breakdown: .person, range: AgentUsageRange(firstDay: day, lastDay: day))
        XCTAssertEqual(usage.totalSizeBytes, 10000)
        XCTAssertEqual(usage.usageRows.map(\.rowKey), ["search", "mail"])
        XCTAssertEqual(usage.usageRows[0].rowTableCount, 7)
        XCTAssertEqual(usage.growth.growthBytesPerDay, 100.5)
        XCTAssertEqual(usage.growth.projectedSize365DaysBytes, 46000)
        XCTAssertEqual(try sentVariables(fake.sent[0])["by"], .string("person"))
        XCTAssertNotNil(try sentVariables(fake.sent[0])["since"])
    }

    func testTheDisksThatFillSoonAreListedSoonestFirstAndAnObjectStoreIsNotMeasured() async throws {
        let (connection, _) = connection(answering: answer)
        let usage = try await connection.storageUsage(breakdown: .area, range: .lastThirtyDays())
        XCTAssertEqual(usage.fillingDisks.map(\.diskLabel), ["fileDirectory", "dataDirectory"])
        let objectStore = try XCTUnwrap(usage.disks.first { $0.diskLabel == "objectStore" })
        XCTAssertFalse(objectStore.isDiskMeasured)
        XCTAssertNil(objectStore.usedFraction, "a disk that is not measured has no share used")
        XCTAssertEqual(try XCTUnwrap(usage.disks[0].usedFraction), 0.9, accuracy: 0.0001)
    }

    func testTheLargestFiveAreDrawnApartAndTheRestTogether() async throws {
        let (connection, _) = connection(answering: answer)
        let usage = try await connection.storageUsage(breakdown: .area, range: .lastThirtyDays())
        let points = usage.chartPoints
        let seriesKeys = Set(points.map(\.seriesKey))
        XCTAssertEqual(seriesKeys, ["a", "b", "c", "d", "e", StorageUsage.restSeriesKey])
        let rest = points.filter { $0.seriesKey == StorageUsage.restSeriesKey }
        XCTAssertEqual(rest.map(\.sizeBytes), [30, 32], "f and g, each day")
        XCTAssertEqual(points.count, 12)
    }

    func testAPersonViewDrawsTheDatabaseAndTheFilesBecausePeopleAreNotSampled() async throws {
        let (connection, _) = connection(answering: answer.replacingOccurrences(of: "\"breakdownBy\":\"area\"", with: "\"breakdownBy\":\"person\""))
        let usage = try await connection.storageUsage(breakdown: .person, range: .lastThirtyDays())
        XCTAssertEqual(Set(usage.chartPoints.map(\.seriesKey)), [StorageUsage.databaseSeriesKey, StorageUsage.filesSeriesKey])
        XCTAssertEqual(usage.chartPoints.filter { $0.seriesKey == StorageUsage.filesSeriesKey }.map(\.sizeBytes), [4000, 4000])
    }

    private func sentBody(_ request: URLRequest) throws -> [String: JSONValue] {
        try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        guard case .object(let variables) = try sentBody(request)["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }
}
