package rollout

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "matrix.config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestResolve_MergePrecedence(t *testing.T) {
	// service.deploy should override environment.deploy.
	p := writeConfig(t, `
global:
  aws_region: eu-central-1
environment:
  dev:
    deploy: auto
    tag: version
  prod:
    deploy: pr
    tag: version
service:
  svc-a:
    deploy: auto  # overrides prod's "pr"
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := cfg.Resolve("svc-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if e.Deploy != "auto" {
			t.Errorf("env %s deploy=%q, want auto (service override)", e.Name, e.Deploy)
		}
	}
}

func TestResolve_ServiceNotFound(t *testing.T) {
	p := writeConfig(t, "environment:\n  dev:\n    deploy: auto\nservice:\n  known:\n    deploy: auto\n")
	cfg, _ := LoadConfig(p)
	_, err := cfg.Resolve("unknown")
	if err == nil {
		t.Fatal("want error for unknown service")
	}
}

func TestResolve_Sorted(t *testing.T) {
	p := writeConfig(t, `
environment:
  zebra:
    deploy: auto
  alpha:
    deploy: auto
  mango:
    deploy: pr
service:
  svc:
    deploy: auto
`)
	cfg, _ := LoadConfig(p)
	envs, err := cfg.Resolve("svc")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "mango", "zebra"}
	if len(envs) != len(want) {
		t.Fatalf("got %d envs, want %d", len(envs), len(want))
	}
	for i, e := range envs {
		if e.Name != want[i] {
			t.Errorf("[%d] got %s, want %s", i, e.Name, want[i])
		}
	}
}

func TestResolve_ValuesModeDefault(t *testing.T) {
	p := writeConfig(t, `
environment:
  dev:
    deploy: auto
service:
  svc:
    deploy: auto
`)
	cfg, _ := LoadConfig(p)
	envs, _ := cfg.Resolve("svc")
	if len(envs) != 1 {
		t.Fatalf("want 1 env, got %d", len(envs))
	}
	if envs[0].ValuesMode != "image" {
		t.Errorf("ValuesMode=%q, want image", envs[0].ValuesMode)
	}
	if envs[0].MergeMethod != "SQUASH" {
		t.Errorf("MergeMethod=%q, want SQUASH", envs[0].MergeMethod)
	}
}

func TestResolve_ValuesModeOverride(t *testing.T) {
	p := writeConfig(t, `
environment:
  dev:
    deploy: auto
    values_mode: image
  prod:
    deploy: pr
    values_mode: image
service:
  svc:
    deploy: auto
    values_mode: key
    values_key: image.tag
`)
	cfg, _ := LoadConfig(p)
	envs, _ := cfg.Resolve("svc")
	for _, e := range envs {
		if e.ValuesMode != "key" {
			t.Errorf("env %s ValuesMode=%q, want key", e.Name, e.ValuesMode)
		}
		if e.ValuesKey != "image.tag" {
			t.Errorf("env %s ValuesKey=%q, want image.tag", e.Name, e.ValuesKey)
		}
	}
}

func TestResolve_AutoMergePointerSemantics(t *testing.T) {
	// env has auto_merge: true, service omits it → should stay true.
	p := writeConfig(t, `
environment:
  prod:
    deploy: pr
    auto_merge: true
service:
  svc:
    deploy: pr
`)
	cfg, _ := LoadConfig(p)
	envs, _ := cfg.Resolve("svc")
	if !envs[0].AutoMerge {
		t.Errorf("want auto_merge=true from env, got false")
	}

	// service has auto_merge: false → should override env's true.
	p2 := writeConfig(t, `
environment:
  prod:
    deploy: pr
    auto_merge: true
service:
  svc:
    deploy: pr
    auto_merge: false
`)
	cfg2, _ := LoadConfig(p2)
	envs2, _ := cfg2.Resolve("svc")
	if envs2[0].AutoMerge {
		t.Errorf("want auto_merge=false from service, got true")
	}
}

func TestLoad_Missing(t *testing.T) {
	_, err := LoadConfig("/no/such/file.yaml")
	if err == nil {
		t.Fatal("want error")
	}
}
