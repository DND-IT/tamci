package yamlx

import (
	"fmt"
	"os"
)

// UpdateOptions selects how to locate the target value(s) inside a file.
type UpdateOptions struct {
	Mode string // "image" (default) | "key" | "marker"
	Key  string // dot-notation path, only for Mode="key"
}

// SetTag locates the target node(s) in `file` and replaces their scalar value
// with `tag`, preserving formatting and comments. Returns the number of
// nodes updated, or an error if no target is found.
//
// Convenience wrapper over LoadYAML + UpdateXxx + DumpYAML.
func SetTag(file, tag string, opts UpdateOptions) (int, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", file, err)
	}

	doc, err := LoadYAML(data)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", file, err)
	}

	changes, err := applyUpdate(doc, opts, tag)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", file, err)
	}
	if len(changes) == 0 {
		return 0, fmt.Errorf("%s: %s", file, notFoundError(opts))
	}

	out, err := DumpYAML(doc)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", file, err)
	}
	if err := os.WriteFile(file, out, 0o644); err != nil {
		return 0, fmt.Errorf("writing %s: %w", file, err)
	}
	return len(changes), nil
}

// HasTarget reports whether the file contains at least one node matching opts.
// File-read or YAML-parse errors surface as errors; "target not found" returns (false, nil).
func HasTarget(file string, opts UpdateOptions) (bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}
	doc, err := LoadYAML(data)
	if err != nil {
		return false, fmt.Errorf("parsing %s: %w", file, err)
	}
	changes, err := applyUpdate(doc, opts, "__probe__")
	if err != nil {
		return false, nil
	}
	return len(changes) > 0, nil
}

// ReadTag reads the current value of the first matching target node.
// Returns ("", nil) if not found or the file does not exist.
func ReadTag(file string, opts UpdateOptions) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", file, err)
	}
	doc, err := LoadYAML(data)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", file, err)
	}
	node, err := firstTarget(doc, opts)
	if err != nil || node == nil {
		return "", nil
	}
	return node.Value, nil
}

// applyUpdate dispatches to the right Update* function based on opts.Mode.
func applyUpdate(doc *Document, opts UpdateOptions, newValue string) ([]Change, error) {
	switch effectiveMode(opts.Mode) {
	case "key":
		if opts.Key == "" {
			return nil, fmt.Errorf("key mode requires a key path")
		}
		return UpdateKeys(doc, []string{opts.Key}, []string{newValue})
	case "marker":
		changes := UpdateByMarker(doc, "x-yaml-update", newValue)
		if len(changes) == 0 {
			return nil, fmt.Errorf("marker # x-yaml-update not found")
		}
		return changes, nil
	default: // "image"
		// SetTag's image mode operates on Helm `tag:` siblings under `repository:`.
		// We use a lightweight scan rather than UpdateImageTags (which requires an
		// imageName) — when SetTag is called in image mode the user's intent is
		// "update every image tag in this file".
		return updateAllImageTags(doc, newValue)
	}
}

func firstTarget(doc *Document, opts UpdateOptions) (*node, error) {
	switch effectiveMode(opts.Mode) {
	case "key":
		if opts.Key == "" {
			return nil, fmt.Errorf("key mode requires a key path")
		}
		return findKey(doc, opts.Key)
	case "marker":
		return findFirstMarker(doc, "x-yaml-update")
	default:
		return findFirstImageTag(doc)
	}
}

func effectiveMode(m string) string {
	if m == "" {
		return "image"
	}
	return m
}

func notFoundError(opts UpdateOptions) error {
	switch effectiveMode(opts.Mode) {
	case "key":
		return fmt.Errorf("key %q not found", opts.Key)
	case "marker":
		return fmt.Errorf("marker # x-yaml-update not found")
	default:
		return fmt.Errorf("no image block with tag: field found")
	}
}
