import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

// The two small requests that come before the bytes of a file are sent:
// creating the upload and asking where it is. A background URLSession cannot
// make them (it takes upload and download tasks only, and its answers are not
// seen until a task ends), so they go through an HTTPTransport while the app
// runs. PhotoBackupRequest builds the requests; this reads the answers.

/// PhotoBackupServiceAnswer is what the server said to one of the small
/// requests: the value asked for, or an outcome that says why there is none,
/// in the words the finished uploads use (sign in again, refused, try later).
public enum PhotoBackupServiceAnswer<Value: Equatable & Sendable>: Equatable, Sendable {
    case answered(Value)
    case unsuccessful(PhotoBackupOutcome)
}

/// PhotoBackupServerUpload is where an upload stands on the server.
public enum PhotoBackupServerUpload: Equatable, Sendable {
    /// The server has this many bytes, whole parts that it has stored.
    case receiving(offsetBytes: Int64)
    /// Every byte arrived and the photo exists.
    case complete(serverPhotoId: String)
    /// The server has no such upload: it was cancelled, failed, cleaned up
    /// after a week without a byte, or never was this account's.
    case gone
}

/// PhotoBackupUploads makes the small requests of an upload.
public protocol PhotoBackupUploads: Sendable {
    /// Sends the request that creates an upload and answers its path.
    func createUpload(_ request: URLRequest) async -> PhotoBackupServiceAnswer<String>
    /// Sends the request that asks where an upload is.
    func uploadState(_ request: URLRequest) async -> PhotoBackupServiceAnswer<PhotoBackupServerUpload>
}

/// PhotoBackupUploadClient sends the small requests through a transport.
public struct PhotoBackupUploadClient: PhotoBackupUploads {
    private let transport: any HTTPTransport

    public init(transport: any HTTPTransport) {
        self.transport = transport
    }

    public func createUpload(_ request: URLRequest) async -> PhotoBackupServiceAnswer<String> {
        switch await send(request) {
        case .answered(let data, let response):
            return Self.interpretCreation(
                statusCode: response.statusCode, locationHeader: response.value(forHTTPHeaderField: "Location"), bodyText: Self.text(of: data),
                retryAfterHeader: response.value(forHTTPHeaderField: "Retry-After"))
        case .failed(let outcome):
            return .unsuccessful(outcome)
        }
    }

    public func uploadState(_ request: URLRequest) async -> PhotoBackupServiceAnswer<PhotoBackupServerUpload> {
        switch await send(request) {
        case .answered(let data, let response):
            return Self.interpretState(
                statusCode: response.statusCode, offsetHeader: response.value(forHTTPHeaderField: PhotoBackupHeader.uploadOffset),
                completeHeader: response.value(forHTTPHeaderField: PhotoBackupHeader.uploadComplete),
                photoIdHeader: response.value(forHTTPHeaderField: PhotoBackupHeader.photoId), bodyText: Self.text(of: data),
                retryAfterHeader: response.value(forHTTPHeaderField: "Retry-After"))
        case .failed(let outcome):
            return .unsuccessful(outcome)
        }
    }

    private enum Sent {
        case answered(Data, HTTPURLResponse)
        case failed(PhotoBackupOutcome)
    }

    /// A request that did not get an answer is a reason to try later, and says
    /// whether the network was the cause.
    private func send(_ request: URLRequest) async -> Sent {
        do {
            let (data, response) = try await transport.send(request)
            return .answered(data, response)
        } catch {
            if (error as? URLError)?.code == .cancelled { return .failed(.cancelled) }
            return .failed(
                .retryLater(failure: .systemDescription(error.localizedDescription), isConnectivityFailure: RecordingLibrary.isConnectivityFailure(error)))
        }
    }

    private static func text(of data: Data) -> String? {
        String(data: data.prefix(2048), encoding: .utf8)
    }

    /// The server made the upload when it answers 201 and says where, in
    /// `Location`. Only the path is kept (see PhotoBackupPartialUpload), and
    /// only if it is an upload's. Any other answer is read as the answer to a
    /// finished upload is.
    static func interpretCreation(statusCode: Int, locationHeader: String?, bodyText: String?, retryAfterHeader: String?, now: Date = Date())
        -> PhotoBackupServiceAnswer<String>
    {
        guard statusCode == 201 else {
            return .unsuccessful(
                otherwise(statusCode: statusCode, bodyText: bodyText, retryAfterHeader: retryAfterHeader, now: now))
        }
        guard let location = locationHeader?.trimmingCharacters(in: .whitespacesAndNewlines), let path = URL(string: location)?.path,
            PhotoBackupRequest.isUploadPath(path)
        else {
            return .unsuccessful(.retryLater(failure: nil, isConnectivityFailure: false))
        }
        return .answered(path)
    }

    /// 204 says where the upload is; 404 says there is no such upload.
    static func interpretState(
        statusCode: Int, offsetHeader: String?, completeHeader: String?, photoIdHeader: String?, bodyText: String?, retryAfterHeader: String?,
        now: Date = Date()
    ) -> PhotoBackupServiceAnswer<PhotoBackupServerUpload> {
        switch statusCode {
        case 204, 200:
            guard let offsetBytes = offsetHeader.flatMap({ Int64($0.trimmingCharacters(in: .whitespaces)) }), offsetBytes >= 0 else {
                return .unsuccessful(.retryLater(failure: nil, isConnectivityFailure: false))
            }
            guard completeHeader?.trimmingCharacters(in: .whitespaces) == "?1" else { return .answered(.receiving(offsetBytes: offsetBytes)) }
            guard let photoId = photoIdHeader?.trimmingCharacters(in: .whitespacesAndNewlines), !photoId.isEmpty else {
                return .unsuccessful(.retryLater(failure: .serverDidNotSayWhichPhoto, isConnectivityFailure: false))
            }
            return .answered(.complete(serverPhotoId: photoId))
        case 404, 410:
            return .answered(.gone)
        default:
            return .unsuccessful(otherwise(statusCode: statusCode, bodyText: bodyText, retryAfterHeader: retryAfterHeader, now: now))
        }
    }

    private static func otherwise(statusCode: Int, bodyText: String?, retryAfterHeader: String?, now: Date) -> PhotoBackupOutcome {
        PhotoBackupOutcome.interpret(
            statusCode: statusCode, photoIdHeader: nil, bodyText: bodyText, errorDescription: nil, isConnectivityFailure: false,
            retryAfterHeader: retryAfterHeader, now: now)
    }
}
