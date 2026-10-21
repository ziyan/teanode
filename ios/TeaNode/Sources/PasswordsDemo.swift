#if DEBUG
    import Foundation
    import TeaNodeKit

    // The invented Passwords that the screenshots photograph (`-TeaNodeDemo`).
    // PasswordsDemoStore does what the server does for Passwords: it keeps only
    // sealed text, revisions, shares and their keys, and answers every operation
    // of the app, so the screens run the real encryption against it. The demo
    // passphrase is fixed, so a demo can unlock. Everything here is invented.

    /// PasswordsDemoFailure is a refusal the demo server answers with.
    struct PasswordsDemoFailure: Error {
        let reason: String
    }

    // The sentences the real server refuses with (internal/api/v1api/apigraph/passwords.go),
    // so the screens meet the same words in a demo.
    private enum PasswordsDemoSentence {
        static let notFound = "api: not found"
        static let itemChanged = "api: conflict: This item changed on another device. Open it again."
        static let keyChanged =
            "api: invalid arguments: That person's key changed since it was read. Read their key again and seal the item for it."
        static let kindCannotChange = "api: invalid arguments: the kind of an item cannot change"
        static let onATap =
            "api: permission denied: a person's own tap is needed: setting up, erasing and changing the secrets of Passwords, sharing an item with someone new, and letting the agent use an item are done on the Passwords page of the dashboard or in the iPhone app, not from a command line or an API token"
    }

    final class PasswordsDemoStore: @unchecked Sendable {
        static let shared = PasswordsDemoStore()

        /// The passphrase every demo opens with.
        static let passphrase = "invented demo passphrase"

        /// A demo item's password, put together when the demo starts so that
        /// no password-looking text sits in the source.
        private static func demoPassword(_ parts: String...) -> String {
            parts.joined(separator: "-")
        }

        /// A one-time code link for a demo login, with the secret put together the same way.
        private static func demoOneTimeCodeUri(label: String, secretParts: [String], query: String) -> String {
            "otpauth://totp/\(label)?secret=\(secretParts.joined())\(query)"
        }

        static let ownUserId = "robin"
        static let loginItemId = "demo-item-login-mail"
        static let agentLoginItemId = "demo-item-login-shop"
        static let cardItemId = "demo-item-card-travel"
        static let noteItemId = "demo-item-note-cabin"
        static let sharedByMeItemId = "demo-item-login-streaming"
        static let sharedWithMeItemId = "demo-item-login-garden"

        private struct Person {
            let userId: String
            let name: String
            /// Nil for somebody who has not set up Passwords.
            let keyPair: PasswordsKeyPair?
        }

        private struct Setup {
            var accountKeyByPassphrase: String
            var passphraseSalt: String
            var passphraseIterationCount: Int
            var passphraseProofHash: String
            var accountKeyByRecoveryKey: String
            var recoveryKeyProofHash: String
            var publicKey: String
            var privateKeyByAccountKey: String
        }

        private struct StoredItem {
            var id: String
            var ownerUserId: String
            var itemKind: String
            var encryptedFields: String
            var itemKeyByAccountKey: String
            var revision: Int64
            var isUsableByAgent = false
            var lastUsedByAgentAt: String?
            var deletedAt: String?
            var createdAt: String
            var grantees: [PasswordShareGranteeInput] = []
            /// Recipient user id to the key sealed for them and the public key it was sealed for.
            var keys: [String: (itemKeyByRecipientKey: String, recipientPublicKey: String)] = [:]
        }

        private let storeLock = NSLock()
        /// Whether the calls are a person's own tap. The app's are, always; the real server
        /// refuses setting up, erasing, changing the secrets, sharing with someone new and
        /// letting the agent use an item to anything else.
        private let isPersonsTap = true
        private var people: [String: Person] = [:]
        private let circles: [String: (name: String, memberUserIds: [String])] = [
            "circle-home": ("Home", ["robin", "sam", "alex", "jordan"]),
            "circle-club": ("Book club", ["sam", "robin", "alex"]),
        ]
        private var setup: Setup?
        private var lockAfterMinutes = 15
        private var items: [String: StoredItem] = [:]
        private var revisionCounter: Int64 = 0
        private let agentKeyPair = PasswordsCrypto.makeKeyPair()

        private init() {
            for userId in ["robin", "sam", "alex"] {
                people[userId] = Person(userId: userId, name: Self.name(of: userId), keyPair: PasswordsCrypto.makeKeyPair())
            }
            // Somebody in the circle who has not set Passwords up yet.
            people["jordan"] = Person(userId: "jordan", name: Self.name(of: "jordan"), keyPair: nil)
            let screensBeforeSetUp = ["passwords-setup", "passwords-recovery-key", "passwords-intro"]
            if !screensBeforeSetUp.contains(DemoServer.screen ?? "") { seed() }
        }

        private static func name(of userId: String) -> String { DemoServer.demoPeople[userId]?.name ?? userId }

        /// Gives a person another key pair, as if they had set Passwords up again: the item
        /// keys sealed for the old one no longer reach them until the owner seals them again.
        func changeKey(of userId: String) {
            storeLock.lock()
            defer { storeLock.unlock() }
            guard let person = people[userId], person.keyPair != nil else { return }
            people[userId] = Person(userId: person.userId, name: person.name, keyPair: PasswordsCrypto.makeKeyPair())
        }

        // MARK: Seeding

        private func day(daysAgo: Int) -> String {
            ISO8601DateFormatter().string(from: Date().addingTimeInterval(-Double(daysAgo) * 86_400))
        }

        private func nextRevision() -> Int64 {
            revisionCounter += 1
            return revisionCounter
        }

        private static func base64URL(_ data: Data) -> String {
            data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
                .replacingOccurrences(of: "=", with: "")
        }

        /// Sets the demo account up as the app does, and fills it with items.
        private func seed() {
            guard let robin = people["robin"]?.keyPair else { return }
            let accountKey = PasswordsCrypto.makeKey()
            let salt = PasswordsCrypto.randomBytes(count: PasswordsCrypto.passphraseSaltByteCount)
            let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
            let recoveryKey = PasswordsCrypto.makeRecoveryKey()
            guard
                let passphraseSecret = try? PasswordsCrypto.passphraseKey(passphrase: Self.passphrase, salt: salt, iterationCount: iterationCount),
                let privateKeySealed = try? PasswordsCrypto.seal(
                    robin.privateKey, key: accountKey, associatedData: PasswordsCrypto.privateKeyAssociatedData),
                let accountKeyByPassphrase = sealAccountKey(accountKey, secret: passphraseSecret),
                let accountKeyByRecoveryKey = sealAccountKey(accountKey, secret: recoveryKey),
                let passphraseProof = try? PasswordsCrypto.proof(secret: passphraseSecret),
                let recoveryProof = try? PasswordsCrypto.proof(secret: recoveryKey)
            else { return }
            setup = Setup(
                accountKeyByPassphrase: accountKeyByPassphrase, passphraseSalt: salt.base64EncodedString(),
                passphraseIterationCount: iterationCount,
                passphraseProofHash: PasswordsCrypto.proofHash(proof: passphraseProof).base64EncodedString(),
                accountKeyByRecoveryKey: accountKeyByRecoveryKey,
                recoveryKeyProofHash: PasswordsCrypto.proofHash(proof: recoveryProof).base64EncodedString(),
                publicKey: robin.publicKey.base64EncodedString(), privateKeyByAccountKey: privateKeySealed.jsonText)

            let passkey = PasswordsPasskeyFields(
                credentialId: Self.base64URL(PasswordsPasskey.makeCredentialId()), relyingPartyId: "shop.example.com",
                userHandle: Self.base64URL(Data("robin".utf8)), userName: "robin", userDisplayName: "Robin",
                privateKey: PasswordsPasskey.makePrivateKey().base64EncodedString(), createdAt: day(daysAgo: 40))

            addOwned(
                Self.loginItemId, .login(
                    PasswordsLoginFields(
                        name: "Example Mail", userName: "robin@example.com", password: Self.demoPassword("river", "lantern", "42", "Gold"),
                        sites: ["https://mail.example.com"],
                        oneTimeCodeUri: Self.demoOneTimeCodeUri(
                            label: "Example%20Mail:robin@example.com", secretParts: ["JBSWY3DP", "EHPK3PXP"], query: "&issuer=Example%20Mail"),
                        notes: "The backup address is robin.backup@example.com.", folder: "Personal")),
                accountKey: accountKey, createdDaysAgo: 120)
            addOwned(
                Self.agentLoginItemId, .login(
                    PasswordsLoginFields(
                        name: "Example Shop", userName: "robin", password: Self.demoPassword("maple", "orbit", "77", "Tide"),
                        sites: ["https://shop.example.com"], passkeys: [passkey], folder: "Personal",
                        access: PasswordsItemAccess(isUsableByAgent: true, grantees: []))),
                accountKey: accountKey, createdDaysAgo: 90, isUsableByAgent: true, lastUsedByAgentDaysAgo: 2)
            addOwned(
                "demo-item-login-router", .login(
                    PasswordsLoginFields(
                        name: "Home router", userName: "admin", password: Self.demoPassword("copper", "wind", "19", "Pine"),
                        sites: ["http://router.example.test"], folder: "Home")),
                accountKey: accountKey, createdDaysAgo: 200)
            addOwned(
                "demo-item-login-bank", .login(
                    PasswordsLoginFields(
                        name: "Example Bank", userName: "robin.example", password: Self.demoPassword("slate", "harbor", "58", "Ember"),
                        sites: ["https://bank.example.com", "https://login.bank.example.com"],
                        oneTimeCodeUri: Self.demoOneTimeCodeUri(
                            label: "Example%20Bank:robin.example", secretParts: ["GEZDGNBV", "GY3TQOJQ"], query: "&digits=8&period=60"),
                        folder: "Money")),
                accountKey: accountKey, createdDaysAgo: 300)
            addOwned(
                Self.cardItemId, .card(
                    PasswordsCardFields(
                        name: "Travel card", cardholderName: "Robin Example", number: "4111 1111 1111 1111", expiryMonth: "09",
                        expiryYear: "2029", securityCode: "123", brand: "Visa", notes: "Used for trips only.", folder: "Money")),
                accountKey: accountKey, createdDaysAgo: 60)
            addOwned(
                Self.noteItemId, .note(
                    PasswordsNoteFields(
                        name: "Wi-Fi at the cabin", text: "Network: cabin-guest\nPassword: pine-needles-42\nThe router is in the hall cupboard.",
                        folder: "Home")),
                accountKey: accountKey, createdDaysAgo: 30)
            addOwned(
                Self.sharedByMeItemId, .login(
                    PasswordsLoginFields(
                        name: "Family streaming", userName: "family@example.com", password: Self.demoPassword("orchard", "signal", "31", "Ruby"),
                        sites: ["https://watch.example.com"], folder: "Home",
                        access: PasswordsItemAccess(isUsableByAgent: false, grantees: [.circle(circleId: "circle-home")]))),
                accountKey: accountKey, createdDaysAgo: 80, sharedWith: [PasswordShareGranteeInput(granteeCircleId: "circle-home")])
            addOwned(
                "demo-item-note-alarm", .note(PasswordsNoteFields(name: "Old alarm code", text: "Front door: 4821")),
                accountKey: accountKey, createdDaysAgo: 400, deletedDaysAgo: 3)
            addOwned(
                "demo-item-login-forum", .login(
                    PasswordsLoginFields(
                        name: "Old forum", userName: "robin", password: Self.demoPassword("willow", "crest", "66", "Mist"), sites: ["https://forum.example.org"])),
                accountKey: accountKey, createdDaysAgo: 500, deletedDaysAgo: 20)
            addSharedWithRobin(
                Self.sharedWithMeItemId, .login(
                    PasswordsLoginFields(
                        name: "Garden shed lock", userName: "sam", password: Self.demoPassword("cedar", "fox", "84", "Clay"), sites: ["https://shed.example.net"])),
                ownerUserId: "sam", robinPublicKey: robin.publicKey)
        }

        private func sealAccountKey(_ accountKey: Data, secret: Data) -> String? {
            guard let wrapKey = try? PasswordsCrypto.wrapKey(secret: secret),
                let sealed = try? PasswordsCrypto.seal(
                    accountKey, key: wrapKey, associatedData: PasswordsCrypto.accountKeyAssociatedData)
            else { return nil }
            return sealed.jsonText
        }

        private func addOwned(
            _ itemId: String, _ fields: PasswordsItemFields, accountKey: Data, createdDaysAgo: Int, isUsableByAgent: Bool = false,
            lastUsedByAgentDaysAgo: Int? = nil, deletedDaysAgo: Int? = nil, sharedWith: [PasswordShareGranteeInput] = []
        ) {
            let itemKey = PasswordsCrypto.makeKey()
            // The record that says who may have the item is signed with the account key, as the app does.
            guard
                let signedFields = try? PasswordsCrypto.withSignedAccess(
                    accountKey: accountKey, itemId: itemId, itemKind: fields.itemKind, fields: fields, access: fields.access,
                    highestVersionSeen: 0),
                let sealedFields = try? PasswordsCrypto.sealItemFields(signedFields, itemKey: itemKey, itemId: itemId),
                let sealedKey = try? PasswordsCrypto.seal(
                    itemKey, key: accountKey, associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: itemId))
            else { return }
            var item = StoredItem(
                id: itemId, ownerUserId: Self.ownUserId, itemKind: fields.itemKind.rawValue, encryptedFields: sealedFields.jsonText,
                itemKeyByAccountKey: sealedKey.jsonText, revision: nextRevision(), createdAt: day(daysAgo: createdDaysAgo))
            item.isUsableByAgent = isUsableByAgent
            item.lastUsedByAgentAt = lastUsedByAgentDaysAgo.map { day(daysAgo: $0) }
            item.deletedAt = deletedDaysAgo.map { day(daysAgo: $0) }
            item.grantees = sharedWith
            for userId in reachedUserIds(item) {
                guard let ownerKeyPair = people[Self.ownUserId]?.keyPair, let publicKey = people[userId]?.keyPair?.publicKey,
                    let sealed = try? PasswordsCrypto.sealForKeyPair(
                        itemKey, ownerKeyPair: ownerKeyPair, ownerId: Self.ownUserId, recipientPublicKey: publicKey, recipientId: userId,
                        itemId: itemId)
                else { continue }
                item.keys[userId] = (sealed.jsonText, publicKey.base64EncodedString())
            }
            items[itemId] = item
        }

        private func addSharedWithRobin(_ itemId: String, _ fields: PasswordsItemFields, ownerUserId: String, robinPublicKey: Data) {
            let itemKey = PasswordsCrypto.makeKey()
            guard let ownerKeyPair = people[ownerUserId]?.keyPair,
                let sealedFields = try? PasswordsCrypto.sealItemFields(fields, itemKey: itemKey, itemId: itemId),
                let sealedForRobin = try? PasswordsCrypto.sealForKeyPair(
                    itemKey, ownerKeyPair: ownerKeyPair, ownerId: ownerUserId, recipientPublicKey: robinPublicKey,
                    recipientId: Self.ownUserId, itemId: itemId)
            else { return }
            var item = StoredItem(
                id: itemId, ownerUserId: ownerUserId, itemKind: fields.itemKind.rawValue, encryptedFields: sealedFields.jsonText,
                itemKeyByAccountKey: "", revision: nextRevision(), createdAt: day(daysAgo: 45))
            item.grantees = [PasswordShareGranteeInput(granteeUserId: Self.ownUserId)]
            item.keys[Self.ownUserId] = (sealedForRobin.jsonText, robinPublicKey.base64EncodedString())
            items[itemId] = item
        }

        // MARK: Who an item reaches

        private func memberUserIds(of grantee: PasswordShareGranteeInput) -> [String] {
            if let granteeUserId = grantee.granteeUserId { return [granteeUserId] }
            if let granteeCircleId = grantee.granteeCircleId { return circles[granteeCircleId]?.memberUserIds ?? [] }
            return []
        }

        private func reachedUserIds(_ item: StoredItem) -> [String] {
            var seen = Set<String>()
            return item.grantees.flatMap { memberUserIds(of: $0) }.filter { $0 != item.ownerUserId && seen.insert($0).inserted }
        }

        private func recipientView(_ userId: String, item: StoredItem?) -> PasswordRecipientView {
            let person = people[userId]
            let publicKey = person?.keyPair?.publicKey.base64EncodedString()
            let hasKey = publicKey != nil && item?.keys[userId]?.recipientPublicKey == publicKey
            return PasswordRecipientView(
                userId: userId, name: person?.name ?? userId, username: userId, publicKey: publicKey, hasKey: hasKey)
        }

        /// The owner's current public key: the signed-in person's own setup, or another person's key pair.
        private func ownerPublicKey(of item: StoredItem) -> String {
            if item.ownerUserId == Self.ownUserId, let setup { return setup.publicKey }
            return people[item.ownerUserId]?.keyPair?.publicKey.base64EncodedString() ?? ""
        }

        private func view(_ item: StoredItem, for viewerUserId: String) -> PasswordItemView? {
            if item.ownerUserId == viewerUserId {
                var shares: [PasswordShareView] = []
                for (index, grantee) in item.grantees.enumerated() {
                    let recipients = memberUserIds(of: grantee).filter { $0 != viewerUserId }.map { recipientView($0, item: item) }
                    let granteeName =
                        grantee.granteeCircleId.flatMap { circles[$0]?.name } ?? grantee.granteeUserId.flatMap { people[$0]?.name } ?? ""
                    shares.append(
                        PasswordShareView(
                            id: "share-\(item.id)-\(index)", granteeUserId: grantee.granteeUserId,
                            granteeCircleId: grantee.granteeCircleId, granteeName: granteeName, recipients: recipients))
                }
                return PasswordItemView(
                    id: item.id, ownerUserId: item.ownerUserId, ownerName: people[item.ownerUserId]?.name ?? "",
                    ownerPublicKey: ownerPublicKey(of: item), itemKind: item.itemKind,
                    encryptedFields: item.encryptedFields, itemKey: item.itemKeyByAccountKey, isSharedWithMe: false,
                    revision: item.revision, isUsableByAgent: item.isUsableByAgent, lastUsedByAgentAt: item.lastUsedByAgentAt,
                    deletedAt: item.deletedAt, createdAt: item.createdAt, updatedAt: item.createdAt, shares: shares)
            }
            guard item.deletedAt == nil, reachedUserIds(item).contains(viewerUserId), let key = item.keys[viewerUserId] else { return nil }
            return PasswordItemView(
                id: item.id, ownerUserId: item.ownerUserId, ownerName: people[item.ownerUserId]?.name ?? "",
                ownerPublicKey: ownerPublicKey(of: item), itemKind: item.itemKind,
                encryptedFields: item.encryptedFields, itemKey: key.itemKeyByRecipientKey, isSharedWithMe: true, revision: item.revision,
                isUsableByAgent: false, createdAt: item.createdAt, updatedAt: item.createdAt)
        }

        // MARK: Answering

        /// What the Passwords operations answer, or nil for any other question.
        func answer(query: String, variables: [String: Any]) -> String? {
            storeLock.lock()
            defer { storeLock.unlock() }
            do {
                if query.contains("ReadPasswordsSetup") { return try respond("ReadPasswordsSetup", setupView()) }
                if query.contains("SetUpPasswords") { return try respond("SetUpPasswords", setUp(variables)) }
                if query.contains("UnlockPasswords") { return try respond("UnlockPasswords", unlock(variables)) }
                if query.contains("ChangePasswordsPassphrase") {
                    return try respond("ChangePasswordsPassphrase", changePassphrase(variables))
                }
                if query.contains("ReplacePasswordsRecoveryKey") {
                    return try respond("ReplacePasswordsRecoveryKey", replaceRecoveryKey(variables))
                }
                if query.contains("UpdatePasswordsSettings") {
                    lockAfterMinutes = (variables["lockAfterMinutes"] as? Int) ?? lockAfterMinutes
                    return try respond("UpdatePasswordsSettings", setupView())
                }
                if query.contains("ErasePasswords") { return try respond("ErasePasswords", erase()) }
                if query.contains("ListPasswordItems") { return try respond("ListPasswordItems", list(variables)) }
                if query.contains("ListPasswordRecipients") { return try respond("ListPasswordRecipients", recipients(variables)) }
                if query.contains("ListWaitingPasswordShares") { return try respond("ListWaitingPasswordShares", waitingShares()) }
                if query.contains("ReadPasswordsAgentKey") {
                    return try respond("ReadPasswordsAgentKey", agentKeyPair.publicKey.base64EncodedString())
                }
                if query.contains("SavePasswordItem") { return try respond("SavePasswordItem", save(variables)) }
                if query.contains("DeletePasswordItem") { return try respond("DeletePasswordItem", delete(variables)) }
                if query.contains("RestorePasswordItem") { return try respond("RestorePasswordItem", restore(variables)) }
                if query.contains("PurgePasswordItem") { return try respond("PurgePasswordItem", purge(variables)) }
                if query.contains("SetPasswordItemShares") { return try respond("SetPasswordItemShares", setShares(variables)) }
                if query.contains("AddPasswordShareKeys") { return try respond("AddPasswordShareKeys", addShareKeys(variables)) }
                if query.contains("SetPasswordItemUsableByAgent") {
                    return try respond("SetPasswordItemUsableByAgent", setUsableByAgent(variables))
                }
                if query.contains("RekeyPasswordItem") { return try respond("RekeyPasswordItem", rekey(variables)) }
            } catch let failure as PasswordsDemoFailure {
                return refusal(failure.reason)
            } catch {
                return refusal("The demo server could not answer.")
            }
            return nil
        }

        private func respond<Value: Encodable>(_ operation: String, _ value: Value) throws -> String {
            let encoded = try JSONEncoder().encode(value)
            return #"{"data":{"\#(operation)":\#(String(decoding: encoded, as: UTF8.self))}}"#
        }

        private func refusal(_ reason: String) -> String {
            let escaped = reason.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
            return #"{"data":null,"errors":[{"message":"\#(escaped)"}]}"#
        }

        private func string(_ variables: [String: Any], _ key: String) throws -> String {
            guard let value = variables[key] as? String else { throw PasswordsDemoFailure(reason: "Missing \(key).") }
            return value
        }

        private func integer(_ variables: [String: Any], _ key: String) -> Int64? {
            (variables[key] as? NSNumber)?.int64Value
        }

        private func requireTap() throws {
            guard isPersonsTap else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.onATap) }
        }

        /// The passphrase's key derivation cost must be within what every client accepts.
        private func requireIterationCount(_ iterationCount: Int) throws {
            guard iterationCount >= PasswordsCrypto.minimumPassphraseIterationCount,
                iterationCount <= PasswordsCrypto.maximumPassphraseIterationCount
            else {
                throw PasswordsDemoFailure(
                    reason:
                        "api: invalid arguments: passphraseIterationCount is from \(PasswordsCrypto.minimumPassphraseIterationCount) to \(PasswordsCrypto.maximumPassphraseIterationCount)"
                )
            }
        }

        /// The key a person has now, standard base64; nil without Passwords.
        private func currentPublicKey(of userId: String) -> String? {
            if userId == Self.ownUserId, let setup { return setup.publicKey }
            return people[userId]?.keyPair?.publicKey.base64EncodedString()
        }

        /// Checks a key made for a recipient the way the server does: the item reaches them,
        /// and the key was made for the public key they have now.
        private func requireKeyForCurrentKey(userId: String, publicKey: String, reached: Set<String>) throws {
            guard reached.contains(userId) else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound) }
            guard let current = currentPublicKey(of: userId), current == publicKey else {
                throw PasswordsDemoFailure(reason: PasswordsDemoSentence.keyChanged)
            }
        }

        // MARK: Setup

        private func setupView() -> PasswordsSetupView {
            guard let setup else { return PasswordsSetupView(isSetUp: false, lockAfterMinutes: lockAfterMinutes, itemCount: 0) }
            let itemCount = items.values.filter { $0.ownerUserId == Self.ownUserId && $0.deletedAt == nil }.count
            return PasswordsSetupView(
                isSetUp: true, passphraseSalt: setup.passphraseSalt, passphraseIterationCount: setup.passphraseIterationCount,
                publicKey: setup.publicKey, lockAfterMinutes: lockAfterMinutes, itemCount: itemCount)
        }

        private func setUp(_ variables: [String: Any]) throws -> PasswordsSetupView {
            try requireTap()
            let iterationCount = Int(integer(variables, "passphraseIterationCount") ?? 0)
            try requireIterationCount(iterationCount)
            guard setup == nil else { throw PasswordsDemoFailure(reason: "api: already exists: Passwords is already set up.") }
            setup = Setup(
                accountKeyByPassphrase: try string(variables, "accountKeyByPassphrase"), passphraseSalt: try string(variables, "passphraseSalt"),
                passphraseIterationCount: iterationCount,
                passphraseProofHash: try string(variables, "passphraseProofHash"),
                accountKeyByRecoveryKey: try string(variables, "accountKeyByRecoveryKey"),
                recoveryKeyProofHash: try string(variables, "recoveryKeyProofHash"), publicKey: try string(variables, "publicKey"),
                privateKeyByAccountKey: try string(variables, "privateKeyByAccountKey"))
            // The person's own public key is the one the client made.
            if let publicKey = Data(base64Encoded: try string(variables, "publicKey")) {
                people["robin"] = Person(userId: "robin", name: Self.name(of: "robin"), keyPair: PasswordsKeyPair(privateKey: Data(), publicKey: publicKey))
            }
            return setupView()
        }

        private func proofIsRight(_ proof: String, hash: String) -> Bool {
            guard let bytes = Data(base64Encoded: proof) else { return false }
            return PasswordsCrypto.proofHash(proof: bytes).base64EncodedString() == hash
        }

        private func unlock(_ variables: [String: Any]) throws -> PasswordsUnlockView {
            guard let setup else { throw PasswordsDemoFailure(reason: "api: not found: Passwords is not set up yet.") }
            let isRecoveryKey = (variables["isRecoveryKey"] as? Bool) ?? false
            let proof = try string(variables, "proof")
            guard proofIsRight(proof, hash: isRecoveryKey ? setup.recoveryKeyProofHash : setup.passphraseProofHash) else {
                throw PasswordsDemoFailure(reason: isRecoveryKey ? "That recovery key is not right." : "That passphrase is not right.")
            }
            return PasswordsUnlockView(
                accountKeyWrapped: isRecoveryKey ? setup.accountKeyByRecoveryKey : setup.accountKeyByPassphrase,
                privateKeyByAccountKey: setup.privateKeyByAccountKey, publicKey: setup.publicKey)
        }

        private func changePassphrase(_ variables: [String: Any]) throws -> PasswordsSetupView {
            try requireTap()
            guard var current = setup else { throw PasswordsDemoFailure(reason: "api: not found: Passwords is not set up yet.") }
            let isRecoveryKey = (variables["isRecoveryKey"] as? Bool) ?? false
            let iterationCount = Int(integer(variables, "passphraseIterationCount") ?? 0)
            try requireIterationCount(iterationCount)
            guard proofIsRight(try string(variables, "proof"), hash: isRecoveryKey ? current.recoveryKeyProofHash : current.passphraseProofHash)
            else { throw PasswordsDemoFailure(reason: isRecoveryKey ? "That recovery key is not right." : "That passphrase is not right.") }
            current.accountKeyByPassphrase = try string(variables, "accountKeyByPassphrase")
            current.passphraseSalt = try string(variables, "passphraseSalt")
            current.passphraseIterationCount = iterationCount
            current.passphraseProofHash = try string(variables, "passphraseProofHash")
            setup = current
            return setupView()
        }

        private func replaceRecoveryKey(_ variables: [String: Any]) throws -> PasswordsSetupView {
            try requireTap()
            guard var current = setup else { throw PasswordsDemoFailure(reason: "api: not found: Passwords is not set up yet.") }
            guard proofIsRight(try string(variables, "proof"), hash: current.passphraseProofHash) else {
                throw PasswordsDemoFailure(reason: "That passphrase is not right.")
            }
            current.accountKeyByRecoveryKey = try string(variables, "accountKeyByRecoveryKey")
            current.recoveryKeyProofHash = try string(variables, "recoveryKeyProofHash")
            setup = current
            return setupView()
        }

        private func erase() throws -> Bool {
            try requireTap()
            setup = nil
            items = items.filter { $0.value.ownerUserId != Self.ownUserId }
            return true
        }

        // MARK: Items

        private func list(_ variables: [String: Any]) -> PasswordItemListView {
            let visible = items.values.compactMap { view($0, for: Self.ownUserId) }
            let since = integer(variables, "sinceRevision") ?? 0
            let changed = visible.filter { $0.revision > since }.sorted { $0.revision < $1.revision }
            return PasswordItemListView(items: changed, itemIds: visible.map(\.id).sorted(), revision: visible.map(\.revision).max() ?? 0)
        }

        private func recipients(_ variables: [String: Any]) -> [PasswordRecipientView] {
            let grantee = PasswordShareGranteeInput(
                granteeUserId: variables["granteeUserId"] as? String, granteeCircleId: variables["granteeCircleId"] as? String)
            return memberUserIds(of: grantee).filter { $0 != Self.ownUserId }.map { recipientView($0, item: nil) }
        }

        private func waitingShares() -> [PasswordWaitingShareView] {
            var waiting: [PasswordWaitingShareView] = []
            for item in items.values.sorted(by: { $0.id < $1.id }) where item.ownerUserId == Self.ownUserId && item.deletedAt == nil {
                for userId in reachedUserIds(item) {
                    let recipient = recipientView(userId, item: item)
                    if recipient.publicKey != nil, !recipient.hasKey {
                        waiting.append(PasswordWaitingShareView(itemId: item.id, recipient: recipient))
                    }
                }
            }
            return waiting
        }

        private func ownedItem(_ itemId: String) throws -> StoredItem {
            guard let item = items[itemId], item.ownerUserId == Self.ownUserId else {
                throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound)
            }
            return item
        }

        private func presented(_ item: StoredItem) throws -> PasswordItemView {
            guard let presented = view(item, for: Self.ownUserId) else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound) }
            return presented
        }

        private func save(_ variables: [String: Any]) throws -> PasswordItemView {
            let itemId = try string(variables, "id")
            let itemKind = try string(variables, "itemKind")
            guard PasswordsItemKind(rawValue: itemKind) != nil else {
                throw PasswordsDemoFailure(reason: "api: invalid arguments: itemKind is login, card or note")
            }
            guard setup != nil else { throw PasswordsDemoFailure(reason: "api: not found: Passwords is not set up yet.") }
            var item: StoredItem
            if var existing = items[itemId] {
                guard existing.ownerUserId == Self.ownUserId else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound) }
                guard existing.itemKind == itemKind else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.kindCannotChange) }
                guard integer(variables, "baseRevision") == existing.revision else {
                    throw PasswordsDemoFailure(reason: PasswordsDemoSentence.itemChanged)
                }
                // A deleted item is restored before it is changed.
                guard existing.deletedAt == nil else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound) }
                existing.encryptedFields = try string(variables, "encryptedFields")
                existing.itemKeyByAccountKey = try string(variables, "itemKey")
                item = existing
            } else {
                guard integer(variables, "baseRevision") == nil else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.itemChanged) }
                item = StoredItem(
                    id: itemId, ownerUserId: Self.ownUserId, itemKind: itemKind,
                    encryptedFields: try string(variables, "encryptedFields"), itemKeyByAccountKey: try string(variables, "itemKey"),
                    revision: 0, createdAt: day(daysAgo: 0))
            }
            item.revision = nextRevision()
            items[itemId] = item
            return try presented(item)
        }

        private func delete(_ variables: [String: Any]) throws -> PasswordItemView {
            var item = try ownedItem(try string(variables, "id"))
            item.deletedAt = day(daysAgo: 0)
            item.isUsableByAgent = false
            item.keys = [:]
            item.revision = nextRevision()
            items[item.id] = item
            return try presented(item)
        }

        private func restore(_ variables: [String: Any]) throws -> PasswordItemView {
            var item = try ownedItem(try string(variables, "id"))
            item.deletedAt = nil
            item.revision = nextRevision()
            items[item.id] = item
            return try presented(item)
        }

        /// Deletes a deleted item for good, as the real server does for a person's tap.
        private func purge(_ variables: [String: Any]) throws -> Bool {
            try requireTap()
            let item = try ownedItem(try string(variables, "id"))
            guard item.deletedAt != nil else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound) }
            items[item.id] = nil
            return true
        }

        /// Whether the person shares a circle with another person.
        private func hasMutualCircle(with userId: String) -> Bool {
            circles.values.contains { $0.memberUserIds.contains(Self.ownUserId) && $0.memberUserIds.contains(userId) }
        }

        private func setShares(_ variables: [String: Any]) throws -> PasswordItemView {
            var item = try ownedItem(try string(variables, "itemId"))
            guard item.deletedAt == nil else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound) }
            let granteeObjects = variables["grantees"] as? [[String: Any]] ?? []
            let grantees = granteeObjects.map {
                PasswordShareGranteeInput(granteeUserId: $0["granteeUserId"] as? String, granteeCircleId: $0["granteeCircleId"] as? String)
            }
            for grantee in grantees {
                guard (grantee.granteeUserId == nil) != (grantee.granteeCircleId == nil) else {
                    throw PasswordsDemoFailure(reason: "api: invalid arguments: share with one person or one circle")
                }
                if let granteeUserId = grantee.granteeUserId {
                    guard granteeUserId != Self.ownUserId else {
                        throw PasswordsDemoFailure(reason: "api: invalid arguments: an item is already the owner's")
                    }
                    // A person who does not exist, or shares no circle with the owner, is not found.
                    guard people[granteeUserId] != nil, hasMutualCircle(with: granteeUserId) else {
                        throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound)
                    }
                }
                if let granteeCircleId = grantee.granteeCircleId {
                    guard circles[granteeCircleId]?.memberUserIds.contains(Self.ownUserId) == true else {
                        throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound)
                    }
                }
            }
            // Adding somebody to who may be given the key discloses the password: a person's tap.
            let isNewGrantee = grantees.contains { grantee in
                !item.grantees.contains { $0.granteeUserId == grantee.granteeUserId && $0.granteeCircleId == grantee.granteeCircleId }
            }
            if isNewGrantee { try requireTap() }
            item.grantees = grantees
            let reached = Set(reachedUserIds(item))
            item.keys = item.keys.filter { reached.contains($0.key) }
            for key in variables["keys"] as? [[String: Any]] ?? [] {
                guard let userId = key["recipientUserId"] as? String, let sealed = key["itemKeyByRecipientKey"] as? String,
                    let publicKey = key["recipientPublicKey"] as? String
                else { throw PasswordsDemoFailure(reason: "api: invalid arguments: a key names the recipient") }
                try requireKeyForCurrentKey(userId: userId, publicKey: publicKey, reached: reached)
                item.keys[userId] = (sealed, publicKey)
            }
            item.revision = nextRevision()
            items[item.id] = item
            return try presented(item)
        }

        private func addShareKeys(_ variables: [String: Any]) throws -> Int {
            var addedCount = 0
            for key in variables["keys"] as? [[String: Any]] ?? [] {
                guard let itemId = key["itemId"] as? String, let userId = key["recipientUserId"] as? String,
                    let sealed = key["itemKeyByRecipientKey"] as? String, let publicKey = key["recipientPublicKey"] as? String
                else { throw PasswordsDemoFailure(reason: "api: invalid arguments: a key names the item") }
                var item = try ownedItem(itemId)
                guard item.deletedAt == nil else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound) }
                try requireKeyForCurrentKey(userId: userId, publicKey: publicKey, reached: Set(reachedUserIds(item)))
                item.keys[userId] = (sealed, publicKey)
                item.revision = nextRevision()
                items[itemId] = item
                addedCount += 1
            }
            return addedCount
        }

        /// Gives an item a new item key: the fields and the keys come sealed by the device; the old share keys go.
        private func rekey(_ variables: [String: Any]) throws -> PasswordItemView {
            var item = try ownedItem(try string(variables, "id"))
            item.encryptedFields = try string(variables, "encryptedFields")
            item.itemKeyByAccountKey = try string(variables, "itemKey")
            let reached = Set(reachedUserIds(item))
            item.keys = [:]
            for key in variables["shareKeys"] as? [[String: Any]] ?? [] {
                guard let userId = key["recipientUserId"] as? String, let sealed = key["itemKeyByRecipientKey"] as? String,
                    let publicKey = key["recipientPublicKey"] as? String
                else { throw PasswordsDemoFailure(reason: "api: invalid arguments: a key names the recipient") }
                try requireKeyForCurrentKey(userId: userId, publicKey: publicKey, reached: reached)
                item.keys[userId] = (sealed, publicKey)
            }
            if (variables["itemKeyByAgentKey"] as? String)?.isEmpty ?? true {
                item.isUsableByAgent = false
                item.lastUsedByAgentAt = nil
            }
            item.revision = nextRevision()
            items[item.id] = item
            return try presented(item)
        }

        private func setUsableByAgent(_ variables: [String: Any]) throws -> PasswordItemView {
            var item = try ownedItem(try string(variables, "itemId"))
            let isUsable = (variables["isUsableByAgent"] as? Bool) ?? false
            if isUsable {
                // Handing an item to the agent sends its password to the model's provider: a person's tap.
                try requireTap()
                guard let sealedForAgent = variables["itemKeyByAgentKey"] as? String, !sealedForAgent.isEmpty else {
                    throw PasswordsDemoFailure(reason: "api: invalid arguments: itemKeyByAgentKey must be the JSON a device sealed for the agent key")
                }
                if item.itemKind == PasswordsItemKind.card.rawValue {
                    throw PasswordsDemoFailure(reason: "api: invalid arguments: A card cannot be used by the agent.")
                }
                guard item.deletedAt == nil else { throw PasswordsDemoFailure(reason: PasswordsDemoSentence.notFound) }
            }
            item.isUsableByAgent = isUsable
            if !isUsable { item.lastUsedByAgentAt = nil }
            item.revision = nextRevision()
            items[item.id] = item
            return try presented(item)
        }
    }

    extension DemoServer {
        /// What the Passwords operations ask, or nil for any other question.
        static func passwordsAnswer(query: String, body: Data?) -> String? {
            PasswordsDemoStore.shared.answer(query: query, variables: requestVariables(body))
        }
    }
#endif
