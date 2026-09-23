// Package gha provides small helpers for GitHub Actions I/O — appending to
// GITHUB_STEP_SUMMARY, GITHUB_OUTPUT, and emitting workflow command annotations.
package gha

import (
	"fmt"
	"os"
	"strings"
)

// AppendStepSummary writes content to $GITHUB_STEP_SUMMARY. If the env var is
// unset (e.g. running locally), it writes to stdout instead.
func AppendStepSummary(content string) error {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		fmt.Print(content)
		return nil
	}
	return appendFile(path, content)
}

// SetOutput appends key=value to $GITHUB_OUTPUT. Multi-line values are written
// using the heredoc delimiter syntax. Falls back to the legacy `::set-output`
// workflow command when GITHUB_OUTPUT is unset.
func SetOutput(key, value string) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		fmt.Printf("::set-output name=%s::%s\n", key, value)
		return nil
	}
	if strings.Contains(value, "\n") {
		// The heredoc delimiter must not appear as a line in the value,
		// otherwise GitHub's parser ends the heredoc early and mis-reads
		// everything after it. Extend the delimiter until it is unique so
		// arbitrary content can never break parsing.
		delim := heredocDelim(value)
		return appendFile(path, fmt.Sprintf("%s<<%s\n%s\n%s\n", key, delim, value, delim))
	}
	return appendFile(path, fmt.Sprintf("%s=%s\n", key, value))
}

// heredocDelim returns a delimiter guaranteed not to appear as a line in value,
// so the heredoc written to $GITHUB_OUTPUT is always well-formed.
func heredocDelim(value string) string {
	base := fmt.Sprintf("ghadelimiter_%d", os.Getpid())
	delim := base
	for i := 0; containsLine(value, delim); i++ {
		delim = fmt.Sprintf("%s_%d", base, i)
	}
	return delim
}

// containsLine reports whether s has a line exactly equal to line.
func containsLine(s, line string) bool {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimRight(l, "\r") == line {
			return true
		}
	}
	return false
}

// SaveState appends key=value to $GITHUB_STATE. The runner exposes it to the
// action's post step as the STATE_<key> env var.
func SaveState(key, value string) error {
	path := os.Getenv("GITHUB_STATE")
	if path == "" {
		return fmt.Errorf("GITHUB_STATE not set")
	}
	return appendFile(path, fmt.Sprintf("%s=%s\n", key, value))
}

// Mask emits an `::add-mask::` command so the runner redacts value from logs.
func Mask(value string) { fmt.Printf("::add-mask::%s\n", value) }

// Notice emits a workflow `::notice::` annotation.
func Notice(msg string) { fmt.Printf("::notice::%s\n", msg) }

// Warning emits a workflow `::warning::` annotation.
func Warning(msg string) { fmt.Printf("::warning::%s\n", msg) }

// Error emits a workflow `::error::` annotation.
func Error(msg string) { fmt.Printf("::error::%s\n", msg) }

// ErrorAt emits a workflow `::error::` annotation attached to file.
func ErrorAt(file, msg string) { fmt.Printf("::error file=%s::%s\n", file, msg) }

func appendFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
