import XCTest
import AppKit
import QuartzCore

final class KiokuUITests: XCTestCase {
    private var home: String { ProcessInfo.processInfo.environment["KIOKU_E2E_HOME"]! }  // set by e2e.sh as TEST_RUNNER_KIOKU_E2E_HOME
    private var recordDirectory = ""
    private var terminalLog: String { recordDirectory + "/terminal.json" }
    @MainActor private func launch(preference: String? = "", editorPreference: String? = "", defaultTerminal: String? = nil, terminalPath: String? = nil, overrides: [String: String] = [:]) -> XCUIApplication {
        continueAfterFailure = false
        recordDirectory = home + "/records/" + UUID().uuidString
        let app = XCUIApplication(bundleIdentifier: "com.kioku.mac")
        app.launchArguments = []
        if let editorPreference { app.launchArguments += ["-preferredEditor", editorPreference] }
        if let preference { app.launchArguments += ["-preferredTerminal", preference] }
        if let terminalPath { app.launchArguments += ["-preferredTerminalPath", terminalPath] }
        app.launchEnvironment = [
            "HOME": home, "KIOKU_INDEX": home + "/index.db", "XDG_CACHE_HOME": home + "/cache",
            "KIOKU_CLAUDE_DIR": home + "/.claude/projects", "KIOKU_CODEX_DIR": home + "/.codex/sessions",
            "KIOKU_PI_DIR": home + "/.pi/agent/sessions", "KIOKU_GROK_DIR": home + "/grok-home/.grok/sessions",
            "KIOKU_CURSOR_DIR": home + "/cursor-home/.cursor/chats", "KIOKU_OPENCODE_DB": home + "/opencode-home/.local/share/opencode/opencode.db",
            "KIOKU_TERMINAL_LAUNCHER": home + "/bin/terminal-stub", "KIOKU_EDITOR_LAUNCHER": home + "/bin/editor-stub",
            "KIOKU_SYSTEM_EDITOR": home + "/Default Editor.app",
            "KIOKU_REDUCE_MOTION": "0", "KIOKU_REDUCE_TRANSPARENCY": "0", "KIOKU_INCREASE_CONTRAST": "0",
            "KIOKU_TEST_HOME": home, "KIOKU_TEST_BACKDROP": "0", "KIOKU_TEST_RECORDS": recordDirectory, "PATH": home + "/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
        ]
        if let defaultTerminal { app.launchEnvironment["KIOKU_SYSTEM_TERMINAL"] = defaultTerminal }
        app.launchEnvironment.merge(overrides) { _, override in override }
        app.launch()
        activate(app)
        XCTAssertTrue(app.searchFields["search"].waitForExistence(timeout: 15))
        return app
    }
    @MainActor private func activate(_ app: XCUIApplication) {
        app.activate()
        XCTAssertTrue(app.wait(for: .runningForeground, timeout: 10), "Kioku must own keyboard and screenshot input")
    }
    @MainActor private func focusSearch(_ app: XCUIApplication) -> XCUIElement {
        activate(app)
        // Native Command-F focuses and selects the query in one responder action.
        app.typeKey("f", modifierFlags: .command)
        let focused = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value CONTAINS 'search-focused=true'"), object: app.windows["Kioku"])
        XCTAssertEqual(XCTWaiter.wait(for: [focused], timeout: 10), .completed, "Search field editor must own focus before typing: " + String(describing: app.windows["Kioku"].value))
        return app.searchFields["search"]
    }
    @MainActor private func search(_ query: String, in app: XCUIApplication) {
        let field = focusSearch(app)
        field.typeText(query)
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value == %@", query), object: field)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 10), .completed, "Search input must equal the requested query: " + String(describing: field.value))
    }
    @MainActor private func waitForLabel(_ element: XCUIElement, prefix: String) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value BEGINSWITH %@", prefix), object: element)
        let result = XCTWaiter.wait(for: [expectation], timeout: 20)
        XCTAssertEqual(result, .completed, "Expected \(prefix), got \(String(describing: element.value))" +
                       (result == .completed ? "" : "\nSearch state: " + String(describing: XCUIApplication(bundleIdentifier: "com.kioku.mac").staticTexts["search-state"].value) +
                        "\n" + XCUIApplication(bundleIdentifier: "com.kioku.mac").debugDescription))
    }
    @MainActor private func conversationState(_ element: XCUIElement) -> [String: Any]? {
        guard let value = element.value as? String,
              let json = value.components(separatedBy: "\nconversation-state=").last,
              let data = json.data(using: .utf8) else { return nil }
        return try? JSONSerialization.jsonObject(with: data) as? [String: Any]
    }
    @MainActor private func waitForConversation(_ app: XCUIApplication) {
        let element = app.groups["conversation"]
        let query = app.searchFields["search"].value as? String ?? ""
        let predicate = NSPredicate { _, _ in
            guard element.exists, let state = self.conversationState(element),
                  state["query"] as? String == query, state["ready"] as? Bool == true,
                  let generation = state["generation"] as? Int, generation > 0 else { return false }
            return (element.value as? String ?? "").contains("Selected hit")
        }
        let expectation = XCTNSPredicateExpectation(predicate: predicate, object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 10), .completed, "Current query=\(query); Conversation AX value: " + String(describing: element.value))
    }
    private func waitForFile(_ path: String) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
            FileManager.default.fileExists(atPath: path)
        }, object: nil)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 15), .completed, "Missing recorder event: " + path)
    }
    @MainActor private func screenshot(_ app: XCUIApplication, name: String) {
        activate(app)
        let attachment = XCTAttachment(screenshot: app.windows.firstMatch.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
    @MainActor private func waitForSettingsPosition(_ window: XCUIElement) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
            window.exists && abs(window.frame.minX - 100) <= 2
        }, object: window)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 10), .completed, "Fixture Settings frame: " + String(describing: window.frame))
    }
    @MainActor private func assertInside(_ element: XCUIElement, _ container: XCUIElement, window: XCUIElement) {
        XCTAssertTrue(element.exists, element.identifier)
        let frame = element.frame
        XCTAssertGreaterThan(frame.width, 0, element.identifier)
        XCTAssertGreaterThan(frame.height, 0, element.identifier)
        // AppKit AX includes a 1 pt optical outset on text and control frames.
        XCTAssertTrue(container.frame.insetBy(dx: -2, dy: -2).contains(frame), "\(element.identifier): \(frame) outside container \(container.frame)")
        XCTAssertTrue(window.frame.insetBy(dx: -2, dy: -2).contains(frame), "\(element.identifier): \(frame) outside window \(window.frame)")
    }
    @MainActor private func assertLayout(_ app: XCUIApplication) {
        let window = app.windows.firstMatch
        let toolbar = window.toolbars.firstMatch
        let controls = [app.buttons["previous-result"], app.buttons["next-result"], app.buttons["copy-resume"], app.buttons["open-project"], app.menuButtons["editor-choice"], app.searchFields["search"], app.buttons["resume"], app.menuButtons["terminal-choice"]]
        for control in controls { assertInside(control, toolbar, window: window) }
        for (index, control) in controls.enumerated() {
            for other in controls.dropFirst(index + 1) {
                XCTAssertFalse(control.frame.intersects(other.frame), "Toolbar controls overlap: \(control.identifier), \(other.identifier)")
            }
        }
        let trafficLights = [window.buttons["_XCUI:CloseWindow"], window.buttons["_XCUI:MinimizeWindow"], window.buttons["_XCUI:FullScreenWindow"]]
        let lightsMaxX = trafficLights.map { $0.frame.maxX }.max()!
        XCTAssertGreaterThan(controls[0].frame.minX, lightsMaxX)
        let toolbarItems = toolbar.children(matching: .group).allElementsBoundByIndex
        let capsules = ["title-capsule", "navigation-capsule", "actions-capsule", "resume-capsule"].map { app.groups[$0] }
        for capsule in capsules {
            XCTAssertTrue(capsule.exists, capsule.identifier)
            XCTAssertEqual(capsule.frame.height, capsules[0].frame.height, accuracy: 1, "All toolbar capsules must have equal height")
        }
        for (index, item) in toolbarItems.enumerated() {
            assertInside(item, toolbar, window: window)
            for other in toolbarItems.dropFirst(index + 1) { XCTAssertFalse(item.frame.intersects(other.frame)) }
        }
        XCTAssertGreaterThan(toolbarItems.first!.frame.minX, lightsMaxX)
        // AX reports the custom content, not the system glass plate's outset.
        // Reserve 8 pt on each plate plus 8 pt of visible separation.
        XCTAssertGreaterThanOrEqual(controls[0].frame.minX - toolbarItems.first!.frame.maxX, 24,
                                   "Title and navigation glass capsules must not touch")
        assertInside(app.staticTexts["query-title"], toolbar, window: window)
        assertInside(app.staticTexts["query-count"], toolbar, window: window)
        XCTAssertEqual(app.staticTexts["query-title"].value as? String, "紅茶")
        let queryTextWidth = ("紅茶" as NSString).size(withAttributes: [.font: NSFont.systemFont(ofSize: 14, weight: .semibold)]).width
        XCTAssertGreaterThanOrEqual(app.staticTexts["query-title"].frame.width, queryTextWidth + 4, "The actual text frame, not only its AX value, must fit the query")
        XCTAssertEqual(app.staticTexts["query-count"].value as? String, "6 messages · 4 sessions")
        XCTAssertEqual(app.buttons["resume"].label, "Resume ⏎")
        let resumeTextWidth = ("Resume ⏎" as NSString).size(withAttributes: [.font: NSFont.systemFont(ofSize: 12, weight: .semibold)]).width
        XCTAssertGreaterThanOrEqual(app.buttons["resume"].frame.width, resumeTextWidth + 16)
        let chips = [("all", "All 6"), ("claude", "Claude 1"), ("codex", "Codex 2"), ("pi", "Pi 2"), ("grok", "Grok 1")]
        let filter = app.scrollViews["filter-scroll"]
        let pane = app.groups["results-pane"]
        for (key, title) in chips {
            let chip = app.buttons["filter-" + key]
            XCTAssertEqual(chip.title, title, "Full chip text, not an ellipsis or a clipped label")
            assertInside(chip, filter, window: window)
            assertInside(chip, pane, window: window)
            let textWidth = (title as NSString).size(withAttributes: [.font: NSFont.systemFont(ofSize: 11, weight: key == "all" ? .semibold : .regular)]).width
            XCTAssertGreaterThanOrEqual(chip.frame.width, textWidth + 20, "10 pt padding on each side")
        }
        XCTAssertFalse(app.buttons["filter-opencode"].exists)
        XCTAssertFalse(app.buttons["filter-cursor"].exists)
        XCTAssertEqual(app.staticTexts["result-summary"].value as? String, "6 messages · ranked")
        assertInside(app.staticTexts["result-summary"], pane, window: window)
        let summary = app.staticTexts["result-summary"]
        for option in [app.popUpButtons["project-filter"], app.popUpButtons["result-mode"]] {
            assertInside(option, pane, window: window)
            XCTAssertGreaterThanOrEqual(option.frame.minY, summary.frame.maxY, "Filters wrap below the summary instead of truncating")
            XCTAssertEqual(option.frame.midY, app.popUpButtons["project-filter"].frame.midY, accuracy: 2)
            XCTAssertEqual(option.frame.height, app.buttons["filter-all"].frame.height, "Options match the filter-chip height, including AX optical outsets")
        }
        let rows = app.tables["results"].tableRows.allElementsBoundByIndex
        XCTAssertEqual(rows.count, 6)
        var offsets: [CGFloat]?
        for (index, row) in rows.enumerated() {
            let title = row.staticTexts.matching(NSPredicate(format: "value CONTAINS[c] 'demo'")).firstMatch
            let snippet = row.staticTexts.matching(NSPredicate(format: "value CONTAINS '茶'")).firstMatch
            let role = row.staticTexts["result-role"]
            let dot = row.staticTexts.matching(NSPredicate(format: "value == '●'")).firstMatch
            for text in [title, snippet, role, dot] { XCTAssertTrue(text.exists) }
            XCTAssertEqual(snippet.frame.minX, title.frame.minX, accuracy: 1, "Snippet must align with the title column")
            XCTAssertEqual(role.frame.minX, title.frame.minX, accuracy: 1, "Role must align with the title column")
            XCTAssertLessThan(dot.frame.maxX, title.frame.minX, "Dot hangs in the gutter")
            let current = [title, snippet].map { $0.frame.minY - row.frame.minY }
            if let offsets {
                for (actual, expected) in zip(current, offsets) {
                    XCTAssertEqual(actual, expected, accuracy: 1, "Every row uses the same title/snippet/role rhythm")
                }
            } else { offsets = current }
            // AX row frames include NSTableView's 2 pt intercell space.
            if index > 0 { XCTAssertEqual(row.frame.minY - rows[index - 1].frame.minY, 96, accuracy: 1) }
            assertInside(row, app.scrollViews["results-scroll"], window: window)
            assertInside(row, pane, window: window)
            XCTAssertGreaterThanOrEqual(row.frame.height, 68)
            XCTAssertLessThanOrEqual(row.frame.height, 96, "Ranked rows must stay compact")
            for text in row.staticTexts.allElementsBoundByIndex { assertInside(text, row, window: window) }
        }
        let conversationPane = app.groups["conversation-pane"]
        let header = app.groups["conversation-header"]
        assertInside(header, conversationPane, window: window)
        XCTAssertGreaterThanOrEqual(header.frame.minX - conversationPane.frame.minX, 23)
        XCTAssertGreaterThanOrEqual(conversationPane.frame.maxX - header.frame.maxX, 23)
        XCTAssertGreaterThan(header.frame.minY, toolbar.frame.maxY)
        for key in ["conversation-agent", "conversation-model", "conversation-path"] {
            assertInside(app.staticTexts[key], header, window: window)
        }
        XCTAssertEqual(app.staticTexts["conversation-model"].value as? String, "fixture-model")
        XCTAssertEqual(app.staticTexts["conversation-path"].value as? String, "~/work/demo")
        let folds = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'fold-'")).allElementsBoundByIndex
        XCTAssertEqual(folds.count, 2)
        XCTAssertEqual(folds[0].title, "⋯ 2 earlier messages")
        XCTAssertEqual(folds[1].title, "⋯ 1 later message")
        for fold in folds { assertInside(fold, app.scrollViews["conversation-scroll"], window: window) }
        XCTAssertLessThan(folds[0].frame.minY - header.frame.maxY, 40, "No large empty gap before the first fold")
        let block = app.groups["message-2"]
        assertInside(block, conversationPane, window: window)
        let body = app.staticTexts["message-body-2"]
        assertInside(body, block, window: window)
        XCTAssertGreaterThanOrEqual(body.frame.minX - block.frame.minX, 12)
        let followingBlock = app.groups["message-3"]
        assertInside(followingBlock, conversationPane, window: window)
        XCTAssertEqual(followingBlock.frame.minY - block.frame.maxY, 4, accuracy: 1)
    }
    private func assertTextInset(_ ink: CGRect, in container: CGRect, sides: CGFloat, id: String) {
        XCTAssertGreaterThanOrEqual(ink.minX - container.minX, sides, id + " leading")
        XCTAssertGreaterThanOrEqual(container.maxX - ink.maxX, sides, id + " trailing")
        XCTAssertGreaterThanOrEqual(ink.minY - container.minY, 8, id + " bottom")
        XCTAssertGreaterThanOrEqual(container.maxY - ink.maxY, 8, id + " top")
    }
    @MainActor private func waitForLayout(_ app: XCUIApplication, query: String, width: Double, after generation: Int = 0) -> [[String: Any]] {
        let window = app.windows.firstMatch
        var published: [String: Any]?
        let predicate = NSPredicate { _, _ in
            guard window.frame.width == width,
                  let value = window.value as? String,
                  let json = value.components(separatedBy: "; text-layout=").last,
                  let data = json.data(using: .utf8),
                  let context = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  context["query"] as? String == query,
                  context["width"] as? Double == width,
                  let counter = context["generation"] as? Int, counter > generation else { return false }
            published = context
            return true
        }
        let expectation = XCTNSPredicateExpectation(predicate: predicate, object: window)
        guard XCTWaiter.wait(for: [expectation], timeout: 10) == .completed,
              let counter = published?["generation"] as? Int else {
            XCTFail("Missing current-query/current-width native glyph measurements: requested query=\(query), width=\(width), after=\(generation); window=\(window.frame), AX=\(String(describing: window.value))")
            return []
        }
        // Each publication owns an immutable file, so another layout cannot overwrite it between AX and disk reads.
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: recordDirectory + "/text-layout-\(counter).json")),
              let records = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]],
              records.first?["query"] as? String == query,
              records.first?["width"] as? Double == width,
              records.first?["generation"] as? Int == counter else {
            XCTFail("Published layout generation \(counter) has no matching glyph measurements")
            return []
        }
        return records
    }
    @MainActor private func assertTextLayout(_ records: [[String: Any]], stress: Bool = false) {
        XCTAssertGreaterThan(records.count, 20, "Audit all text-in-container categories")
        var truncatedSnippets = 0
        for record in records {
            let id = record["id"] as! String
            if id == "layout-context" { continue }
            if let gap = record["gap"] as? Double {
                XCTAssertGreaterThanOrEqual(gap, 0, id)
                XCTAssertLessThanOrEqual(gap, 4, "No empty band before the role")
                continue
            }
            func rect(_ key: String) -> CGRect {
                let r = record[key] as! [Double]
                return CGRect(x: r[0], y: r[1], width: r[2], height: r[3])
            }
            assertTextInset(rect("ink"), in: rect("container"), sides: record["sides"] as! Double, id: id)
            let rendered = record["rendered"] as! String
            let full = record["full"] as! String
            let truncated = record["truncated"] as! Bool
            if ["project-filter", "result-mode"].contains(id) {
                XCTAssertFalse(truncated, id + " must never truncate")
            }
            if truncated {
                XCTAssertTrue(rendered.hasSuffix("…") || rendered.hasSuffix("..."), id + " must draw a tail ellipsis")
                XCTAssertNotEqual(rendered, full, id)
            } else { XCTAssertEqual(rendered, full, id + " may not hard-clip") }
            let maxLines = record["maxLines"] as! Int
            if maxLines > 0 { XCTAssertLessThanOrEqual(record["lines"] as! Int, maxLines, id) }
            if id == "result-snippet" && truncated { truncatedSnippets += 1 }
        }
        if stress { XCTAssertGreaterThan(truncatedSnippets, 0, "Long CJK/Latin snippets must exercise actual truncation") }
    }
    @MainActor private func assertMaterial(_ app: XCUIApplication, solid: Bool) {
        activate(app)
        let control = app.buttons["filter-all"]
        let visible = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == true AND hittable == true"), object: control)
        XCTAssertEqual(XCTWaiter.wait(for: [visible], timeout: 10), .completed, "Reading pane must be unobscured before pixel sampling")
        let state = app.windows["Kioku"].value as? String ?? "nil"
        XCTAssertTrue(state.contains("backdrop=known-gradient"), "Fixture backdrop must be visible and match Kioku's frame: " + state)
        let capture = app.windows.firstMatch.screenshot()
        let bitmap = NSBitmapImageRep(data: capture.pngRepresentation)!
        // Empty reading-surface areas over opposite ends of the wallpaper.
        let upper = bitmap.colorAt(x: bitmap.pixelsWide * 9 / 10, y: bitmap.pixelsHigh / 2)!.usingColorSpace(.deviceRGB)!
        let lower = bitmap.colorAt(x: bitmap.pixelsWide * 12 / 100, y: bitmap.pixelsHigh * 95 / 100)!.usingColorSpace(.deviceRGB)!
        let difference = abs(upper.redComponent - lower.redComponent) + abs(upper.greenComponent - lower.greenComponent) + abs(upper.blueComponent - lower.blueComponent)
        if solid { XCTAssertLessThan(difference, 0.02, "Solid fallback must not transmit the wallpaper") }
        else { XCTAssertGreaterThan(difference, 0.03, "The reading pane must transmit the wallpaper, not an opaque fill") }
    }
    @MainActor private func checkLayoutWidths(_ scheme: String) {
        for width in [800, 1000, 1400] {
            let app = launch(overrides: ["KIOKU_APPEARANCE": scheme, "KIOKU_TEST_BACKDROP": "1", "KIOKU_TEST_WINDOW_WIDTH": String(width), "KIOKU_TEST_TEXT_LAYOUT": "1"])
            defer { app.terminate() }
            let initial = waitForLayout(app, query: "", width: Double(width))
            XCTAssertEqual(app.windows.firstMatch.frame.width, CGFloat(width), "Settled window must match the requested width")
            let initialGeneration = initial.first?["generation"] as? Int ?? 0
            search("紅茶", in: app)
            waitForLabel(app.staticTexts["result-summary"], prefix: "6 messages")
            waitForConversation(app)
            let records = waitForLayout(app, query: "紅茶", width: Double(width), after: initialGeneration)
            screenshot(app, name: "mode-\(scheme)-\(width)")
            assertLayout(app)
            assertMaterial(app, solid: false)
            assertTextLayout(records)
            app.buttons["fold-0"].click()
            let expanded = waitForLayout(app, query: "紅茶", width: Double(width), after: records.first?["generation"] as? Int ?? 0)
            XCTAssertTrue((app.groups["conversation"].value as? String ?? "").contains("SNAPSHOT: json_extract"))
            screenshot(app, name: "fold-expanded-\(scheme)-\(width)")
            search("layoutprobe", in: app)
            waitForLabel(app.staticTexts["result-summary"], prefix: "2 messages")
            waitForConversation(app)
            let stress = waitForLayout(app, query: "layoutprobe", width: Double(width), after: expanded.first?["generation"] as? Int ?? 0)
            screenshot(app, name: "text-stress-\(scheme)-\(width)")
            assertTextLayout(stress, stress: true)
        }
    }
    @MainActor func testLayoutLightWidths() { checkLayoutWidths("light") }
    @MainActor func testLayoutDarkWidths() { checkLayoutWidths("dark") }
    @MainActor func testFilterHorizontalOverflow() {
        let app = launch(overrides: ["KIOKU_TEST_WINDOW_WIDTH": "800"])
        defer { app.terminate() }
        waitForLabel(app.staticTexts["result-summary"], prefix: "10 indexed sessions")
        let window = app.windows.firstMatch
        XCTAssertEqual(window.frame.width, 800)
        let scroll = app.scrollViews["filter-scroll"]
        assertInside(app.buttons["filter-all"], scroll, window: window)
        XCTAssertGreaterThan(app.buttons["filter-cursor"].frame.maxX, scroll.frame.maxX)
        scroll.hover()
        scroll.scroll(byDeltaX: -300, deltaY: 0)
        assertInside(app.buttons["filter-cursor"], scroll, window: window)
        XCTAssertEqual(app.buttons["filter-cursor"].title, "Cursor 1")
        screenshot(app, name: "filter-horizontal-overflow")
        scroll.scroll(byDeltaX: 300, deltaY: 0)
        assertInside(app.buttons["filter-all"], scroll, window: window)
    }
    @MainActor func testRapidCJKRaceTwentyTimes() throws {
        let app = launch()
        defer { app.terminate() }
        let field = app.searchFields["search"]
        for _ in 0..<20 {
            _ = focusSearch(app)
            field.typeText("紅")
            field.typeText("茶")
            XCTAssertEqual(field.value as? String, "紅茶")
            waitForLabel(app.staticTexts["result-summary"], prefix: "6 messages")
            XCTAssertEqual(app.tables["results"].tableRows.count, 6)
            let visible = app.tables["results"].descendants(matching: .staticText).allElementsBoundByIndex.map { $0.value as? String ?? $0.label }.joined()
            XCTAssertFalse(visible.contains("紅色的筆記"))
        }
        waitForConversation(app)
        screenshot(app, name: "race-20")
    }
    @MainActor func testThirtyRapidDownArrows() throws {
        let app = launch()
        defer { app.terminate() }
        search("arrowprobe", in: app)
        waitForLabel(app.staticTexts["result-summary"], prefix: "35 messages")
        waitForConversation(app)
        let started = Date()
        for _ in 0..<30 { app.typeKey(XCUIKeyboardKey.downArrow, modifierFlags: []) }
        XCTAssertTrue(app.tables["results"].tableRows.element(boundBy: 30).isSelected)
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value CONTAINS 'arrowprobe 04'"), object: app.groups["conversation"])
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 2), .completed)
        let elapsed = Date().timeIntervalSince(started)
        print("KIOKU_METRIC held-arrows-30-seconds=\(elapsed) budget=30")
        XCTAssertLessThan(elapsed, 30, "Includes XCUITest event synthesis; final conversation must settle within 2 seconds")
        assertInside(app.groups["message-4"], app.scrollViews["conversation-scroll"], window: app.windows.firstMatch)
        screenshot(app, name: "arrows-30")
    }
    @MainActor func testFirstResultsBudget() throws {
        let app = launch()
        defer { app.terminate() }
        let field = focusSearch(app)
        let started = Date()
        field.typeText("紅茶")
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "value BEGINSWITH '6 messages'"), object: app.staticTexts["result-summary"])
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 2), .completed)
        let elapsed = Date().timeIntervalSince(started)
        print("KIOKU_METRIC first-results-seconds=\(elapsed) budget=2")
        XCTAssertLessThan(elapsed, 2, "Includes typing and the AX observation round trip")
    }
    @MainActor private func checkMode(_ overrides: [String: String], expected: String, name: String) {
        let app = launch(overrides: overrides.merging(["KIOKU_TEST_BACKDROP": "1"]) { _, fixture in fixture })
        defer { app.terminate() }
        search("紅茶", in: app)
        waitForLabel(app.staticTexts["result-summary"], prefix: "6 messages")
        waitForConversation(app)
        let value = app.windows.firstMatch.value as? String ?? ""
        XCTAssertTrue(value.contains(expected), value)
        XCTAssertTrue(value.contains("material=solid") || value.contains("material=visual-effect") || value.contains("material=glass"))
        screenshot(app, name: "mode-" + name)
        assertMaterial(app, solid: expected.contains("material=solid"))
        if overrides["KIOKU_APPEARANCE"] == "light" {
            let bitmap = NSBitmapImageRep(data: app.windows.firstMatch.screenshot().pngRepresentation)!
            let background = bitmap.colorAt(x: bitmap.pixelsWide * 9 / 10, y: bitmap.pixelsHigh / 2)!.usingColorSpace(.deviceRGB)!
            XCTAssertGreaterThan(background.redComponent, 0.7, "Light solid mode must not leave dark layer colors behind light text styling")
        }
    }
    @MainActor func testModeDefault() {
        checkMode([:], expected: "motion=standard; transparency=standard; contrast=standard", name: "default")
    }
    @MainActor func testModeReduceTransparency() {
        checkMode(["KIOKU_REDUCE_TRANSPARENCY": "1"], expected: "transparency=reduced; contrast=standard; material=solid", name: "reduce-transparency")
        checkMode(["KIOKU_REDUCE_TRANSPARENCY": "1", "KIOKU_APPEARANCE": "light"], expected: "transparency=reduced; contrast=standard; material=solid", name: "reduce-transparency-light")
    }
    @MainActor func testModeIncreaseContrast() {
        checkMode(["KIOKU_INCREASE_CONTRAST": "1"], expected: "contrast=increased; material=solid", name: "increase-contrast")
        checkMode(["KIOKU_INCREASE_CONTRAST": "1", "KIOKU_APPEARANCE": "light"], expected: "contrast=increased; material=solid", name: "increase-contrast-light")
    }
    @MainActor func testModeReduceMotion() {
        checkMode(["KIOKU_REDUCE_MOTION": "1"], expected: "motion=reduced", name: "reduce-motion")
    }
    @MainActor func testInlineEngineErrorAndFix() {
        let app = launch(overrides: ["KIOKU_ENGINE": home + "/missing-engine"])
        defer { app.terminate() }
        waitForLabel(app.staticTexts["result-summary"], prefix: "Indexing failed")
        XCTAssertTrue((app.staticTexts["search-state"].value as? String ?? "").contains("Rebuild the app"))
        XCTAssertTrue(app.buttons["Retry indexing"].exists)
    }
    @MainActor func testGhosttyRecipeWithInjectedAppAndStub() async throws {
        let fake = home + "/Ghostty Fixture.app"
        let app = launch(defaultTerminal: fake)
        defer { app.terminate() }
        search("紅茶", in: app); waitForConversation(app)
        app.typeKey(XCUIKeyboardKey.return, modifierFlags: [])
        waitForFile(recordDirectory + "/session-launch.json")
        let record = try JSONSerialization.jsonObject(with: Data(contentsOf: URL(fileURLWithPath: terminalLog))) as! [String: Any]
        let cwd = home + "/work/demo"
        let argv = ["claude", "--resume", "11111111-1111-4111-8111-111111111111"]
        XCTAssertEqual(record["argv"] as? [String], ["-na", fake, "--args", "--working-directory=" + cwd, "-e"] + argv)
        XCTAssertEqual(record["cwd"] as? String, cwd)
        XCTAssertEqual(record["resume_argv"] as? [String], argv)
    }
    @MainActor func testConversationReadinessRejectsPreviousQuery() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let app = launch(overrides: ["KIOKU_ENGINE": home + "/bin/engine-gate", "KIOKU_TEST_RECORDS": directory.path])
        recordDirectory = directory.path
        defer { app.terminate(); try? FileManager.default.removeItem(at: directory) }
        let gate = recordDirectory + "/show-gate"
        XCTAssertEqual(mkfifo(gate, 0o600), 0)
        let descriptor = open(gate, O_RDWR)
        XCTAssertGreaterThanOrEqual(descriptor, 0)
        let writer = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        defer { try? writer.write(contentsOf: Data("go\n".utf8)); try? writer.close() }
        search("紅茶", in: app)
        waitForConversation(app)
        let conversation = app.groups["conversation"]
        let previous = (conversation.value as? String)?.components(separatedBy: "\nconversation-state=").first
        search("layoutprobe", in: app)
        waitForFile(recordDirectory + "/show-pending")
        // The old transcript is still visible, but must not satisfy a current-query wait.
        XCTAssertEqual((conversation.value as? String)?.components(separatedBy: "\nconversation-state=").first, previous)
        let state = conversationState(conversation)
        XCTAssertEqual(state?["query"] as? String, "layoutprobe", "Publish the requested query while the previous transcript remains visible")
        XCTAssertEqual(state?["ready"] as? Bool, false)
        try writer.write(contentsOf: Data("go\n".utf8))
        waitForConversation(app)
        XCTAssertTrue((conversation.value as? String ?? "").contains("layoutprobe"))
    }
    @MainActor func testCJKSearchAndHighlightedConversation() throws {
        let app = launch()
        defer { app.terminate() }
        search("紅茶", in: app)
        waitForLabel(app.staticTexts["result-summary"], prefix: "6 messages")
        waitForConversation(app)
        let text = app.groups["conversation"].value as? String ?? ""
        XCTAssertTrue(text.contains("紅茶"))
        XCTAssertTrue(text.contains("Selected hit"))
        XCTAssertTrue(text.contains("earlier messages") && text.contains("later message"))
        XCTAssertTrue(app.buttons["inline-resume"].firstMatch.exists)
        screenshot(app, name: "cjk-conversation")
    }
    @MainActor func testConversationHitsBeforeToolHits() throws {
        let app = launch()
        defer { app.terminate() }
        search("rankprobe", in: app)
        waitForLabel(app.staticTexts["result-summary"], prefix: "4 messages")
        let roles = app.tables["results"].staticTexts.matching(identifier: "result-role").allElementsBoundByIndex.map { $0.value as? String ?? $0.label }
        XCTAssertEqual(roles.count, 4, "All four fixture messages must be actual accessibility elements")
        XCTAssertEqual(roles.last, "TOOL")
        XCTAssertFalse(roles.dropLast().contains("TOOL"))
        screenshot(app, name: "tool-order")
    }
    @MainActor func testHarnessFilter() throws {
        let app = launch()
        defer { app.terminate() }
        search("紅茶", in: app)
        waitForLabel(app.staticTexts["result-summary"], prefix: "6 messages")
        app.buttons["filter-codex"].click()
        waitForLabel(app.staticTexts["result-summary"], prefix: "2 messages")
        waitForConversation(app)
        let cells = app.tables["results"].cells.allElementsBoundByIndex
        XCTAssertEqual(cells.count, 2)
        XCTAssertTrue(cells.allSatisfy { $0.label.hasPrefix("codex · demo") }, "Both actual table cells must be Codex hits")
        screenshot(app, name: "codex-filter")
    }
    @MainActor func testCopyResumeCommand() throws {
        let app = launch()
        defer { app.terminate() }
        search("紅茶", in: app)
        waitForConversation(app)
        app.buttons["copy-resume"].click()
        XCTAssertEqual(NSPasteboard.general.string(forType: .string), "claude --resume 11111111-1111-4111-8111-111111111111")
    }
    @MainActor private func resumeAndCheck(preference: String, injectedDefault: String?, expectedApp: String) async throws {
        let app = launch(preference: preference, defaultTerminal: injectedDefault)
        defer { app.terminate() }
        search("紅茶", in: app)
        waitForConversation(app)
        XCTAssertFalse(FileManager.default.fileExists(atPath: terminalLog), "A recorder must not predate the Return action")
        XCTAssertFalse(FileManager.default.fileExists(atPath: recordDirectory + "/session-launch.json"))
        app.typeKey(XCUIKeyboardKey.return, modifierFlags: [])
        waitForFile(recordDirectory + "/session-launch.json")
        let record = try JSONSerialization.jsonObject(with: Data(contentsOf: URL(fileURLWithPath: terminalLog))) as! [String: Any]
        let args = record["argv"] as! [String]
        XCTAssertEqual(args.count, 3)
        XCTAssertEqual(args, ["-a", expectedApp, args[2]])
        XCTAssertTrue(args[2].hasSuffix(".command"))
        XCTAssertEqual(record["cwd"] as? String, home + "/work/demo")
        XCTAssertEqual(record["resume_argv"] as? [String], ["claude", "--resume", "11111111-1111-4111-8111-111111111111"])
        let execution = try JSONSerialization.jsonObject(with: Data(contentsOf: URL(fileURLWithPath: recordDirectory + "/session-launch.json"))) as! [String: Any]
        XCTAssertEqual(execution["argv"] as? [String], ["claude", "--resume", "11111111-1111-4111-8111-111111111111"])
        XCTAssertEqual(execution["cwd"] as? String, home + "/work/demo")
        screenshot(app, name: "return-resume")
    }
    @MainActor func testResumeMissingSettingFallsBack() async throws {
        // A unique nonexistent id exercises the stale-setting branch on any host.
        try await resumeAndCheck(preference: "com.kioku.fixture.missing-" + UUID().uuidString, injectedDefault: nil,
                                 expectedApp: "/System/Applications/Utilities/Terminal.app")
    }
    @MainActor func testResumeUsesInjectedSystemDefault() async throws {
        let fake = home + "/Default Terminal.app"
        try await resumeAndCheck(preference: "", injectedDefault: fake, expectedApp: fake)
    }
    @MainActor func testResumeNotFoundFallsBack() async throws {
        try await resumeAndCheck(preference: "com.example.not-installed-terminal", injectedDefault: home + "/missing.app", expectedApp: "/System/Applications/Utilities/Terminal.app")
    }
    @MainActor private func menuItem(_ app: XCUIApplication, prefix: String) -> XCUIElement {
        // AppKit exposes a tracking menu under the app, not reliably under its popup button.
        let item = app.menuItems.matching(NSPredicate(format: "title BEGINSWITH %@", prefix)).firstMatch
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == true AND hittable == true"), object: item)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 10), .completed, "Missing open menu item: " + prefix)
        return item
    }
    @MainActor func testOpenWithMenusAndPersistedChoice() throws {
        let app = launch(preference: "com.kioku.fixture.alternate-terminal", defaultTerminal: home + "/Default Terminal.app",
                         terminalPath: home + "/Alternate Terminal.app")
        activate(app)
        app.typeKey(",", modifierFlags: .command)
        let terminal = app.popUpButtons["preferred-terminal"]
        XCTAssertTrue(terminal.waitForExistence(timeout: 10))
        waitForSettingsPosition(app.windows["Settings"])
        terminal.click()
        let first = menuItem(app, prefix: "Default Terminal")
        XCTAssertTrue(first.title.hasSuffix("(Default)") || first.title.hasSuffix("(預設)"), first.title)
        menuItem(app, prefix: "Alternate Terminal").click()
        XCTAssertTrue((terminal.value as? String ?? terminal.label).hasPrefix("Alternate Terminal"))
        app.popUpButtons["preferred-editor"].click()
        menuItem(app, prefix: "Default Editor").click()
        app.terminate()
        let relaunched = launch(preference: nil, editorPreference: nil, defaultTerminal: home + "/Default Terminal.app", overrides: ["KIOKU_SYSTEM_EDITOR": home + "/Alternate Editor.app"])
        defer { relaunched.terminate() }
        activate(relaunched)
        relaunched.typeKey(",", modifierFlags: .command)
        let saved = relaunched.popUpButtons["preferred-terminal"]
        XCTAssertTrue(saved.waitForExistence(timeout: 10))
        waitForSettingsPosition(relaunched.windows["Settings"])
        XCTAssertTrue((saved.value as? String ?? saved.label).hasPrefix("Alternate Terminal"))
        let editor = relaunched.popUpButtons["preferred-editor"]
        XCTAssertTrue((editor.value as? String ?? editor.label).hasPrefix("Default Editor"), "Chosen editor must survive a different system default after relaunch")
        editor.click()
        let editorDefault = menuItem(relaunched, prefix: "Alternate Editor")
        XCTAssertTrue(editorDefault.title.hasSuffix("(Default)") || editorDefault.title.hasSuffix("(預設)"))
        XCTAssertTrue(relaunched.menuItems["Other…"].exists || relaunched.menuItems["其他⋯"].exists)
        screenshot(relaunched, name: "open-with-menus")
    }
    @MainActor func testOpenProjectAndSessionMode() async throws {
        let app = launch()
        defer { app.terminate() }
        search("紅茶", in: app)
        waitForConversation(app)
        app.buttons["open-project"].click()
        waitForFile(recordDirectory + "/editor.json")
        let editor = try JSONSerialization.jsonObject(with: Data(contentsOf: URL(fileURLWithPath: recordDirectory + "/editor.json"))) as! [String: Any]
        XCTAssertEqual(editor["argv"] as? [String], ["-a", home + "/Default Editor.app", home + "/work/demo"])
        XCTAssertEqual(editor["cwd"] as? String, home + "/work/demo")
        app.popUpButtons["result-mode"].click()
        app.menuItems.matching(NSPredicate(format: "title == 'Sessions'")).firstMatch.click()
        waitForLabel(app.staticTexts["result-summary"], prefix: "4 sessions")
        screenshot(app, name: "sessions")
    }
}
