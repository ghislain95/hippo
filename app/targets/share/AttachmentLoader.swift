import CoreGraphics
import UIKit
import UniformTypeIdentifiers

/// What the share sheet gave, read without loading any image bytes (design D4).
struct LoadedShare {
  var content = SharedContent()
  /// The providers of `content.images`, in the same order.
  var imageProviders: [NSItemProvider] = []
  /// The title the share gave, for the link preview; no network.
  var title: String?
}

/// The image to send, ready or refused, and its thumbnail. Unchecked: a `CGImage` is immutable
/// once made, so handing it from the provider's queue to the main actor is safe.
struct LoadedImage: @unchecked Sendable {
  let result: ImagePreparation.Result
  let thumbnail: CGImage?
}

/// Turns the attachments of the share into a `SharedContent`, then loads the chosen image as a
/// file (design D4, D5). The decisions are `Core`'s; this layer only reads the providers.
enum AttachmentLoader {
  static func load(_ items: [NSExtensionItem]) async -> LoadedShare {
    var share = LoadedShare()
    share.title = items.first?.attributedTitle?.string
    for item in items {
      for provider in item.attachments ?? [] {
        if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier) {
          if let url = await loadURL(provider) { share.content.urls.append(url) }
        } else if provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) {
          if let text = await loadText(provider) { share.content.texts.append(text) }
        }
        if provider.hasItemConformingToTypeIdentifier(UTType.image.identifier) {
          share.content.images.append(
            SharedImage(typeIdentifiers: provider.registeredTypeIdentifiers))
          share.imageProviders.append(provider)
        }
      }
    }
    return share
  }

  /// Loads the image as a file, copied or converted by `Core` inside the completion handler,
  /// where the given file is valid: first of the type `load` names, then of each other image
  /// type the provider registers (as `Core` would keep or convert it). Only when no file comes is
  /// the image asked for as an object, which decodes it whole (design, Risks).
  static func loadImage(
    _ provider: NSItemProvider, as load: ImageLoad, clientId: Ulid, in directory: URL
  ) async -> LoadedImage {
    var loads = [load]
    for identifier in provider.registeredTypeIdentifiers where identifier != load.typeIdentifier {
      if let other = SharedImage(typeIdentifiers: [identifier]).load { loads.append(other) }
    }
    for candidate in loads {
      if let loaded = await loadFile(provider, as: candidate, clientId: clientId, in: directory),
        loaded.result != .unreadable
      {
        return loaded
      }
    }
    return await loadImageObject(provider, clientId: clientId, in: directory)
  }

  /// The image prepared from a file of the type `load` names, or nil when no file comes.
  private static func loadFile(
    _ provider: NSItemProvider, as load: ImageLoad, clientId: Ulid, in directory: URL
  ) async -> LoadedImage? {
    await withCheckedContinuation { continuation in
      _ = provider.loadFileRepresentation(forTypeIdentifier: load.typeIdentifier) { file, _ in
        guard let file else {
          continuation.resume(returning: nil)
          return
        }
        continuation.resume(
          returning: prepared(
            ImagePreparation.prepare(loadedFile: file, as: load, clientId: clientId, in: directory)
          ))
      }
    }
  }

  private static func loadImageObject(
    _ provider: NSItemProvider, clientId: Ulid, in directory: URL
  ) async -> LoadedImage {
    guard provider.canLoadObject(ofClass: UIImage.self) else {
      return LoadedImage(result: .unreadable, thumbnail: nil)
    }
    return await withCheckedContinuation { continuation in
      _ = provider.loadObject(ofClass: UIImage.self) { object, _ in
        let source = directory.appendingPathComponent("\(clientId.value)-object.jpg")
        defer { try? FileManager.default.removeItem(at: source) }
        guard let image = object as? UIImage, let data = image.jpegData(compressionQuality: 0.9),
          (try? data.write(to: source)) != nil
        else {
          continuation.resume(returning: LoadedImage(result: .unreadable, thumbnail: nil))
          return
        }
        continuation.resume(
          returning: prepared(
            ImagePreparation.prepare(
              loadedFile: source, as: .keep(.jpeg, typeIdentifier: UTType.jpeg.identifier),
              clientId: clientId, in: directory)))
      }
    }
  }

  /// The thumbnail is made here, off the main thread, from the file to send.
  private static func prepared(_ result: ImagePreparation.Result) -> LoadedImage {
    guard case .ready(let image) = result else {
      return LoadedImage(result: result, thumbnail: nil)
    }
    return LoadedImage(result: result, thumbnail: Thumbnail.make(from: image.file))
  }

  private static func loadURL(_ provider: NSItemProvider) async -> URL? {
    await withCheckedContinuation { continuation in
      provider.loadItem(forTypeIdentifier: UTType.url.identifier, options: nil) { item, _ in
        let url: URL? =
          switch item {
          case let url as URL: url
          case let data as Data: URL(dataRepresentation: data, relativeTo: nil)
          case let text as String: URL(string: text)
          default: nil
          }
        continuation.resume(returning: url)
      }
    }
  }

  private static func loadText(_ provider: NSItemProvider) async -> String? {
    await withCheckedContinuation { continuation in
      provider.loadItem(forTypeIdentifier: UTType.plainText.identifier, options: nil) { item, _ in
        let text: String? =
          switch item {
          case let text as String: text
          case let text as NSAttributedString: text.string
          case let data as Data: String(data: data, encoding: .utf8)
          default: nil
          }
        continuation.resume(returning: text)
      }
    }
  }
}
