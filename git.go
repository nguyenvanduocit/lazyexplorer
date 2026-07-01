package main

// git.go — the git change-state layer (PRD docs/prd-git-change-indicator.md §5.1).
//
// This is a pure-data layer: it shells out to the `git` CLI, parses the output,
// and hands the model a gitState (a map of repo-relative path → change). It
// holds NO lipgloss/view concern — badge glyphs map here (code.badge()), but
// color and layout live in theme.go/view.go. The collection runs off the Update
// goroutine via a tea.Cmd (see model.go), mirroring the async preview pipeline,
// so a slow `git` on a huge repo never freezes a keystroke or the poll loop.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// gitCmdTimeout caps each git exec so a huge or lock-contended repo degrades
	// (FR10) instead of outliving the in-flight guard and stacking goroutines.
	gitCmdTimeout = 2 * time.Second

	// maxUntrackedScan caps how many untracked files we line-count per refresh.
	// A fresh repo (or un-gitignored build output) can hold thousands of untracked
	// files; reading every one each tick would defeat the "cheap glance" goal.
	// Past the cap the remaining untracked files still show the "?" badge, just
	// without a "+N" count. Mirrors maxWalkEntries (fs.go) as a defensive ceiling.
	maxUntrackedScan = 2000
)

// gitCode is the single collapsed change-type for a path (working-tree vs HEAD),
// distilled from porcelain's two-char XY status (PRD D6 — staged/unstaged not
// distinguished). The iota order is not meaningful; badge()/gitColor() switch on it.
type gitCode int

const (
	gitModified  gitCode = iota // M — amber
	gitUntracked                // ? — green
	gitAdded                    // A — green
	gitDeleted                  // D — red
	gitRenamed                  // R — amber
	gitConflict                 // ! — red (unmerged)
)

// badge is the one-glyph marker drawn in the listing row's right column.
func (c gitCode) badge() string {
	switch c {
	case gitUntracked:
		return "?"
	case gitAdded:
		return "A"
	case gitDeleted:
		return "D"
	case gitRenamed:
		return "R"
	case gitConflict:
		return "!"
	default: // gitModified
		return "M"
	}
}

// gitChange is the change-state of one path: its collapsed code plus the line
// delta vs HEAD. hasDelta separates "0 lines changed" from "delta unknown"
// (binary, or a numstat we could not parse).
type gitChange struct {
	code     gitCode
	added    int
	deleted  int
	hasDelta bool
}

// deltaString renders the diffstat, omitting a zero side: "+41 -3" / "+88" /
// "-54" / "" (no delta known). The view renders it muted (dimStyle) so the
// colored badge stays the focal point (D12).
func (g gitChange) deltaString() string {
	if !g.hasDelta {
		return ""
	}
	parts := make([]string, 0, 2)
	if g.added > 0 {
		parts = append(parts, "+"+strconv.Itoa(g.added))
	}
	if g.deleted > 0 {
		parts = append(parts, "-"+strconv.Itoa(g.deleted))
	}
	return strings.Join(parts, " ")
}

// gitState is the snapshot the model holds and the view reads each frame.
//   - repoRoot == "" ⇒ not a git repo (git mode OFF, D10); changes/dirtyDirs nil.
//   - changes: keyed by repo-root-relative slash path; the authoritative badge set.
//   - dirtyDirs: every ancestor directory (repo-rel slash) of a changed path, so a
//     folder roll-up (●) is an O(1) lookup at render instead of an O(n) map scan.
//   - ignored: every git-ignored entry, keyed by repo-rel slash (a directory record
//     is the collapsed dir key, slash stripped). The list pane dims + sinks these
//     (prd-ignored-entry-dimming §5.1); looked up via isIgnored's ancestor-walk.
type gitState struct {
	repoRoot  string
	changes   map[string]gitChange
	dirtyDirs map[string]bool
	ignored   map[string]bool
}

// isIgnored reports whether a repo-relative slash path is git-ignored: true when the
// path itself OR any ancestor directory is in the ignored set (D7). The collapsed
// "!! tmp/" record means "tmp and everything beneath it", so an entry inside an
// ignored dir — whose own path is not a literal record — still resolves ignored. It
// is the inverse walk of markAncestors. A nil map (git primed, no refresh landed yet)
// reads as not-ignored, which is safe.
func (s gitState) isIgnored(rel string) bool {
	if s.ignored[rel] {
		return true
	}
	for {
		i := strings.LastIndexByte(rel, '/')
		if i < 0 {
			return false
		}
		rel = rel[:i]
		if s.ignored[rel] {
			return true
		}
	}
}

// runGit execs `git -C dir <args>` with a timeout, returning stdout. stderr is
// discarded — callers only care about exit status + stdout (FR10: a git error is
// a degrade signal, never surfaced as a frame error).
func runGit(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// detectRepoRoot returns the absolute toplevel of the git repo containing dir, or
// "" when dir is not inside a repo (or git is unavailable). Called once at startup
// — the jail root is fixed, so the repo root never changes for the session.
func detectRepoRoot(dir string) string {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// untrackedStat caches one untracked file's line count keyed by its on-disk
// identity (mtime+size). On the next refresh a cache hit skips re-reading the
// file: the steady-state untracked-scan cost drops from a full read (up to
// maxPreviewBytes per file) to a single stat. ok=false caches "binary/unreadable"
// so such a file is not re-read every tick either.
type untrackedStat struct {
	mtime int64
	size  int64
	lines int
	ok    bool
}

// untrackedCache maps a repo-relative slash path to its cached line count. The
// model holds it between refreshes and threads the prior snapshot into
// gitRefreshCmd; the async goroutine only READS that prior cache and returns a
// freshly built one, so the map is never mutated across goroutines (see
// model.gitUntrackedCache and the apply path in Update).
type untrackedCache map[string]untrackedStat

// collectGitState gathers the full change snapshot for a repo. It is the body of
// the async tea.Cmd. `prev` is the untracked line-count cache from the last
// refresh; collectGitState returns the refreshed cache alongside the snapshot so
// the model can thread it into the next dispatch. Resilience (FR10): any exec
// failure degrades that step rather than the whole state — a failed `git status`
// yields empty changes (no badges) and preserves `prev` for the next attempt, and
// a failed numstat (e.g. a no-commit repo where `diff HEAD` aborts) leaves badges
// intact with no deltas (per-command independence).
func collectGitState(repoRoot string, prev untrackedCache) (gitState, untrackedCache) {
	st := gitState{repoRoot: repoRoot, changes: map[string]gitChange{}, dirtyDirs: map[string]bool{}, ignored: map[string]bool{}}
	if repoRoot == "" {
		return st, prev
	}

	// 1. Status drives the badges. -uall expands untracked DIRECTORIES into their
	//    individual files (default collapses a new folder to one "?? sub/" record,
	//    which would leave the folder ● dark and its files unbadged).
	statusOut, err := runGit(repoRoot, "status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return st, prev // degrade: keep the cache for the next attempt
	}
	parseStatus(statusOut, st.changes)

	// 2. Line deltas, HEAD-aware: `git diff HEAD` aborts on a repo with no commits,
	//    so fall back to `git diff --cached` there. Either failing leaves badges
	//    intact (per-command independence) — numOut stays empty, no deltas applied.
	var numOut []byte
	if _, herr := runGit(repoRoot, "rev-parse", "--verify", "-q", "HEAD"); herr == nil {
		numOut, _ = runGit(repoRoot, "diff", "HEAD", "--numstat", "-z")
	} else {
		numOut, _ = runGit(repoRoot, "diff", "--cached", "--numstat", "-z")
	}
	applyNumstat(numOut, st.changes)

	// 3. Untracked files have no numstat row → count their lines directly (capped,
	//    cache-backed so unchanged files are not re-read).
	next := countUntracked(repoRoot, st.changes, prev, maxUntrackedScan)

	// 4. Roll-up: mark every ancestor dir of a change so folder rows can show ●.
	for k := range st.changes {
		markAncestors(k, st.dirtyDirs)
	}

	// 5. Ignored set (prd-ignored-entry-dimming D1): a SEPARATE status WITHOUT -uall,
	//    so an ignored directory collapses to one "!! dir/" record instead of expanding
	//    to every file under it (the -uall call above would yield hundreds of file rows
	//    and no directory key). Per-command degrade (FR9): a failure leaves `ignored`
	//    empty — nothing dimmed, nothing reordered — and never blanks the change state.
	if ignOut, ierr := runGit(repoRoot, "status", "--porcelain=v1", "-z", "--ignored"); ierr == nil {
		parseIgnored(ignOut, st.ignored)
	}
	return st, next
}

// parseIgnored picks the "!! path" records from `git status --porcelain -z --ignored`
// (run WITHOUT -uall, D1) into the ignored set, keyed by repo-relative slash path. A
// directory record carries a trailing slash ("!! tmp/") that is stripped so the key
// matches the changes/dirtyDirs key space (and isIgnored's ancestor-walk). The regular
// change records the same stream also emits (" M …", "?? …") are skipped — only "!!".
func parseIgnored(data []byte, ignored map[string]bool) {
	for _, f := range splitNUL(data) {
		if len(f) < 4 || f[0] != '!' || f[1] != '!' { // "!! " + ≥1 path char
			continue
		}
		p := strings.TrimSuffix(f[3:], "/") // skip the "!! " prefix; drop a dir's trailing slash
		ignored[filepath.ToSlash(p)] = true
	}
}

// splitNUL splits NUL-separated git -z output, dropping the trailing empty field
// after the final NUL.
func splitNUL(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := strings.Split(string(data), "\x00")
	if n := len(s); n > 0 && s[n-1] == "" {
		s = s[:n-1]
	}
	return s
}

// parseStatus parses `git status --porcelain=v1 -z -uall` into changes keyed by
// repo-relative slash path. The -z record is "XY PATH" (paths unquoted); a
// rename/copy (R/C in either column) is followed by a SECOND NUL field holding
// the OLD path — we keep the NEW path (the one that exists on disk) and skip the
// old one. Unrecognized status pairs are ignored.
func parseStatus(data []byte, changes map[string]gitChange) {
	fields := splitNUL(data)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 { // "XY " + ≥1 path char
			continue
		}
		x, y := f[0], f[1]
		path := f[3:] // skip the "XY " prefix (two status chars + one space)
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++ // the next NUL field is the rename/copy source — consume + ignore it
		}
		if code, ok := collapseStatus(x, y); ok {
			changes[filepath.ToSlash(path)] = gitChange{code: code}
		}
	}
}

// collapseStatus distills porcelain's XY pair into one code (PRD D6 precedence):
// untracked → conflict → deleted → renamed → added → modified.
func collapseStatus(x, y byte) (gitCode, bool) {
	if x == '?' && y == '?' {
		return gitUntracked, true
	}
	// Unmerged: any U, or the symmetric AA/DD both-sides cases.
	if x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D') {
		return gitConflict, true
	}
	switch {
	case x == 'D' || y == 'D':
		return gitDeleted, true
	case x == 'R' || y == 'R':
		return gitRenamed, true
	case x == 'A' || y == 'A':
		return gitAdded, true
	case x == 'M' || y == 'M':
		return gitModified, true
	}
	return 0, false
}

// applyNumstat parses `git diff --numstat -z` and fills added/deleted on the
// matching (status-known) entries. Per-file record: "<add>\t<del>\t<path>". A
// binary file shows "-\t-\t<path>" → unparseable numbers → badge kept, no delta.
// A rename emits "<add>\t<del>\t" with an EMPTY path, then two more NUL fields
// (old, new); we attribute the delta to <new>. Deltas land only on paths the
// status pass already recorded, so status stays the authoritative badge source.
func applyNumstat(data []byte, changes map[string]gitChange) {
	fields := splitNUL(data)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		tab1 := strings.IndexByte(f, '\t')
		if tab1 < 0 {
			continue
		}
		rest := f[tab1+1:]
		tab2 := strings.IndexByte(rest, '\t')
		if tab2 < 0 {
			continue
		}
		addStr, delStr, path := f[:tab1], rest[:tab2], rest[tab2+1:]
		if path == "" { // rename: old + new follow as separate NUL fields
			if i+2 < len(fields) {
				path = fields[i+2] // new path
				i += 2
			} else if i+1 < len(fields) {
				path = fields[i+1]
				i++
			}
		}
		add, aerr := strconv.Atoi(addStr)
		del, derr := strconv.Atoi(delStr)
		if aerr != nil || derr != nil {
			continue // binary ("-") or garbage → keep badge, skip delta
		}
		key := filepath.ToSlash(path)
		if chg, ok := changes[key]; ok {
			chg.added, chg.deleted, chg.hasDelta = add, del, true
			changes[key] = chg
		}
	}
}

// countUntracked fills "+N" line counts for untracked files (badge "?") and
// returns the refreshed line-count cache. Each untracked path is stat'd; on a
// cache hit (same mtime+size as `prev`) the cached count is reused without
// re-reading the file. Only a fresh read (new or changed file) counts against
// `limit`, so steady-state refreshes do near-zero file I/O. A binary / unreadable
// file keeps its badge but gets no delta; an over-limit fresh file is left
// uncounted (and uncached) so a later tick under the limit can still pick it up.
func countUntracked(repoRoot string, changes map[string]gitChange, prev untrackedCache, limit int) untrackedCache {
	next := untrackedCache{}
	reads := 0
	for key, chg := range changes {
		if chg.code != gitUntracked {
			continue
		}
		full := filepath.Join(repoRoot, filepath.FromSlash(key))
		info, serr := os.Stat(full)
		if serr != nil || info.IsDir() {
			continue // unreadable or a dir: no delta, nothing to cache
		}
		mt, sz := info.ModTime().UnixNano(), info.Size()
		if c, hit := prev[key]; hit && c.mtime == mt && c.size == sz {
			next[key] = c // unchanged since last refresh → reuse the count, skip the read
			if c.ok {
				chg.added, chg.deleted, chg.hasDelta = c.lines, 0, true
				changes[key] = chg
			}
			continue
		}
		if reads >= limit {
			continue // over the read budget this tick: badge stays, no delta, retry next tick
		}
		reads++
		n, ok := countLines(full)
		next[key] = untrackedStat{mtime: mt, size: sz, lines: n, ok: ok}
		if ok {
			chg.added, chg.deleted, chg.hasDelta = n, 0, true
			changes[key] = chg
		}
	}
	return next
}

// countLines reads up to maxPreviewBytes of path and counts lines (newlines, +1
// for a final unterminated line). Returns ok=false for a directory, an
// unreadable file, or binary content — reusing fs.go's isBinary heuristic.
func countLines(path string) (int, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	buf := make([]byte, min64(info.Size(), maxPreviewBytes))
	n, _ := f.Read(buf)
	buf = buf[:n]
	if isBinary(buf) {
		return 0, false
	}
	if n == 0 {
		return 0, true // empty file: 0 lines
	}
	lines := bytes.Count(buf, []byte{'\n'})
	if buf[len(buf)-1] != '\n' {
		lines++
	}
	return lines, true
}

// diffFullContext is the -U (unified context) value diffHunks passes so the WHOLE
// file renders — every unchanged line kept as dim context around the coloured
// edits (prd-preview-diff-view FR1) — instead of git's default 3-line hunk window,
// which would truncate the file to a few lines around each change. A value larger
// than any realistic source file's line count collapses all hunks into one that
// spans the file; 1e9 sits well inside git's int32 -U arg and no real file
// approaches it. An UNCHANGED file still produces an empty diff (there is no
// context without a change), so the empty-diff → full-content fallback (D10/FR6)
// is unaffected by the larger window.
const diffFullContext = "1000000000"

// diffHunks fetches the unified diff of one repo-relative path against the same
// HEAD-aware base the +N/-N badge uses (prd-preview-diff-view FR2/D7), with
// FULL-FILE context (diffFullContext) so the whole file is shown — unchanged lines
// as dim context, the edits coloured in place — not a truncated hunk window.
// It colorizes each line by its leading character (D11) and returns preview lines
// with preStyled=true (verbatim ANSI, like code). On any failure OR an empty diff
// it returns (nil, false, err) — a non-nil error — so the syncPreview closure
// degrades to full content (FR6/FR10). repoRoot + relPath are captured from model
// state by the syncPreview closure, NOT passed through the registry render
// signature (D2). Each line is colored full-width; the preview pane windows it
// horizontally at render time, mirroring code (D9).
func diffHunks(repoRoot, relPath string) ([]string, bool, error) {
	// HEAD-aware base (D7): the same branch numstat uses (collectGitState above).
	// `git diff HEAD` aborts (exit 128) in a repo with no commits, so fall back to
	// `git diff --cached` there. --no-color forces plain output: a repo/global
	// `color.ui=always` (or color.diff=always) colorizes even into a pipe, which
	// would prefix every content line with git's own SGR escape and defeat our own
	// +/- colorization (D11/FR1). relPath goes after `--` so it can never be read as
	// a flag; the arg slice never touches a shell.
	var out []byte
	var err error
	if _, herr := runGit(repoRoot, "rev-parse", "--verify", "-q", "HEAD"); herr == nil {
		out, err = runGit(repoRoot, "diff", "--no-color", "-U"+diffFullContext, "HEAD", "--", relPath)
	} else {
		out, err = runGit(repoRoot, "diff", "--no-color", "-U"+diffFullContext, "--cached", "--", relPath)
	}
	if err != nil {
		return nil, false, err // FR10: any git failure → degrade to full content
	}
	if len(bytes.TrimSpace(out)) == 0 {
		// Empty diff despite an M/R badge (mode-only / whitespace-config change,
		// D10/FR6) → signal "no diff" so the caller falls back to full content.
		return nil, false, errEmptyDiff
	}

	// Expand tabs to previewTabWidth spaces BEFORE styling, so the diff's
	// indentation matches every other preview (normalizeText) instead of being
	// left to lipgloss's Render-time TabWidth (which defaults to 4 — a different,
	// shallower indent). Doing it on the source bytes (not at render) also keeps
	// the buffer tab-free for the Y/V copy path. CRLF is deliberately NOT touched
	// here: in a diff a trailing \r can be the change being shown (an EOL-only
	// edit), so stripping it would hide the hunk. Tab expansion never moves the
	// leading +/-/@ gutter byte the header/prefix logic keys on, so applying it to
	// the whole blob before the split is safe.
	expanded := strings.ReplaceAll(string(out), "\t", strings.Repeat(" ", previewTabWidth))
	raw := strings.Split(strings.TrimRight(expanded, "\n"), "\n")

	// The "diff --git"/"index"/"--- a/…"/"+++ b/…" preamble appears exactly once, before
	// the lone "@@" hunk header (diffHunks runs on a single path). Find it POSITIONALLY,
	// never by a "---"/"+++" prefix — a content line beginning '-'/'+' (a removed
	// `-- comment` renders `--- keep comment`) would be misread otherwise.
	hunkIdx := -1
	for i, line := range raw {
		if strings.HasPrefix(line, "@@") {
			hunkIdx = i
			break
		}
	}
	if hunkIdx < 0 {
		// No "@@" at all (defensive — a real unified diff always has one): dim it all
		// rather than guess a body split.
		lines := make([]string, 0, len(raw))
		for _, line := range raw {
			lines = append(lines, dimStyle.Render(line))
		}
		return lines, true, nil
	}
	body := raw[hunkIdx+1:]
	lines := make([]string, 0, len(raw))

	// Each line is styled with self-contained ANSI (open+close SGR per line), like
	// chroma's code output, so renderHWindow's horizontal slicing never cuts mid-escape
	// (FR8). Non-code files keep the git-CLI role coloring (D11): dim the preamble (incl.
	// the "@@" header), then colour each body line green/red/dim by its leading byte. The
	// leading +/-/space gutter byte is kept so the diff reads even without colour.
	name := filepath.Base(relPath)
	if matchLang(name) == "" {
		for _, line := range raw[:hunkIdx+1] {
			lines = append(lines, dimStyle.Render(line))
		}
		for _, line := range body {
			lines = append(lines, diffLineStyle(diffPrefix(line)).Render(line))
		}
		return lines, true, nil
	}

	// Source file: a clean SYNTAX-HIGHLIGHTED view of the whole file (context and edits
	// alike, like the `v` full-content view), with the changes flagged by a BOLD coloured
	// gutter BAR (diffGutter) — a 2-cell green "+ " / red "- " block on changed lines, two
	// spaces on context. The git preamble (diff --git/index/---/+++/@@) is DROPPED here:
	// it is plumbing noise for a "read the file with its changes" view, and the gutter
	// bars already say what changed (a non-code file keeps it as dim header rows above).
	// The signal rides the reset-free gutter cells, NOT a whole-line background — chroma
	// emits a per-token "\x1b[0m" reset that would clobber a line background mid-line. The
	// glyph stays one +/-/space byte (the trailing bar cell is a space), so the Y/V copy
	// buffer reads as a normal diff.
	//
	// Full-file context (-U<big>) makes the diff a complete file with +/- markers, so the
	// NEW side (context + additions) and OLD side (context + removals) each reconstruct an
	// entire source file. Tokenizing each as ONE unit gives chroma the multi-line context
	// (block comments, multi-line strings) per-line highlighting can't see — a changed
	// line inside a `/* */` colours as a comment. Context lines feed BOTH the tokenizer
	// (right indices + surrounding syntax) and the rendered output (drawn from newHL).
	var newSrc, oldSrc []string
	for _, line := range body {
		code := diffBodyCode(line)
		switch diffPrefix(line) {
		case '+':
			newSrc = append(newSrc, code)
		case '-':
			oldSrc = append(oldSrc, code)
		case ' ', 0: // context (incl. a blank context line) belongs to both sides
			newSrc = append(newSrc, code)
			oldSrc = append(oldSrc, code)
		}
	}
	// Three tokenization passes over the reconstructed sides — chroma sees each as a whole
	// file (multi-line context right), and the SAME source is highlighted twice with
	// different washes so a line's role decides its background with no per-line surgery:
	//   ctxHL — new side, no wash    → context lines
	//   addHL — new side, green wash → added lines
	//   delHL — old side, red wash   → removed lines
	// The wash IS the diff signal (no gutter glyph), so code starts at column 0.
	ctxHL, e1 := highlightCode(strings.Join(newSrc, "\n"), name)
	addHL, e2 := highlightCodeBg(strings.Join(newSrc, "\n"), name, colDiffAddBg)
	delHL, e3 := highlightCodeBg(strings.Join(oldSrc, "\n"), name, colDiffDelBg)
	if e1 != nil || e2 != nil || e3 != nil || ctxHL == nil {
		// Highlighter failed (rare): degrade to the dim-preamble + role-coloured body,
		// never an error.
		for _, line := range raw[:hunkIdx+1] {
			lines = append(lines, dimStyle.Render(line))
		}
		for _, line := range body {
			lines = append(lines, diffLineStyle(diffPrefix(line)).Render(line))
		}
		return lines, true, nil
	}

	ni, oi := 0, 0
	for _, line := range body {
		switch diffPrefix(line) {
		case '+':
			lines = append(lines, hlAt(addHL, ni, diffBodyCode(line)))
			ni++
		case '-':
			lines = append(lines, hlAt(delHL, oi, diffBodyCode(line)))
			oi++
		case ' ', 0:
			// Context: highlighted, no wash. ni/oi both advance — context is in both
			// reconstructions, keeping the changed lines' indices aligned.
			lines = append(lines, hlAt(ctxHL, ni, diffBodyCode(line)))
			ni++
			oi++
		default: // '\' "No newline at end of file" — metadata, not a content line.
			lines = append(lines, dimStyle.Render(line))
		}
	}
	return lines, true, nil
}

// diffBodyCode strips the single leading +/-/space gutter byte from a hunk-body line,
// yielding the bare source text the line carries (empty for a zero-length line). It is
// the inverse of the gutter glyph diffHunks re-prepends, and what the new/old side
// reconstructions tokenize.
func diffBodyCode(line string) string {
	if len(line) == 0 {
		return ""
	}
	return line[1:]
}

// hlAt returns the i-th highlighted line, or fallback when i is past the slice —
// highlightCode trims trailing blank lines, so a reconstruction ending in blanks can be
// shorter than its source line count; those tail lines are blank anyway.
func hlAt(hl []string, i int, fallback string) string {
	if i >= 0 && i < len(hl) {
		return hl[i]
	}
	return fallback
}

// diffPrefix reduces a hunk-body line to the single byte diffLineStyle keys on
// (D11). It is only called for lines AFTER the first "@@" (diffHunks dims the
// preamble headers positionally), so a leading '+'/'-' is always a real
// addition/removal — even when the source text itself starts with "++"/"--". A
// hunk header ('@'), a context line (' '), a "\ No newline" marker ('\'), and a
// blank line all fall through to 0 (the dim default arm of diffLineStyle).
func diffPrefix(line string) byte {
	if len(line) == 0 {
		return 0
	}
	return line[0]
}

// errEmptyDiff is the sentinel diffHunks returns when `git diff` is empty despite
// an M/R badge (D10/FR6) — the caller degrades to full content, never an empty pane.
var errEmptyDiff = errors.New("git diff produced no hunks")

// markAncestors marks every ancestor directory of a repo-relative slash path as
// dirty ("a/b/c.go" → "a", "a/b"). The path itself is NOT marked — folder ● means
// "a change lives INSIDE me", and a path's own badge comes from `changes`.
func markAncestors(path string, dirtyDirs map[string]bool) {
	for {
		i := strings.LastIndexByte(path, '/')
		if i < 0 {
			return
		}
		path = path[:i]
		dirtyDirs[path] = true
	}
}
