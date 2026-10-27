#if DEBUG
    import Foundation

    // The invented shares that the screenshots of the Sharing screen
    // photograph (`-TeaNodeDemo -TeaNodeScreen sharing`): one of every kind on
    // both sides, a share through a circle, one that cannot be removed, and a
    // name of the longest length a row has to hold. The people and circles are
    // those of CirclesDemo.

    extension DemoServer {
        /// What the Sharing screen asks, or nil for any other question.
        static func sharingAnswer(query: String) -> String? {
            if query.contains("DeclineShare") {
                return #"{"data":{"DeclineShare":true}}"#
            }
            guard query.contains("ListSharing") else { return nil }
            return #"{"data":{"ListSharing":{"sharesMade":[\#(demoSharesMade.joined(separator: ","))],"sharesReceived":[\#(demoSharesReceived.joined(separator: ","))]}}}"#
        }

        /// A share the demo person made, with a person (`granteeUserId`) or a
        /// circle (`granteeCircleName`).
        private static func madeShareJSON(
            kind: String, id: String, subjectId: String = "", subjectName: String = "", subjectMask: String = "", isSubjectDeleted: Bool = false,
            level: String = "", scopes: [String] = [],
            itemCount: Int = 0, granteeUserId: String? = nil, granteeCircleName: String = "", createdAt: String
        ) -> String {
            let scopeList = scopes.map { #""\#($0)""# }.joined(separator: ",")
            let grantee = granteeUserId.map { demoPerson($0) }
            let granteeUsername = grantee?.userId ?? ""
            let granteeName = grantee?.name ?? ""
            let circle = granteeUserId == nil ? #""circle-home""# : "null"
            let granteeUser = granteeUserId.map { #""\#($0)""# } ?? "null"
            return
                #"{"shareKind":"\#(kind)","shareId":"\#(id)","subjectId":"\#(subjectId)","subjectName":"\#(subjectName)","subjectMask":"\#(subjectMask)","isSubjectDeleted":\#(isSubjectDeleted),"shareLevel":"\#(level)","shareScopes":[\#(scopeList)],"passwordItemCount":\#(itemCount),"granteeUserId":\#(granteeUser),"granteeUsername":"\#(granteeUsername)","granteeName":"\#(granteeName)","granteeCircleId":\#(circle),"granteeCircleName":"\#(granteeCircleName)","createdAt":"\#(createdAt)"}"#
        }

        /// A share made to the demo person, by name (`circleName` empty) or
        /// through a circle.
        private static func receivedShareJSON(
            kind: String, id: String, subjectId: String = "", subjectName: String = "", subjectMask: String = "", level: String = "", scopes: [String] = [],
            itemCount: Int = 0, ownerUserId: String, circleName: String = "", canDecline: Bool, createdAt: String
        ) -> String {
            let scopeList = scopes.map { #""\#($0)""# }.joined(separator: ",")
            let owner = demoPerson(ownerUserId)
            let isDirect = circleName.isEmpty
            let circle = isDirect ? "null" : #""circle-home""#
            return
                #"{"shareKind":"\#(kind)","shareId":"\#(id)","subjectId":"\#(subjectId)","subjectName":"\#(subjectName)","subjectMask":"\#(subjectMask)","shareLevel":"\#(level)","shareScopes":[\#(scopeList)],"passwordItemCount":\#(itemCount),"ownerUserId":"\#(ownerUserId)","ownerUsername":"\#(ownerUserId)","ownerName":"\#(owner.name)","granteeCircleId":\#(circle),"granteeCircleName":"\#(circleName)","isDirect":\#(isDirect),"canDecline":\#(canDecline),"createdAt":"\#(createdAt)"}"#
        }

        static var demoSharesMade: [String] {
            [
                madeShareJSON(kind: "calendar", id: "demo-share-1", subjectId: "demo-calendar", subjectName: "Calendar", level: "titles", granteeUserId: "sam", createdAt: "2026-09-12T09:00:00Z"),
                madeShareJSON(kind: "location", id: "demo-share-2", level: "current", granteeCircleName: "Home", createdAt: "2026-09-13T09:00:00Z"),
                madeShareJSON(kind: "health", id: "demo-share-3", scopes: ["activity", "sleep"], granteeUserId: "sam", createdAt: "2026-09-14T09:00:00Z"),
                madeShareJSON(kind: "memory_page", id: "demo-share-4", subjectId: "people/morgan", subjectName: "Morgan", scopes: ["quotes"], granteeUserId: "alex", createdAt: "2026-09-15T09:00:00Z"),
                madeShareJSON(kind: "photo_album", id: "demo-share-5", subjectId: "demo-album", subjectName: "Hiking trip", level: "view", scopes: ["location"], granteeCircleName: "Home", createdAt: "2026-09-16T09:00:00Z"),
                madeShareJSON(kind: "recording", id: "demo-share-7", subjectId: "demo-recording", subjectName: "Weekly planning", level: "transcript", granteeUserId: "sam", createdAt: "2026-09-17T08:00:00Z"),
                madeShareJSON(kind: "finance", id: "demo-share-8", subjectId: "demo-account", subjectName: "Everyday checking", subjectMask: "0001", level: "spending", granteeCircleName: "Home", createdAt: "2026-09-17T08:30:00Z"),
                madeShareJSON(kind: "skill", id: "demo-share-6", subjectId: "groceries", subjectName: "groceries", scopes: ["order"], granteeUserId: "sam", createdAt: "2026-09-17T09:00:00Z"),
                madeShareJSON(kind: "shopping_list", id: "demo-share-9", subjectId: "list-groceries", subjectName: "Groceries", granteeCircleName: "Home", createdAt: "2026-09-17T10:00:00Z"),
                madeShareJSON(kind: "passwords", id: "", itemCount: 3, granteeUserId: "sam", createdAt: "2026-09-18T09:00:00Z"),
            ]
        }

        static var demoSharesReceived: [String] {
            [
                receivedShareJSON(kind: "calendar", id: "demo-share-11", subjectId: "demo-calendar-sam", subjectName: "Calendar", level: "details", ownerUserId: "sam", canDecline: true, createdAt: "2026-09-02T09:00:00Z"),
                receivedShareJSON(kind: "location", id: "demo-share-12", level: "history", ownerUserId: "alex", circleName: "Home", canDecline: false, createdAt: "2026-09-03T09:00:00Z"),
                receivedShareJSON(kind: "health", id: "demo-share-13", scopes: ["heart"], ownerUserId: "sam", canDecline: true, createdAt: "2026-09-04T09:00:00Z"),
                receivedShareJSON(kind: "memory_page", id: "demo-share-14", subjectId: "people/sam/recipes", subjectName: "Recipes", ownerUserId: "sam", canDecline: true, createdAt: "2026-09-05T09:00:00Z"),
                receivedShareJSON(kind: "photo_album", id: "demo-share-15", subjectId: "demo-album-birthday", subjectName: "Birthday", level: "contribute", ownerUserId: "alex", circleName: "Home", canDecline: false, createdAt: "2026-09-06T09:00:00Z"),
                receivedShareJSON(kind: "recording", id: "demo-share-17", subjectId: "demo-recording-sam", subjectName: "Team standup", level: "summary", ownerUserId: "sam", canDecline: true, createdAt: "2026-09-06T12:00:00Z"),
                receivedShareJSON(kind: "finance", id: "demo-share-18", subjectId: "demo-account-alex", subjectName: "Joint checking", subjectMask: "7788", level: "balance", ownerUserId: "alex", circleName: "Home", canDecline: false, createdAt: "2026-09-06T13:00:00Z"),
                receivedShareJSON(kind: "skill", id: "demo-share-16", subjectId: "taxi", subjectName: "taxi", scopes: ["book"], ownerUserId: "jordan", canDecline: true, createdAt: "2026-09-07T09:00:00Z"),
                receivedShareJSON(kind: "shopping_list", id: "demo-share-19", subjectId: "list-party", subjectName: "Party supplies", ownerUserId: "sam", canDecline: true, createdAt: "2026-09-07T10:00:00Z"),
                receivedShareJSON(kind: "passwords", id: "", itemCount: 2, ownerUserId: "sam", canDecline: false, createdAt: "2026-09-08T09:00:00Z"),
            ]
        }
    }
#endif
