// Package promote decides what a CI run deploys (Resolve) and prepares the
// prod tag a human pushes to promote a stack or a service (Prepare).
package promote

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/dnd-it/tamci/internal/git"
)

// Event is the part of a GitHub Actions run that decides what it deploys.
type Event struct {
	Name          string
	Action        string
	ChangedLabel  string
	Labels        []string
	Environment   string
	DefaultBranch string
	RefType       string
	RefName       string
	SHA           string
}

// LoadEvent reads the event payload at path for the event called name.
func LoadEvent(name, path, refType, refName, sha string) (Event, error) {
	ev := Event{Name: name, RefType: refType, RefName: refName, SHA: sha}
	if path == "" {
		return ev, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ev, fmt.Errorf("reading event payload: %w", err)
	}
	var p struct {
		Action string `json:"action"`
		Label  struct {
			Name string `json:"name"`
		} `json:"label"`
		PullRequest struct {
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"pull_request"`
		Inputs struct {
			Environment string `json:"environment"`
		} `json:"inputs"`
		Repository struct {
			DefaultBranch string `json:"default_branch"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return ev, fmt.Errorf("parsing event payload %s: %w", path, err)
	}
	ev.Action = p.Action
	ev.ChangedLabel = p.Label.Name
	for _, l := range p.PullRequest.Labels {
		ev.Labels = append(ev.Labels, l.Name)
	}
	ev.Environment = p.Inputs.Environment
	ev.DefaultBranch = p.Repository.DefaultBranch
	return ev, nil
}

// Rules configure Resolve.
type Rules struct {
	TagPrefix     string
	Label         string
	DefaultBranch string
	Dev           string
	Prod          string
}

// Decision is what a run does: plan, apply to an environment, release the dev lock.
type Decision struct {
	Plan    bool
	Apply   string
	Release bool
}

// Resolve decides what the run described by ev deploys. g is only used to
// check that a prod tag points at a commit on the default branch.
func Resolve(ev Event, r Rules, g *git.Client) (Decision, error) {
	var d Decision
	switch ev.Name {
	case "pull_request":
		switch ev.Action {
		case "opened", "reopened", "synchronize":
			d.Plan = true
			if slices.Contains(ev.Labels, r.Label) {
				d.Apply = r.Dev
			}
		case "labeled":
			if ev.ChangedLabel == r.Label {
				d.Apply = r.Dev
			}
		case "unlabeled":
			d.Release = ev.ChangedLabel == r.Label
		case "closed":
			d.Release = true
		}
	case "push":
		if ev.RefType == "tag" {
			if err := requireProdTag(ev, r, g); err != nil {
				return d, err
			}
			d.Apply = r.Prod
		} else {
			if err := requireDefaultBranch(ev, r); err != nil {
				return d, err
			}
			d.Apply = r.Dev
		}
	case "workflow_dispatch":
		switch env := cmp.Or(ev.Environment, r.Dev); env {
		case r.Dev:
			if err := requireDefaultBranch(ev, r); err != nil {
				return d, err
			}
			d.Apply = r.Dev
		case r.Prod:
			if err := requireProdTag(ev, r, g); err != nil {
				return d, err
			}
			d.Apply = r.Prod
		default:
			return d, fmt.Errorf("unknown environment %s", env)
		}
	}
	return d, nil
}

func requireProdTag(ev Event, r Rules, g *git.Client) error {
	if ev.RefType != "tag" || !strings.HasPrefix(ev.RefName, r.TagPrefix) {
		return fmt.Errorf("%s is only applied from a %s<semver> tag, not %s", r.Prod, r.TagPrefix, ev.RefName)
	}
	if err := g.Fetch(r.DefaultBranch); err != nil {
		return err
	}
	onBranch, err := g.IsAncestor(ev.SHA, "origin/"+r.DefaultBranch)
	if err != nil {
		return err
	}
	if !onBranch {
		return fmt.Errorf("%s points at %s, which is not on %s", ev.RefName, ev.SHA, r.DefaultBranch)
	}
	return nil
}

func requireDefaultBranch(ev Event, r Rules) error {
	if ev.RefName != r.DefaultBranch {
		return fmt.Errorf("%s is applied from %s or from a pull request labelled %s; label the pull request instead", r.Dev, r.DefaultBranch, r.Label)
	}
	return nil
}
