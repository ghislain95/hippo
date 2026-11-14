import Foundation
import UniformTypeIdentifiers

/// An image offered by a share, described by the types its provider registers. The extension's
/// loader keeps the provider itself; the choice only needs its types (design D4).
struct SharedImage: Equatable, Sendable {
  let typeIdentifiers: [String]

  /// Whether one of its types is an image at all.
  var isImage: Bool {
    typeIdentifiers.contains { UTType($0)?.conforms(to: .image) == true }
  }
}

/// Everything a share offers, in the order the share sheet gave it: no image bytes (design D4).
struct SharedContent: Equatable, Sendable {
  var urls: [URL] = []
  var texts: [String] = []
  var images: [SharedImage] = []
}

/// The form of the item a share captures (RG-01).
enum CaptureForm: Equatable, Sendable {
  /// A web link, as written (the scheme keeps its case).
  case link(String)
  /// An image, from the first image of the share.
  case image(SharedImage)
  /// A note, with its text trimmed (RG-09).
  case note(String)

  /// A note has no capture note field (RG-07).
  var hasCaptureNote: Bool {
    if case .note = self { return false }
    return true
  }
}

/// The capture a share makes: one item, by priority web URL, then image, then text, and the text
/// that prefills its capture note (RG-01, RG-03, RG-09, design D4).
struct CaptureChoice: Equatable, Sendable {
  let form: CaptureForm
  /// The accompanying text, trimmed, without the captured URL; empty when there is none.
  let prefilledNote: String

  /// The capture of a share, or nil when there is nothing to save (blank text, no image type).
  static func choose(_ content: SharedContent) -> CaptureChoice? {
    if let url = content.urls.first(where: isWebURL) {
      let link = url.absoluteString
      return CaptureChoice(form: .link(link), prefilledNote: accompanying(content.texts, link))
    }
    if let index = content.texts.firstIndex(where: { webURL(inText: $0) != nil }),
      let link = webURL(inText: content.texts[index])
    {
      var others = content.texts
      others.remove(at: index)
      return CaptureChoice(form: .link(link), prefilledNote: accompanying(others, link))
    }
    if let image = content.images.first(where: \.isImage) {
      return CaptureChoice(form: .image(image), prefilledNote: accompanying(content.texts, nil))
    }
    if let text = content.texts.map(trimmed).first(where: { !$0.isEmpty }) {
      return CaptureChoice(form: .note(text), prefilledNote: "")
    }
    return nil
  }

  /// An `http` or `https` URL, in any case, with a host.
  static func isWebURL(_ url: URL) -> Bool {
    guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https" else {
      return false
    }
    return url.host?.isEmpty == false
  }

  /// The text itself when, trimmed, it is only a web URL: no whitespace inside.
  static func webURL(inText text: String) -> String? {
    let candidate = trimmed(text)
    guard !candidate.isEmpty, !candidate.contains(where: \.isWhitespace),
      let url = URL(string: candidate), isWebURL(url)
    else { return nil }
    return candidate
  }

  /// The first text that is not blank once every occurrence of the captured URL is removed and
  /// it is trimmed; empty when there is none (design D4).
  private static func accompanying(_ texts: [String], _ link: String?) -> String {
    let candidates = texts.lazy.map { text in
      trimmed(link.map { text.replacingOccurrences(of: $0, with: "") } ?? text)
    }
    return candidates.first { !$0.isEmpty } ?? ""
  }

  /// Surrounding whitespace and line breaks removed, inner ones kept (RG-09).
  static func trimmed(_ text: String) -> String {
    text.trimmingCharacters(in: .whitespacesAndNewlines)
  }
}
