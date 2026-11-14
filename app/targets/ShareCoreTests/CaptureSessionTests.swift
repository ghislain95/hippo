import Foundation
import Testing

@testable import ShareCore

/// The session the extension sends with and how it reads an answer (design D6, D7).
struct CaptureSessionTests {
  private let json = RequestBody.json(Data("{}".utf8))
  private let multipart = RequestBody.multipart(
    file: URL(fileURLWithPath: "/tmp/capture.multipart"), boundary: "hippo-x")

  @Test("the session stores nothing and does not wait for a network")
  func ephemeralSession() {
    let configuration = CaptureSession.configuration(for: json)

    #expect(configuration.httpCookieStorage == nil || configuration.httpShouldSetCookies == false)
    #expect(configuration.urlCache == nil)
    #expect(configuration.requestCachePolicy == .reloadIgnoringLocalCacheData)
    #expect(configuration.waitsForConnectivity == false)
  }

  @Test("a server that sends no byte for 10 seconds is told unreachable")
  func requestTimeLimit() {
    #expect(CaptureSession.configuration(for: json).timeoutIntervalForRequest == 10)
    #expect(CaptureSession.configuration(for: multipart).timeoutIntervalForRequest == 10)
  }

  @Test("a text gets 30 seconds in all, an image 330 seconds to upload")
  func resourceTimeLimit() {
    #expect(CaptureSession.configuration(for: json).timeoutIntervalForResource == 30)
    #expect(CaptureSession.configuration(for: multipart).timeoutIntervalForResource == 330)
  }

  @Test("an HTTP answer keeps its status and its body")
  func httpAnswer() throws {
    let url = try #require(URL(string: "https://hippo.example.ts.net/api/items"))
    let response = try #require(
      HTTPURLResponse(url: url, statusCode: 409, httpVersion: nil, headerFields: nil))
    let body = Data(#"{"detail":"x"}"#.utf8)

    #expect(ServerAnswer.received(body, response) == .http(status: 409, body: body))
  }

  @Test("an answer that is not HTTP counts as no answer")
  func notHTTP() throws {
    let url = try #require(URL(string: "https://hippo.example.ts.net/api/items"))
    let response = URLResponse(
      url: url, mimeType: nil, expectedContentLength: 0, textEncodingName: nil)

    #expect(ServerAnswer.received(Data(), response) == .noAnswer)
  }

  @Test("a task cancelled by « Annuler » gives no answer to show")
  func cancelled() {
    #expect(ServerAnswer.failure(CancellationError()) == nil)
    #expect(ServerAnswer.failure(URLError(.cancelled)) == nil)
    #expect(ServerAnswer.failure(URLError(.cannotFindHost)) == .noAnswer)
  }
}
