// Package gha provides small helpers for GitHub Actions I/O — appending to
// GITHUB_STEP_SUMMARY, GITHUB_OUTPUT, and emitting workflow command annotations.
package gha

import (
	"fmt"
	"os"
	"strings"
	"time"
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
		delim := fmt.Sprintf("ghadelimiter_%d", time.Now().UnixNano())
		return appendFile(path, fmt.Sprintf("%s<<%s\n%s\n%s\n", key, delim, value, delim))
	}
	return appendFile(path, fmt.Sprintf("%s=%s\n", key, value))
}

// Notice emits a workflow `::notice::` annotation.
func Notice(msg string) { fmt.Printf("::notice::%s\n", msg) }

// Warning emits a workflow `::warning::` annotation.
func Warning(msg string) { fmt.Printf("::warning::%s\n", msg) }

// Error emits a workflow `::error::` annotation.
func Error(msg string) { fmt.Printf("::error::%s\n", msg) }

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
