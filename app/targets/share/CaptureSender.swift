import Foundation

/// Sends the capture to `POST <server>/api/items` with the build-time URL and token (design D6,
/// D7). « Réessayer » hands it the same `CaptureRequest`: the same bytes, from memory for a link
/// or a note, from the same file for an image (RG-08). Nothing is logged (design D14).
@MainActor
final class CaptureSender {
  private var session: URLSession?

  /// The server's answer, or nil when the task was cancelled (« Annuler »).
  func send(_ request: CaptureRequest) async -> ServerAnswer? {
    let session =
      self.session ?? URLSession(configuration: CaptureSession.configuration(for: request.body))
    self.session = session
    let urlRequest = request.urlRequest(server: BuildConfig.serverURL, token: BuildConfig.apiToken)
    do {
      let (body, response) =
        switch request.body {
        case .json: try await session.data(for: urlRequest)
        case .multipart(let file, _): try await session.upload(for: urlRequest, fromFile: file)
        }
      if Task.isCancelled { return nil }
      return .received(body, response)
    } catch {
      if Task.isCancelled { return nil }
      return ServerAnswer.failure(error)
    }
  }

  /// Cancels any request in flight; the sheet is closing.
  func invalidate() {
    session?.invalidateAndCancel()
    session = nil
  }
}
