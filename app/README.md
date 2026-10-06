# Kioku for Mac

AppKit only. Xcode 26 / macOS 26 SDK, deployment target macOS 14.
The app bundles `kioku` in `Contents/Resources` and invokes it with `Process`:
background `index`, JSON message/session search, and JSON `show` with ±12 turns.
Swift never opens SQLite, reads session formats, ranks results, or parses queries.
Index/session semantics remain in Go; sources are never modified.

## Build

Go and its module dependencies must be available on Xcode's PATH. The build
script also checks `/opt/homebrew/bin` and `/usr/local/bin`.

```sh
xcodebuild -project app/Kioku.xcodeproj -scheme Kioku \
  -configuration Debug -destination 'platform=macOS' build
```

Open `app/Kioku.xcodeproj` to run. Local builds use ad-hoc signing, no team required.
`generate-project.py` regenerates the checked-in project/scheme without dependencies;
ordinary builds don't run it. The engine build phase supports both architectures.

## Flow and shortcuts

Two panes: ranked results on the left; selected conversation on the right.
Filter capsules have exact Go totals; project and Messages/Sessions selectors
sit below them. The selected row exposes an inline **Resume ⏎** pill.
The toolbar and floating **Hit N of M** control navigate ranked results.
Matching turns in the preview are highlighted; the selected turn's following context stays visible. Earlier/later runs fold to centered, clickable ⋯ rows.

- **⌘F:** search; **↑/↓:** select; **⌘G / ⇧⌘G:** next/previous result.
- **Return:** resume the selected, loaded session. Mouse Resume commits on mouse-up.
- **Esc:** clear search; if already empty, close the window.
- **⌘,:** Settings. Copy command and Open project are toolbar actions.

## Open With

Settings has Terminal and Editor popups. The toolbar has the same menus in
**Resume in ▾** and **Open in ▾**. Menus show 16 pt app icons, the system default
first with “(Default)” (“預設” in Traditional Chinese), an alphabetical, bundle-id
deduplicated list, and **Other…** opening an Applications app picker.

Terminal discovery uses Launch Services **unix executable**, plus installed known
terminals. Editor discovery unions **source code / plain text**, plus installed
known editors. Choices persist by bundle id, with a saved path for Other apps.
No selection means the system default; a missing terminal falls back to Terminal,
and a missing editor to an available editor.

Launch recipes live in one table in `Kioku/Actions.swift`:
- Ghostty: `open -na <app> --args --working-directory=<cwd> -e <argv…>`.
  Verified against [Ghostty's command / initial-command / working-directory reference](https://ghostty.org/docs/config/reference).
- Terminal, iTerm and generic terminal fallback: `open -a <app> <temporary.command>`.
  The private, self-removing script quotes every argument, changes cwd, then execs
  the harness. Grok has no resume CLI; it opens a login shell in its project.
- Editors: `open -a <app> <cwd>`. Harness commands must be on the chosen terminal's PATH.

**Debug-only integration overrides:** `KIOKU_TERMINAL_LAUNCHER` /
`KIOKU_EDITOR_LAUNCHER` replace **open**, preserving its exact argv and cwd.
`KIOKU_EDITOR` selects an editor executable; `KIOKU_SYSTEM_TERMINAL` /
`KIOKU_SYSTEM_EDITOR` inject a default app URL; `KIOKU_ENGINE` selects a test
engine. All are compiled out with `#if DEBUG`. Release uses only the bundled
kioku engine, `/usr/bin/open`, and native/persisted app choices.

## Desktop fluid-interface pass

| Principle | Implementation / deliberate omission |
|---|---|
| Response | Mouse-down row selection and pressed button/capsule feedback; Resume on mouse-up. 30 ms debounce; keep old results/preview until replacement arrives. |
| Interruptibility | Every engine call has task cancellation, SIGTERM and a 200 ms SIGKILL backstop. Generation/ref checks reject stale data; selection requests coalesce for 25 ms. No synchronous process wait on the main thread. |
| Springs | `CASpringAnimation(perceptualDuration: 0.35, bounce: 0)`. Retarget opacity/offset from presentation values; replace animations rather than queue them. |
| Spatial consistency | Native button-origin menus; conversation changes cross-fade snapshots with ±6 pt travel, entering/exiting along the same direction. Search replacements cross-fade without directional movement. |
| Materials | macOS 26 `NSGlassEffectView` and grouped `NSGlassEffectContainerView`; older `NSVisualEffectView`. Translucent, behind-window reading panes; glass toolbar/filter/hit capsules, including a tinted Resume split button. Full-size content runs under the unified toolbar for AppKit's native edge treatment. The iOS-style scroll-edge API is not exposed by this AppKit SDK. |
| Accessibility | Observe display-options notification live. Reduced motion: 120 ms cross-fade, no spring/offset. Reduced transparency: opaque panes and chrome. Increased contrast: solid fills and defined borders. Pixel assertions check that the reading panes stop transmitting the backdrop. |
| Haptics | Native level-change feedback only on valid Resume/copy, in their visual action frame. Physical feedback requires supported hardware; not verified on this Mac mini. |
| Typography | Dense system 13 pt conversation text, 12 pt two-line snippets, 11 pt metadata, small-cap role tags; monospaced digits for ages/counts. **skipped:** large-title tracking, there are no large display titles in this dense reader. |
| Forgiveness | Empty/no-results states, first-scan status, inline errors with rebuild/source-path fixes and Retry indexing; standard shortcuts. |
| Restraint | Only pressed-state and content-change motion. **skipped:** decorative/bouncy/gesture-momentum effects; keyboard selection carries no physical momentum. |

In Debug, `KIOKU_REDUCE_MOTION=1`, `KIOKU_REDUCE_TRANSPARENCY=1`, and
`KIOKU_INCREASE_CONTRAST=1` force the same application-owned paths as the system
settings. These overrides and `KIOKU_APPEARANCE` are compiled out of Release,
which always follows macOS accessibility and appearance settings. The resolved
mode is the window's accessibility value, not a fake test element.
Framework-owned menus follow macOS's own accessibility settings in both builds.

## E2E only

```sh
SDKROOT="$(xcrun --sdk macosx --show-sdk-path)" test/e2e.sh
app/test/e2e.sh
app/test/release-overrides.sh
git checkout -- test  # restore volatile CLI test artifacts
```

The app script builds app + XCUITest runner, creates a synthetic HOME with
`test/fixture.sh`, passes all source/index overrides, then runs real UI tests.
Terminal/editor launcher stubs record app path, exact argv and cwd; terminal
.command execution reaches a harmless fixture harness recorder.
A synthetic Ghostty app exercises the verified launch-argv recipe, not real Ghostty.
No real editor/terminal/paid coding agent is invoked by the suite.

Window screenshots are XCTest attachments, exported to `app/test/screens/` with stable names such as `mode-light-1000.png` (UUID originals are retained).
The report is `app/test/e2e-report.md`, with full logs / xcresult summary alongside.
Each app launch uses a unique recorder directory: macOS can let the runner read
fixture output while denying deletion, so ignored cleanup errors must not reuse
stale launch records.
Volatile outputs are ignored and existing CLI outputs are restored before commits.

UI assertions and budgets:
- first results: **2 seconds**, including typing and accessibility observation;
- **20** 紅 → 紅茶 races, final list must omit the 紅-only fixture;
- **30** rapid down arrows: final index 30 and correct preview, **30 seconds**
  including event synthesis, with **2 seconds** to settle after the final event;
- screenshots and actual window mode values for default / solid / contrast /
  reduced-motion launches;
- fresh 800 / 1000 / 1400 pt launches in **both light and dark**, asserting exact
  window widths rather than dragging; toolbar/traffic-light separation,
  containment and non-overlap, full chip/Resume text and padding;
- 94 pt rows with 96 pt pitch; aligned title/snippet/role columns and gutter dots,
  actual two-line tail truncation, role labels immediately below the snippet;
- native glyph/control-cell bounds: ≥8 pt text insets, ≥12 pt pane-edge and title
  side insets, visible ellipses instead of clipping; longest CJK and unbroken
  Latin fixtures at every width/appearance; header, folds and message padding;
- equal-height toolbar glass capsules, spacing including system paint outsets,
  and a two-line results header that keeps project/mode titles untruncated;
  narrow toolbar titles shrink before actions;
- horizontal filter overflow and native scrolling, with zero-count agents hidden;
- screenshot pixel checks distinguish translucent reading panes from opaque
  Reduce Transparency / Increase Contrast fallbacks.

Material tests opt into `KIOKU_TEST_BACKDROP=1` with the fixture HOME: a
**DEBUG-only app-owned borderless child window**, ordered immediately below
Kioku, supplies a fixed high-contrast wallpaper. Its window, opaque canvas and
gradient resize with Kioku; tests verify their actual visibility and dimensions
before sampling. Thresholds remain >0.03 for transmission and <0.02 for solid
fallbacks, independent of the user's desktop. Release builds exclude the hook.
`KIOKU_APPEARANCE=light|dark` provides deterministic appearance for these tests.
Fixture-only DEBUG `KIOKU_TEST_WINDOW_WIDTH` applies the requested frame after
AppKit installs the visible toolbar. `KIOKU_TEST_TEXT_LAYOUT=1` publishes actual
TextKit glyph/control-cell measurements after layout, outside accessibility getters.
Each audit waits for the requested query, width, and generation in the window's
accessibility value before reading that generation's immutable measurements.
Conversation waits also require current-query/current-generation readiness, not
merely text left over from a previous query. Keyboard tests wait for the actual
search field editor to own focus, then verify the exact query. Native Command-F
selects the existing query in one responder action; tests do not assume a separate
Command-A succeeded. Fixture Settings windows are placed natively at x=100,
not pointer-dragged. Release excludes these diagnostic and positioning hooks.
Stress captures use
`text-stress-{light,dark}-{800,1000,1400}.png`.
Host-owned permission panels may still occlude screenshots; tests do not answer
those permission requests; a capture is not guaranteed to be unobstructed.

The CLI E2E check holds a real SQLite writer lock while the bundled engine opens
a connection. CJK auto-extension initialization installs the existing five-second
lock policy before its first SQL prepare (the Go driver's handler arrives too
late); contention must not be misreported as missing FTS5. No test retries.

### Verification host

Local development used macOS **27.0.1 (26A434)**, Xcode **26.0**, and its
macOS 26 SDK. XCUITest requires macOS's **Enable UI Automation** authorization;
without it, the report records execution as BLOCKED, not passed.

Verification output is **local, gitignored run output**, not evidence shipped in
this checkout. From the repository root, run `app/test/e2e.sh` to regenerate
`app/test/e2e-report.md`, `app/test/screens/ui-tests.log`, the xcresult summary,
and screenshot attachments. The emitted counts are authoritative; fixture and
build checks are counted separately from actual XCTest cases. Copy each run's
report and log before starting another run, which replaces those artifacts.

Run `app/test/release-overrides.sh` to build Debug and Release and check their
Mach-O strings: Debug must retain all 10 integration/accessibility/appearance
keys, and optimized and unoptimized Release must contain no `KIOKU_*` override
keys. The unoptimized Release check uses the same compilation conditions and
catches short literals that optimized Swift may encode in instructions. Its logs and string
dumps are retained in the printed local proof directory and can be regenerated
from a fresh checkout with the same command.

Real Ghostty, Terminal, iTerm and editor launches are not exercised.
The suite uses harmless recorder stubs, verifies exact argv/cwd, and checks
persisted choices and missing-terminal fallbacks. Physical haptics and the
macOS 14/15 material fallback are not verified on this host.

## Round-1 limits

- **skipped:** result pagination beyond 200; counts are full-engine totals and
  the UI labels truncated pages. Project choices come from 200 recent sessions.
- **skipped:** loading the entire conversation beyond ±12 turns, Markdown/code
  rendering, editing, account/sync/cloud features. This is a local reader/resumer.
- Highlight offsets reuse the Go TUI's display matcher (not a Swift parser);
  orthographic-only tokenizer matches may lack word-level pen offsets.
- **skipped:** iOS/iPadOS and export. The previous attempt remains in the
  local `ios-attempt` stash; it was not applied or deleted.
