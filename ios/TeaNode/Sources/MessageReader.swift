import AVFoundation
import NaturalLanguage
import Observation
import TeaNodeKit

/// MessageReader reads one message aloud. The server cuts it into pieces and
/// says each in the voice the person chose, the next asked for while one plays,
/// so a long message starts soon. When the server has no voice, the person no
/// agent or the day's budget is spent, the phone's own voice reads the rest.
@MainActor
@Observable
final class MessageReader {
    /// The line being read, nil when nothing is.
    private(set) var readingLineId: TranscriptLine.ID?

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
        // Lets what the person was listening to before carry on.
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    private func play(_ text: String, connection: Connection) async {
        let pieces = (try? await connection.speechPieces(of: text)) ?? [text]
        guard !pieces.isEmpty else { return }
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.playback, mode: .spokenAudio)
        try? session.setActive(true)
        var next: Task<Data?, Never>? = Self.asking(for: pieces[0], connection: connection)
        for index in pieces.indices {
            let audio = await next?.value
            if Task.isCancelled { return }
            guard let audio, let piecePlayer = try? AVAudioPlayer(data: audio) else {
                await speakOnThePhone(Array(pieces[index...]))
                return
            }
            next = index + 1 < pieces.count ? Self.asking(for: pieces[index + 1], connection: connection) : nil
            player = piecePlayer
            piecePlayer.play()
            await waitWhile { piecePlayer.isPlaying }
            if Task.isCancelled { return }
        }
    }

    private static func asking(for piece: String, connection: Connection) -> Task<Data?, Never> {
        Task { try? await connection.speechAudio(of: piece) }
    }

    private func speakOnThePhone(_ pieces: [String]) async {
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
