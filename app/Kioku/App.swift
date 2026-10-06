import AppKit
import QuartzCore

@main
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var controller: MainWindowController?
    static func main() {
        let app = NSApplication.shared
        let delegate = AppDelegate()
        app.delegate = delegate
        app.setActivationPolicy(.regular)
        app.run()
        withExtendedLifetime(delegate) {}
    }
    func applicationDidFinishLaunching(_ notification: Notification) {
        let controller = MainWindowController()
        self.controller = controller
        controller.showWindow(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
    func applicationWillTerminate(_ notification: Notification) { controller?.cancelOperations() }
}

// Reading panes use window material; glass is reserved for floating controls.
final class GlassSurface: NSView {
    let content: NSView
    private let material: NSVisualEffectView.Material
    private let prominent: Bool
    private var observer: NSObjectProtocol?
    init(content: NSView, material: NSVisualEffectView.Material = .headerView, prominent: Bool = false) {
        self.content = content; self.material = material; self.prominent = prominent
        super.init(frame: .zero)
        rebuild()
        observer = NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.accessibilityDisplayOptionsDidChangeNotification,
                                                                      object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.rebuild() }
        }
    }
    required init?(coder: NSCoder) { fatalError("Programmatic UI") }
    override func viewDidChangeEffectiveAppearance() { super.viewDidChangeEffectiveAppearance(); rebuild() }
    isolated deinit { if let observer { NSWorkspace.shared.notificationCenter.removeObserver(observer) } }
    private func rebuild() {
        content.removeFromSuperview()
        subviews.forEach { $0.removeFromSuperview() }
        let surface: NSView
        let mode = DisplayMode.current
        if mode.reduceTransparency || mode.increaseContrast {
            surface = NSView()
            surface.wantsLayer = true
            surface.layer?.cornerRadius = 18
            effectiveAppearance.performAsCurrentDrawingAppearance {
                surface.layer?.backgroundColor = (prominent ? NSColor.systemRed : NSColor.windowBackgroundColor).cgColor
                surface.layer?.borderWidth = mode.increaseContrast ? 1.5 : 1
                surface.layer?.borderColor = (mode.increaseContrast ? NSColor.labelColor.withAlphaComponent(0.5) : NSColor.separatorColor).cgColor
            }
            surface.addSubview(content)
        } else if #available(macOS 26, *) {
            let glass = NSGlassEffectView()
            glass.style = .regular
            glass.cornerRadius = 18
            if prominent { glass.tintColor = .systemRed }
            let padding = NSView()
            padding.addSubview(content)
            glass.contentView = padding
            pin(padding, to: glass)
            surface = groupedGlass(glass)
            addSubview(surface)
            pin(surface, to: self)
            constrainContent(to: padding)
            return
        } else {
            let effect = NSVisualEffectView()
            effect.material = material
            effect.blendingMode = .behindWindow
            effect.state = .active
            effect.wantsLayer = true
            effect.layer?.cornerRadius = 18
            effect.layer?.masksToBounds = true
            effect.addSubview(content)
            surface = effect
        }
        addSubview(surface)
        pin(surface, to: self)
        constrainContent(to: surface)
    }
    private func constrainContent(to surface: NSView) {
        content.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            content.leadingAnchor.constraint(equalTo: surface.leadingAnchor, constant: 14),
            content.trailingAnchor.constraint(equalTo: surface.trailingAnchor, constant: -14),
            content.topAnchor.constraint(equalTo: surface.topAnchor, constant: 8),
            content.bottomAnchor.constraint(equalTo: surface.bottomAnchor, constant: -8)
        ])
    }
}

final class PaneMaterial: NSVisualEffectView {
    private let solidBackground = NSView()
    override init(frame: NSRect) {
        super.init(frame: frame)
        solidBackground.wantsLayer = true
        addSubview(solidBackground)
        pin(solidBackground, to: self)
        material = .underWindowBackground
        blendingMode = .behindWindow
        state = .active
        setAccessibilityElement(true)
        setAccessibilityRole(.group)
        updateMaterial()
    }
    required init?(coder: NSCoder) { fatalError("Programmatic UI") }
    func updateMaterial() {
        let solid = DisplayMode.current.reduceTransparency || DisplayMode.current.increaseContrast
        solidBackground.isHidden = !solid
        effectiveAppearance.performAsCurrentDrawingAppearance {
            solidBackground.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
        }
    }
    override func viewDidChangeEffectiveAppearance() { super.viewDidChangeEffectiveAppearance(); updateMaterial() }
}

final class ConversationDocument: NSView {
    var transcript = ""
    #if DEBUG
    var conversationState = ""
    #endif
    override var isFlipped: Bool { true }
    override func accessibilityValue() -> Any? {
        #if DEBUG
        return transcript + "\nconversation-state=" + conversationState
        #else
        return transcript
        #endif
    }
}

@MainActor func pin(_ view: NSView, to parent: NSView) {
    view.translatesAutoresizingMaskIntoConstraints = false
    NSLayoutConstraint.activate([view.leadingAnchor.constraint(equalTo: parent.leadingAnchor),
        view.trailingAnchor.constraint(equalTo: parent.trailingAnchor), view.topAnchor.constraint(equalTo: parent.topAnchor),
        view.bottomAnchor.constraint(equalTo: parent.bottomAnchor)])
}
@MainActor func label(_ text: String, size: CGFloat = 13, weight: NSFont.Weight = .regular) -> NSTextField {
    let label = NSTextField(labelWithString: text)
    label.font = NSFont.systemFont(ofSize: size, weight: weight)
    label.setContentCompressionResistancePriority(.required, for: .vertical)
    return label
}
@MainActor func highlighted(_ text: String, ranges: [Highlight]?, font: NSFont) -> NSAttributedString {
    let result = NSMutableAttributedString(string: text, attributes: [.font: font, .foregroundColor: NSColor.labelColor])
    for item in ranges ?? [] where item.location >= 0 && item.length > 0 && item.location <= result.length - item.length {
        result.addAttribute(.backgroundColor, value: NSColor.systemYellow.withAlphaComponent(0.5),
                            range: NSRange(location: item.location, length: item.length))
    }
    return result
}
@MainActor func agentColor(_ harness: String) -> NSColor {
    switch harness {
    case "claude": .systemOrange
    case "codex": .systemBlue
    case "pi": .systemPurple
    case "grok": .systemTeal
    case "opencode": .systemGreen
    case "cursor": .systemPink
    default: .secondaryLabelColor
    }
}
@MainActor func transitionAnimation(key: String, from: Any, to: Any, reduced: Bool) -> CABasicAnimation {
    let animation: CABasicAnimation
    if reduced {
        animation = CABasicAnimation(keyPath: key)
        animation.duration = 0.12 // Reduced Motion: cross-fade only.
    } else {
        let spring = CASpringAnimation(perceptualDuration: 0.35, bounce: 0)
        spring.keyPath = key
        spring.duration = spring.settlingDuration
        animation = spring
    }
    animation.fromValue = from; animation.toValue = to
    return animation
}

final class FilterChip: PressedButton {
    override var intrinsicContentSize: NSSize {
        NSSize(width: super.intrinsicContentSize.width + 24, height: 32)
    }
    func select(_ selected: Bool) {
        wantsLayer = true
        layer?.cornerRadius = 14
        layer?.backgroundColor = selected ? NSColor.systemRed.withAlphaComponent(0.18).cgColor : nil
        font = .systemFont(ofSize: 11, weight: selected ? .semibold : .regular)
        setAccessibilityValue(selected ? 1 : 0)
    }
}

final class ResultCell: NSTableCellView {
    let metadata = label("")
    let age = label("", size: 11)
    let snippet = ReadingText(lines: 2)
    let role = label("", size: 11, weight: .medium)
    let resume = PressedButton(title: "Resume ⏎", target: nil, action: nil)
    let dot = label("●", size: 11)
    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        age.textColor = .secondaryLabelColor
        age.font = .monospacedDigitSystemFont(ofSize: NSFont.preferredFont(forTextStyle: .caption1, options: [:]).pointSize, weight: .regular)
        role.textColor = .secondaryLabelColor
        snippet.setAccessibilityIdentifier("result-snippet")
        resume.isBordered = false
        resume.font = .systemFont(ofSize: 11, weight: .semibold)
        resume.contentTintColor = .white
        resume.wantsLayer = true
        resume.layer?.backgroundColor = NSColor.systemRed.cgColor
        resume.layer?.cornerRadius = 10
        resume.widthAnchor.constraint(equalToConstant: 86).isActive = true
        resume.heightAnchor.constraint(equalToConstant: 30).isActive = true
        let heading = NSStackView(views: [metadata, NSView(), age])
        heading.orientation = .horizontal; heading.spacing = 8
        metadata.lineBreakMode = .byTruncatingTail
        addSubview(dot)
        dot.translatesAutoresizingMaskIntoConstraints = false
        dot.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 12).isActive = true
        dot.widthAnchor.constraint(equalToConstant: 10).isActive = true
        let footer = NSStackView(views: [role, NSView(), resume])
        footer.orientation = .horizontal; footer.alignment = .top
        let stack = NSStackView(views: [heading, snippet, footer])
        stack.orientation = .vertical; stack.alignment = .leading; stack.spacing = 2
        addSubview(stack)
        stack.translatesAutoresizingMaskIntoConstraints = false
        for row in [heading, footer] { row.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true }
        NSLayoutConstraint.activate([stack.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 32),
            stack.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -12),
            stack.topAnchor.constraint(equalTo: topAnchor, constant: 8),
            stack.bottomAnchor.constraint(lessThanOrEqualTo: bottomAnchor, constant: -2),
            snippet.widthAnchor.constraint(equalTo: stack.widthAnchor),
            dot.centerYAnchor.constraint(equalTo: metadata.centerYAnchor)])
    }
    required init?(coder: NSCoder) { fatalError("Programmatic UI") }
    func configure(_ hit: SearchHit, selected: Bool, target: AnyObject) {
        let title = NSMutableAttributedString(string: hit.project, attributes: [.font: NSFont.systemFont(ofSize: 13, weight: .semibold), .foregroundColor: NSColor.labelColor])
        title.append(NSAttributedString(string: "  " + hit.harness, attributes: [.font: NSFont.systemFont(ofSize: 11), .foregroundColor: NSColor.secondaryLabelColor]))
        metadata.attributedStringValue = title
        age.stringValue = hit.age
        dot.textColor = agentColor(hit.harness)
        snippet.setText(highlighted(hit.snippet, ranges: hit.highlights, font: .systemFont(ofSize: 12)))
        role.stringValue = hit.role == "asst" ? "ASSISTANT" : hit.role == "user" ? "YOU" : hit.role.uppercased()
        role.font = .systemFont(ofSize: 9, weight: .medium)
        role.setAccessibilityIdentifier("result-role")
        resume.isHidden = !selected
        resume.target = target; resume.action = #selector(MainWindowController.resumeSelected)
        resume.setAccessibilityIdentifier("inline-resume")
        setAccessibilityIdentifier("result-\(hit.ref)")
        setAccessibilityLabel("\(hit.harness) · \(hit.project) · \(hit.role) · \(hit.snippet)")
    }
}

final class SelectionRow: NSTableRowView {
    override func drawSelection(in dirtyRect: NSRect) {
        let contrast = DisplayMode.current.increaseContrast
        NSColor.systemRed.withAlphaComponent(contrast ? 0.22 : 0.10).setFill()
        let shape = NSBezierPath(roundedRect: bounds.insetBy(dx: 6, dy: 2), xRadius: 10, yRadius: 10)
        shape.fill()
        if contrast { NSColor.labelColor.setStroke(); shape.lineWidth = 1.5; shape.stroke() }
    }
}

final class MainWindowController: NSWindowController, NSWindowDelegate, NSToolbarDelegate, NSSearchFieldDelegate,
                                  NSTableViewDataSource, NSTableViewDelegate {
    private let engine = Engine()
    private let split = NSSplitViewController()
    private let results = NSViewController()
    private let conversationPane = NSViewController()
    private let searchItem = NSSearchToolbarItem(itemIdentifier: .init("search"))
    private let table = ImmediateTableView()
    private let stateLabel = label("Scanning your local sessions…", size: 15)
    private let retry = PressedButton(title: "Retry indexing", target: nil, action: nil)
    private let conversationScroll = NSScrollView()
    private let conversationDocument = ConversationDocument()
    private let messageStack = NSStackView()
    private let filterScroll = NSScrollView()
    private let summary = label("Indexing sessions…", size: 12)
    private let queryTitle = label("Kioku", size: 14, weight: .semibold)
    private let queryCount = label("", size: 11)
    private var titleWidth: NSLayoutConstraint?
    private let conversationTitle = label("Choose a message", size: 11, weight: .semibold)
    private let conversationModel = label("", size: 11)
    private let conversationDetail = label("Search your local coding-agent history", size: 12)
    private let hitPosition = label("No hit selected", size: 12, weight: .medium)
    private let resumeButton = PressedButton(title: "Resume ⏎", target: nil, action: nil)
    private let project = NSPopUpButton()
    private let mode = NSPopUpButton()
    private var filterButtons: [String: NSButton] = [:]
    private var filterBar: NSStackView?
    private var chromeBackgrounds: [PaneMaterial] = []
    #if DEBUG
    private var testBackdrop: NSWindow?
    private var completedSearchGeneration = -1
    private var renderedSelectionGeneration = -1
    #endif
    private let harnesses = [("all", "All"), ("claude", "Claude"), ("codex", "Codex"), ("pi", "Pi"), ("grok", "Grok"), ("opencode", "OpenCode"), ("cursor", "Cursor")]
    private var harness = "all"
    private var hits: [SearchHit] = []
    private var conversation: Conversation?
    private var expanded = Set<Int>()
    private var resultTotal = 0
    private var startupTask: Task<Void, Never>?
    private var searchTask: Task<Void, Never>?
    private var sessionCount = 0
    private var loadedRef: String?
    private var renderedRow = 0
    private var renderedQuery = ""
    private var exitLayer: CALayer?
    private var displayObserver: NSObjectProtocol?
    private var showTask: Task<Void, Never>?
    private var generation = 0
    private var selectionGeneration = 0
    private var keyMonitor: Any?
    private var settingsWindow: NSWindow?
    private var choiceItems: [OpenWithKind: NSPopUpButton] = [:]
    private var choicePopups: [OpenWithKind: NSPopUpButton] = [:]
    init() {
        let window = DisplayWindow(contentRect: NSRect(x: 0, y: 0, width: 1320, height: 820),
                              styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                              backing: .buffered, defer: false)
        window.title = "Kioku"
        window.titlebarAppearsTransparent = true
        window.titlebarSeparatorStyle = .shadow
        window.titleVisibility = .hidden
        window.minSize = NSSize(width: 800, height: 680)
        window.isOpaque = false
        window.backgroundColor = .clear
        #if DEBUG
        if let scheme = ProcessInfo.processInfo.environment["KIOKU_APPEARANCE"] {
            window.appearance = NSAppearance(named: scheme == "light" ? .aqua : .darkAqua)
        }
        #endif
        window.center()
        super.init(window: window)
        results.view = PaneMaterial(); conversationPane.view = PaneMaterial()
        results.view.setAccessibilityIdentifier("results-pane")
        conversationPane.view.setAccessibilityIdentifier("conversation-pane")
        let left = NSSplitViewItem(viewController: results)
        left.minimumThickness = 340; left.maximumThickness = 560
        left.preferredThicknessFraction = 0.40
        let right = NSSplitViewItem(viewController: conversationPane)
        right.minimumThickness = 420
        split.addSplitViewItem(left); split.addSplitViewItem(right)
        window.contentViewController = split
        makeResults()
        makeConversation()
        let toolbar = NSToolbar(identifier: "kioku-toolbar")
        toolbar.delegate = self
        toolbar.allowsUserCustomization = false
        toolbar.displayMode = .iconOnly
        window.toolbar = toolbar
        window.toolbarStyle = .unified
        searchItem.searchField.placeholderString = "Search sessions"
        searchItem.searchField.delegate = self
        searchItem.searchField.setAccessibilityIdentifier("search")
        searchItem.searchField.sendsSearchStringImmediately = true
        window.initialFirstResponder = searchItem.searchField
        makeMenu()
        keyMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let self, event.window == self.window,
                  event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return event }
            switch event.keyCode {
            case 36: self.resumeSelected()
            case 125: self.selectResult(delta: 1)
            case 126: self.selectResult(delta: -1)
            case 53:
                if self.searchItem.searchField.stringValue.isEmpty { self.window?.performClose(nil) }
                else { self.searchItem.searchField.stringValue = ""; self.refresh() }
            default: return event
            }
            return nil
        }
        displayObserver = NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.accessibilityDisplayOptionsDidChangeNotification,
                                                                           object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.applyDisplayMode() }
        }
        applyDisplayMode()
        window.setContentSize(NSSize(width: 1100, height: 740))
        split.splitView.setPosition(440, ofDividerAt: 0)
        window.center()
        #if DEBUG
        window.textLayout = { [weak self] in self?.textLayoutRecords() ?? [] }
        #endif
        window.delegate = self
        #if DEBUG
        if ProcessInfo.processInfo.environment["KIOKU_TEST_BACKDROP"] == "1",
           ProcessInfo.processInfo.environment["KIOKU_TEST_HOME"] != nil {
            // A same-app child stays immediately behind Kioku even when the runner
            // loses activation. Its gradient scales with the tested window, not the desktop.
            let backdrop = NSWindow(contentRect: window.frame, styleMask: .borderless, backing: .buffered, defer: false)
            backdrop.identifier = .init("test-wallpaper")
            backdrop.isReleasedWhenClosed = false
            backdrop.ignoresMouseEvents = true
            backdrop.setAccessibilityElement(false)
            let gradient = CAGradientLayer()
            gradient.frame = backdrop.contentView!.bounds
            gradient.autoresizingMask = [.layerWidthSizable, .layerHeightSizable]
            gradient.colors = [NSColor(srgbRed: 0, green: 0.8, blue: 1, alpha: 1).cgColor,
                               NSColor(srgbRed: 1, green: 0, blue: 0.8, alpha: 1).cgColor]
            gradient.startPoint = CGPoint(x: 0, y: 0.5)
            gradient.endPoint = CGPoint(x: 1, y: 0.5)
            backdrop.contentView!.wantsLayer = true
            backdrop.contentView!.layer?.addSublayer(gradient)
            testBackdrop = backdrop
            window.addChildWindow(backdrop, ordered: .below)
            backdrop.order(.below, relativeTo: window.windowNumber)
        }
        #endif
        startIndexing()
    }
    override func showWindow(_ sender: Any?) {
        super.showWindow(sender)
        #if DEBUG
        // Apply fixture dimensions after AppKit installs/restores the visible toolbar,
        // not during construction where showing the window can replace the frame.
        if let window, ProcessInfo.processInfo.environment["KIOKU_TEST_HOME"] != nil,
           let raw = ProcessInfo.processInfo.environment["KIOKU_TEST_WINDOW_WIDTH"],
           let width = Double(raw), width.isFinite, (800...1400).contains(width) {
            titleWidth?.constant = min(198, max(94, width - 706))
            window.layoutIfNeeded()
            var frame = window.frame
            frame.size = NSSize(width: width, height: 800)
            window.setFrame(frame, display: true)
            split.splitView.setPosition(min(width - 421, max(378, min(520, width * 0.42))), ofDividerAt: 0)
            window.setFrameTopLeftPoint(NSPoint(x: 100, y: (NSScreen.main?.visibleFrame.maxY ?? 1050) - 80))
            (window as? DisplayWindow)?.requestTextLayout()
        }
        #endif
    }
    required init?(coder: NSCoder) { fatalError("Programmatic UI") }
    isolated deinit {
        startupTask?.cancel(); searchTask?.cancel(); showTask?.cancel()
        if let keyMonitor { NSEvent.removeMonitor(keyMonitor) }
        if let displayObserver { NSWorkspace.shared.notificationCenter.removeObserver(displayObserver) }
    }
    func cancelOperations() { startupTask?.cancel(); searchTask?.cancel(); showTask?.cancel() }
    @objc private func startIndexing() {
        cancelOperations(); generation += 1; selectionGeneration += 1
        #if DEBUG
        publishConversationState()
        #endif
        summary.stringValue = "Indexing sessions…"
        if hits.isEmpty { stateLabel.stringValue = "Indexing local sessions…"; stateLabel.isHidden = false }
        retry.isHidden = true
        startupTask = Task {
            do {
                try await engine.index()
                let projects = try await engine.search(query: "", harness: "all", project: nil, sessions: true)
                sessionCount = projects.total
                project.removeAllItems(); project.addItem(withTitle: "All projects")
                for name in Set(projects.hits.map(\.project)).sorted() where !name.isEmpty { project.addItem(withTitle: name) }
                refresh()
            } catch is CancellationError { }
            catch {
                summary.stringValue = "Indexing failed"
                stateLabel.stringValue = error.localizedDescription
                stateLabel.isHidden = false; retry.isHidden = false
            }
        }
    }
    private var selectedConversation: Conversation? {
        guard hits.indices.contains(table.selectedRow), loadedRef == hits[table.selectedRow].ref else { return nil }
        return conversation
    }
    private func applyDisplayMode() {
        let mode = DisplayMode.current
        (window as? DisplayWindow)?.displayMode = mode
        let solid = mode.reduceTransparency || mode.increaseContrast
        window?.isOpaque = solid
        window?.backgroundColor = solid ? .windowBackgroundColor : .clear
        NSAccessibility.post(element: window!, notification: .valueChanged)
        exitLayer?.removeFromSuperlayer(); exitLayer = nil
        conversationDocument.layer?.removeAllAnimations()
        conversationDocument.layer?.opacity = 1; conversationDocument.layer?.transform = CATransform3DIdentity
        table.enumerateAvailableRowViews { row, _ in row.needsDisplay = true }
        (results.view as? PaneMaterial)?.updateMaterial()
        (conversationPane.view as? PaneMaterial)?.updateMaterial()
        chromeBackgrounds.forEach { $0.updateMaterial() }
    }

    func windowDidResize(_ notification: Notification) {
        titleWidth?.constant = min(198, max(94, (window?.frame.width ?? 1100) - 706))
        #if DEBUG
        (window as? DisplayWindow)?.requestTextLayout()
        if let window, let backdrop = window.childWindows?.first(where: { $0.identifier?.rawValue == "test-wallpaper" }) {
            backdrop.setFrame(window.frame, display: true)
            // Resize the opaque canvas too, before AppKit's deferred content layout.
            backdrop.contentView?.setFrameSize(window.frame.size)
            backdrop.contentView?.layer?.sublayers?.first?.frame = NSRect(origin: .zero, size: window.frame.size)
        }
        #endif
    }
    #if DEBUG
    private func publishConversationState() {
        let ready = completedSearchGeneration == generation && renderedSelectionGeneration == selectionGeneration &&
            selectedConversation != nil && renderedQuery == searchItem.searchField.stringValue
        let context: [String: Any] = ["query": searchItem.searchField.stringValue, "generation": selectionGeneration,
                                     "searchGeneration": generation, "selectedRow": table.selectedRow, "ready": ready]
        if let data = try? JSONSerialization.data(withJSONObject: context, options: [.sortedKeys]) {
            conversationDocument.conversationState = String(decoding: data, as: UTF8.self)
            NSAccessibility.post(element: conversationDocument, notification: .valueChanged)
        }
        (window as? DisplayWindow)?.requestTextLayout()
    }
    private func textLayoutRecords() -> [[String: Any]] {
        guard let window else { return [] }
        let context: [String: Any] = ["id": "layout-context", "query": searchItem.searchField.stringValue, "width": window.frame.width]
        guard completedSearchGeneration == generation,
              hits.isEmpty || (renderedSelectionGeneration == selectionGeneration && selectedConversation != nil && renderedQuery == searchItem.searchField.stringValue) else {
            return [context.merging(["ready": false, "searchGeneration": generation, "completedSearchGeneration": completedSearchGeneration,
                                     "renderedQuery": renderedQuery, "selectedRow": table.selectedRow, "loadedRef": loadedRef ?? "nil",
                                     "selectedRef": hits.indices.contains(table.selectedRow) ? hits[table.selectedRow].ref : "nil",
                                     "summary": summary.stringValue]) { _, new in new }]
        }
        var records: [[String: Any]] = [context]
        func rect(_ r: NSRect) -> [Double] { [r.minX, r.minY, r.width, r.height].map(Double.init) }
        func record(_ view: NSView, in container: NSView, sides: CGFloat = 8) {
            guard !view.isHiddenOrHasHiddenAncestor else { return }
            guard let drawn = textMeasurement(view), !drawn.text.isEmpty else { return }
            let ink = view.convert(drawn.rect, to: nil)
            let bounds = container.convert(container.bounds, to: nil)
            let full = (view as? ReadingText)?.string ?? (view as? NSButton)?.title ?? (view as? NSTextField)?.stringValue ?? ""
            records.append(["id": view.accessibilityIdentifier() ?? String(describing: type(of: view)),
                            "ink": rect(ink), "container": rect(bounds), "sides": sides,
                            "rendered": drawn.text, "full": full, "truncated": drawn.truncated,
                            "lines": drawn.lines, "maxLines": (view as? ReadingText)?.textContainer.maximumNumberOfLines ?? ((view as? NSTextField)?.cell?.wraps == true ? 0 : 1)])
        }
        func glass(_ view: NSView) -> NSView {
            var parent = view.superview
            while let next = parent { if next is GlassSurface { return next }; parent = next.superview }
            return view
        }
        record(queryTitle, in: glass(queryTitle), sides: 12)
        record(queryCount, in: glass(queryCount), sides: 12)
        for button in filterButtons.values { record(button, in: button, sides: 12) }
        record(resumeButton, in: glass(resumeButton), sides: 12)
        record(hitPosition, in: glass(hitPosition), sides: 12)
        record(project, in: project); record(mode, in: mode)
        record(summary, in: results.view, sides: 12)
        record(conversationTitle, in: conversationTitle, sides: 12)
        record(conversationModel, in: conversationPane.view, sides: 12)
        record(conversationDetail, in: conversationPane.view, sides: 12)
        for row in 0..<table.numberOfRows {
            guard let cell = table.view(atColumn: 0, row: row, makeIfNecessary: false) as? ResultCell else { continue }
            for text in [cell.metadata, cell.age, cell.role, cell.dot] { record(text, in: cell) }
            record(cell.snippet, in: cell, sides: 12)
            record(cell.snippet, in: results.view, sides: 12)
            record(cell.resume, in: cell.resume)
            let ink = cell.snippet.convert(cell.snippet.drawnText.rect, to: nil)
            if let role = textMeasurement(cell.role) {
                let r = cell.role.convert(role.rect, to: nil)
                records.append(["id": "snippet-role-gap", "gap": min(abs(ink.minY - r.maxY), abs(r.minY - ink.maxY))])
            }
        }
        for block in messageStack.arrangedSubviews {
            if let fold = block as? NSButton { record(fold, in: fold); continue }
            func walk(_ view: NSView) {
                if view is NSTextField || view is ReadingText { record(view, in: block, sides: 12) }
                view.subviews.forEach(walk)
            }
            walk(block)
        }
        return records
    }
    #endif
    private func toolbarActions() -> NSView {
        let previous = icon("chevron.up", label: "Previous message", action: #selector(previousResult), identifier: "previous-result")
        let next = icon("chevron.down", label: "Next message", action: #selector(nextResult), identifier: "next-result")
        let navigation = NSStackView(views: [previous, next])
        navigation.spacing = 2
        let copy = icon("doc.on.doc", label: "Copy command", action: #selector(copyResume), identifier: "copy-resume")
        let editor = icon("folder", label: "Open project", action: #selector(openProject), identifier: "open-project")
        let choice = choiceButton(.editor)
        let actions = NSStackView(views: [copy, editor, choice])
        actions.spacing = 2
        let stack = NSStackView(views: [toolbarCapsule(navigation, identifier: "navigation-capsule"), toolbarCapsule(actions, identifier: "actions-capsule")])
        stack.orientation = .horizontal; stack.spacing = 8
        return stack
    }
    private func toolbarCapsule(_ content: NSView, identifier: String, prominent: Bool = false) -> GlassSurface {
        let capsule = GlassSurface(content: content, prominent: prominent)
        capsule.heightAnchor.constraint(equalToConstant: 40).isActive = true
        capsule.setAccessibilityElement(true); capsule.setAccessibilityRole(.group)
        capsule.setAccessibilityIdentifier(identifier)
        return capsule
    }
    private func icon(_ symbol: String, label: String, action: Selector, identifier: String) -> NSButton {
        let control = button("", action: action, identifier: identifier)
        control.image = NSImage(systemSymbolName: symbol, accessibilityDescription: label)
        control.setAccessibilityLabel(label)
        control.toolTip = label
        control.widthAnchor.constraint(equalToConstant: 24).isActive = true
        control.heightAnchor.constraint(equalToConstant: 24).isActive = true
        return control
    }
    private func choiceButton(_ kind: OpenWithKind) -> NSPopUpButton {
        let control = NSPopUpButton(frame: .zero, pullsDown: true)
        control.isBordered = false
        (control.cell as? NSPopUpButtonCell)?.arrowPosition = .noArrow
        control.menu = choiceMenu(kind)
        control.setAccessibilityIdentifier(kind == .terminal ? "terminal-choice" : "editor-choice")
        control.setAccessibilityLabel(kind == .terminal ? "Resume in" : "Open in")
        control.widthAnchor.constraint(equalToConstant: 24).isActive = true
        control.heightAnchor.constraint(equalToConstant: 24).isActive = true
        choiceItems[kind] = control
        return control
    }
    private func choiceMenu(_ kind: OpenWithKind) -> NSMenu {
        let menu = openWithMenu(kind)
        let indicator = NSMenuItem(title: "", action: nil, keyEquivalent: "")
        indicator.image = NSImage(systemSymbolName: "chevron.down", accessibilityDescription: nil)
        menu.insertItem(indicator, at: 0)
        return menu
    }
    private func layoutFilters() {
        guard let filterBar else { return }
        filterBar.layoutSubtreeIfNeeded()
        filterBar.setFrameSize(NSSize(width: filterBar.fittingSize.width, height: 32))
        filterScroll.contentView.scroll(to: .zero)
        filterScroll.reflectScrolledClipView(filterScroll.contentView)
    }
    private func replaceConversation(direction: Int, update: () -> Void) {
        let clip = conversationScroll.contentView
        clip.wantsLayer = true; conversationDocument.wantsLayer = true
        guard let layer = conversationDocument.layer else { update(); return }
        let reduced = DisplayMode.current.reduceMotion
        let opacity = layer.animation(forKey: "content-opacity") == nil ? 0 : layer.presentation()?.opacity ?? 0
        let offset = layer.animation(forKey: "content-offset") == nil ? CGFloat(direction * 6) : layer.presentation()?.value(forKeyPath: "transform.translation.y") as? CGFloat ?? 0
        layer.removeAllAnimations()
        exitLayer?.removeFromSuperlayer()
        let bitmap = clip.bitmapImageRepForCachingDisplay(in: clip.bounds)
        if let bitmap { clip.cacheDisplay(in: clip.bounds, to: bitmap) }
        update() // Lay out the new text once, before any layer animation.
        conversationDocument.layoutSubtreeIfNeeded()
        let fade = transitionAnimation(key: "opacity", from: opacity, to: 1, reduced: reduced)
        layer.opacity = 1; layer.transform = CATransform3DIdentity
        layer.add(fade, forKey: "content-opacity")
        if !reduced && direction != 0 {
            layer.add(transitionAnimation(key: "transform.translation.y", from: offset, to: 0, reduced: false), forKey: "content-offset")
        }
        if let bitmap {
            let old = CALayer()
            old.contents = bitmap.cgImage; old.frame = clip.bounds
            old.opacity = 0
            clip.layer?.addSublayer(old)
            old.add(transitionAnimation(key: "opacity", from: 1, to: 0, reduced: reduced), forKey: "exit-opacity")
            if !reduced && direction != 0 {
                old.transform = CATransform3DMakeTranslation(0, CGFloat(-direction * 6), 0)
                old.add(transitionAnimation(key: "transform.translation.y", from: 0, to: -direction * 6, reduced: false), forKey: "exit-offset")
            }
            exitLayer = old
            DispatchQueue.main.asyncAfter(deadline: .now() + fade.duration) { old.removeFromSuperlayer() }
        }
    }
    private func button(_ title: String, action: Selector, identifier: String) -> NSButton {
        let button = PressedButton(title: title, target: self, action: action)
        button.isBordered = false
        button.font = .systemFont(ofSize: 12, weight: .medium)
        button.setAccessibilityIdentifier(identifier)
        return button
    }
    private func addChromeBackground(to root: NSView, above scroll: NSScrollView, bottom: NSLayoutYAxisAnchor, inset: CGFloat) {
        let chrome = PaneMaterial()
        chrome.setAccessibilityElement(false)
        root.addSubview(chrome, positioned: .above, relativeTo: scroll)
        chrome.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            chrome.topAnchor.constraint(equalTo: root.topAnchor),
            chrome.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            chrome.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            chrome.bottomAnchor.constraint(equalTo: bottom, constant: inset)
        ])
        chromeBackgrounds.append(chrome)
    }
    private func makeResults() {
        let root = results.view
        let scroll = NSScrollView()
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.automaticallyAdjustsContentInsets = false
        scroll.contentInsets = NSEdgeInsets(top: 170, left: 0, bottom: 12, right: 0)
        scroll.setAccessibilityIdentifier("results-scroll")
        table.addTableColumn(NSTableColumn(identifier: .init("message")))
        table.headerView = nil
        table.rowHeight = 94
        table.intercellSpacing = NSSize(width: 0, height: 2)
        table.backgroundColor = .clear
        table.selectionHighlightStyle = .regular
        table.columnAutoresizingStyle = .uniformColumnAutoresizingStyle
        table.dataSource = self; table.delegate = self
        table.setAccessibilityIdentifier("results")
        scroll.documentView = table
        root.addSubview(scroll); pin(scroll, to: root)
        var buttons: [NSView] = []
        for (key, title) in harnesses {
            let control = FilterChip(title: title + " —", target: self, action: #selector(filterChanged(_:)))
            control.identifier = .init(key)
            control.setAccessibilityIdentifier("filter-" + key)
            control.isBordered = false
            control.select(key == harness)
            control.setContentCompressionResistancePriority(.required, for: .horizontal)
            filterButtons[key] = control
            buttons.append(control)
        }
        let filters = NSStackView(views: buttons)
        filters.orientation = .horizontal; filters.spacing = 2
        filters.detachesHiddenViews = true
        filterBar = filters
        filters.translatesAutoresizingMaskIntoConstraints = true
        filterScroll.hasHorizontalScroller = false
        filterScroll.autohidesScrollers = true
        filterScroll.scrollerStyle = .overlay
        filterScroll.drawsBackground = false
        filterScroll.horizontalScrollElasticity = .allowed
        filterScroll.documentView = filters
        filterScroll.setAccessibilityIdentifier("filter-scroll")
        filterScroll.heightAnchor.constraint(equalToConstant: 32).isActive = true
        layoutFilters()
        let glass = GlassSurface(content: filterScroll, material: .sidebar)
        glass.setAccessibilityIdentifier("filter-capsule")
        root.addSubview(glass)
        glass.translatesAutoresizingMaskIntoConstraints = false
        project.setAccessibilityIdentifier("project-filter")
        project.cell = PaddedPopupCell(textCell: "", pullsDown: false)
        project.target = self; project.action = #selector(optionsChanged)
        project.addItem(withTitle: "All projects")
        project.controlSize = .mini
        project.isBordered = false
        project.font = .systemFont(ofSize: 11)
        project.heightAnchor.constraint(equalToConstant: 32).isActive = true
        mode.cell = PaddedPopupCell(textCell: "", pullsDown: false)
        mode.target = self; mode.action = #selector(optionsChanged)
        mode.addItems(withTitles: ["Messages", "Sessions"])
        mode.controlSize = .mini
        mode.isBordered = false
        mode.font = .systemFont(ofSize: 11)
        mode.heightAnchor.constraint(equalToConstant: 32).isActive = true
        mode.setAccessibilityIdentifier("result-mode")
        summary.textColor = .secondaryLabelColor
        summary.font = .systemFont(ofSize: 11)
        summary.setAccessibilityIdentifier("result-summary")
        for popup in [project, mode] {
            popup.setContentCompressionResistancePriority(.required, for: .horizontal)
            // NSPopUpButton's intrinsic width omits our cell's added title padding.
            popup.widthAnchor.constraint(greaterThanOrEqualToConstant: popup.cell!.cellSize.width).isActive = true
        }
        let options = NSStackView(views: [project, mode, NSView()])
        options.orientation = .horizontal; options.alignment = .centerY; options.spacing = 6
        let line = NSStackView(views: [summary, options])
        line.orientation = .vertical; line.alignment = .leading; line.spacing = 6
        line.heightAnchor.constraint(equalToConstant: 54).isActive = true
        options.widthAnchor.constraint(equalTo: line.widthAnchor).isActive = true
        stateLabel.preferredMaxLayoutWidth = 240
        stateLabel.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        stateLabel.maximumNumberOfLines = 5
        stateLabel.lineBreakMode = .byWordWrapping
        stateLabel.cell?.wraps = true
        stateLabel.cell?.isScrollable = false
        stateLabel.alignment = .center
        stateLabel.setAccessibilityIdentifier("search-state")
        retry.target = self; retry.action = #selector(startIndexing)
        retry.isHidden = true
        let empty = NSStackView(views: [stateLabel, retry])
        empty.orientation = .vertical; empty.spacing = 16
        root.addSubview(empty); empty.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(line); line.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            empty.centerXAnchor.constraint(equalTo: root.centerXAnchor),
            empty.centerYAnchor.constraint(equalTo: root.centerYAnchor),
            empty.widthAnchor.constraint(lessThanOrEqualTo: root.widthAnchor, constant: -60),
            glass.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 12),
            glass.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -12),
            glass.topAnchor.constraint(equalTo: root.safeAreaLayoutGuide.topAnchor, constant: 8),
            line.topAnchor.constraint(equalTo: glass.bottomAnchor, constant: 4),
            line.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 18),
            line.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -18)
        ])
        addChromeBackground(to: root, above: scroll, bottom: line.bottomAnchor, inset: 4)
    }
    private func makeConversation() {
        let root = conversationPane.view
        let scroll = conversationScroll
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.automaticallyAdjustsContentInsets = false
        scroll.contentInsets = NSEdgeInsets(top: 116, left: 0, bottom: 60, right: 0)
        scroll.setAccessibilityIdentifier("conversation-scroll")
        conversationDocument.setAccessibilityElement(true)
        conversationDocument.setAccessibilityRole(.group)
        conversationDocument.setAccessibilityIdentifier("conversation")
        conversationDocument.setAccessibilityLabel("Conversation")
        messageStack.orientation = .vertical; messageStack.alignment = .leading
        messageStack.spacing = 4
        conversationDocument.addSubview(messageStack)
        messageStack.translatesAutoresizingMaskIntoConstraints = false
        scroll.documentView = conversationDocument
        conversationDocument.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            conversationDocument.widthAnchor.constraint(equalTo: scroll.contentView.widthAnchor),
            messageStack.leadingAnchor.constraint(equalTo: conversationDocument.leadingAnchor, constant: 24),
            messageStack.trailingAnchor.constraint(equalTo: conversationDocument.trailingAnchor, constant: -24),
            messageStack.topAnchor.constraint(equalTo: conversationDocument.topAnchor, constant: 8),
            messageStack.bottomAnchor.constraint(equalTo: conversationDocument.bottomAnchor, constant: -16)
        ])
        root.addSubview(scroll); pin(scroll, to: root)
        conversationDetail.textColor = .secondaryLabelColor
        conversationDetail.font = .systemFont(ofSize: 11)
        conversationDetail.lineBreakMode = .byTruncatingMiddle
        conversationDetail.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        conversationModel.textColor = .secondaryLabelColor
        conversationTitle.alignment = .center
        conversationTitle.setAccessibilityIdentifier("conversation-agent")
        let agentFont = conversationTitle.font
        conversationTitle.cell = PaddedLabelCell(textCell: "Choose a message")
        conversationTitle.font = agentFont
        conversationTitle.isBordered = false; conversationTitle.drawsBackground = false
        conversationTitle.setContentHuggingPriority(.required, for: .horizontal)
        conversationTitle.setContentCompressionResistancePriority(.required, for: .horizontal)
        conversationModel.lineBreakMode = .byTruncatingTail
        conversationDetail.lineBreakMode = .byTruncatingTail
        conversationModel.setAccessibilityIdentifier("conversation-model")
        conversationDetail.setAccessibilityIdentifier("conversation-path")
        let heading = NSStackView(views: [conversationTitle, conversationModel, conversationDetail])
        heading.orientation = .horizontal; heading.spacing = 10
        heading.setAccessibilityElement(true)
        heading.setAccessibilityRole(.group)
        heading.setAccessibilityIdentifier("conversation-header")
        root.addSubview(heading); heading.translatesAutoresizingMaskIntoConstraints = false
        let previous = icon("chevron.up", label: "Previous matching message", action: #selector(previousResult), identifier: "conversation-previous")
        let next = icon("chevron.down", label: "Next matching message", action: #selector(nextResult), identifier: "conversation-next")
        hitPosition.setAccessibilityIdentifier("hit-position")
        hitPosition.toolTip = "Position in the ranked matching messages"
        hitPosition.font = .monospacedDigitSystemFont(ofSize: 12, weight: .medium)
        let nav = NSStackView(views: [previous, hitPosition, next])
        nav.orientation = .horizontal; nav.spacing = 10
        let floating = GlassSurface(content: nav)
        floating.setAccessibilityIdentifier("hit-capsule")
        root.addSubview(floating); floating.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            heading.topAnchor.constraint(equalTo: root.safeAreaLayoutGuide.topAnchor, constant: 12),
            heading.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 24),
            heading.trailingAnchor.constraint(lessThanOrEqualTo: root.trailingAnchor, constant: -24),
            heading.heightAnchor.constraint(equalToConstant: 32),
            conversationTitle.widthAnchor.constraint(greaterThanOrEqualToConstant: 64),
            conversationTitle.heightAnchor.constraint(equalToConstant: 32),
            floating.centerXAnchor.constraint(equalTo: root.centerXAnchor),
            floating.bottomAnchor.constraint(equalTo: root.bottomAnchor, constant: -16)
        ])
        addChromeBackground(to: root, above: scroll, bottom: heading.bottomAnchor, inset: 14)
    }
    func toolbarAllowedItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        ["query-title", "actions", "search", "resume"].map { NSToolbarItem.Identifier($0) } + [.flexibleSpace]
    }
    func toolbarDefaultItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        [.init("query-title"), .flexibleSpace, .init("actions"), .init("search"), .init("resume")]
    }
    func toolbar(_ toolbar: NSToolbar, itemForItemIdentifier identifier: NSToolbarItem.Identifier,
                 willBeInsertedIntoToolbar flag: Bool) -> NSToolbarItem? {
        if identifier.rawValue == "search" {
            searchItem.searchField.widthAnchor.constraint(greaterThanOrEqualToConstant: 176).isActive = true
            searchItem.searchField.widthAnchor.constraint(lessThanOrEqualToConstant: 200).isActive = true
            return searchItem
        }
        let item = NSToolbarItem(itemIdentifier: identifier)
        item.isBordered = false
        switch identifier.rawValue {
        case "query-title":
            queryTitle.setAccessibilityIdentifier("query-title")
            queryCount.setAccessibilityIdentifier("query-count")
            queryCount.textColor = .secondaryLabelColor
            let title = NSStackView(views: [queryTitle, queryCount])
            title.spacing = 8
            titleWidth = title.widthAnchor.constraint(equalToConstant: min(198, max(94, (window?.frame.width ?? 1100) - 706)))
            titleWidth?.isActive = true
            queryCount.lineBreakMode = .byTruncatingTail
            queryCount.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            queryTitle.widthAnchor.constraint(greaterThanOrEqualToConstant: 36).isActive = true
            queryTitle.lineBreakMode = .byTruncatingTail
            queryTitle.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            let capsule = toolbarCapsule(title, identifier: "title-capsule")
            item.view = capsule
            item.label = "Search results"
        case "actions":
            item.view = toolbarActions()
            item.label = "Session actions"
        case "resume":
            resumeButton.target = self; resumeButton.action = #selector(resumeSelected)
            resumeButton.isBordered = false
            resumeButton.font = .systemFont(ofSize: 12, weight: .semibold)
            resumeButton.contentTintColor = .white
            resumeButton.setAccessibilityIdentifier("resume")
            resumeButton.setAccessibilityLabel("Resume ⏎")
            resumeButton.isEnabled = false
            resumeButton.widthAnchor.constraint(equalToConstant: 88).isActive = true
            resumeButton.heightAnchor.constraint(equalToConstant: 24).isActive = true
            let choice = choiceButton(.terminal)
            choice.contentTintColor = .white
            let divider = NSBox()
            divider.boxType = .separator
            divider.widthAnchor.constraint(equalToConstant: 1).isActive = true
            let content = NSStackView(views: [resumeButton, divider, choice])
            content.spacing = 4
            item.view = toolbarCapsule(content, identifier: "resume-capsule", prominent: true)
            item.label = "Resume"
        default: return nil
        }
        if let view = item.view {
            view.layoutSubtreeIfNeeded()
            view.frame.size = view.fittingSize
        }
        return item
    }
    private func makeMenu() {
        let main = NSMenu()
        let application = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "Settings…", action: #selector(settings), keyEquivalent: ",").target = self
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Quit Kioku", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        application.submenu = appMenu; main.addItem(application)
        let edit = NSMenuItem(title: "Edit", action: nil, keyEquivalent: "")
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        edit.submenu = editMenu; main.addItem(edit)
        let navigate = NSMenuItem(title: "Navigate", action: nil, keyEquivalent: "")
        let navigation = NSMenu(title: "Navigate")
        navigation.addItem(withTitle: "Search", action: #selector(focusSearch), keyEquivalent: "f").target = self
        navigation.addItem(withTitle: "Next matching message", action: #selector(nextResult), keyEquivalent: "g").target = self
        let previous = navigation.addItem(withTitle: "Previous matching message", action: #selector(previousResult), keyEquivalent: "g")
        previous.keyEquivalentModifierMask = [.command, .shift]; previous.target = self
        navigate.submenu = navigation; main.addItem(navigate)
        NSApp.mainMenu = main
    }
    @objc private func focusSearch() {
        window?.makeKeyAndOrderFront(nil)
        searchItem.searchField.selectText(nil)
        #if DEBUG
        (window as? DisplayWindow)?.testSearchEditor = searchItem.searchField.currentEditor()
        #endif
    }
    #if DEBUG
    func controlTextDidBeginEditing(_ notification: Notification) {
        (window as? DisplayWindow)?.testSearchEditor = searchItem.searchField.currentEditor()
    }
    #endif
    func controlTextDidChange(_ obj: Notification) { refresh() }
    @objc private func filterChanged(_ sender: NSButton) {
        harness = sender.identifier?.rawValue ?? "all"
        for (key, button) in filterButtons {
            (button as? FilterChip)?.select(key == harness)
        }
        refresh()
    }
    @objc private func optionsChanged() { refresh() }
    private func refresh() {
        searchTask?.cancel()
        generation += 1
        let current = generation
        let query = searchItem.searchField.stringValue
        let projectName = project.indexOfSelectedItem > 0 ? project.titleOfSelectedItem : nil
        let sessions = mode.indexOfSelectedItem == 1
        let selectedHarness = harness
        // Stale-while-revalidate: no empty/spinner flash on keystrokes.
        selectionGeneration += 1; showTask?.cancel()
        #if DEBUG
        publishConversationState()
        #endif
        searchTask = Task {
            do {
                try await Task.sleep(for: .milliseconds(30))
                let page = try await engine.search(query: query, harness: selectedHarness, project: projectName, sessions: sessions)
                guard !Task.isCancelled, current == generation else { return }
                resultTotal = page.total
                queryTitle.stringValue = query.isEmpty ? "Kioku" : query
                queryTitle.toolTip = query
                queryCount.stringValue = query.isEmpty ? "\(sessionCount) sessions" : sessions ? "\(page.total) sessions" : "\(page.total) messages · \(page.totalSessions ?? 0) sessions"
                hits = query.isEmpty ? [] : page.hits
                table.deselectAll(nil); table.reloadData()
                stateLabel.isHidden = !hits.isEmpty
                retry.isHidden = true
                if query.isEmpty {
                    stateLabel.stringValue = "Type to search \(sessionCount) sessions across your agents"
                    summary.stringValue = "\(sessionCount) indexed sessions"
                } else if page.total == 0 {
                    stateLabel.stringValue = "No messages match “\(query)”\nTry fewer words or remove quotes."
                    summary.stringValue = "No matching messages"
                } else {
                    let count = page.total > hits.count ? "\(hits.count) of \(page.total)" : "\(page.total)"
                    summary.stringValue = "\(count) \(sessions ? "sessions" : "messages") · ranked"
                }
                if hits.isEmpty { clearConversation() }
                else { table.selectRowIndexes(IndexSet(integer: 0), byExtendingSelection: false) }
                for (key, title) in harnesses {
                    let total = key == selectedHarness ? page.total : try await engine.search(query: query, harness: key, project: projectName, sessions: sessions, limit: 1).total
                    guard !Task.isCancelled, current == generation else { return }
                    filterButtons[key]?.title = "\(title) \(total)"
                    filterButtons[key]?.isHidden = key != "all" && total == 0
                    filterButtons[key]?.invalidateIntrinsicContentSize()
                    layoutFilters()
                }
                #if DEBUG
                completedSearchGeneration = current
                publishConversationState()
                #endif
            } catch is CancellationError { }
            catch {
                guard current == generation else { return }
                // Preserve the last usable list on an engine error.
                summary.stringValue = "Search failed"
                stateLabel.stringValue = error.localizedDescription
                stateLabel.isHidden = false; retry.isHidden = false
            }
        }
    }
    private func clearConversation() {
        selectionGeneration += 1; showTask?.cancel()
        conversation = nil; loadedRef = nil; resumeButton.isEnabled = false
        conversationTitle.stringValue = "Choose a message"
        conversationDetail.stringValue = "Search your local coding-agent history"
        conversationDocument.transcript = ""
        messageStack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        conversationModel.stringValue = ""
        hitPosition.stringValue = "No hit selected"
    }
    func numberOfRows(in tableView: NSTableView) -> Int { hits.count }
    func tableView(_ tableView: NSTableView, rowViewForRow row: Int) -> NSTableRowView? { SelectionRow() }
    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let cell = tableView.makeView(withIdentifier: .init("cell"), owner: self) as? ResultCell ?? ResultCell()
        cell.identifier = .init("cell")
        cell.configure(hits[row], selected: row == tableView.selectedRow, target: self)
        return cell
    }
    func tableViewSelectionDidChange(_ notification: Notification) {
        let row = table.selectedRow
        showTask?.cancel(); selectionGeneration += 1
        #if DEBUG
        publishConversationState()
        #endif
        updatePosition()
        guard hits.indices.contains(row) else { return }
        for index in table.rows(in: table.visibleRect).location..<NSMaxRange(table.rows(in: table.visibleRect)) where hits.indices.contains(index) {
            (table.view(atColumn: 0, row: index, makeIfNecessary: false) as? ResultCell)?.configure(hits[index], selected: index == row, target: self)
        }
        let current = selectionGeneration
        let hit = hits[row]
        let query = searchItem.searchField.stringValue
        resumeButton.isEnabled = false
        let queryGeneration = generation
        let direction = renderedQuery == query ? (row >= renderedRow ? 1 : -1) : 0
        showTask = Task {
            do {
                try await Task.sleep(for: .milliseconds(25))
                let page = try await engine.show(ref: hit.ref, query: query)
                guard !Task.isCancelled, current == selectionGeneration, queryGeneration == generation else { return }
                conversation = page; loadedRef = hit.ref
                renderedRow = row; renderedQuery = query
                expanded = []
                resumeButton.isEnabled = true
                conversationTitle.stringValue = page.harness
                conversationTitle.textColor = .white
                conversationTitle.wantsLayer = true
                conversationTitle.layer?.backgroundColor = agentColor(page.harness).cgColor
                conversationTitle.layer?.cornerRadius = 9
                conversationModel.stringValue = page.model ?? "model not recorded"
                let home = ProcessInfo.processInfo.environment["HOME"] ?? NSHomeDirectory()
                conversationDetail.stringValue = page.cwd.hasPrefix(home + "/") ? "~" + page.cwd.dropFirst(home.count) : page.cwd
                conversationDetail.toolTip = page.cwd
                replaceConversation(direction: direction) { renderConversation(scrollToSelection: true) }
            } catch is CancellationError { }
            catch {
                guard current == selectionGeneration else { return }
                conversationDetail.stringValue = error.localizedDescription
            }
        }
    }
    private func renderConversation(scrollToSelection: Bool) {
        guard let conversation else { return }
        #if DEBUG
        let renderedGeneration = selectionGeneration
        #endif
        messageStack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        var transcript = ""
        var index = 0
        var selectedBlock: NSView?
        let firstMatch = conversation.messages.firstIndex { $0.hit || $0.matches } ?? 0
        let trailingContext = (conversation.messages.firstIndex { $0.hit } ?? -2) + 1
        while index < conversation.messages.count {
            let message = conversation.messages[index]
            if !message.hit && !message.matches && !expanded.contains(index) && index != trailingContext {
                let start = index
                repeat { index += 1 } while index < conversation.messages.count && !conversation.messages[index].hit && !conversation.messages[index].matches && !expanded.contains(index) && index != trailingContext
                let count = index - start
                let title = "⋯ \(count) \(start < firstMatch ? "earlier" : "later") \(count == 1 ? "message" : "messages")"
                let fold = button(title, action: #selector(expandFold(_:)), identifier: "fold-\(start)")
                fold.identifier = .init("fold:\(start):\(index)")
                fold.contentTintColor = .secondaryLabelColor
                fold.font = .systemFont(ofSize: 11)
                fold.heightAnchor.constraint(equalToConstant: 32).isActive = true
                messageStack.addArrangedSubview(fold)
                fold.widthAnchor.constraint(equalTo: messageStack.widthAnchor).isActive = true
                transcript += title + "\n"
                continue
            }
            let role = message.role == "asst" ? "Assistant" : message.role == "user" ? "You" : message.role.capitalized
            let roleLabel = label(role, size: 11, weight: .medium)
            roleLabel.textColor = .systemRed
            let time = label(message.time, size: 11)
            time.textColor = .secondaryLabelColor
            let heading = NSStackView(views: [roleLabel, NSView(), time])
            heading.spacing = 8
            let body = NSTextField(wrappingLabelWithString: "")
            body.attributedStringValue = highlighted(message.text, ranges: message.highlights, font: .systemFont(ofSize: 13))
            body.isSelectable = true
            body.setContentCompressionResistancePriority(.required, for: .vertical)
            body.setAccessibilityIdentifier("message-body-\(index)")
            let content = NSStackView(views: [heading, body])
            content.orientation = .vertical; content.alignment = .leading; content.spacing = 6
            let block = NSView()
            block.wantsLayer = true
            block.layer?.cornerRadius = 10
            if message.hit { block.layer?.backgroundColor = NSColor.systemYellow.withAlphaComponent(0.10).cgColor }
            block.setAccessibilityElement(true)
            block.setAccessibilityRole(.group)
            block.setAccessibilityIdentifier("message-\(index)")
            block.addSubview(content)
            content.translatesAutoresizingMaskIntoConstraints = false
            messageStack.addArrangedSubview(block)
            NSLayoutConstraint.activate([
                block.widthAnchor.constraint(equalTo: messageStack.widthAnchor),
                content.leadingAnchor.constraint(equalTo: block.leadingAnchor, constant: 14),
                content.trailingAnchor.constraint(equalTo: block.trailingAnchor, constant: -14),
                content.topAnchor.constraint(equalTo: block.topAnchor, constant: 14),
                content.bottomAnchor.constraint(equalTo: block.bottomAnchor, constant: -14),
                heading.widthAnchor.constraint(equalTo: content.widthAnchor),
                body.widthAnchor.constraint(equalTo: content.widthAnchor)
            ])
            if message.hit { selectedBlock = block }
            transcript += "\(role) · \(message.time)\(message.hit ? " · Selected hit" : "")\n\(message.text)\n"
            index += 1
        }
        conversationDocument.transcript = transcript
        conversationDocument.layoutSubtreeIfNeeded()
        if scrollToSelection {
            if conversationDocument.frame.height <= conversationScroll.contentView.bounds.height {
                conversationScroll.contentView.scroll(to: NSPoint(x: 0, y: -conversationScroll.contentInsets.top))
            } else if let selectedBlock {
                conversationDocument.scrollToVisible(conversationDocument.convert(selectedBlock.bounds, from: selectedBlock))
            }
            conversationScroll.reflectScrolledClipView(conversationScroll.contentView)
        }
        NSAccessibility.post(element: conversationDocument, notification: .valueChanged)
        updatePosition()
        #if DEBUG
        renderedSelectionGeneration = renderedGeneration
        publishConversationState()
        #endif
    }
    @objc private func expandFold(_ sender: NSButton) {
        let parts = (sender.identifier?.rawValue ?? "").split(separator: ":")
        guard parts.count == 3, let start = Int(parts[1]), let end = Int(parts[2]),
              let conversation, start >= 0, start < end, end <= conversation.messages.count else { return }
        expanded.formUnion(start..<end)
        renderConversation(scrollToSelection: false)
    }
    private func updatePosition() {
        hitPosition.stringValue = hits.indices.contains(table.selectedRow) ? "Hit \(table.selectedRow + 1) of \(resultTotal)" : "No matching message"
    }
    private func selectResult(delta: Int) {
        guard !hits.isEmpty else { return }
        let row = min(hits.count - 1, max(0, table.selectedRow + delta))
        table.selectRowIndexes(IndexSet(integer: row), byExtendingSelection: false)
        table.scrollRowToVisible(row)
    }
    @objc private func nextResult() { selectResult(delta: 1) }
    @objc private func previousResult() { selectResult(delta: -1) }
    @objc private func copyResume() {
        guard let conversation = selectedConversation else { return }
        NSHapticFeedbackManager.defaultPerformer.perform(.levelChange, performanceTime: .now)
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(conversation.resumeCmd, forType: .string)
    }
    @objc func resumeSelected() {
        guard let conversation = selectedConversation else { return }
        NSHapticFeedbackManager.defaultPerformer.perform(.levelChange, performanceTime: .now)
        Task {
            do {
                let fallback = try await SessionActions.resume(conversation)
                if fallback { summary.stringValue = "Preferred terminal unavailable · opened Terminal" }
            } catch { showError(error) }
        }
    }
    @objc private func openProject() {
        guard let conversation = selectedConversation else { return }
        Task { do { try await SessionActions.openEditor(conversation.cwd) } catch { showError(error) } }
    }
    private func showError(_ error: Error) {
        let alert = NSAlert(); alert.messageText = "Cannot open session"; alert.informativeText = error.localizedDescription
        if let window { alert.beginSheetModal(for: window) }
    }
    @objc private func settings() {
        if let settingsWindow, settingsWindow.isVisible { settingsWindow.makeKeyAndOrderFront(nil); return }
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 420, height: 240), styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.title = "Settings"
        choicePopups = [:]
        var views: [NSView] = []
        for kind in [OpenWithKind.terminal, .editor] {
            let choice = NSPopUpButton()
            choice.menu = openWithMenu(kind)
            choice.setAccessibilityIdentifier("preferred-" + kind.rawValue)
            selectChoice(in: choice, kind: kind)
            choicePopups[kind] = choice
            views += [label(kind == .terminal ? "Terminal" : "Editor", weight: .semibold), choice]
        }
        let stack = NSStackView(views: views)
        stack.orientation = .vertical; stack.alignment = .leading; stack.spacing = 12
        window.contentView?.addSubview(stack); stack.translatesAutoresizingMaskIntoConstraints = false
        stack.centerXAnchor.constraint(equalTo: window.contentView!.centerXAnchor).isActive = true
        stack.centerYAnchor.constraint(equalTo: window.contentView!.centerYAnchor).isActive = true
        settingsWindow = window
        window.center(); window.makeKeyAndOrderFront(nil)
        #if DEBUG
        if ProcessInfo.processInfo.environment["KIOKU_TEST_HOME"] != nil {
            // Place only our fixture window; pointer dragging can hit other apps or window chrome.
            window.setFrameTopLeftPoint(NSPoint(x: 100, y: (NSScreen.main?.visibleFrame.maxY ?? 1050) - 80))
        }
        #endif
    }
    private func openWithMenu(_ kind: OpenWithKind) -> NSMenu {
        let menu = NSMenu(title: kind.rawValue)
        let choices = OpenWith.choices(kind)
        let defaultID = OpenWith.systemDefault(kind).map(AppChoice.init)?.bundleID
        let selectedID = OpenWith.resolve(kind)?.0.bundleID
        for (index, choice) in choices.enumerated() {
            let suffix = choice.bundleID == defaultID ? " (" + NSLocalizedString("Default", comment: "Default app") + ")" : ""
            let item = NSMenuItem(title: choice.name + suffix, action: #selector(selectOpenWith(_:)), keyEquivalent: "")
            item.target = self
            item.representedObject = (kind, choice)
            item.identifier = .init("choose-" + kind.rawValue + "-" + choice.bundleID)
            let icon = NSWorkspace.shared.icon(forFile: choice.url.path)
            icon.size = NSSize(width: 16, height: 16)
            item.image = icon
            item.state = choice.bundleID == selectedID ? .on : .off
            menu.addItem(item)
            if index == 0 && defaultID != nil { menu.addItem(.separator()) }
        }
        if menu.items.last?.isSeparatorItem == false { menu.addItem(.separator()) }
        let other = NSMenuItem(title: NSLocalizedString("Other…", comment: "Choose app"), action: #selector(otherApp(_:)), keyEquivalent: "")
        other.target = self; other.representedObject = kind
        menu.addItem(other)
        return menu
    }
    private func selectChoice(in popup: NSPopUpButton, kind: OpenWithKind) {
        let selected = OpenWith.resolve(kind)?.0.bundleID
        if let item = popup.menu?.items.first(where: { ($0.representedObject as? (OpenWithKind, AppChoice))?.1.bundleID == selected }) { popup.select(item) }
    }
    @objc private func selectOpenWith(_ sender: NSMenuItem) {
        guard let (kind, choice) = sender.representedObject as? (OpenWithKind, AppChoice) else { return }
        saveChoice(kind, choice: choice)
    }
    private func saveChoice(_ kind: OpenWithKind, choice: AppChoice) {
        // A deliberate UI choice takes precedence over a launch-time setting.
        var arguments = UserDefaults.standard.volatileDomain(forName: UserDefaults.argumentDomain)
        arguments.removeValue(forKey: kind.preferenceKey)
        arguments.removeValue(forKey: kind.preferenceKey + "Path")
        UserDefaults.standard.setVolatileDomain(arguments, forName: UserDefaults.argumentDomain)
        UserDefaults.standard.set(choice.bundleID, forKey: kind.preferenceKey)
        UserDefaults.standard.set(choice.url.path, forKey: kind.preferenceKey + "Path")
        updateChoiceMenus()
    }
    private func updateChoiceMenus() {
        for (kind, item) in choiceItems { item.menu = choiceMenu(kind) }
        for (kind, popup) in choicePopups { popup.menu = openWithMenu(kind); selectChoice(in: popup, kind: kind) }
    }
    @objc private func otherApp(_ sender: NSMenuItem) {
        guard let kind = sender.representedObject as? OpenWithKind else { return }
        let panel = NSOpenPanel()
        panel.directoryURL = URL(fileURLWithPath: "/Applications", isDirectory: true)
        panel.allowedContentTypes = [.applicationBundle]
        panel.canChooseDirectories = false
        panel.canChooseFiles = true
        panel.allowsMultipleSelection = false
        panel.begin { [weak self] result in
            guard result == .OK, let url = panel.url, Bundle(url: url)?.bundleIdentifier != nil else { return }
            self?.saveChoice(kind, choice: AppChoice(url))
        }
    }
}
