import Foundation
import XCTest

@testable import TeaNodeKit

final class MailRuleTests: XCTestCase {
    func testDecodesTheServersRule() throws {
        let json = #"""
            {"name":"Receipts","enabled":true,"stop":false,
             "conditions":[{"field":"subject","header":"","operator":"contains","value":"receipt"},{"field":"any","header":null,"operator":null,"value":null}],
             "actions":[{"kind":"move","folderId":"folder-1","address":""}]}
            """#
        let rule = try JSONDecoder().decode(MailRule.self, from: Data(json.utf8))
        XCTAssertEqual(rule.name, "Receipts")
        XCTAssertEqual(rule.conditions[0].comparisonOperator, "contains")
        XCTAssertEqual(rule.conditions[1].comparisonOperator, "")
        XCTAssertTrue(rule.conditions[1].isBare)
        XCTAssertEqual(rule.actions[0].folderId, "folder-1")
    }

    func testSendsEachPartOnlyWhereItMeansSomething() {
        let rule = MailRule(
            name: "Mixed",
            conditions: [
                MailRule.Condition(field: "from", header: "X-Left-Over", comparisonOperator: "contains", value: "example.com"),
                MailRule.Condition(field: "needs-reply", comparisonOperator: "contains", value: ""),
                MailRule.Condition(field: "header", header: "List-Id", comparisonOperator: "matches", value: "news"),
            ],
            actions: [
                MailRule.Action(kind: "forward", folderId: "folder-1", address: "someone@example.com"),
                MailRule.Action(kind: "move", folderId: "folder-2", address: "left@example.com"),
            ])
        guard case .object(let fields) = rule.variables,
            case .array(let conditions) = fields["conditions"],
            case .array(let actions) = fields["actions"]
        else {
            return XCTFail("not an object")
        }
        XCTAssertEqual(conditions[0], .object(["field": .string("from"), "header": .string(""), "operator": .string("contains"), "value": .string("example.com")]))
        XCTAssertEqual(conditions[1], .object(["field": .string("needs-reply"), "header": .string(""), "operator": .string(""), "value": .string("")]))
        XCTAssertEqual(conditions[2], .object(["field": .string("header"), "header": .string("List-Id"), "operator": .string("matches"), "value": .string("news")]))
        XCTAssertEqual(actions[0], .object(["kind": .string("forward"), "folderId": .string(""), "address": .string("someone@example.com")]))
        XCTAssertEqual(actions[1], .object(["kind": .string("move"), "folderId": .string("folder-2"), "address": .string("")]))
    }

    func testOperatorsFollowTheField() {
        XCTAssertEqual(MailRule.Condition.operators(for: "score"), ["above", "below"])
        XCTAssertEqual(MailRule.Condition.operators(for: "priority"), ["equals"])
        XCTAssertEqual(MailRule.Condition.operators(for: "any"), [""])
        XCTAssertEqual(MailRule.Condition.operators(for: "subject"), ["contains", "equals", "matches"])
    }
}
