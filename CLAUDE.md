# lazyexplorer

A small, lazygit-style terminal file explorer. Go + Bubbletea v2 + lipgloss v2.

## Stack & v2 API notes

Built on the **charmbracelet v2 ecosystem**: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`,
markdown via the fork `github.com/nguyenvanduocit/glamour/v2`, syntax highlight via
`chroma/v2`. Key v2 differences from v1 (bite future edits):

- `View()` returns `tea.View` (struct), not `string` — content in `.Content`. Alt-screen and
  mouse are fields on the View (`v.AltScreen`, `v.MouseMode = tea.MouseModeCellMotion`), not
  `tea.NewProgram` options.
- Mouse is an **interface** (`tea.MouseClickMsg`/`MouseReleaseMsg`/`MouseWheelMsg`/`MouseMotionMsg`,
  each with `.Mouse() tea.Mouse{X,Y,Button}`) — no `.Action` field; the action is the message type.
- Keys: `tea.KeyPressMsg{Code rune, Text string}` — no `.Runes`. `Init()`/`Update()` signatures
  unchanged.
- lipgloss v2 removed `SetColorProfile`: styles render full truecolor; downsampling happens at
  the output writer. Detect background via `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)`.
- lipgloss v2 `.Width(n)` is the **total outer width** of a styled block — border + padding
  **included**, not the content width. A bordered/padded box's usable text area is therefore
  `n - GetHorizontalFrameSize()`. When sizing a box from an inner (text) width, pass
  `inner + frame` to `.Width()`; passing the inner width alone shrinks the text area by the
  frame and silently **wraps** the widest rows (the box also ends up `frame` cols too narrow).
  This bit `renderModal`/`modalBoxStyle` — `modalSize` returns the inner width, so renderModal
  passes `bw + GetHorizontalFrameSize()` to `.Width()`. Probe before guessing:
  `lipgloss.Width(style.Width(n).Render("x"))` tells you exactly what `n` maps to.
- lipgloss v2 `Canvas`/`Compositor` overlays are **opaque at the cell level**: a top layer's
  cells — even space cells with no background — overwrite the layer below, so the background
  does **not** bleed through a box's interior. A floating box thus needs **no** background fill
  to hide what's behind it (`overlayCentered` relies on this). An opaque fill only sets the
  box's *color*; a fill that differs from the terminal/pane background reads as a distinct
  panel inside the border (looks "double-framed"). For a box that floats cleanly on the app
  (crush's look), use **border only, no `Background`** — the interior then matches the terminal.

## Goal & Positioning

lazyexplorer is purpose-built for the **vibe-code workflow**: the user keeps it open
in a terminal pane *beside* an agent (Claude Code / coding agent) and uses it to glance
at, navigate, and lightly manage the project tree while the agent works.

[superfile](https://github.com/yorukot/superfile) shares the same broad goal (a TUI file
manager). We are **not** a clone or fork of it, and we do not copy its code. Our scope is
deliberately **smaller and focused on simplicity** — superfile is a full-featured manager;
lazyexplorer is a lightweight companion.

Because it lives next to an agent in a cramped terminal, the **UI must stay simpler than
superfile's**: fewer panels, fewer modes, fewer keybinds, minimal chrome. When a feature
would add UI complexity, prefer leaving it out. This simplicity governs the *surface only* —
the engineering beneath it is held to the opposite standard (see Design Principles).

## Design Principles

- **Simple in function, uncompromising in engineering.** Simplicity is a constraint on
  *scope* — fewer panels, modes, keybinds — never on craft. The code behind that small
  surface must be excellent: deep, considered, robust, heavily reasoned. A small UI *raises*
  the engineering bar rather than lowering it — every line is load-bearing and there is
  nowhere to hide a shortcut. Choose the hard-won correct design over the quick one, every
  time; pay down complexity at the root rather than patching around it.
- **Simpler UI than superfile.** Every added panel, mode, or keybind must earn its place.
  Default answer to "should we add this to the UI?" is no.
- **Glance-friendly.** Optimized for quick looks beside an agent, not for being the primary
  workspace. Two panels (list + preview) is the ceiling, not a starting point.
- **Jailed to a root.** Navigation descends below the launch directory but never above it
  (see `withinRoot` in `fs.go`). This keeps it scoped to the project the agent is editing.
- **Keyboard + mouse.** lazygit-style keys, plus click/drag/wheel for the mouse crowd.

## Layout

- `main.go` — entrypoint; resolves the jail root, starts the Bubbletea program.
- `model.go` — Elm-architecture model: state, `Update`, navigation, mouse handling, modes,
  and the async preview pipeline (`syncPreview`/`applyPreview` + gen-counter stale guard).
- `view.go` — rendering + `layout()` geometry (shared by render and mouse hit-testing).
- `fs.go` — filesystem: dir listing, root-jail guard, and the **preview-renderer registry**
  (`previewRenderer`/`previewRenderers`/`rendererFor`) — markdown (glamour), code (chroma),
  image (metadata scaffold); add a file type = one registry entry.
- `theme.go` — lipgloss palette and styles (one accent, restrained).

Preview rendering is **async** (off the `Update` goroutine via `tea.Cmd`) so a slow renderer
never freezes the UI; the design rationale lives in `docs/adr-async-markdown-render.md` and
`docs/adr-preview-renderer-registry.md`.

## Reference Code (`./tmp`)

`./tmp/` holds full clones of high-quality Go/TUI projects, kept purely as **reading
reference**. They are *not* dependencies and *not* compiled into lazyexplorer (real deps
live in `go.mod`: `bubbletea/v2`, `lipgloss/v2`, the `glamour/v2` fork, `chroma/v2`). The
`tmp/` clones are mostly v1-era — read them for patterns/idioms, but match our v2 API (see
Stack & v2 API notes). When a need arises — "how do good
TUIs do X?" — read the relevant clone before inventing an approach. Borrow **patterns and
idioms**, never copy code wholesale, and always filter through our simplicity ethos: these
projects are mostly bigger and more featured than we want to be.

When the task is to match a clone's **look** ("make X look like crush"), read the actual style
definition in the clone source — do **not** rely on a subagent's prose summary. Summaries drop
load-bearing details: e.g. crush's command palette is `Dialog.View = base.Border(RoundedBorder).
BorderForeground(primary)` with **no `Background`** (`tmp/crush/internal/ui/styles/quickstyle.go`),
border-only — a summary that says "rounded border" but omits "no background fill" sends you to
add an opaque panel that looks nothing like the original. Verify the chrome (border, background,
padding) and the exact colors from the `.NewStyle()` chain itself.

| Clone | Upstream | Consult it for |
| --- | --- | --- |
| `tmp/lipgloss` | charmbracelet/lipgloss | Styling, layout primitives, `JoinHorizontal/Vertical`, borders, padding — our styling dep's own source. |
| `tmp/glow` | charmbracelet/glow | Markdown reading TUI built on glamour — preview rendering, viewport, paging, file-watching patterns. |
| `tmp/crush` | charmbracelet/crush | Coding-agent TUI; reference for height/tab/width rendering, multi-pane layout, theming (see [[reference_crush_tui]]). |
| `tmp/gh-dash` | dlvhdr/gh-dash | Rich dashboard TUI — sections, keybind/help system, config-driven views, list rendering. |
| `tmp/superfile` | yorukot/superfile | The full-featured file manager we deliberately stay *smaller* than — file ops, navigation, preview ideas. Cherry-pick, don't clone. |
| `tmp/bubblezone` | lrstanley/bubblezone | Mouse zone tracking / hit-testing for clickable regions in Bubbletea — relevant to our mouse handling. |
| `tmp/ntcharts` | NimbleMarkets/ntcharts | Terminal charts (canvas, bar, line, heatmap) on Bubbletea — only if we ever visualize data. |
| `tmp/harmonica` | charmbracelet/harmonica | Spring-physics animation for smooth motion — only if we add easing/animated transitions. |

## Docs sync (`docs/`)

`docs/` holds the living specs — PRD, ADR, AC/Gherkin, task breakdowns (format governed by
`docs/CLAUDE.md`). They are the source of truth for *intended* behavior, so they track the code.

Every code change reviews the relevant docs in the same pass. If the change touches behavior,
state, a signature, or a `file:line` a doc describes, update that doc in the same commit so spec
and code never drift:

- A PRD describing **shipped** code matches it exactly.
- A still-**draft** PRD never instructs building something the code no longer has.
- When a change invalidates a doc's design, surface it and reconcile in the same pass.

The "why we changed from A to B" lives in git history or an ADR's *Hệ quả* section, never inlined
into a living spec (see Positive framing in `docs/CLAUDE.md`).

## Testing

Engineering excellence is *proven by tests*, not asserted. Two non-negotiables:

- **Test-Driven Development, always.** Write the failing test first, run it, watch it fail
  for the right reason, then write the minimum code to reach green, then refactor. Production
  code without a test that drove it is incomplete.
- **Cover every layer.** Each layer earns its own tests:
  - `fs.go` — root-jail guard, dir listing + sort order, file/dir preview, binary/size handling.
  - `model.go` — `Update` transitions, navigation, mouse hit-testing, the poll loop, modes.
  - `view.go` — `layout()` geometry, rendering, width-fitting, render-routine consistency.
  - integrated — the whole program via teatest for multi-step interaction flows.

### Technical tests

Pure unit tests on `Update`/`View`, golden snapshots, and teatest for flows. The **`/test`
skill is the source of truth**: it carries the level-picker (which mechanism for which job)
and the project-specific pitfalls (poll-loop `tickCmd`, teatest v1 import path, color-profile
determinism). Invoke `/test` before writing tests.

### UI tests — render to image, evaluate with an agent

String assertions cannot see alignment, color, spacing, or truncation the way a human does.
So **besides** technical tests, render the TUI **to an image** and have an **agent evaluate**
it against the intended design:

1. Capture rendered output — `View()` or a live session → image (e.g. `charmbracelet/freeze`
   for ANSI→PNG, or `charmbracelet/vhs` for a `.tape`→PNG/GIF).
2. An agent inspects the image and returns a structured visual verdict (pass/fail + reasons)
   against the design intent — e.g. the `oh-my-claudecode:visual-verdict` skill.
3. Treat a failed visual verdict like a failed assertion: fix, re-render, re-evaluate.

### Gate

Every change passes before it is "done":

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

plus the relevant UI visual verdict whenever the change touches rendering.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **lazyexplorer** (739 symbols, 2262 relationships, 62 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> If any GitNexus tool warns the index is stale, run `npx gitnexus analyze` in terminal first.

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `gitnexus_impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `gitnexus_detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `gitnexus_query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `gitnexus_context({name: "symbolName"})`.

## When Debugging

1. `gitnexus_query({query: "<error or symptom>"})` — find execution flows related to the issue
2. `gitnexus_context({name: "<suspect function>"})` — see all callers, callees, and process participation
3. `READ gitnexus://repo/lazyexplorer/process/{processName}` — trace the full execution flow step by step
4. For regressions: `gitnexus_detect_changes({scope: "compare", base_ref: "main"})` — see what your branch changed

## When Refactoring

- **Renaming**: MUST use `gitnexus_rename({symbol_name: "old", new_name: "new", dry_run: true})` first. Review the preview — graph edits are safe, text_search edits need manual review. Then run with `dry_run: false`.
- **Extracting/Splitting**: MUST run `gitnexus_context({name: "target"})` to see all incoming/outgoing refs, then `gitnexus_impact({target: "target", direction: "upstream"})` to find all external callers before moving code.
- After any refactor: run `gitnexus_detect_changes({scope: "all"})` to verify only expected files changed.

## Never Do

- NEVER edit a function, class, or method without first running `gitnexus_impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `gitnexus_rename` which understands the call graph.
- NEVER commit changes without running `gitnexus_detect_changes()` to check affected scope.

## Tools Quick Reference

| Tool | When to use | Command |
|------|-------------|---------|
| `query` | Find code by concept | `gitnexus_query({query: "auth validation"})` |
| `context` | 360-degree view of one symbol | `gitnexus_context({name: "validateUser"})` |
| `impact` | Blast radius before editing | `gitnexus_impact({target: "X", direction: "upstream"})` |
| `detect_changes` | Pre-commit scope check | `gitnexus_detect_changes({scope: "staged"})` |
| `rename` | Safe multi-file rename | `gitnexus_rename({symbol_name: "old", new_name: "new", dry_run: true})` |
| `cypher` | Custom graph queries | `gitnexus_cypher({query: "MATCH ..."})` |

## Impact Risk Levels

| Depth | Meaning | Action |
|-------|---------|--------|
| d=1 | WILL BREAK — direct callers/importers | MUST update these |
| d=2 | LIKELY AFFECTED — indirect deps | Should test |
| d=3 | MAY NEED TESTING — transitive | Test if critical path |

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/lazyexplorer/context` | Codebase overview, check index freshness |
| `gitnexus://repo/lazyexplorer/clusters` | All functional areas |
| `gitnexus://repo/lazyexplorer/processes` | All execution flows |
| `gitnexus://repo/lazyexplorer/process/{name}` | Step-by-step execution trace |

## Self-Check Before Finishing

Before completing any code modification task, verify:
1. `gitnexus_impact` was run for all modified symbols
2. No HIGH/CRITICAL risk warnings were ignored
3. `gitnexus_detect_changes()` confirms changes match expected scope
4. All d=1 (WILL BREAK) dependents were updated

## Keeping the Index Fresh

After committing code changes, the GitNexus index becomes stale. Re-run analyze to update it:

```bash
npx gitnexus analyze
```

If the index previously included embeddings, preserve them by adding `--embeddings`:

```bash
npx gitnexus analyze --embeddings
```

To check whether embeddings exist, inspect `.gitnexus/meta.json` — the `stats.embeddings` field shows the count (0 means no embeddings). **Running analyze without `--embeddings` will delete any previously generated embeddings.**

> Claude Code users: A PostToolUse hook handles this automatically after `git commit` and `git merge`.

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
