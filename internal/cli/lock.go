package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/dnd-it/tamci/internal/gha"
	"github.com/dnd-it/tamci/internal/lock"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newLockCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Distributed mutex via GitHub git refs (refs/locks/<name>).",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLock(v)
		},
	}

	f := cmd.Flags()
	f.String("action", "", "acquire | release")
	f.String("lock-name", "", "Name of the lock (used as ref name under refs/locks/).")
	f.Int("timeout", 300, "Maximum seconds to wait for lock acquisition.")
	f.Int("poll-interval", 10, "Seconds between acquisition attempts.")
	f.Int("stale-threshold", 600, "Seconds after which a lock is considered stale (0 disables).")
	f.Bool("fail-on-timeout", true, "Fail the step if the lock cannot be acquired within timeout.")
	f.String("token", "", "GitHub token with contents:write permission.")

	return cmd
}

func runLock(v *viper.Viper) error {
	action := v.GetString("action")
	if action != "acquire" && action != "release" {
		return fmt.Errorf("invalid action %q: must be 'acquire' or 'release'", action)
	}

	lockName := v.GetString("lock-name")
	if lockName == "" {
		return fmt.Errorf("--lock-name (INPUT_LOCK_NAME) is required")
	}

	token := v.GetString("token")
	if token == "" {
		return fmt.Errorf("--token (INPUT_TOKEN) is required")
	}

	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return fmt.Errorf("GITHUB_REPOSITORY not set")
	}

	sha := os.Getenv("GITHUB_SHA")
	if sha == "" && action == "acquire" {
		return fmt.Errorf("GITHUB_SHA not set")
	}

	client := lock.New(repo, token)
	lockRef := fmt.Sprintf("refs/locks/%s", lockName)

	switch action {
	case "acquire":
		acquired := acquireLoop(client, lockName, sha, v)
		_ = gha.SetOutput("acquired", fmt.Sprintf("%t", acquired))
		_ = gha.SetOutput("lock_ref", lockRef)
		if !acquired && v.GetBool("fail-on-timeout") {
			return fmt.Errorf("failed to acquire lock %q within %ds", lockName, v.GetInt("timeout"))
		}
	case "release":
		if err := client.Release(lockName); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to release lock: %v\n", err)
		} else {
			fmt.Printf("Lock %q released\n", lockName)
		}
		_ = gha.SetOutput("acquired", "false")
		_ = gha.SetOutput("lock_ref", lockRef)
	}
	return nil
}

func acquireLoop(client *lock.Client, lockName, sha string, v *viper.Viper) bool {
	timeout := v.GetInt("timeout")
	pollInterval := v.GetInt("poll-interval")
	staleThreshold := v.GetInt("stale-threshold")

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	interval := time.Duration(pollInterval) * time.Second

	for {
		acquired, err := client.Acquire(lockName, sha)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: lock attempt failed: %v\n", err)
		}
		if acquired {
			fmt.Printf("Lock %q acquired\n", lockName)
			return true
		}

		age, err := client.LockAge(lockName)
		if staleThreshold > 0 && err == nil && age > staleThreshold {
			fmt.Printf("Stale lock detected (%ds old, threshold %ds), removing...\n", age, staleThreshold)
			if err := client.Release(lockName); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to remove stale lock: %v\n", err)
			}
			continue
		}

		if time.Now().After(deadline) {
			return false
		}

		remaining := time.Until(deadline).Seconds()
		fmt.Printf("Lock %q held by another process, retrying in %ds... (%.0fs remaining)\n", lockName, pollInterval, remaining)
		time.Sleep(interval)
	}
}
