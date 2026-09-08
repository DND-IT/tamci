package strategy

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dnd-it/tamci/internal/release/config"
)

// CalVer implements VersionStrategy with YYYY.MM.N format (UTC), where N is an
// incrementing counter that starts at 0 and resets each month.
type CalVer struct {
	// Now allows injecting time for testing. If nil, uses time.Now().UTC().
	Now func() time.Time
}

func (d *CalVer) Name() string         { return "calver" }
func (d *CalVer) AlwaysReleases() bool { return true }

func (d *CalVer) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now().UTC()
}

func (d *CalVer) NextVersion(tags []string, cfg config.Config) (Result, error) {
	prefix := cfg.TagPrefix
	month := d.now().Format("2006.01")

	// Find the highest counter already used this month so the next release
	// continues the sequence. The counter resets when the month rolls over.
	maxCounter := -1
	var latestTag string
	for _, t := range tags {
		v := strings.TrimPrefix(t, prefix)
		if m, counter, err := ParseCalVerVersion(v); err == nil && m == month {
			if counter > maxCounter {
				maxCounter = counter
			}
		}
		if latestTag == "" && strings.HasPrefix(t, prefix) {
			latestTag = t
		}
	}

	version := fmt.Sprintf("%s.%d", month, maxCounter+1)

	return Result{
		Version:         version,
		PreviousVersion: latestTag,
	}, nil
}

// ParseCalVerVersion extracts the month (YYYY.MM) and counter from a calver
// version string of the form YYYY.MM.N.
func ParseCalVerVersion(version string) (month string, counter int, err error) {
	parts := strings.SplitN(version, ".", 3) // YYYY.MM.N
	if len(parts) < 3 {
		return "", 0, fmt.Errorf("invalid calver version %q", version)
	}
	month = parts[0] + "." + parts[1]
	counter, err = strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, fmt.Errorf("invalid counter in %q: %w", version, err)
	}
	return month, counter, nil
}
