# lazyexplorer

A small, lazygit-style terminal file explorer. Go + Bubbletea v2 + lipgloss v2.

Purpose-built for the **vibe-code workflow**: keep it open in a terminal pane
beside your coding agent (Claude Code / similar) and use it to glance at,
navigate, and lightly manage the project tree while the agent works.

- Two panels (list + preview); preview handles markdown (glamour), source
  (chroma), and image metadata
- Keyboard (lazygit-style) + mouse (click, drag, wheel)
- Jailed to the launch directory — navigation can descend, never above
- Async preview rendering — never freezes on a big file

```bash
go build -o lazyexplorer .
./lazyexplorer           # explores cwd
./lazyexplorer ./src     # explores ./src as the jail root
```

## Copying content

- `Y` copies the **whole** previewed file's raw text to the clipboard — the clean
  source, never the colorized markdown/diff render — so it pastes straight into the
  agent's chat. (`y` copies the file's project-relative path; the two are distinct.)
- To grab just a **visible span**, let the terminal select it natively: hold **Shift**
  (or **Option** on iTerm2 / macOS Terminal / tmux-on-macOS) while you drag the mouse.
  The terminal owns that selection — lazyexplorer adds no in-app copy mode.

For design rationale and contributor docs, see `docs/` and `CLAUDE.md`.

---

## Telemetry (opt-in)

lazyexplorer can emit lifecycle events to a Datadog Logs HTTP intake endpoint
for teams who want to see runtime behaviour beside the CI quality gates
(`docs/prd-datadog-integration.md`). Telemetry is **off by default** and
controlled entirely by environment variables — there is no UI surface, no
panel, no keybind. Paths and filenames never leave your machine: the
recorder ships a redacted extension class (`.md`, `.go`, `(noext)`) and a
jail-relative depth count, never raw paths (PRD §5.4 / D5).

### Environment variables

| Variable | Default | Effect |
|----------|---------|--------|
| `LE_TELEMETRY` | unset (off) | `1` / `true` / `yes` (case-insensitive) turns telemetry on. Anything else keeps it off. |
| `DD_API_KEY` | — | Required when telemetry is on. If missing, lazyexplorer prints one stderr line and runs unchanged. |
| `DD_SITE` | `datadoghq.com` | Datadog site. Use `datadoghq.eu`, `us3.datadoghq.com`, etc. for other regions. |
| `DD_LOGS_URL` | derived from `DD_SITE` | Full URL override for sandbox / non-default intake. |

### What gets sent

Five event names cover the session lifecycle:

| Event | Fired when | Fields |
|-------|-----------|--------|
| `session.start` | Once at startup | version, go_version, os, arch, term, color_profile |
| `view.change` | Cursor moves to a new entry | entry_kind (`file`/`dir`/`parent`), ext_class, cwd_depth |
| `action.preview_rendered` | Preview render completes | renderer, width, lines, duration_ms |
| `error.render_fail` | Preview renderer errors | renderer, error_class (`glamour`/`chroma`/`io`/`other`) |
| `session.end` | Process exit | duration_ms, views_total, renders_total, errors_total, dropped |

Every event carries `service: lazyexplorer-tui`, `ddsource: lazyexplorer`, a
per-session UUID, a hostname hash (sha256 prefix — never the raw hostname),
and the build version.

### Guarantees

- **Zero overhead when off.** With `LE_TELEMETRY` unset, lazyexplorer spawns
  no extra goroutine, opens no socket, makes no syscall. The rendered TUI
  is byte-for-byte identical to a build without the telemetry code
  (`byte_identity_test.go` pins this against drift).
- **Never blocks the UI.** The recorder is a non-blocking enqueue; a full
  channel drops the event (and counts it in `dropped`) rather than stalling
  a keystroke. Quit honours a one-second flush cap — telemetry can never
  extend exit past that.
- **No raw paths or filenames on the wire.** Enforced by the redaction
  helpers (`extClass`, `cwdDepth`, `errorClass`, `hostnameHash`) and a
  serializer-level invariant test (`TestSerializerNeverLeaksPath`) that
  regex-scans every payload for path / "secret" / "password" / "key="
  patterns.

### Quick verification

```bash
LE_TELEMETRY=1 DD_API_KEY=your-key DD_SITE=datadoghq.com ./lazyexplorer
# navigate a few files, then q to quit — events flush within 1s
# query in Datadog Logs Explorer: service:lazyexplorer-tui
```
