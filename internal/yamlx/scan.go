package yamlx

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// node aliases yaml.Node for the file-level helpers — the public API uses
// yaml.Node directly via Document.Root.
type node = yaml.Node

// updateAllImageTags walks the doc looking for any mapping that has both
// `repository:` and `tag:` keys, and replaces the tag value with newTag.
// Mirrors the action-deployer image-mode behavior (Helm pattern, no imageName filter).
func updateAllImageTags(doc *Document, newTag string) ([]Change, error) {
	root := doc.Root
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}

	var changes []Change
	walk(root, func(n *yaml.Node) {
		if n.Kind != yaml.MappingNode {
			return
		}
		var hasRepo bool
		var tagVal *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			switch key.Value {
			case "repository":
				hasRepo = true
			case "tag":
				if val.Kind == yaml.ScalarNode {
					tagVal = val
				}
			}
		}
		if !hasRepo || tagVal == nil {
			return
		}
		oldValue := nodeValue(tagVal)
		coerced := coerceValue(newTag, tagVal)
		if tagVal.Value == coerced {
			return
		}
		doc.edits = append(doc.edits, valueEdit{
			Line:     tagVal.Line,
			Column:   tagVal.Column,
			OldValue: tagVal.Value,
			NewValue: coerced,
			Style:    tagVal.Style,
		})
		changes = append(changes, Change{
			Key: "tag",
			Old: oldValue,
			New: parseValue(coerced),
		})
		tagVal.Value = coerced
	})

	if len(changes) == 0 {
		return nil, fmt.Errorf("no image block with tag: field found")
	}
	return changes, nil
}

func findKey(doc *Document, keyPath string) (*yaml.Node, error) {
	root := doc.Root
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	return resolveKeyPath(root, keyPath)
}

func findFirstMarker(doc *Document, marker string) (*yaml.Node, error) {
	root := doc.Root
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	var found *yaml.Node
	walk(root, func(n *yaml.Node) {
		if found != nil {
			return
		}
		if n.Kind == yaml.ScalarNode && hasMarker(n.LineComment, marker) {
			found = n
		}
	})
	if found == nil {
		return nil, fmt.Errorf("marker not found")
	}
	return found, nil
}

func findFirstImageTag(doc *Document) (*yaml.Node, error) {
	root := doc.Root
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	var found *yaml.Node
	walk(root, func(n *yaml.Node) {
		if found != nil || n.Kind != yaml.MappingNode {
			return
		}
		var hasRepo bool
		var tagVal *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			switch n.Content[i].Value {
			case "repository":
				hasRepo = true
			case "tag":
				if n.Content[i+1].Kind == yaml.ScalarNode {
					tagVal = n.Content[i+1]
				}
			}
		}
		if hasRepo && tagVal != nil {
			found = tagVal
		}
	})
	if found == nil {
		return nil, fmt.Errorf("no image block found")
	}
	return found, nil
}

func walk(n *yaml.Node, fn func(*yaml.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, child := range n.Content {
		walk(child, fn)
	}
}

// trimSpaceLower lower-cases and trims a string. Unused for now but kept for
// future case-insensitive marker matching.
var _ = strings.ToLower
