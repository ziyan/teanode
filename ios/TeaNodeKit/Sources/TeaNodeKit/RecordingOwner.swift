import Foundation

extension Connection {
    public func verifiedRecordingOwner(_ recording: LocalRecording) async throws -> String {
        let actualOwnerId = try await recordingOwnerId()
        guard recording.belongsTo(accountKey: accountKey, recordingUserId: actualOwnerId) else {
            throw TeaNodeError.refused(
                "The local original belongs to a different or unverified account. It was kept and will not be uploaded.")
        }
        return actualOwnerId
    }

    /// Verifies the server user before touching durable recording originals.
    public func recordingOwnerId() async throws -> String {
        let session = try await accessSession()
        guard let userId = session.userId, !userId.isEmpty else { throw TeaNodeError.signedOut }
        if let previous = account.cachedRecordingUserId, previous != userId { throw TeaNodeError.signedOut }
        if let store {
            guard let retained = try store.load(accountKey: accountKey), retained.accessToken == account.accessToken,
                retained.refreshToken == account.refreshToken
            else { throw TeaNodeError.signedOut }
            var verified = retained
            verified.cachedRecordingUserId = userId
            try store.save(verified)
            account = verified
        } else {
            account.cachedRecordingUserId = userId
        }
        return userId
    }
}

extension LocalRecording {
    public func belongsTo(accountKey: String, recordingUserId: String?) -> Bool {
        guard let recordingUserId, !recordingUserId.isEmpty else { return false }
        return self.accountKey == accountKey && self.recordingUserId == recordingUserId
    }
}
