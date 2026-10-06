import AppKit

struct DisplayMode: Equatable {
    let reduceMotion: Bool
    let reduceTransparency: Bool
    let increaseContrast: Bool
    @MainActor static var current: Self {
        let workspace = NSWorkspace.shared
        #if DEBUG
        func resolve(_ key: String, _ system: Bool) -> Bool {
            ProcessInfo.processInfo.environment[key].map { $0 == "1" } ?? system
        }
        return Self(reduceMotion: resolve("KIOKU_REDUCE_MOTION", workspace.accessibilityDisplayShouldReduceMotion),
                    reduceTransparency: resolve("KIOKU_REDUCE_TRANSPARENCY", workspace.accessibilityDisplayShouldReduceTransparency),
                    increaseContrast: resolve("KIOKU_INCREASE_CONTRAST", workspace.accessibilityDisplayShouldIncreaseContrast))
        #else
        return Self(reduceMotion: workspace.accessibilityDisplayShouldReduceMotion,
                    reduceTransparency: workspace.accessibilityDisplayShouldReduceTransparency,
                    increaseContrast: workspace.accessibilityDisplayShouldIncreaseContrast)
        #endif
    }
    var description: String {
        let material: String
        if reduceTransparency || increaseContrast { material = "solid" }
        else if #available(macOS 26, *) { material = "glass" }
        else { material = "visual-effect" }
        return "motion=\(reduceMotion ? "reduced" : "standard"); transparency=\(reduceTransparency ? "reduced" : "standard"); contrast=\(increaseContrast ? "increased" : "standard"); material=\(material)"
    }
}
final class DisplayWindow: NSWindow {
    var displayMode = DisplayMode.current
    #if DEBUG
    weak var testSearchEditor: NSText?
    var textLayout: (() -> [[String: Any]])?
    private var textLayoutState = ""
    private var layoutGeneration = 0
    private var layoutPublicationPending = false
    func requestTextLayout() {
        guard ProcessInfo.processInfo.environment["KIOKU_TEST_TEXT_LAYOUT"] == "1" else { return }
        textLayoutState = ""
        guard !layoutPublicationPending else { return }
        layoutPublicationPending = true
        // Coalesce model/resize events, then flush AppKit's deferred layout before measuring.
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.layoutPublicationPending = false
            self.layoutIfNeeded()
            self.contentView?.layoutSubtreeIfNeeded()
            self.displayIfNeeded()
            self.captureTextLayout()
        }
    }
    private func captureTextLayout() {
        guard var records = textLayout?(), !records.isEmpty,
              let directory = ProcessInfo.processInfo.environment["KIOKU_TEST_RECORDS"] else { return }
        if records[0]["ready"] as? Bool == false {
            if let data = try? JSONSerialization.data(withJSONObject: records[0], options: [.sortedKeys]) {
                textLayoutState = "; layout-pending=" + String(decoding: data, as: UTF8.self)
            }
            return
        }
        layoutGeneration += 1
        records[0]["generation"] = layoutGeneration
        let url = URL(fileURLWithPath: directory).appendingPathComponent("text-layout-\(layoutGeneration).json")
        do {
            let data = try JSONSerialization.data(withJSONObject: records, options: [.sortedKeys])
            let context = try JSONSerialization.data(withJSONObject: records[0], options: [.sortedKeys])
            try FileManager.default.createDirectory(atPath: directory, withIntermediateDirectories: true)
            try data.write(to: url, options: .atomic)
            textLayoutState = "; text-layout=" + String(decoding: context, as: UTF8.self)
        } catch { textLayoutState = "; text-layout=error" }
        NSAccessibility.post(element: self, notification: .valueChanged)
    }
    #endif
    override func accessibilityValue() -> Any? {
        var value = displayMode.description
        #if DEBUG
        if let backdrop = childWindows?.first(where: { $0.identifier?.rawValue == "test-wallpaper" }),
           backdrop.isVisible, backdrop.frame == frame,
           backdrop.contentView?.bounds.size == frame.size,
           backdrop.contentView?.layer?.sublayers?.first?.frame == NSRect(origin: .zero, size: frame.size) {
            value += "; backdrop=known-gradient"
        }
        let editor = testSearchEditor
        let editorWindow = editor?.window
        let editorKey = editorWindow?.isKeyWindow == true
        let editorResponder = editor != nil && editorWindow?.firstResponder === editor
        value += "; search-focused=\(NSApp.isActive && editorKey && editorResponder)"
        value += "; app-active=\(NSApp.isActive); main-key=\(isKeyWindow); editor-key=\(editorKey); editor-responder=\(editorResponder)"
        value += textLayoutState
        #endif
        return value
    }
}

class PressedButton: NSButton {
    override func mouseDown(with event: NSEvent) {
        wantsLayer = true
        layer?.opacity = 0.65
        defer { layer?.opacity = 1 }
        // AppKit tracks until mouse-up and only then sends the primary action.
        super.mouseDown(with: event)
    }
}
final class ImmediateTableView: NSTableView {
    override func mouseDown(with event: NSEvent) {
        let row = row(at: convert(event.locationInWindow, from: nil))
        if row >= 0 { selectRowIndexes(IndexSet(integer: row), byExtendingSelection: false) }
        super.mouseDown(with: event)
    }
}

// Adjacent toolbar capsules share one glass pass on macOS 26. Older systems
// contain their individual visual-effect surfaces without another translucent layer.
@MainActor func groupedGlass(_ content: NSView) -> NSView {
    let container: NSView
    if #available(macOS 26, *), !DisplayMode.current.reduceTransparency, !DisplayMode.current.increaseContrast {
        let glass = NSGlassEffectContainerView()
        glass.spacing = 8
        glass.contentView = content
        container = glass
    } else {
        container = NSView()
        container.addSubview(content)
    }
    pin(content, to: container)
    return container
}
