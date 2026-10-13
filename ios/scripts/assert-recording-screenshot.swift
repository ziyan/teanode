import Darwin
import Foundation
import Vision

// Inspect rendered content so a navigation fallback cannot count as a recording screenshot.
guard CommandLine.arguments.count == 3 else {
    fputs("Provide the screenshot name and PNG path.\n", stderr)
    exit(1)
}
let screenshotName = CommandLine.arguments[1]
let screenshotFileURL = URL(fileURLWithPath: CommandLine.arguments[2])
let appearanceSuffixes = ["-largest-text", "-light", "-dark"]
var recordingScreenName = screenshotName
if let appearanceSuffix = appearanceSuffixes.first(where: { recordingScreenName.hasSuffix($0) }) {
    recordingScreenName = String(recordingScreenName.dropLast(appearanceSuffix.count))
}
let expectedMarkersByScreen: [String: [String]] = [
    "recordings": ["recordings", "ontheserver"],
    "recording-detail": ["originalmedia"],
    "recording-transcript": ["transcript"],
    "recording-version": ["processing"],
    "recording-speakers": ["confirmedspeakers"],
    "recording-text-history": ["transcripte"],
    "recording-text-editor": ["correctedtranscript"],
    "recording-model-settings": ["recordingtranscription"],
    "recording-quota-settings": ["originalrecordingfilelimit"],
    "recording-attachment-import": ["importrecord"],
    "recording-source-settings": ["editthissource"],
    "recording-citation": ["citedpassage"],
    "recording-learning-stopped": ["stoppedreadingtranscripts", "resumereading"],
]
guard let expectedMarkers = expectedMarkersByScreen[recordingScreenName] else {
    fputs("No rendered-content assertion for \(recordingScreenName).\n", stderr)
    exit(1)
}
let recognitionRequest = VNRecognizeTextRequest()
recognitionRequest.recognitionLevel = .accurate
recognitionRequest.recognitionLanguages = ["en-US"]
recognitionRequest.usesLanguageCorrection = true
do {
    try VNImageRequestHandler(url: screenshotFileURL, options: [:]).perform([recognitionRequest])
    let renderedText = (recognitionRequest.results ?? []).compactMap { $0.topCandidates(1).first?.string }.joined(separator: " ")
    let normalizedRenderedText = renderedText.lowercased().filter { $0.isLetter || $0.isNumber }
    let missingMarkers = expectedMarkers.filter { !normalizedRenderedText.contains($0) }
    guard missingMarkers.isEmpty else {
        fputs("\(screenshotName) does not show its expected recording content: \(missingMarkers.joined(separator: ", ")).\n", stderr)
        exit(1)
    }
    print("Verified rendered content: \(screenshotName)")
} catch {
    fputs("Cannot inspect \(screenshotName): \(error.localizedDescription)\n", stderr)
    exit(1)
}
