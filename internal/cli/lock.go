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
	f.String("action", "", "acquire | release | status")
	f.String("lock-name", "", "Name of the lock (used as ref name under refs/locks/).")
	f.String("sha", "", "Commit the lock is taken on (defaults to GITHUB_SHA).")
	f.String("holder", "", "Who holds the lock; recorded on acquire and checked on release.")
	f.Int("timeout", 300, "Maximum seconds to wait for lock acquisition.")
	f.Int("poll-interval", 10, "Seconds between acquisition attempts.")
	f.Int("stale-threshold", 600, "Seconds after which a lock is considered stale (0 disables).")
	f.Bool("fail-on-timeout", true, "Fail the step if the lock cannot be acquired within timeout.")
	f.String("token", "", "GitHub token with contents:write permission.")

	return cmd
}

func runLock(v *viper.Viper) error {
	action := v.GetString("action")
	if action != "acquire" && action != "release" && action != "status" {
		return fmt.Errorf("invalid action %q: must be 'acquire', 'release' or 'status'", action)
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

	sha := v.GetString("sha")
	if sha == "" {
		sha = os.Getenv("GITHUB_SHA")
	}
	if sha == "" && action == "acquire" {
		return fmt.Errorf("--sha (INPUT_SHA) or GITHUB_SHA is required")
	}
	holder := v.GetString("holder")

	client := lock.New(repo, token)
	lockRef := fmt.Sprintf("refs/locks/%s", lockName)

	switch action {
	case "acquire":
		acquired := acquireLoop(client, lockName, sha, holder, v)
		current := holder
		if !acquired {
			current, _, _ = client.Holder(lockName)
		}
		_ = gha.SetOutput("acquired", fmt.Sprintf("%t", acquired))
		_ = gha.SetOutput("holder", current)
		_ = gha.SetOutput("lock_ref", lockRef)
		if !acquired && v.GetBool("fail-on-timeout") {
			return fmt.Errorf("failed to acquire lock %q within %ds", lockName, v.GetInt("timeout"))
		}
	case "release":
		released, err := release(client, lockName, holder)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "Warning: failed to release lock: %v\n", err)
		case released:
			fmt.Printf("Lock %q released\n", lockName)
		default:
			fmt.Printf("Lock %q is not held by %q, left in place\n", lockName, holder)
		}
		_ = gha.SetOutput("acquired", "false")
		_ = gha.SetOutput("released", fmt.Sprintf("%t", released))
		_ = gha.SetOutput("lock_ref", lockRef)
	case "status":
		current, locked, err := client.Holder(lockName)
		if err != nil {
			return fmt.Errorf("read lock %q: %w", lockName, err)
		}
		_ = gha.SetOutput("locked", fmt.Sprintf("%t", locked))
		_ = gha.SetOutput("holder", current)
		_ = gha.SetOutput("lock_ref", lockRef)
	}
	return nil
}

func release(client *lock.Client, lockName, holder string) (bool, error) {
	if holder != "" {
		return client.ReleaseHeld(lockName, holder)
	}
	if err := client.Release(lockName); err != nil {
		return false, err
	}
	return true, nil
}

func acquireLoop(client *lock.Client, lockName, sha, holder string, v *viper.Viper) bool {
	timeout := v.GetInt("timeout")
	pollInterval := v.GetInt("poll-interval")
	staleThreshold := v.GetInt("stale-threshold")

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	interval := time.Duration(pollInterval) * time.Second

	for {
		acquired, err := client.Acquire(lockName, sha, holder)
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
				// Fall through to the deadline check and poll sleep so a
				// persistently failing release can't spin without bound.
				fmt.Fprintf(os.Stderr, "Warning: failed to remove stale lock: %v\n", err)
			} else {
				continue
			}
		}

		if time.Now().After(deadline) {
			return false
		}

		remaining := time.Until(deadline).Seconds()
		fmt.Printf("Lock %q held by another process, retrying in %ds... (%.0fs remaining)\n", lockName, pollInterval, remaining)
		time.Sleep(interval)
	}
}
