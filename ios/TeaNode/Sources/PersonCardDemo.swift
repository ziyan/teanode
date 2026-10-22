#if DEBUG
    import Foundation
    import TeaNodeKit

    // The invented cards that the screenshots of a person's card photograph
    // (`-TeaNodeDemo`): Sam, whose contact is linked and who shares where she
    // is, her health and her albums; Alex, whom the person has no contact for;
    // and a bookshop, a contact that is not an account. Every name, address and
    // number is made up, and the people are the circles demo's.

    extension DemoServer {
        // MARK: Which screen opens a card

        /// The card a screenshot opens as a sheet over the app (`-TeaNodeScreen`
        /// `person-card` and `person-card-full` for Sam, `person-card-unlinked`
        /// for Alex, `location-person-card` for the person the Location map has
        /// chosen), or nil for any other screen.
        static var openedPerson: PersonCardTarget? {
            guard isRequested else { return nil }
            switch screen ?? "" {
            case "person-card", "person-card-full": return .account(userId: "sam")
            case "person-card-unlinked": return .account(userId: "alex")
            case "location-person-card": return .account(userId: LocationDemo.historyPersonId)
            default: return nil
            }
        }

        /// The contact the Contacts list opens on for `person-card-contact`: the
        /// bookshop, who is no account.
        static var openedPersonContactId: String? {
            guard isRequested, screen == "person-card-contact" else { return nil }
            return "shop"
        }

        /// Whether the card opens at full height, to show all of it, instead of
        /// at half.
        static var opensPersonCardWhole: Bool {
            guard isRequested else { return false }
            return ["person-card-full", "person-card-unlinked"].contains(screen ?? "")
        }

        // MARK: The answers

        /// The answer to `ReadPersonCard`, or nil for any other question.
        static func personCardAnswering(query: String, body: Data?) -> String? {
            guard query.contains("ReadPersonCard") else { return nil }
            let variables = requestVariables(body)
            if let userId = variables["userId"] as? String {
                return personCardAnswer(target: .account(userId: userId))
            }
            return personCardAnswer(
                target: .contact(addressBookId: variables["addressBookId"] as? String ?? "", contactId: variables["contactId"] as? String ?? ""))
        }

        /// The whole answer for a card.
        static func personCardAnswer(target: PersonCardTarget) -> String {
            #"{"data":{"ReadPersonCard":\#(personCardJSON(target: target))}}"#
        }

        private static func personCardJSON(target: PersonCardTarget) -> String {
            switch target {
            case .account(let userId): userId == "sam" ? samCardJSON : accountWithoutContactCardJSON(userId: userId)
            case .contact(_, let contactId): contactId == "sam" ? samCardJSON : bookshopCardJSON
            }
        }

        // MARK: Sam, whose contact is linked

        private static let sharedCirclesJSON =
            #"[{"circleId":"circle-home","circleName":"Home","circleKind":"family"},{"circleId":"circle-club","circleName":"Book club","circleKind":"friends"}]"#

        private static var samContactJSON: String {
            #"{"id":"sam","name":"Sam","organization":null,"emails":["sam@example.org","sam.garden@example.org"],"phones":["+1 555 0142"],"note":"Likes tea and long walks by the river.","hasPhoto":false,"addresses":[{"street":"12 Elm Street","locality":"Exampleton","region":"","postalCode":"00000","country":"Exampleland"}],"accountUserId":"sam","account":\#(personJSON("sam"))}"#
        }

        private static var samCardJSON: String {
            let healthTypes = serialized(Array(HealthDemo.typesJSON(ownerUserId: "sam").prefix(3)))
            let hasMoreHealth = HealthDemo.typesJSON(ownerUserId: "sam").count > 3
            let photos = demoVisitPhotosJSON(["demo-01", "demo-02", "demo-05", "demo-06"])
            let visits = LocationDemo.historyPersonVisits.joined(separator: ",")
            return #"""
                {"person":\#(circleMemberJSON("sam", memberRole: "member")),"contact":\#(samContactJSON),"sharedCircles":\#(sharedCirclesJSON),"canMessage":true,
                 "location":{"sectionState":"shown","location":\#(LocationDemo.sharedPersonJSON(userId: "sam")),"todayVisits":[\#(visits)],"hasMoreTodayVisits":false},
                 "health":{"sectionState":"shown","healthTypes":\#(healthTypes),"hasMore":\#(hasMoreHealth)},
                 "finance":{"sectionState":"shown","financeAccounts":\#(FinanceSharingDemo.cardAccountsJSON),"hasMore":true},
                 "photos":{"photosState":"shown","photos":\#(photos),"hasMorePhotos":true,"albumsState":"shown","albums":[\#(sharedAlbumJSON(ownerUserId: "sam", title: "Riverside weekend", photoCount: 4, coverPhotoId: "demo-01"))],"hasMoreAlbums":false},
                 "mail":{"sectionState":"shown","hasMore":true,"mails":[
                   \#(mailJSON("mail-lunch", subject: "Lunch on Friday?", isFromPerson: true, isUnread: true, daysAgo: 0, hour: 8, minute: 20)),
                   \#(mailJSON("mail-lunch-reply", subject: "Re: Lunch on Friday?", isFromPerson: false, isUnread: false, daysAgo: 0, hour: 8, minute: 45)),
                   \#(mailJSON("mail-recipe", subject: "The apple tart recipe", isFromPerson: true, isUnread: false, daysAgo: 3, hour: 17, minute: 5))]},
                 "recordings":{"sectionState":"shown","hasMore":false,"recordings":[
                   {"recordingId":"recording-harbor","title":"Planning the harbor walk","recordedAt":"\#(demoTime(daysAgo: 4, hour: 10, minute: 0))","durationMS":1560000,"spokenPassageCount":12},
                   {"recordingId":"recording-dinner","title":"Family dinner","recordedAt":"\#(demoTime(daysAgo: 9, hour: 19, minute: 30))","durationMS":2400000,"spokenPassageCount":7}]},
                 "memory":{"sectionState":"shown","hasMore":false,"pages":[
                   {"nodePath":"people/sam","nodeName":"Sam","nodeKind":"person","summary":"Robin's mother. Likes tea and long walks by the river.","updatedAt":"\#(demoTime(daysAgo: 2, hour: 9, minute: 0))"},
                   {"nodePath":"projects/pottery-class","nodeName":"Pottery class","nodeKind":"project","summary":"A Thursday evening class Sam goes to with a neighbor.","updatedAt":"\#(demoTime(daysAgo: 6, hour: 9, minute: 0))"}]}}
                """#
        }

        // MARK: Alex, whom the person has no contact for

        private static func accountWithoutContactCardJSON(userId: String) -> String {
            let album = sharedAlbumJSON(ownerUserId: userId, title: "Garden beds", photoCount: 3, coverPhotoId: "demo-10")
            return #"""
                {"person":\#(circleMemberJSON(userId, memberRole: "member")),"contact":null,"sharedCircles":\#(sharedCirclesJSON),"canMessage":true,
                 "location":{"sectionState":"shown","location":\#(LocationDemo.sharedPersonJSON(userId: userId)),"todayVisits":[],"hasMoreTodayVisits":false},
                 "health":{"sectionState":"notShared","healthTypes":[],"hasMore":false},
                 "finance":{"sectionState":"notShared","financeAccounts":[],"hasMore":false},
                 "photos":{"photosState":"needsContact","photos":[],"hasMorePhotos":false,"albumsState":"shown","albums":[\#(album)],"hasMoreAlbums":false},
                 "mail":{"sectionState":"needsContact","mails":[],"hasMore":false},
                 "recordings":{"sectionState":"needsContact","recordings":[],"hasMore":false},
                 "memory":{"sectionState":"needsContact","pages":[],"hasMore":false}}
                """#
        }

        // MARK: A bookshop, a contact that is not an account

        private static var bookshopCardJSON: String {
            #"""
            {"person":null,"contact":{"id":"shop","name":null,"organization":"Harbor Books","emails":["orders@bookshop.example"],"phones":["+1 555 0188"],"note":"Closed on Mondays.","hasPhoto":false,"addresses":[{"street":"4 Quay Lane","locality":"Exampleton","region":"","postalCode":"00000","country":"Exampleland"}],"accountUserId":null,"account":null},
             "sharedCircles":[],"canMessage":false,
             "location":{"sectionState":"unavailable","location":null,"todayVisits":[],"hasMoreTodayVisits":false},
             "health":{"sectionState":"unavailable","healthTypes":[],"hasMore":false},
             "photos":{"photosState":"shown","photos":[],"hasMorePhotos":false,"albumsState":"unavailable","albums":[],"hasMoreAlbums":false},
             "mail":{"sectionState":"shown","hasMore":false,"mails":[
               \#(mailJSON("mail-shipped", subject: "Your order has shipped", isFromPerson: true, isUnread: false, daysAgo: 1, hour: 11, minute: 0, fromName: "Harbor Books", fromAddress: "orders@bookshop.example")),
               \#(mailJSON("mail-order", subject: "Order confirmation", isFromPerson: true, isUnread: false, daysAgo: 3, hour: 9, minute: 30, fromName: "Harbor Books", fromAddress: "orders@bookshop.example"))]},
             "recordings":{"sectionState":"shown","recordings":[],"hasMore":false},
             "memory":{"sectionState":"shown","hasMore":false,"pages":[
               {"nodePath":"places/harbor-books","nodeName":"Harbor Books","nodeKind":"organization","summary":"The bookshop by the quay. Orders are posted the next day.","updatedAt":"\#(demoTime(daysAgo: 5, hour: 9, minute: 0))"}]}}
            """#
        }

        // MARK: Pieces

        private static func mailJSON(
            _ id: String, subject: String, isFromPerson: Bool, isUnread: Bool, daysAgo: Int, hour: Int, minute: Int, fromName: String = "Sam",
            fromAddress: String = "sam@example.org"
        ) -> String {
            let sender = isFromPerson ? (name: fromName, address: fromAddress) : (name: "Robin", address: "robin@example.com")
            return
                #"{"mailboxItemId":"\#(id)","mailboxId":"mailbox-demo","folderPath":"\#(isFromPerson ? "Inbox" : "Sent")","subject":"\#(subject)","fromAddress":"\#(sender.address)","fromName":"\#(sender.name)","isFromPerson":\#(isFromPerson),"isUnread":\#(isUnread),"receivedAt":"\#(demoTime(daysAgo: daysAgo, hour: hour, minute: minute))","snippet":""}"#
        }

        /// An album the person owning it shared with the person.
        private static func sharedAlbumJSON(ownerUserId: String, title: String, photoCount: Int, coverPhotoId: String) -> String {
            let owner = demoPerson(ownerUserId)
            return
                #"{"id":"album-\#(ownerUserId)","albumTitle":"\#(title)","createdAt":"\#(demoTime(daysAgo: 20, hour: 9, minute: 0))","updatedAt":"\#(demoTime(daysAgo: 2, hour: 9, minute: 0))","coverPhotoId":"\#(coverPhotoId)","photoCount":\#(photoCount),"firstTakenAt":null,"lastTakenAt":null,"coverPreviewUrl":"/api/v1/photos/\#(coverPhotoId)/previews/small?v=1","ownerUserId":"\#(ownerUserId)","circleId":null,"ownerUsername":"\#(ownerUserId)","ownerName":"\#(owner.name)","ownerAvatarUrl":\#(owner.avatarUrlJSON),"ownerAvatarColorIndex":\#(owner.avatarColorIndex),"circleName":"","isOwnedByCaller":false,"shareLevel":"view","canCallerContribute":false,"canCallerManage":false,"isLocationIncluded":true}"#
        }

        /// Dictionaries written out as JSON text.
        private static func serialized(_ objects: [[String: Any]]) -> String {
            guard let data = try? JSONSerialization.data(withJSONObject: objects), let text = String(data: data, encoding: .utf8) else { return "[]" }
            return text
        }
    }
#endif
