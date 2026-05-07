package strategy

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dnd-it/tamci/internal/release/config"
)

// DateRolling implements VersionStrategy with YYYY.MM.DD[.N] format (UTC).
type DateRolling struct {
	// Now allows injecting time for testing. If nil, uses time.Now().UTC().
	Now func() time.Time
}

func (d *DateRolling) Name() string         { return "date-rolling" }
func (d *DateRolling) AlwaysReleases() bool { return true }

func (d *DateRolling) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now().UTC()
}

func (d *DateRolling) NextVersion(tags []string, cfg config.Config) (Result, error) {
	prefix := cfg.TagPrefix
	today := d.now().Format("2006.01.02")

	// Find all tags matching today's date.
	var todayCount int
	var latestTag string
	for _, t := range tags {
		v := strings.TrimPrefix(t, prefix)
		if v == today || strings.HasPrefix(v, today+".") {
			todayCount++
		}
		if latestTag == "" && strings.HasPrefix(t, prefix) {
			latestTag = t
		}
	}

	var version string
	if todayCount == 0 {
		// First release of the day — no suffix.
		version = today
	} else {
		// Subsequent releases: .2, .3, etc.
		version = fmt.Sprintf("%s.%d", today, todayCount+1)
	}

	return Result{
		Version:         version,
		PreviousVersion: latestTag,
	}, nil
}

// ParseDateVersion extracts the date and counter from a date-rolling version.
func ParseDateVersion(version string) (date string, counter int, err error) {
	parts := strings.SplitN(version, ".", 4) // YYYY.MM.DD[.N]
	if len(parts) < 3 {
		return "", 0, fmt.Errorf("invalid date version %q", version)
	}
	date = strings.Join(parts[:3], ".")
	if len(parts) == 4 {
		counter, err = strconv.Atoi(parts[3])
		if err != nil {
			return "", 0, fmt.Errorf("invalid counter in %q: %w", version, err)
		}
	}
	return date, counter, nil
}
