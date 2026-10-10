import SwiftUI
import VisionKit

/// CodeScanner is the camera reading a QR code. SwiftUI has no scanner of
/// its own, so this wraps VisionKit's, which brings the system's highlight
/// and focus.
struct CodeScanner: UIViewControllerRepresentable {
    let onFound: (String) -> Void

    /// Whether this phone can scan, and the person allowed the camera.
    @MainActor static var isAvailable: Bool {
        DataScannerViewController.isSupported && DataScannerViewController.isAvailable
    }

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let scanner = DataScannerViewController(
            recognizedDataTypes: [.barcode(symbologies: [.qr])], qualityLevel: .balanced, isHighlightingEnabled: true)
        scanner.delegate = context.coordinator
        return scanner
    }

    func updateUIViewController(_ scanner: DataScannerViewController, context: Context) {
        if !scanner.isScanning {
            try? scanner.startScanning()
        }
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(onFound: onFound)
    }

    @MainActor
    final class Coordinator: NSObject, @preconcurrency DataScannerViewControllerDelegate {
        let onFound: (String) -> Void
        private var isFound = false

        init(onFound: @escaping (String) -> Void) {
            self.onFound = onFound
        }

        func dataScanner(_ dataScanner: DataScannerViewController, didAdd addedItems: [RecognizedItem], allItems: [RecognizedItem]) {
            guard !isFound else { return }
            for item in addedItems {
                if case .barcode(let barcode) = item, let text = barcode.payloadStringValue {
                    isFound = true
                    dataScanner.stopScanning()
                    onFound(text)
                    return
                }
            }
        }
    }
}
