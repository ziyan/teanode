import Foundation

@testable import TeaNodeKit

/// FakePasswordsServer stands in for the server's Passwords half in tests. It
/// keeps what the real one keeps (sealed text, revisions, shares and their
/// keys) and refuses what the real one refuses, with the same words. It never
/// holds a key, except the agent's, which it keeps the way the real one does.
actor FakePasswordsServer {
    struct Person {
        var userId: String
        var name: String
        var username: String
        var setup: PasswordsSetUpInput?
        var lockAfterMinutes = 15
    }

    struct StoredItem {
        var id: String
        var ownerUserId: String
        var itemKind: String
        var encryptedFields: String
        var itemKeyByAccountKey: String
        var revision: Int64
        var isUsableByAgent = false
        var itemKeyByAgentKey: String?
        var deletedAt: String?
        /// Set when access narrowed while the item kept the key somebody held; cleared by a re-key.
        var needsNewKey = false
        var grantees: [PasswordShareGranteeInput] = []
        /// Recipient user id to the key and the public key it was sealed for.
        var keys: [String: (itemKeyByRecipientKey: String, recipientPublicKey: String)] = [:]
    }

    /// An earlier version of an item, kept when a save or a re-key replaced it.
    struct StoredVersion {
        var versionId: String
        var itemKind: String
        var encryptedFields: String
        var itemKeyByAccountKey: String
        var isSavedByPerson: Bool
        var savedAt: String
    }

    private(set) var people: [String: Person] = [:]
    /// The earlier versions of each item, oldest first.
    private(set) var versions: [String: [StoredVersion]] = [:]
    private var versionCounter = 0
    /// Whether the next saves count as the person's own tap (a token's do not).
    private var areSavesByPerson = true
    private(set) var items: [String: StoredItem] = [:]
    private var circles: [String: [String]] = [:]
    private var revisionCounter: Int64 = 0
    let agentKeyPair = PasswordsCrypto.makeKeyPair()
    /// When true, every call fails the way a missing connection does.
    var isOffline = false
    private(set) var listCalls: [Int64?] = []
    /// People who have the most the server lets others share with them: it stores no new key for them.
    private var tooMuchSharedUserIds: Set<String> = []
    /// When true, every waiting share it lists says that more are waiting.
    private var hasMoreWaiting = false
    private var listCallMark = 0

    /// Starts counting list calls from here (`listCallsSinceMark`).
    func markListCalls() {
        listCallMark = listCalls.count
    }

    func listCallsSinceMark() -> [Int64?] {
        Array(listCalls.dropFirst(listCallMark))
    }
    /// When set, the next re-key calls are refused with this sentence (until cleared).
    var rekeyRefusal: String?
    private(set) var rekeyCallCount = 0

    func setRekeyRefusal(_ refusal: String?) {
        rekeyRefusal = refusal
    }

    /// Changes an item's sealed fields as another device's save would (a new revision, the same key).
    func touch(itemId: String) {
        guard var changed = items[itemId] else { return }
        changed.revision = nextRevision()
        items[itemId] = changed
    }

    func setHasTooMuchShared(_ userId: String, _ isTooMuch: Bool) {
        if isTooMuch { tooMuchSharedUserIds.insert(userId) } else { tooMuchSharedUserIds.remove(userId) }
    }

    func setSavesAreByPerson(_ isByPerson: Bool) {
        areSavesByPerson = isByPerson
    }

    /// Keeps what an item holds now as an earlier version, as the real server does before it is replaced.
    private func keepVersion(of item: StoredItem) {
        versionCounter += 1
        let version = StoredVersion(
            versionId: "version-\(versionCounter)", itemKind: item.itemKind, encryptedFields: item.encryptedFields,
            itemKeyByAccountKey: item.itemKeyByAccountKey, isSavedByPerson: areSavesByPerson,
            savedAt: String(format: "2026-10-20T10:%02d:00Z", versionCounter % 60))
        versions[item.id, default: []].append(version)
    }

    func setHasMoreWaiting(_ hasMore: Bool) {
        hasMoreWaiting = hasMore
    }

    func addPerson(userId: String, name: String) {
        people[userId] = Person(userId: userId, name: name, username: name.lowercased())
    }

    /// Sets the members of a circle. Somebody an item no longer reaches loses its key, and the item is
    /// marked as needing a new key, as the real server does when a member leaves.
    func setCircle(_ circleId: String, members: [String]) {
        let reachedBefore = items.mapValues { Set(reached($0)) }
        circles[circleId] = members
        for (itemId, item) in items {
            let reachedNow = Set(reached(item))
            guard !(reachedBefore[itemId] ?? []).subtracting(reachedNow).isEmpty else { continue }
            var changed = item
            changed.keys = changed.keys.filter { reachedNow.contains($0.key) }
            changed.needsNewKey = true
            changed.revision = nextRevision()
            items[itemId] = changed
        }
    }

    func setOffline(_ offline: Bool) {
        isOffline = offline
    }

    /// When true, every unlock is refused the way the server refuses after too many wrong tries.
    private var isRateLimited = false

    func setRateLimited(_ limited: Bool) {
        isRateLimited = limited
    }

    /// Gives an item another revision without touching anything else, as if it
    /// had been changed (or shared with the person) earlier than the copies know of.
    func setRevision(itemId: String, revision: Int64) {
        guard var changed = items[itemId] else { return }
        changed.revision = revision
        items[itemId] = changed
    }

    func item(_ itemId: String) -> StoredItem? { items[itemId] }

    /// A person's public key as standard base64; empty before they set up.
    func publicKey(of userId: String) -> String { people[userId]?.setup?.publicKey ?? "" }

    /// Gives a person another public key without touching anything else, as the
    /// server would show it if the person's key changed (or a server lied about it).
    func replacePublicKey(userId: String, publicKey: String) {
        guard var changed = people[userId], var setup = changed.setup else { return }
        setup.publicKey = publicKey
        changed.setup = setup
        people[userId] = changed
    }

    /// Stores another sealed key for a recipient, for the public key that recipient has now.
    func replaceShareKey(itemId: String, recipientUserId: String, itemKeyByRecipientKey: String) {
        guard var changed = items[itemId], let publicKey = people[recipientUserId]?.setup?.publicKey else { return }
        changed.keys[recipientUserId] = (itemKeyByRecipientKey, publicKey)
        changed.revision = nextRevision()
        items[itemId] = changed
    }

    private func requireOnline() throws {
        if isOffline { throw TeaNodeError.refused("The Internet connection appears to be offline.") }
    }

    private func nextRevision() -> Int64 {
        revisionCounter += 1
        return revisionCounter
    }

    private func time() -> String { "2026-10-20T10:00:00Z" }

    private func person(_ userId: String) throws -> Person {
        guard let person = people[userId] else { throw TeaNodeError.refused("api: not found") }
        return person
    }

    // MARK: Setup

    func readSetup(userId: String) throws -> PasswordsSetupView {
        try requireOnline()
        let person = try person(userId)
        let itemCount = items.values.filter { $0.ownerUserId == userId && $0.deletedAt == nil }.count
        guard let setup = person.setup else {
            return PasswordsSetupView(isSetUp: false, lockAfterMinutes: person.lockAfterMinutes, itemCount: 0)
        }
        return PasswordsSetupView(
            isSetUp: true, passphraseSalt: setup.passphraseSalt, passphraseIterationCount: setup.passphraseIterationCount,
            publicKey: setup.publicKey, lockAfterMinutes: person.lockAfterMinutes, itemCount: itemCount)
    }

    func setUp(userId: String, input: PasswordsSetUpInput) throws -> PasswordsSetupView {
        try requireOnline()
        var person = try person(userId)
        guard person.setup == nil else { throw TeaNodeError.refused("api: already exists: Passwords is already set up.") }
        person.setup = input
        people[userId] = person
        return try readSetup(userId: userId)
    }

    private func proofIsRight(_ proof: String, hash: String) -> Bool {
        guard let bytes = Data(base64Encoded: proof) else { return false }
        return PasswordsCrypto.proofHash(proof: bytes).base64EncodedString() == hash
    }

    func unlock(userId: String, proof: String, isRecoveryKey: Bool) throws -> PasswordsUnlockView {
        try requireOnline()
        if isRateLimited { throw TeaNodeError.refused("api: too many attempts, try again later") }
        guard let setup = try person(userId).setup else { throw TeaNodeError.refused("Passwords is not set up yet.") }
        let hash = isRecoveryKey ? setup.recoveryKeyProofHash : setup.passphraseProofHash
        guard proofIsRight(proof, hash: hash) else {
            throw TeaNodeError.refused(isRecoveryKey ? "That recovery key is not right." : "That passphrase is not right.")
        }
        return PasswordsUnlockView(
            accountKeyWrapped: isRecoveryKey ? setup.accountKeyByRecoveryKey : setup.accountKeyByPassphrase,
            privateKeyByAccountKey: setup.privateKeyByAccountKey, publicKey: setup.publicKey)
    }

    func changePassphrase(userId: String, input: PasswordsChangePassphraseInput) throws -> PasswordsSetupView {
        try requireOnline()
        var person = try person(userId)
        guard var setup = person.setup else { throw TeaNodeError.refused("Passwords is not set up yet.") }
        let hash = input.isRecoveryKey ? setup.recoveryKeyProofHash : setup.passphraseProofHash
        guard proofIsRight(input.proof, hash: hash) else {
            throw TeaNodeError.refused(input.isRecoveryKey ? "That recovery key is not right." : "That passphrase is not right.")
        }
        setup.accountKeyByPassphrase = input.accountKeyByPassphrase
        setup.passphraseSalt = input.passphraseSalt
        setup.passphraseIterationCount = input.passphraseIterationCount
        setup.passphraseProofHash = input.passphraseProofHash
        person.setup = setup
        people[userId] = person
        return try readSetup(userId: userId)
    }

    func replaceRecoveryKey(userId: String, input: PasswordsReplaceRecoveryKeyInput) throws -> PasswordsSetupView {
        try requireOnline()
        var person = try person(userId)
        guard var setup = person.setup else { throw TeaNodeError.refused("Passwords is not set up yet.") }
        guard proofIsRight(input.proof, hash: setup.passphraseProofHash) else { throw TeaNodeError.refused("That passphrase is not right.") }
        setup.accountKeyByRecoveryKey = input.accountKeyByRecoveryKey
        setup.recoveryKeyProofHash = input.recoveryKeyProofHash
        person.setup = setup
        people[userId] = person
        return try readSetup(userId: userId)
    }

    func updateSettings(userId: String, lockAfterMinutes: Int) throws -> PasswordsSetupView {
        try requireOnline()
        var person = try person(userId)
        guard (1...10080).contains(lockAfterMinutes) else { throw TeaNodeError.refused("The lock time is 1 minute to 7 days.") }
        person.lockAfterMinutes = lockAfterMinutes
        people[userId] = person
        return try readSetup(userId: userId)
    }

    func erase(userId: String) throws -> Bool {
        try requireOnline()
        var person = try person(userId)
        person.setup = nil
        people[userId] = person
        for (itemId, item) in items where item.ownerUserId == userId { items[itemId] = nil }
        for (itemId, item) in items {
            var changed = item
            changed.keys[userId] = nil
            items[itemId] = changed
        }
        return true
    }

    // MARK: Items

    private func reached(_ item: StoredItem) -> [String] {
        var userIds: [String] = []
        for grantee in item.grantees {
            if let granteeUserId = grantee.granteeUserId { userIds.append(granteeUserId) }
            if let circleId = grantee.granteeCircleId { userIds += circles[circleId] ?? [] }
        }
        var seen = Set<String>()
        return userIds.filter { $0 != item.ownerUserId && seen.insert($0).inserted }
    }

    private func recipientView(_ userId: String, item: StoredItem) -> PasswordRecipientView {
        let person = people[userId]
        let publicKey = person?.setup?.publicKey
        let hasKey = publicKey != nil && item.keys[userId]?.recipientPublicKey == publicKey
        return PasswordRecipientView(
            userId: userId, name: person?.name ?? "", username: person?.username ?? "", publicKey: publicKey, hasKey: hasKey,
            hasTooMuchShared: !hasKey && tooMuchSharedUserIds.contains(userId))
    }

    private func view(_ item: StoredItem, for viewerUserId: String) -> PasswordItemView? {
        let owner = people[item.ownerUserId]
        if item.ownerUserId == viewerUserId {
            var shares: [PasswordShareView] = []
            for (index, grantee) in item.grantees.enumerated() {
                let members = grantee.granteeUserId.map { [$0] } ?? (circles[grantee.granteeCircleId ?? ""] ?? [])
                let recipients = members.filter { $0 != item.ownerUserId }.map { recipientView($0, item: item) }
                shares.append(
                    PasswordShareView(
                        id: "share-\(item.id)-\(index)", granteeUserId: grantee.granteeUserId, granteeCircleId: grantee.granteeCircleId,
                        granteeName: grantee.granteeUserId.flatMap { people[$0]?.name } ?? grantee.granteeCircleId ?? "",
                        recipients: recipients))
            }
            return PasswordItemView(
                id: item.id, ownerUserId: item.ownerUserId, ownerName: owner?.name ?? "", ownerPublicKey: owner?.setup?.publicKey ?? "",
                itemKind: item.itemKind, encryptedFields: item.encryptedFields, itemKey: item.itemKeyByAccountKey, isSharedWithMe: false, revision: item.revision,
                isUsableByAgent: item.isUsableByAgent, deletedAt: item.deletedAt, createdAt: time(), updatedAt: time(), shares: shares,
                needsNewKey: item.needsNewKey)
        }
        guard item.deletedAt == nil, reached(item).contains(viewerUserId), let key = item.keys[viewerUserId],
            key.recipientPublicKey == people[viewerUserId]?.setup?.publicKey
        else { return nil }
        return PasswordItemView(
            id: item.id, ownerUserId: item.ownerUserId, ownerName: owner?.name ?? "", ownerPublicKey: owner?.setup?.publicKey ?? "",
            itemKind: item.itemKind, encryptedFields: item.encryptedFields, itemKey: key.itemKeyByRecipientKey, isSharedWithMe: true, revision: item.revision,
            isUsableByAgent: false, createdAt: time(), updatedAt: time())
    }

    func list(userId: String, sinceRevision: Int64?) throws -> PasswordItemListView {
        try requireOnline()
        listCalls.append(sinceRevision)
        let visible = items.values.compactMap { view($0, for: userId) }
        let changed = visible.filter { $0.revision > (sinceRevision ?? 0) }.sorted { $0.revision < $1.revision }
        return PasswordItemListView(items: changed, itemIds: visible.map(\.id).sorted(), revision: visible.map(\.revision).max() ?? 0)
    }

    func recipients(userId: String, granteeUserId: String?, granteeCircleId: String?) throws -> [PasswordRecipientView] {
        try requireOnline()
        let members = granteeUserId.map { [$0] } ?? (circles[granteeCircleId ?? ""] ?? [])
        return members.filter { $0 != userId }.map { recipientView($0, item: StoredItem(id: "", ownerUserId: userId, itemKind: "", encryptedFields: "", itemKeyByAccountKey: "", revision: 0)) }
    }

    func waiting(userId: String) throws -> [PasswordWaitingShareView] {
        try requireOnline()
        var waiting: [PasswordWaitingShareView] = []
        for item in items.values.sorted(by: { $0.id < $1.id }) where item.ownerUserId == userId && item.deletedAt == nil {
            for recipientId in reached(item) {
                let recipient = recipientView(recipientId, item: item)
                if recipient.publicKey != nil && !recipient.hasKey {
                    waiting.append(PasswordWaitingShareView(itemId: item.id, recipient: recipient, hasMoreWaiting: hasMoreWaiting))
                }
            }
        }
        return waiting
    }

    func save(userId: String, input: PasswordItemSaveInput) throws -> PasswordItemView {
        try requireOnline()
        var item: StoredItem
        if var existing = items[input.id] {
            guard existing.ownerUserId == userId else { throw TeaNodeError.refused("api: not found") }
            guard input.baseRevision == existing.revision else {
                throw TeaNodeError.refused("This item changed on another device. Open it again.")
            }
            guard existing.itemKind == input.itemKind else { throw TeaNodeError.refused("An item's kind cannot change.") }
            keepVersion(of: existing)
            existing.encryptedFields = input.encryptedFields
            existing.itemKeyByAccountKey = input.itemKey
            item = existing
        } else {
            guard input.baseRevision == nil else { throw TeaNodeError.refused("api: not found") }
            item = StoredItem(
                id: input.id, ownerUserId: userId, itemKind: input.itemKind, encryptedFields: input.encryptedFields,
                itemKeyByAccountKey: input.itemKey, revision: 0)
        }
        item.revision = nextRevision()
        items[item.id] = item
        return view(item, for: userId)!
    }

    private func ownedItem(_ itemId: String, userId: String) throws -> StoredItem {
        guard let item = items[itemId], item.ownerUserId == userId else { throw TeaNodeError.refused("api: not found") }
        return item
    }

    func delete(userId: String, itemId: String) throws -> PasswordItemView {
        try requireOnline()
        var item = try ownedItem(itemId, userId: userId)
        if !item.keys.isEmpty || item.isUsableByAgent { item.needsNewKey = true }
        item.deletedAt = time()
        item.isUsableByAgent = false
        item.itemKeyByAgentKey = nil
        item.keys = [:]
        item.revision = nextRevision()
        items[itemId] = item
        return view(item, for: userId)!
    }

    func restore(userId: String, itemId: String) throws -> PasswordItemView {
        try requireOnline()
        var item = try ownedItem(itemId, userId: userId)
        item.deletedAt = nil
        item.revision = nextRevision()
        items[itemId] = item
        return view(item, for: userId)!
    }

    /// Deletes a deleted item for good, as the real server does for a tap.
    func purge(userId: String, itemId: String) throws -> Bool {
        try requireOnline()
        let item = try ownedItem(itemId, userId: userId)
        guard item.deletedAt != nil else { throw TeaNodeError.refused("api: not found") }
        items[itemId] = nil
        return true
    }

    /// Puts an item back to sealed text captured earlier, as a server that kept an old copy would.
    func replaceStoredFields(itemId: String, encryptedFields: String, itemKeyByAccountKey: String, needsNewKey: Bool) {
        guard var changed = items[itemId] else { return }
        changed.encryptedFields = encryptedFields
        changed.itemKeyByAccountKey = itemKeyByAccountKey
        changed.needsNewKey = needsNewKey
        changed.revision = nextRevision()
        items[itemId] = changed
    }

    /// Marks an item as needing a new key without anything else changing, as the server does on its own word.
    func markNeedsNewKey(itemId: String) {
        guard var changed = items[itemId] else { return }
        changed.needsNewKey = true
        changed.revision = nextRevision()
        items[itemId] = changed
    }

    /// Takes one person's key to an item away without anything else changing, so that person waits for a key.
    func removeKey(itemId: String, userId: String) {
        guard var changed = items[itemId] else { return }
        changed.keys[userId] = nil
        changed.revision = nextRevision()
        items[itemId] = changed
    }

    func setShares(userId: String, itemId: String, grantees: [PasswordShareGranteeInput], keys: [PasswordShareKeyInput]) throws
        -> PasswordItemView
    {
        try requireOnline()
        var item = try ownedItem(itemId, userId: userId)
        let reachedBefore = Set(reached(item))
        item.grantees = grantees
        let reachedUserIds = Set(reached(item))
        if !reachedBefore.subtracting(reachedUserIds).isEmpty { item.needsNewKey = true }
        item.keys = item.keys.filter { reachedUserIds.contains($0.key) }
        for key in keys {
            guard key.itemKey == item.itemKeyByAccountKey else {
                throw TeaNodeError.refused("This item changed on another device. Open it again.")
            }
            guard reachedUserIds.contains(key.recipientUserId) else { throw TeaNodeError.refused("That person is not reached by this share.") }
            guard people[key.recipientUserId]?.setup?.publicKey == key.recipientPublicKey else {
                throw TeaNodeError.refused("api: invalid arguments: That person's key changed since it was read. Read their key again and seal the item for it.")
            }
            item.keys[key.recipientUserId] = (key.itemKeyByRecipientKey, key.recipientPublicKey)
        }
        item.revision = nextRevision()
        items[itemId] = item
        return view(item, for: userId)!
    }

    func addShareKeys(userId: String, keys: [PasswordItemShareKeyInput]) throws -> Int {
        try requireOnline()
        var storedCount = 0
        for key in keys {
            var item = try ownedItem(key.itemId, userId: userId)
            guard key.itemKey == item.itemKeyByAccountKey else {
                throw TeaNodeError.refused("This item changed on another device. Open it again.")
            }
            guard reached(item).contains(key.recipientUserId), people[key.recipientUserId]?.setup?.publicKey == key.recipientPublicKey else {
                throw TeaNodeError.refused("api: not found")
            }
            // A person who has too much shared with them gets no key, and the call goes on for the others.
            if tooMuchSharedUserIds.contains(key.recipientUserId) { continue }
            item.keys[key.recipientUserId] = (key.itemKeyByRecipientKey, key.recipientPublicKey)
            item.revision = nextRevision()
            items[key.itemId] = item
            storedCount += 1
        }
        return storedCount
    }

    func setUsableByAgent(userId: String, itemId: String, isUsableByAgent: Bool, itemKeyByAgentKey: String?) throws -> PasswordItemView {
        try requireOnline()
        var item = try ownedItem(itemId, userId: userId)
        if isUsableByAgent && item.itemKind == "card" { throw TeaNodeError.refused("A card cannot be used by the agent.") }
        if !isUsableByAgent && item.isUsableByAgent { item.needsNewKey = true }
        item.isUsableByAgent = isUsableByAgent
        item.itemKeyByAgentKey = isUsableByAgent ? itemKeyByAgentKey : nil
        item.revision = nextRevision()
        items[itemId] = item
        return view(item, for: userId)!
    }

    func rekey(userId: String, input: PasswordItemRekeyInput) throws -> PasswordItemView {
        try requireOnline()
        rekeyCallCount += 1
        if let rekeyRefusal { throw TeaNodeError.refused(rekeyRefusal) }
        var item = try ownedItem(input.id, userId: userId)
        guard input.baseRevision == item.revision else {
            throw TeaNodeError.refused("This item changed on another device. Open it again.")
        }
        let reachedUserIds = Set(reached(item))
        var newKeys: [String: (itemKeyByRecipientKey: String, recipientPublicKey: String)] = [:]
        for key in input.shareKeys {
            guard key.itemId == item.id, reachedUserIds.contains(key.recipientUserId) else { throw TeaNodeError.refused("api: not found") }
            guard people[key.recipientUserId]?.setup?.publicKey == key.recipientPublicKey else {
                throw TeaNodeError.refused("api: invalid arguments: That person's key changed since it was read. Read their key again and seal the item for it.")
            }
            newKeys[key.recipientUserId] = (key.itemKeyByRecipientKey, key.recipientPublicKey)
        }
        keepVersion(of: item)
        if let agentCopy = input.itemKeyByAgentKey {
            guard item.isUsableByAgent else { throw TeaNodeError.refused("api: invalid arguments: The agent cannot use this item.") }
            item.itemKeyByAgentKey = agentCopy
        } else {
            item.isUsableByAgent = false
            item.itemKeyByAgentKey = nil
        }
        item.encryptedFields = input.encryptedFields
        item.itemKeyByAccountKey = input.itemKey
        item.keys = item.deletedAt == nil ? newKeys : [:]
        item.needsNewKey = false
        item.revision = nextRevision()
        items[item.id] = item
        return view(item, for: userId)!
    }

    func listVersions(userId: String, itemId: String) throws -> [PasswordItemVersionView] {
        try requireOnline()
        let item = try ownedItem(itemId, userId: userId)
        return (versions[itemId] ?? []).reversed().map {
            PasswordItemVersionView(
                versionId: $0.versionId, savedAt: $0.savedAt, isSavedByPerson: $0.isSavedByPerson, encryptedFields: $0.encryptedFields,
                itemKey: $0.itemKeyByAccountKey, itemKind: item.itemKind)
        }
    }

    /// Makes an earlier version current, drops the keys sealed for recipients and the agent's copy, and marks the item.
    func restoreVersion(userId: String, itemId: String, versionId: String, baseRevision: Int64) throws -> PasswordItemView {
        try requireOnline()
        var item = try ownedItem(itemId, userId: userId)
        guard baseRevision == item.revision else { throw TeaNodeError.refused("This item changed on another device. Open it again.") }
        guard let version = versions[itemId]?.first(where: { $0.versionId == versionId }) else { throw TeaNodeError.refused("api: not found") }
        keepVersion(of: item)
        item.encryptedFields = version.encryptedFields
        item.itemKeyByAccountKey = version.itemKeyByAccountKey
        item.keys = [:]
        item.isUsableByAgent = false
        item.itemKeyByAgentKey = nil
        item.needsNewKey = true
        item.revision = nextRevision()
        items[itemId] = item
        return view(item, for: userId)!
    }

    func readAgentKey() throws -> String {
        try requireOnline()
        return agentKeyPair.publicKey.base64EncodedString()
    }
}

/// FakePasswordsAPI is one person's connection to the fake server.
struct FakePasswordsAPI: PasswordsAPI {
    let server: FakePasswordsServer
    let userId: String

    func readPasswordsSetup() async throws -> PasswordsSetupView { try await server.readSetup(userId: userId) }
    func setUpPasswords(_ input: PasswordsSetUpInput) async throws -> PasswordsSetupView {
        try await server.setUp(userId: userId, input: input)
    }
    func unlockPasswords(proof: String, isRecoveryKey: Bool) async throws -> PasswordsUnlockView {
        try await server.unlock(userId: userId, proof: proof, isRecoveryKey: isRecoveryKey)
    }
    func changePasswordsPassphrase(_ input: PasswordsChangePassphraseInput) async throws -> PasswordsSetupView {
        try await server.changePassphrase(userId: userId, input: input)
    }
    func replacePasswordsRecoveryKey(_ input: PasswordsReplaceRecoveryKeyInput) async throws -> PasswordsSetupView {
        try await server.replaceRecoveryKey(userId: userId, input: input)
    }
    func updatePasswordsSettings(lockAfterMinutes: Int) async throws -> PasswordsSetupView {
        try await server.updateSettings(userId: userId, lockAfterMinutes: lockAfterMinutes)
    }
    func erasePasswords() async throws -> Bool { try await server.erase(userId: userId) }
    func listPasswordItems(sinceRevision: Int64?) async throws -> PasswordItemListView {
        try await server.list(userId: userId, sinceRevision: sinceRevision)
    }
    func listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?) async throws -> [PasswordRecipientView] {
        try await server.recipients(userId: userId, granteeUserId: granteeUserId, granteeCircleId: granteeCircleId)
    }
    func listWaitingPasswordShares() async throws -> [PasswordWaitingShareView] { try await server.waiting(userId: userId) }
    func readPasswordsAgentKey() async throws -> String { try await server.readAgentKey() }
    func savePasswordItem(_ input: PasswordItemSaveInput) async throws -> PasswordItemView {
        try await server.save(userId: userId, input: input)
    }
    func deletePasswordItem(id: String) async throws -> PasswordItemView { try await server.delete(userId: userId, itemId: id) }
    func restorePasswordItem(id: String) async throws -> PasswordItemView { try await server.restore(userId: userId, itemId: id) }
    func purgePasswordItem(id: String) async throws -> Bool { try await server.purge(userId: userId, itemId: id) }
    func setPasswordItemShares(itemId: String, grantees: [PasswordShareGranteeInput], keys: [PasswordShareKeyInput]) async throws
        -> PasswordItemView
    {
        try await server.setShares(userId: userId, itemId: itemId, grantees: grantees, keys: keys)
    }
    func addPasswordShareKeys(_ keys: [PasswordItemShareKeyInput]) async throws -> Int {
        try await server.addShareKeys(userId: userId, keys: keys)
    }
    func rekeyPasswordItem(_ input: PasswordItemRekeyInput) async throws -> PasswordItemView {
        try await server.rekey(userId: userId, input: input)
    }
    func setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Bool, itemKeyByAgentKey: String?) async throws -> PasswordItemView {
        try await server.setUsableByAgent(
            userId: userId, itemId: itemId, isUsableByAgent: isUsableByAgent, itemKeyByAgentKey: itemKeyByAgentKey)
    }
    func listPasswordItemVersions(itemId: String) async throws -> [PasswordItemVersionView] {
        try await server.listVersions(userId: userId, itemId: itemId)
    }
    func restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Int64) async throws -> PasswordItemView {
        try await server.restoreVersion(userId: userId, itemId: itemId, versionId: versionId, baseRevision: baseRevision)
    }
}
