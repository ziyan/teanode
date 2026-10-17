import Foundation
import TeaNodeKit

/// PhotoBackupSession is the background URLSession that sends the backup's
/// files. The system keeps it running with the app closed; each task carries
/// its PhotoBackupJob in its description, so a task that finished while the app
/// was not running is matched to its photo when the app is started to hear of
/// it (`AppDelegate.application(_:handleEventsForBackgroundURLSession:)`).
///
/// The class keeps its few mutable fields behind a lock, because URLSession
/// calls its delegate on a queue of its own.
final class PhotoBackupSession: NSObject, PhotoBackupTransfer, URLSessionDataDelegate, @unchecked Sendable {
    static let identifier = "photo-backup"
    static let shared = PhotoBackupSession()

    /// How much of an answer's body is kept, which is the server's reason for a
    /// refusal and is short.
    private static let bodyLimitBytes = 2048

    typealias FinishHandler = @Sendable (PhotoBackupJob, PhotoBackupOutcome) async -> Void
    /// Writes the backup's index to disk.
    typealias FlushHandler = @Sendable () async -> Void
    /// Queues more files in the short time the system gives the app when it was
    /// woken because uploads finished. It ends by a deadline of its own.
    typealias TopUpHandler = @Sendable () async -> Void

    private let lock = NSLock()
    private var bodies: [Int: Data] = [:]
    private var finishHandler: FinishHandler?
    private var flushHandler: FlushHandler?
    private var topUpHandler: TopUpHandler?
    /// The tasks reporting finished uploads that have not finished, by an id of
    /// their own; each removes itself when it is done, so a long run with the
    /// app open does not collect one for every file ever sent.
    private var handlerTasks: [UUID: Task<Void, Never>] = [:]
    private var backgroundCompletion: (() -> Void)?

    private lazy var session: URLSession = {
        let configuration = URLSessionConfiguration.background(withIdentifier: Self.identifier)
        configuration.isDiscretionary = false
        configuration.sessionSendsLaunchEvents = true
        // Whether mobile data may be used is said by each request.
        configuration.allowsCellularAccess = true
        // The queue holds hundreds of files, and the system sends them four at a
        // time: the server is not flooded however many are waiting.
        configuration.httpMaximumConnectionsPerHost = 4
        return URLSession(configuration: configuration, delegate: self, delegateQueue: nil)
    }()

    /// Says who hears of a finished upload and who writes the index, and wakes
    /// the session so that it reports the ones that finished while the app was
    /// closed.
    func attach(finishHandler: @escaping FinishHandler, flushHandler: @escaping FlushHandler, topUpHandler: @escaping TopUpHandler) {
        lock.withLock {
            self.finishHandler = finishHandler
            self.flushHandler = flushHandler
            self.topUpHandler = topUpHandler
        }
        _ = session
    }

    /// Keeps the system's completion handler for the events it started the app
    /// for, to call when they have all been dealt with.
    func keep(backgroundCompletion: @escaping () -> Void) {
        lock.withLock { self.backgroundCompletion = backgroundCompletion }
        _ = session
    }

    // MARK: PhotoBackupTransfer

    func start(request: URLRequest, job: PhotoBackupJob, fileURL: URL) async throws {
        let task = session.uploadTask(with: request, fromFile: fileURL)
        task.taskDescription = String(data: try JSONEncoder().encode(job), encoding: .utf8)
        task.resume()
    }

    func queuedJobs() async -> [PhotoBackupJob] {
        await session.allTasks.compactMap { task in
            guard task.state == .running || task.state == .suspended else { return nil }
            return Self.job(of: task)
        }
    }

    func cancelAll() async {
        for task in await session.allTasks { task.cancel() }
    }

    private static func job(of task: URLSessionTask) -> PhotoBackupJob? {
        guard let description = task.taskDescription, let data = description.data(using: .utf8) else { return nil }
        return try? JSONDecoder().decode(PhotoBackupJob.self, from: data)
    }

    // MARK: URLSessionDataDelegate

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        lock.withLock {
            var body = bodies[dataTask.taskIdentifier] ?? Data()
            if body.count < Self.bodyLimitBytes { body.append(data.prefix(Self.bodyLimitBytes - body.count)) }
            bodies[dataTask.taskIdentifier] = body
        }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        let body = lock.withLock { bodies.removeValue(forKey: task.taskIdentifier) } ?? Data()
        guard let job = Self.job(of: task) else { return }
        let response = task.response as? HTTPURLResponse
        // The app cancels tasks itself (backup turned off, a setting changed):
        // that is no error to show and no failure to count.
        let isCancelled = (error as NSError?).map { $0.domain == NSURLErrorDomain && $0.code == NSURLErrorCancelled } ?? false
        let outcome = PhotoBackupOutcome.interpret(
            statusCode: response?.statusCode, photoIdHeader: response?.value(forHTTPHeaderField: PhotoBackupHeader.photoId),
            bodyText: String(data: body, encoding: .utf8), errorDescription: error?.localizedDescription,
            isConnectivityFailure: error.map(RecordingLibrary.isConnectivityFailure) ?? false, isCancelled: isCancelled,
            retryAfterHeader: response?.value(forHTTPHeaderField: "Retry-After"))
        lock.lock()
        defer { lock.unlock() }
        guard let finishHandler else {
            // Nobody has attached yet; the file is not kept for an upload that
            // is over, and the photo is sent again by the next pass.
            Self.removeTemporaryFile(of: job)
            return
        }
        // The lock is held until the task is in the table, and the task takes
        // it to take itself out, so it cannot try to leave before it is there.
        let handlerId = UUID()
        handlerTasks[handlerId] = Task { [weak self] in
            await finishHandler(job, outcome)
            self?.forgetHandlerTask(handlerId)
        }
    }

    private func forgetHandlerTask(_ handlerId: UUID) {
        lock.withLock { _ = handlerTasks.removeValue(forKey: handlerId) }
    }

    /// The system has delivered every event it started the app for. Once they
    /// have been dealt with the app queues more files for the time it is
    /// awake (the handler ends by its own deadline, well inside what the
    /// system allows), and its completion handler is called when the index is
    /// written: the system takes the app's snapshot and suspends it at that
    /// call, and an index not yet written would be lost with it.
    func urlSessionDidFinishEvents(forBackgroundURLSession session: URLSession) {
        let (tasks, completion, flush, topUp) = lock.withLock {
            let tasks = Array(handlerTasks.values)
            handlerTasks = [:]
            let completion = backgroundCompletion
            backgroundCompletion = nil
            return (tasks, completion, flushHandler, topUpHandler)
        }
        guard let completion else { return }
        nonisolated(unsafe) let finish = completion
        Task { @MainActor in
            for task in tasks { await task.value }
            await topUp?()
            await flush?()
            finish()
        }
    }

    /// Deletes the file an upload was read from, which only that upload needed.
    static func removeTemporaryFile(of job: PhotoBackupJob) {
        guard let directory = try? PhotoBackupEngine.outgoingDirectory() else { return }
        try? FileManager.default.removeItem(at: directory.appendingPathComponent((job.outgoingFilename as NSString).lastPathComponent))
    }
}
