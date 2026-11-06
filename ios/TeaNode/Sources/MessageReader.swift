import AVFoundation
import NaturalLanguage
import Observation
import TeaNodeKit

/// MessageReader reads one message aloud. The server cuts it into pieces and
/// says each in the voice the person chose, the next asked for while one plays,
/// so a long message starts soon. When the server has no voice, the person no
/// agent or the day's budget is spent, the phone's own voice reads the rest.
/// When the phone cannot play it, the person is told why rather than left with
/// silence.
@MainActor
@Observable
final class MessageReader {
    /// The line being read, nil when nothing is.
    private(set) var readingLineId: TranscriptLine.ID?
    /// Told why a message cannot be heard, for the conversation to say.
    @ObservationIgnored var onProblem: ((String) -> Void)?

    /// Whether this reader activated the audio session, and so ends it.
    private var hasAudioSession = false
    private var task: Task<Void, Never>?
    private var player: AVAudioPlayer?
    private let synthesizer = AVSpeechSynthesizer()

    var isReading: Bool { readingLineId != nil }

    func isReading(_ line: TranscriptLine) -> Bool {
        readingLineId == line.id
    }

    func read(_ line: TranscriptLine, connection: Connection) {
        stop()
        readingLineId = line.id
        let text = line.readableText
        task = Task { [weak self] in
            await self?.play(text, connection: connection)
            if !Task.isCancelled { self?.finish() }
        }
    }

    func stop() {
        task?.cancel()
        task = nil
        player?.stop()
        synthesizer.stopSpeaking(at: .immediate)
        finish()
    }

    #if DEBUG
        /// Shows a line as being read, for the screenshots CI takes.
        func showReading(_ line: TranscriptLine) {
            readingLineId = line.id
        }
    #endif

    private func finish() {
        guard readingLineId != nil else { return }
        readingLineId = nil
        player = nil
        guard hasAudioSession else { return }
        hasAudioSession = false
        // Lets what the person was listening to before carry on.
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    /// Takes the speaker, right before sound starts: the wait for the audio
    /// is seconds long, and the session may have changed during it.
    private func takeAudioSession() throws {
        // A meeting being recorded needs the session as the recorder set it:
        // taking the category away would stop the recording.
        guard !MeetingRecorder.shared.hasSession else { return }
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playback, mode: .spokenAudio)
        try session.setActive(true)
        hasAudioSession = true
    }

    private func play(_ text: String, connection: Connection) async {
        let pieces = (try? await connection.speechPieces(of: text)) ?? [text]
        guard !pieces.isEmpty else { return }
        var next: Task<Data?, Never>? = Self.asking(for: pieces[0], connection: connection)
        for index in pieces.indices {
            let audio = await next?.value
            if Task.isCancelled { return }
            guard let audio, let piecePlayer = try? AVAudioPlayer(data: audio) else {
                await speakOnThePhone(Array(pieces[index...]))
                return
            }
            next = index + 1 < pieces.count ? Self.asking(for: pieces[index + 1], connection: connection) : nil
            do {
                try takeAudioSession()
            } catch {
                onProblem?(String(localized: "This phone could not play the message: \(error.localizedDescription)"))
                return
            }
            if index == 0, AVAudioSession.sharedInstance().outputVolume < 0.01 {
                onProblem?(String(localized: "Your phone's volume is all the way down."))
            }
            player = piecePlayer
            piecePlayer.prepareToPlay()
            var didPlay = piecePlayer.play()
            if didPlay { didPlay = await waitForEnd(of: piecePlayer) }
            if Task.isCancelled { return }
            guard didPlay else {
                onProblem?(String(localized: "This phone could not play the message, so it will read it in its own voice."))
                await speakOnThePhone(Array(pieces[index...]))
                return
            }
        }
    }

    /// Waits for a piece to end, and says whether it played: audio that never
    /// moved, or stopped well before its end, was not heard.
    private func waitForEnd(of piecePlayer: AVAudioPlayer) async -> Bool {
        let startedAt = Date()
        try? await Task.sleep(for: .seconds(1))
        if piecePlayer.isPlaying, piecePlayer.currentTime == 0 {
            piecePlayer.stop()
            return false
        }
        await waitWhile { piecePlayer.isPlaying }
        return Task.isCancelled || Date().timeIntervalSince(startedAt) >= piecePlayer.duration / 2
    }

    private static func asking(for piece: String, connection: Connection) -> Task<Data?, Never> {
        Task { try? await connection.speechAudio(of: piece) }
    }

    private func speakOnThePhone(_ pieces: [String]) async {
        do {
            try takeAudioSession()
        } catch {
            onProblem?(String(localized: "This phone could not play the message: \(error.localizedDescription)"))
            return
        }
        for piece in pieces {
            if Task.isCancelled { return }
            let utterance = AVSpeechUtterance(string: piece)
            if let language = NLLanguageRecognizer.dominantLanguage(for: piece) {
                utterance.voice = AVSpeechSynthesisVoice(language: language.rawValue)
            }
            synthesizer.speak(utterance)
            await waitWhile { self.synthesizer.isSpeaking }
        }
    }

    private func waitWhile(_ isGoing: () -> Bool) async {
        while isGoing(), !Task.isCancelled {
            try? await Task.sleep(for: .milliseconds(100))
        }
    }
}
