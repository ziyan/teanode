import Foundation

/// VoiceEvent is one thing the server says on the voice socket
/// (`/api/v1/agent/voice`, the same socket the dashboard's call uses).
public struct VoiceEvent: Decodable, Sendable, Equatable {
    /// ready, speechStarted, speechStopped, transcriptDelta,
    /// transcriptFinal, transcriptFailed, answerAudio, answerAudioDone,
    /// answerAudioFailed, problem, refused or error.
    public let voiceEvent: String
    public var utteranceId: String? = nil
    public var transcriptText: String? = nil
    public var errorMessage: String? = nil
    public var answerSegmentId: String? = nil
    /// Some of a spoken answer: base64 of mono 16-bit PCM at 24000 Hz.
    public var answerAudio: String? = nil
}

/// VoiceAvailability is whether the person may call their agent, the rate
/// the server takes speech at, and the voices answers may be read in.
public struct VoiceAvailability: Decodable, Sendable {
    public let isVoiceAvailable: Bool
    public let sampleRate: Int?
    /// The voice answers are read in: the person's choice, or the server's.
    public var speechVoice: String? = nil
    /// The voices the person may choose.
    public var speechVoices: [String]? = nil
    /// The server's voice, which an empty choice means.
    public var serverSpeechVoice: String? = nil
}

extension Connection {
    public func voiceAvailability() async throws -> VoiceAvailability {
        struct Answer: Decodable, Sendable { let ReadAgentVoice: VoiceAvailability }
        return try await execute(Documents.voice, as: Answer.self).ReadAgentVoice
    }
}

/// SpokenText turns an answer written for the screen into what is said:
/// code, pictures and formatting left out, and the text cut into sentences,
/// each spoken as soon as it is whole.
public enum SpokenText {
    /// The words of a Markdown answer, as they are read out: no code
    /// blocks, no formatting marks, a link's words without its address, no
    /// hidden lines.
    public static func of(_ markdown: String) -> String {
        var text = Suggestions.split(markdown).text
        // Code is for the screen.
        text = text.replacingOccurrences(of: #"```[\s\S]*?(```|$)"#, with: " ", options: .regularExpression)
        text = HiddenMarkers.removed(from: text)
        // A picture says nothing; a link says its words.
        text = text.replacingOccurrences(of: #"!\[[^\]]*\]\([^)]*\)"#, with: " ", options: .regularExpression)
        text = text.replacingOccurrences(of: #"\[([^\]]*)\]\([^)]*\)"#, with: "$1", options: .regularExpression)
        text = text.replacingOccurrences(of: #"`([^`]*)`"#, with: "$1", options: .regularExpression)
        // Formatting marks, headings, list marks, quotes and table rules.
        text = text.replacingOccurrences(of: #"(\*\*|__|\*|~~)"#, with: "", options: .regularExpression)
        text = text.replacingOccurrences(of: #"(?m)^\s{0,3}(#{1,6}|>|[-*+]|\d{1,3}[.)])\s+"#, with: "", options: .regularExpression)
        text = text.replacingOccurrences(of: #"(?m)^\s*\|?[\s:|-]+\|?\s*$"#, with: "", options: .regularExpression)
        text = text.replacingOccurrences(of: "|", with: ", ")
        return text.replacingOccurrences(of: #"\s+"#, with: " ", options: .regularExpression)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The whole sentences at the start of text being spoken as it is
    /// written, and how much of it they take; what is left is not finished.
    public static func sentences(in text: String) -> (sentences: [String], consumed: Int) {
        var sentences: [String] = []
        var consumed = 0
        var current = ""
        let characters = Array(text)
        var index = 0
        while index < characters.count {
            let character = characters[index]
            current.append(character)
            index += 1
            let isEnd = ".!?。！？".contains(character) || character == "\n"
            let isFollowed = index == characters.count ? false : characters[index].isWhitespace || "。！？".contains(character)
            if isEnd, isFollowed || character == "\n" {
                let sentence = current.trimmingCharacters(in: .whitespacesAndNewlines)
                if !sentence.isEmpty { sentences.append(sentence) }
                consumed = index
                current = ""
            }
        }
        return (sentences, consumed)
    }
}
