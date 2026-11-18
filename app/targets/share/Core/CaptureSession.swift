import Foundation

/// The session the capture is sent with (design D6, D7): ephemeral, so the extension stores no
/// cookie and no cache (ADR-0014), and bounded so that a NAS that does not answer is told within
/// seconds while a large image keeps the time its upload needs.
enum CaptureSession {
  /// The longest wait without any byte, name resolution and connection included.
  static let requestTimeout: TimeInterval = 10
  /// The whole exchange of a link or a note.
  static let textResourceTimeout: TimeInterval = 30
  /// The whole upload of an image: the server allows 5 minutes, plus a margin.
  static let imageResourceTimeout: TimeInterval = 330

  static func configuration(for body: RequestBody) -> URLSessionConfiguration {
    let configuration = URLSessionConfiguration.ephemeral
    configuration.timeoutIntervalForRequest = requestTimeout
    switch body {
    case .json: configuration.timeoutIntervalForResource = textResourceTimeout
    case .multipart: configuration.timeoutIntervalForResource = imageResourceTimeout
    }
    configuration.waitsForConnectivity = false
    configuration.urlCache = nil
    configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
    configuration.httpShouldSetCookies = false
    return configuration
  }
}

extension ServerAnswer {
  /// The answer a session gave: its status and body when it is HTTP.
  static func received(_ body: Data, _ response: URLResponse) -> ServerAnswer {
    guard let response = response as? HTTPURLResponse else { return .noAnswer }
    return .http(status: response.statusCode, body: body)
  }
}
