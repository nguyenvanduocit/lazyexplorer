//go:build sastcanary

// sast_canary.go is the deliberate SAST tripwire (PRD D11, G004). Two known-
// unsafe patterns from the go-security ruleset live here so every PR's SAST
// upload produces at least one finding on this file — the verify-sast-live
// workflow (PRD §5.6 FR5) queries Datadog Findings API for that finding and
// fails the build if it's missing. Without this file the scanner could
// silently rot (rule retirement, config drift, suppressions) and nobody
// would notice until a real vulnerability slips by.
//
// Two layers of containment keep these patterns out of the prod binary:
//  1. Go convention skips any directory named testdata/ — `go build .` never
//     walks here.
//  2. The build tag //go:build sastcanary above further pins this file to a
//     non-default build. Compiling the patterns requires `go build -tags
//     sastcanary ./testdata/...` and is exercised by no CI step (deliberate).
//
// DO NOT remove either layer. DO NOT silence the SAST findings — they are
// the contract that keeps verify-sast-live meaningful.

package sastcanary

import (
	"os"
	"os/exec"
)

// canaryUnsafeShell wires raw argv into exec.Command — a textbook command
// injection sink the go-security ruleset MUST flag.
func canaryUnsafeShell() {
	_ = exec.Command(os.Args[0]).Run()
}

// canaryUnsafeOpen opens an arbitrary path from a tainted env var — another
// classic path-injection sink for the go-security ruleset.
func canaryUnsafeOpen() {
	f, err := os.Open(os.Getenv("X"))
	if err != nil {
		return
	}
	_ = f.Close()
}
