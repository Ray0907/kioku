import Foundation

struct Highlight: Decodable, Sendable { let location: Int; let length: Int }
struct SearchHit: Decodable, Sendable {
    let ref: String
    let harness: String
    let project: String
    let age: String
    let role: String
    let snippet: String
    let highlights: [Highlight]?
}
struct SearchPage: Decodable, Sendable {
    let shown: Int
    let total: Int
    let totalSessions: Int?
    let hits: [SearchHit]
    enum CodingKeys: String, CodingKey {
        case shown, total, hits
        case totalSessions = "total_sessions"
    }
}
struct SessionHit: Decodable, Sendable {
    let ref: String
    let harness: String
    let project: String
    let age: String
    let hits: Int
    let bestRef: String
    let best: String
    let highlights: [Highlight]?
    enum CodingKeys: String, CodingKey { case ref, harness, project, age, hits, best, highlights; case bestRef = "best_ref" }
}
struct SessionPage: Decodable, Sendable {
    let total: Int
    let sessions: [SessionHit]
}
struct Conversation: Decodable, Sendable {
    struct Message: Decodable, Sendable {
        let time: String
        let role: String
        let text: String
        let hit: Bool
        let matches: Bool
        let highlights: [Highlight]?
    }
    let harness: String
    let project: String
    let model: String?
    let cwd: String
    let resumeCmd: String
    let resumeArgv: [String]
    let messages: [Message]
    let sessionTotal: Int
    enum CodingKeys: String, CodingKey {
        case harness, project, model, cwd, messages
        case resumeCmd = "resume_cmd", resumeArgv = "resume_argv", sessionTotal = "session_total"
    }
}
struct EngineError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

// Foundation Process isn't Sendable. Its lifecycle/cancellation state is protected
// by one lock; only the worker owns stdout/stderr. No waiting on the main actor.
private final class CLIInvocation: @unchecked Sendable {
    private let process = Process()
    private let lock = NSLock()
    private var cancelled = false
    init(executable: URL, arguments: [String]) {
        process.executableURL = executable
        process.arguments = arguments
        process.environment = ProcessInfo.processInfo.environment
    }
    func cancel() {
        lock.lock()
        cancelled = true
        if process.isRunning { process.terminate() }
        lock.unlock()
        // A hung child must not survive cancellation indefinitely.
        DispatchQueue.global().asyncAfter(deadline: .now() + 0.2) { [self] in
            lock.lock(); defer { lock.unlock() }
            if process.isRunning { kill(process.processIdentifier, SIGKILL) }
        }
    }
    func execute() throws -> Data {
        let errorURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        guard FileManager.default.createFile(atPath: errorURL.path, contents: nil, attributes: [.posixPermissions: 0o600]) else {
            throw EngineError(message: "Cannot create kioku error log. Check temporary-directory permissions.")
        }
        defer { try? FileManager.default.removeItem(at: errorURL) }
        let errorFile = try FileHandle(forWritingTo: errorURL)
        defer { try? errorFile.close() }
        let output = Pipe()
        lock.lock()
        if cancelled { lock.unlock(); throw CancellationError() }
        process.standardOutput = output
        process.standardError = errorFile
        do { try process.run() }
        catch {
            lock.unlock()
            throw EngineError(message: "Cannot start bundled kioku. Rebuild the app or check execution permissions.\n" + error.localizedDescription)
        }
        lock.unlock()
        // Drain stdout during execution; stderr uses a file to avoid pipe deadlocks.
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        lock.lock(); let wasCancelled = cancelled; lock.unlock()
        if wasCancelled { throw CancellationError() }
        guard process.terminationStatus == 0 else {
            let reason = String(data: (try? Data(contentsOf: errorURL)) ?? Data(), encoding: .utf8) ?? ""
            throw EngineError(message: (reason.isEmpty ? "kioku exited \(process.terminationStatus)." : reason)
                              + "\nCheck source paths, then retry indexing.")
        }
        return data
    }
    func value() async throws -> Data {
        try await withTaskCancellationHandler {
            try Task.checkCancellation()
            return try await withCheckedThrowingContinuation { continuation in
                DispatchQueue.global(qos: .userInitiated).async { [self] in
                    do { continuation.resume(returning: try execute()) }
                    catch { continuation.resume(throwing: error) }
                }
            }
        } onCancel: { cancel() }
    }
}

// Reentrant during process execution: cancelled searches never queue behind a
// synchronous wait. Swift knows nothing about SQLite, parsers, or query grammar.
actor Engine {
    private let executable: URL?
    init() {
        #if DEBUG
        if let path = ProcessInfo.processInfo.environment["KIOKU_ENGINE"] {
            executable = URL(fileURLWithPath: path)
            return
        }
        #endif
        executable = Bundle.main.url(forResource: "kioku", withExtension: nil)
    }
    private func run(_ arguments: [String]) async throws -> Data {
        guard let executable else { throw EngineError(message: "Bundled kioku is missing. Rebuild the app.") }
        let data = try await CLIInvocation(executable: executable, arguments: arguments).value()
        try Task.checkCancellation()
        return data
    }
    private func decode<T: Decodable>(_ type: T.Type, data: Data) throws -> T {
        do { return try JSONDecoder().decode(type, from: data) }
        catch { throw EngineError(message: "kioku returned incompatible JSON. Rebuild the app with its matching engine.\n" + error.localizedDescription) }
    }
    func index() async throws { _ = try await run(["index"]) }
    func search(query: String, harness: String, project: String?, sessions: Bool, limit: Int = 200) async throws -> SearchPage {
        var arguments = ["--json", "--limit", String(limit), "--harness", harness]
        if let project { arguments += ["--project", project] }
        if sessions { arguments.append("--sessions") }
        // -- preserves flag-like queries without shell interpretation.
        arguments += ["--", query]
        let data = try await run(arguments)
        if sessions {
            let page = try decode(SessionPage.self, data: data)
            let hits = page.sessions.map { SearchHit(ref: $0.bestRef, harness: $0.harness, project: $0.project,
                age: $0.age, role: "\($0.hits) hits", snippet: $0.best, highlights: $0.highlights) }
            return SearchPage(shown: hits.count, total: page.total, totalSessions: page.total, hits: hits)
        }
        return try decode(SearchPage.self, data: data)
    }
    func show(ref: String, query: String) async throws -> Conversation {
        let data = try await run(["show", ref, "--query", query, "--context", "12", "--limit", "200", "--full", "--json"])
        return try decode(Conversation.self, data: data)
    }
}

func shellQuote(_ value: String) -> String { "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'" }
