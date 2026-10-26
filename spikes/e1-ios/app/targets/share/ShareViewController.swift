import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// Principal class of the share extension (design D2): reads the shared item, then hosts the
/// SwiftUI form. Nothing is shared with the app (ADR-0014).
final class ShareViewController: UIViewController {
  private let model = ShareModel()

  override func viewDidLoad() {
    super.viewDidLoad()
    model.onDone = { [weak self] in self?.extensionContext?.completeRequest(returningItems: nil) }
    model.onCancel = { [weak self] in
      self?.extensionContext?.cancelRequest(withError: CocoaError(.userCancelled))
    }

    let host = UIHostingController(rootView: ShareView(model: model))
    addChild(host)
    host.view.translatesAutoresizingMaskIntoConstraints = false
    view.addSubview(host.view)
    NSLayoutConstraint.activate([
      host.view.topAnchor.constraint(equalTo: view.topAnchor),
      host.view.bottomAnchor.constraint(equalTo: view.bottomAnchor),
      host.view.leadingAnchor.constraint(equalTo: view.leadingAnchor),
      host.view.trailingAnchor.constraint(equalTo: view.trailingAnchor),
    ])
    host.didMove(toParent: self)

    let providers = (extensionContext?.inputItems as? [NSExtensionItem] ?? [])
      .flatMap { $0.attachments ?? [] }
    Task { await model.load(from: providers) }
  }
}

/// What the extension sends: the three forms of RG-01.
enum Capture {
  /// A link, with the text shared around it when it came as text.
  case link(URL, sharedText: String = "")
  case note(String)
  case image(data: Data, mimeType: String, fileExtension: String, preview: UIImage?)
}

enum CaptureReader {
  /// Picks one capture among the shared providers: a URL first, then an image, then text (a
  /// text holding a single URL is a link, as Instagram often shares text).
  static func read(_ providers: [NSItemProvider]) async throws -> Capture {
    if let provider = providers.first(where: { $0.hasItemConformingToTypeIdentifier(UTType.url.identifier) }),
      let url = try await provider.loadItem(forTypeIdentifier: UTType.url.identifier) as? URL,
      url.scheme == "http" || url.scheme == "https"
    {
      return .link(url)
    }
    if let provider = providers.first(where: { $0.hasItemConformingToTypeIdentifier(UTType.image.identifier) }) {
      return try await readImage(provider)
    }
    if let provider = providers.first(where: { $0.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) }),
      let text = try await provider.loadItem(forTypeIdentifier: UTType.plainText.identifier) as? String
    {
      if let url = singleURL(in: text) { return .link(url, sharedText: textAround(url, in: text)) }
      let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
      if !trimmed.isEmpty { return .note(trimmed) }
    }
    throw ShareError.nothingToShare
  }

  private static func readImage(_ provider: NSItemProvider) async throws -> Capture {
    // The most precise image type the provider offers (PNG for a screenshot, HEIC or JPEG for a photo).
    let type = provider.registeredTypeIdentifiers.compactMap(UTType.init)
      .first { $0.conforms(to: .image) } ?? .image
    let data: Data = try await withCheckedThrowingContinuation { continuation in
      _ = provider.loadDataRepresentation(forTypeIdentifier: type.identifier) { data, error in
        if let data { continuation.resume(returning: data) } else {
          continuation.resume(throwing: error ?? ShareError.nothingToShare)
        }
      }
    }
    return .image(
      data: data,
      mimeType: type.preferredMIMEType ?? "application/octet-stream",
      fileExtension: type.preferredFilenameExtension ?? "img",
      preview: UIImage(data: data)?.preparingThumbnail(of: CGSize(width: 240, height: 240)))
  }

  static func singleURL(in text: String) -> URL? {
    guard let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue) else {
      return nil
    }
    let matches = detector.matches(in: text, range: NSRange(text.startIndex..., in: text))
    guard matches.count == 1, let url = matches[0].url, url.scheme == "http" || url.scheme == "https" else {
      return nil
    }
    return url
  }

  /// The text around the single URL of a shared text, offered as the capture note so that
  /// nothing shared is lost. What Instagram really sends is a result of the prototype.
  static func textAround(_ url: URL, in text: String) -> String {
    text.replacingOccurrences(of: url.absoluteString, with: "")
      .trimmingCharacters(in: .whitespacesAndNewlines)
  }
}

enum ShareError: LocalizedError {
  case nothingToShare
  case unreachable
  case rejected(status: Int)

  var errorDescription: String? {
    switch self {
    case .nothingToShare: return "Rien à enregistrer dans ce partage."
    case .unreachable: return "Serveur injoignable. Vérifie le réseau et Tailscale, puis réessaie."
    case .rejected(let status): return "Le serveur a refusé la capture (HTTP \(status))."
    }
  }
}
