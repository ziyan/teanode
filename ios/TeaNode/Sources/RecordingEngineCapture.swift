import AVFoundation
import Foundation
import TeaNodeKit

/// RecordingEngineCapture captures the microphone once for a whole stretch of a recording and writes it to files, one per
/// piece, without the audio ever stopping.
///
/// An earlier version closed one AVAudioRecorder and made another at every cut. While the phone was locked, making the
/// next recorder (and asking the audio session for the microphone again) failed, and the audio until the person came
/// back was lost. Here one AVAudioEngine input tap runs from the start until the capture is paused or stopped, the audio
/// session is activated by the caller once, and a cut only changes which file the writer queue appends to: the old file
/// is closed and the next opened in the same queue block, between two buffers, so no buffer falls between files.
///
/// The files are what MeetingRecorder always wrote: 16000 Hz mono 16-bit linear PCM in CAF, protected until first unlock. The
/// same tap feeds the live stream's sender (24000 Hz), which is best effort and cannot affect the files.
final class RecordingEngineCapture: @unchecked Sendable {
    static let fileSampleRate: Double = 16000

    let engine = AVAudioEngine()
    private let writeQueue = DispatchQueue(label: "com.teanode.recording-writer")
    private let liveSender: CaptureSender
    private let lock = NSLock()
    private var _decibels: Float = -160
    private var _failureMessage: String?
    // Set before the tap is installed and only read by it afterwards.
    private var fileConverter: AVAudioConverter?
    private var fileFormat: AVAudioFormat?
    // Owned by the write queue.
    private var file: AVAudioFile?
    private var openedURL: URL?
    private var finalURL: URL?
    private var writtenFrames: Int64 = 0
    private var isTapInstalled = false

    init(liveSender: CaptureSender) {
        self.liveSender = liveSender
    }

    /// The meter's reading, in decibels, of the latest buffer.
    var decibels: Float { lock.withLock { _decibels } }

    /// A problem writing the audio since this was last asked, once.
    func takeFailureMessage() -> String? {
        lock.withLock {
            defer { _failureMessage = nil }
            return _failureMessage
        }
    }

    private func fail(_ message: String) {
        lock.withLock { _failureMessage = _failureMessage ?? message }
    }

    /// Opens `url` for the first piece, then starts the microphone. The audio session is already active.
    ///
    /// The input goes through Apple's voice processing (noise suppression and automatic gain), which also lets the person choose
    /// Voice Isolation or Wide Spectrum for the microphone from the system's menu. A real capture without it was far too quiet.
    /// If it cannot be turned on, or the engine will not start with it, the recording is made without it: it never fails the
    /// capture.
    func start(writingTo url: URL) throws {
        do {
            try begin(writingTo: url, isVoiceProcessingWanted: true)
        } catch {
            NSLog("Recording: starting with voice processing failed (%@); recording without it", error.localizedDescription)
            _ = stopSynchronously()
            try begin(writingTo: url, isVoiceProcessingWanted: false)
        }
    }

    private func begin(writingTo url: URL, isVoiceProcessingWanted: Bool) throws {
        let input = engine.inputNode
        if isVoiceProcessingWanted {
            do {
                try AudioExceptions.run {
                    try input.setVoiceProcessingEnabled(true)
                    input.isVoiceProcessingBypassed = false
                    // Other audio the phone plays is not turned down for a recording.
                    input.voiceProcessingOtherAudioDuckingConfiguration = AVAudioVoiceProcessingOtherAudioDuckingConfiguration(
                        enableAdvancedDucking: false, duckingLevel: .min)
                }
            } catch {
                NSLog("Recording: voice processing could not be turned on (%@)", error.localizedDescription)
                try? AudioExceptions.run { try input.setVoiceProcessingEnabled(false) }
            }
        } else if input.isVoiceProcessingEnabled {
            try? AudioExceptions.run { try input.setVoiceProcessingEnabled(false) }
        }
        // Voice processing changes the format the tap gives; both outputs are converted from whatever it is.
        let inputFormat = input.outputFormat(forBus: 0)
        guard inputFormat.sampleRate > 0, inputFormat.channelCount > 0,
            let fileFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: Self.fileSampleRate, channels: 1, interleaved: true),
            let fileConverter = AVAudioConverter(from: inputFormat, to: fileFormat)
        else {
            throw TeaNodeError.refused(String(localized: "Recording could not start. Check microphone access and available storage."))
        }
        self.fileFormat = fileFormat
        self.fileConverter = fileConverter
        // The live stream's conversion is best effort: without it the files are written all the same.
        if let liveFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 24000, channels: 1, interleaved: true),
            let liveConverter = AVAudioConverter(from: inputFormat, to: liveFormat)
        {
            liveSender.converter = liveConverter
            liveSender.sendFormat = liveFormat
        }
        try writeQueue.sync { try openFile(url) }
        // A tenth of a second at a time, as voice mode sends it.
        let frames = AVAudioFrameCount(inputFormat.sampleRate / 10)
        try AudioExceptions.run {
            input.installTap(onBus: 0, bufferSize: frames, format: inputFormat) { [weak self] buffer, _ in
                self?.take(buffer)
            }
        }
        isTapInstalled = true
        let engine = engine
        do {
            try AudioExceptions.run { try engine.start() }
        } catch {
            _ = stopSynchronously()
            throw error
        }
    }

    /// Closes the current file and continues in `url`, or in none for a stop; gives the number of frames the closed file holds.
    /// Buffers that arrive meanwhile wait in the write queue, so none is lost between the two files.
    func switchFile(to url: URL?) async -> Int64 {
        await withCheckedContinuation { continuation in
            writeQueue.async {
                let closedFrames = self.closeFile()
                if let url {
                    do { try self.openFile(url) } catch { self.fail(error.localizedDescription) }
                }
                continuation.resume(returning: closedFrames)
            }
        }
    }

    /// Stops the microphone and closes the file; gives the number of frames the closed file holds.
    func stop() async -> Int64 {
        removeTapAndStopEngine()
        return await switchFile(to: nil)
    }

    private func stopSynchronously() -> Int64 {
        removeTapAndStopEngine()
        return writeQueue.sync { closeFile() }
    }

    private func removeTapAndStopEngine() {
        let engine = engine
        if isTapInstalled {
            try? AudioExceptions.run { engine.inputNode.removeTap(onBus: 0) }
            isTapInstalled = false
        }
        try? AudioExceptions.run { engine.stop() }
    }

    // MARK: On the audio thread

    private struct ConvertedBuffer: @unchecked Sendable {
        let buffer: AVAudioPCMBuffer
    }

    private func take(_ buffer: AVAudioPCMBuffer) {
        // The live stream first and apart: whatever it does, the files are written.
        liveSender.send(buffer)
        guard let fileConverter, let fileFormat, buffer.frameLength > 0 else { return }
        let ratio = fileFormat.sampleRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount(Double(buffer.frameLength) * ratio) + 16
        guard let converted = AVAudioPCMBuffer(pcmFormat: fileFormat, frameCapacity: capacity) else { return }
        var isGiven = false
        var error: NSError?
        fileConverter.convert(to: converted, error: &error) { _, status in
            if isGiven {
                status.pointee = .noDataNow
                return nil
            }
            isGiven = true
            status.pointee = .haveData
            return buffer
        }
        if let error {
            fail(error.localizedDescription)
            return
        }
        guard converted.frameLength > 0, let samples = converted.int16ChannelData?[0] else { return }
        var sum: Float = 0
        for index in 0..<Int(converted.frameLength) {
            let sample = Float(samples[index]) / Float(Int16.max)
            sum += sample * sample
        }
        let rootMeanSquare = (sum / Float(converted.frameLength)).squareRoot()
        let decibels = rootMeanSquare > 0 ? max(-160, 20 * log10(rootMeanSquare)) : -160
        lock.withLock { _decibels = decibels }
        let toWrite = ConvertedBuffer(buffer: converted)
        writeQueue.async {
            guard let file = self.file else { return }
            do {
                try file.write(from: toWrite.buffer)
                self.writtenFrames += Int64(toWrite.buffer.frameLength)
            } catch {
                self.fail(error.localizedDescription)
            }
        }
    }

    // MARK: On the write queue

    /// The file is written beside its place with a `.caf` ending, which names its container, and moved there when closed.
    private func openFile(_ url: URL) throws {
        let writing = url.appendingPathExtension("caf")
        try? FileManager.default.removeItem(at: writing)
        var opened: AVAudioFile?
        try AudioExceptions.run {
            opened = try AVAudioFile(
                forWriting: writing,
                settings: [
                    AVFormatIDKey: kAudioFormatLinearPCM, AVSampleRateKey: Self.fileSampleRate, AVNumberOfChannelsKey: 1,
                    AVLinearPCMBitDepthKey: 16, AVLinearPCMIsFloatKey: false, AVLinearPCMIsBigEndianKey: false,
                ], commonFormat: .pcmFormatInt16, interleaved: true)
        }
        try FileManager.default.setAttributes(
            [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication, .posixPermissions: 0o600], ofItemAtPath: writing.path)
        file = opened
        openedURL = writing
        finalURL = url
        writtenFrames = 0
    }

    private func closeFile() -> Int64 {
        let frames = writtenFrames
        // Releasing the file finishes its header.
        file = nil
        if let openedURL, let finalURL {
            do {
                try? FileManager.default.removeItem(at: finalURL)
                try FileManager.default.moveItem(at: openedURL, to: finalURL)
            } catch {
                fail(error.localizedDescription)
            }
        }
        openedURL = nil
        finalURL = nil
        writtenFrames = 0
        return frames
    }
}
