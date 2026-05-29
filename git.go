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
// "-54" / "" (no delta known). The view colors "+N" green and "-N" red.
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
type gitState struct {
	repoRoot  string
	changes   map[string]gitChange
	dirtyDirs map[string]bool
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

// collectGitState gathers the full change snapshot for a repo. It is the body of
// the async tea.Cmd. Resilience (FR10): any exec failure degrades that step
// rather than the whole state — a failed `git status` yields empty changes (no
// badges), and a failed numstat (e.g. a no-commit repo where `diff HEAD` aborts)
// leaves badges intact with no deltas (per-command independence).
func collectGitState(repoRoot string) gitState {
	st := gitState{repoRoot: repoRoot, changes: map[string]gitChange{}, dirtyDirs: map[string]bool{}}
	if repoRoot == "" {
		return st
	}

	// 1. Status drives the badges. -uall expands untracked DIRECTORIES into their
	//    individual files (default collapses a new folder to one "?? sub/" record,
	//    which would leave the folder ● dark and its files unbadged).
	statusOut, err := runGit(repoRoot, "status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return st // degrade: no git info this refresh
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

	// 3. Untracked files have no numstat row → count their lines directly (capped).
	countUntracked(repoRoot, st.changes, maxUntrackedScan)

	// 4. Roll-up: mark every ancestor dir of a change so folder rows can show ●.
	for k := range st.changes {
		markAncestors(k, st.dirtyDirs)
	}
	return st
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

// countUntracked fills "+N" line counts for untracked files (badge "?") by
// reading each (capped at maxPreviewBytes) and counting lines. Capped at `limit`
// files total per refresh; a binary / unreadable / over-limit file keeps its
// badge but gets no delta.
func countUntracked(repoRoot string, changes map[string]gitChange, limit int) {
	scanned := 0
	for key, chg := range changes {
		if chg.code != gitUntracked {
			continue
		}
		if scanned >= limit {
			break
		}
		scanned++
		if n, ok := countLines(filepath.Join(repoRoot, filepath.FromSlash(key))); ok {
			chg.added, chg.deleted, chg.hasDelta = n, 0, true
			changes[key] = chg
		}
	}
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
