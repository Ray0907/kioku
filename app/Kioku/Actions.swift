import AppKit
import UniformTypeIdentifiers

enum OpenWithKind: String, Sendable { case terminal, editor
    var preferenceKey: String { self == .terminal ? "preferredTerminal" : "preferredEditor" }
}
struct KnownApp: Sendable {
    enum Recipe: Sendable { case commandFile, ghostty, directory }
    let kind: OpenWithKind
    let name: String
    let bundleID: String
    let recipe: Recipe
    static let all = [
        KnownApp(kind: .terminal, name: "Terminal", bundleID: "com.apple.Terminal", recipe: .commandFile),
        KnownApp(kind: .terminal, name: "Ghostty", bundleID: "com.mitchellh.ghostty", recipe: .ghostty),
        KnownApp(kind: .terminal, name: "iTerm2", bundleID: "com.googlecode.iterm2", recipe: .commandFile),
        KnownApp(kind: .terminal, name: "WezTerm", bundleID: "com.github.wez.wezterm", recipe: .commandFile),
        KnownApp(kind: .terminal, name: "Kitty", bundleID: "net.kovidgoyal.kitty", recipe: .commandFile),
        KnownApp(kind: .terminal, name: "Alacritty", bundleID: "org.alacritty", recipe: .commandFile),
        KnownApp(kind: .terminal, name: "Warp", bundleID: "dev.warp.Warp-Stable", recipe: .commandFile),
        KnownApp(kind: .editor, name: "Zed", bundleID: "dev.zed.Zed", recipe: .directory),
        KnownApp(kind: .editor, name: "Cursor", bundleID: "com.todesktop.230313mzl4w4u92", recipe: .directory),
        KnownApp(kind: .editor, name: "Visual Studio Code", bundleID: "com.microsoft.VSCode", recipe: .directory),
        KnownApp(kind: .editor, name: "Sublime Text", bundleID: "com.sublimetext.4", recipe: .directory),
        KnownApp(kind: .editor, name: "Sublime Text", bundleID: "com.sublimetext.3", recipe: .directory),
        KnownApp(kind: .editor, name: "Kiro", bundleID: "dev.kiro.desktop", recipe: .directory)
    ]
}
struct AppChoice: Sendable {
    let url: URL
    let bundleID: String
    let name: String
    init(_ url: URL) {
        self.url = url
        bundleID = Bundle(url: url)?.bundleIdentifier ?? url.path
        name = FileManager.default.displayName(atPath: url.path).replacingOccurrences(of: ".app", with: "")
    }
}
@MainActor enum OpenWith {
    static func systemDefault(_ kind: OpenWithKind) -> URL? {
        #if DEBUG
        let key = kind == .terminal ? "KIOKU_SYSTEM_TERMINAL" : "KIOKU_SYSTEM_EDITOR"
        if let injected = ProcessInfo.processInfo.environment[key] {
            return FileManager.default.fileExists(atPath: injected) ? URL(fileURLWithPath: injected) : nil
        }
        #endif
        let workspace = NSWorkspace.shared
        return kind == .terminal ? workspace.urlForApplication(toOpen: .unixExecutable)
            : workspace.urlForApplication(toOpen: .sourceCode) ?? workspace.urlForApplication(toOpen: .plainText)
    }
    static func choices(_ kind: OpenWithKind) -> [AppChoice] {
        let workspace = NSWorkspace.shared
        let types: [UTType] = kind == .terminal ? [.unixExecutable] : [.sourceCode, .plainText]
        var urls = types.flatMap { workspace.urlsForApplications(toOpen: $0) }
        urls += KnownApp.all.filter { $0.kind == kind }.compactMap { workspace.urlForApplication(withBundleIdentifier: $0.bundleID) }
        if let path = UserDefaults.standard.string(forKey: kind.preferenceKey + "Path"),
           let id = UserDefaults.standard.string(forKey: kind.preferenceKey),
           FileManager.default.fileExists(atPath: path), Bundle(path: path)?.bundleIdentifier == id {
            urls.append(URL(fileURLWithPath: path))
        }
        let first = systemDefault(kind).map(AppChoice.init)
        var seen = Set<String>()
        if let first { seen.insert(first.bundleID) }
        let others = urls.map(AppChoice.init).filter { seen.insert($0.bundleID).inserted }
            .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
        return (first.map { [$0] } ?? []) + others
    }
    static func resolve(_ kind: OpenWithKind) -> (AppChoice, Bool)? {
        let saved = UserDefaults.standard.string(forKey: kind.preferenceKey) ?? ""
        if !saved.isEmpty {
            if let app = NSWorkspace.shared.urlForApplication(withBundleIdentifier: saved) { return (AppChoice(app), false) }
            if let path = UserDefaults.standard.string(forKey: kind.preferenceKey + "Path"),
               FileManager.default.fileExists(atPath: path), Bundle(path: path)?.bundleIdentifier == saved {
                return (AppChoice(URL(fileURLWithPath: path)), false)
            }
            return fallback(kind).map { ($0, true) }
        }
        return systemDefault(kind).map { (AppChoice($0), false) } ?? fallback(kind).map { ($0, true) }
    }
    private static func fallback(_ kind: OpenWithKind) -> AppChoice? {
        if kind == .terminal {
            return AppChoice(NSWorkspace.shared.urlForApplication(withBundleIdentifier: "com.apple.Terminal")
                ?? URL(fileURLWithPath: "/System/Applications/Utilities/Terminal.app"))
        }
        return choices(kind).first
    }
}

enum SessionActions {
    static func command(_ conversation: Conversation) -> String { conversation.resumeArgv.map(shellQuote).joined(separator: " ") }
    static func launch(_ executable: URL, arguments: [String], cwd: String) throws {
        let process = Process()
        process.executableURL = executable; process.arguments = arguments
        process.currentDirectoryURL = URL(fileURLWithPath: cwd, isDirectory: true)
        let output = Pipe()
        process.standardOutput = output; process.standardError = output
        try process.run()
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else { throw EngineError(message: String(data: data, encoding: .utf8) ?? "Launcher failed") }
    }
    @MainActor static func resume(_ conversation: Conversation) async throws -> Bool {
        guard let (terminal, fallback) = OpenWith.resolve(.terminal) else { throw EngineError(message: "No terminal available") }
        #if DEBUG
        let launcher = URL(fileURLWithPath: ProcessInfo.processInfo.environment["KIOKU_TERMINAL_LAUNCHER"] ?? "/usr/bin/open")
        #else
        let launcher = URL(fileURLWithPath: "/usr/bin/open")
        #endif
        let recipe = KnownApp.all.first { $0.bundleID == terminal.bundleID }?.recipe ?? .commandFile
        let arguments: [String]
        var script: URL?
        if recipe == .ghostty {
            // https://ghostty.org/docs/config/reference#initial-command:
            // -e consumes separate arguments and disables shell expansion.
            var argv = conversation.resumeArgv
            if conversation.harness == "grok" { argv = ["/bin/zsh", "-l"] }
            arguments = ["-na", terminal.url.path, "--args", "--working-directory=\(conversation.cwd)", "-e"] + argv
        } else {
            let url = FileManager.default.temporaryDirectory.appendingPathComponent("kioku-\(UUID().uuidString).command")
            let path = ProcessInfo.processInfo.environment["PATH"] ?? "/usr/bin:/bin"
            let invocation = conversation.harness == "grok" ? "exec /bin/zsh -l" : "exec " + command(conversation)
            let source = "#!/bin/zsh\nrm -- \"$0\"\nexport PATH=" + shellQuote(path) + ":\"$PATH\"\ncd -- " + shellQuote(conversation.cwd) + " || exit 1\n" + invocation + "\n"
            try source.write(to: url, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: url.path)
            script = url
            arguments = ["-a", terminal.url.path, url.path]
        }
        let cwd = conversation.cwd
        do { try await Task.detached { try launch(launcher, arguments: arguments, cwd: cwd) }.value }
        catch { if let script { try? FileManager.default.removeItem(at: script) }; throw error }
        return fallback
    }
    @MainActor static func openEditor(_ cwd: String) async throws {
        #if DEBUG
        if let stub = ProcessInfo.processInfo.environment["KIOKU_EDITOR"] {
            try await Task.detached { try launch(URL(fileURLWithPath: stub), arguments: [cwd], cwd: cwd) }.value
            return
        }
        #endif
        guard let (editor, _) = OpenWith.resolve(.editor) else { throw EngineError(message: "No editor available; choose Other…") }
        #if DEBUG
        let executable = URL(fileURLWithPath: ProcessInfo.processInfo.environment["KIOKU_EDITOR_LAUNCHER"] ?? "/usr/bin/open")
        #else
        let executable = URL(fileURLWithPath: "/usr/bin/open")
        #endif
        let arguments = ["-a", editor.url.path, cwd]
        try await Task.detached { try launch(executable, arguments: arguments, cwd: cwd) }.value
    }
}
