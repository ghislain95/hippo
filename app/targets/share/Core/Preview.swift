import CoreGraphics
import Foundation
import ImageIO

/// The preview card of a link (screen 2): the site, and the page title or else the URL. No
/// network: the title is the one the share gave, if any (design D4).
struct LinkPreview: Equatable, Sendable {
  let site: String
  let headline: String

  init(url: String, title: String?) {
    site = URL(string: url)?.host() ?? url
    headline = title.flatMap(CaptureBody.nonBlank) ?? url
  }
}

/// The thumbnail of an image (screen 4), made by ImageIO from the file: the image is never
/// decoded at full size (design D5).
enum Thumbnail {
  static func make(from file: URL, maxPixelSize: Int = 600) -> CGImage? {
    // The source itself caches no decoded image: only the reduced thumbnail is ever decoded.
    let sourceOptions = [kCGImageSourceShouldCache: false] as CFDictionary
    guard let source = CGImageSourceCreateWithURL(file as CFURL, sourceOptions) else {
      return nil
    }
    let options =
      [
        kCGImageSourceThumbnailMaxPixelSize: maxPixelSize,
        kCGImageSourceCreateThumbnailWithTransform: true,
        kCGImageSourceCreateThumbnailFromImageAlways: true,
        kCGImageSourceShouldCacheImmediately: true,
      ] as CFDictionary
    return CGImageSourceCreateThumbnailAtIndex(source, 0, options)
  }
}
