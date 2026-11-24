package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// memRepo is an in-memory Repo for tests.
type memRepo map[string]string

func (m memRepo) Files() []string {
	set := map[string]bool{}
	for p := range m {
		set[p] = true
		for d := filepath.Dir(p); d != "."; d = filepath.Dir(d) {
			set[d+"/"] = true
		}
	}
	var out []string
	for p := range set {
		out = append(out, p)
	}
	sortStrings(out)
	return out
}
func (m memRepo) Read(p string) ([]byte, error) {
	s, ok := m[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(s), nil
}
func (m memRepo) IsLink(string) (string, bool) { return "", false }

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func rules(docs []Doc) map[string][]string {
	out := map[string][]string{}
	for _, d := range docs {
		for _, f := range d.Findings {
			out[f.Rule] = append(out[f.Rule], f.Text)
		}
	}
	return out
}

func TestFindsStaleInstructions(t *testing.T) {
	repo := memRepo{
		"AGENTS.md": "# Agents\n\nRun `npm run lint` and `npm run typecheck` before committing.\n" +
			"Tests live in `src/__tests__/` and the entry point is `src/index.ts`.\n" +
			"Old docs: see `docs/architecture.md`.\n\n```bash\nmake build\nmake release\n./scripts/setup.sh\npytest tests/unit/test_api.py\n```\n",
		"package.json":            `{"scripts":{"lint":"eslint .","test":"vitest"}}`,
		"package-lock.json":       "{}",
		"Makefile":                "build:\n\tgo build ./...\n\n.PHONY: build\n",
		"src/index.ts":            "",
		"src/__tests__/a.test.ts": "",
		"tests/unit/test_api.py":  "",
	}
	r := rules(Lint(repo, Options{}))
	want := map[string]string{
		"missing-script":      "npm run typecheck",
		"missing-make-target": "make release",
		"missing-path":        "docs/architecture.md",
	}
	for rule, text := range want {
		found := false
		for _, x := range r[rule] {
			if strings.Contains(x, text) {
				found = true
			}
		}
		if !found {
			t.Errorf("rule %s: want a finding for %q, got %v", rule, text, r[rule])
		}
	}
	for _, x := range r["missing-path"] {
		if strings.Contains(x, "src/index.ts") || strings.Contains(x, "test_api.py") || strings.Contains(x, "__tests__") {
			t.Errorf("false positive on an existing path: %q", x)
		}
	}
	if !contains(r["missing-path"], "./scripts/setup.sh") {
		t.Errorf("missing script file not reported: %v", r["missing-path"])
	}
}

func TestMonorepoAndPlaceholdersAreNotErrors(t *testing.T) {
	repo := memRepo{
		"AGENTS.md": "In `apps/api` run `pnpm harness`. Components go in `ComponentName/ComponentName.tsx`.\n" +
			"See `workbench/browser/` and `utils.py`, call `agent/run.py::main`, and edit `<your-file>.ts`.\n" +
			"```\ncd my-repo\n./mybinary serve\n```\n",
		"pnpm-lock.yaml":                "",
		"apps/api/package.json":         `{"scripts":{"harness":"vitest"}}`,
		"src/vs/workbench/browser/x.ts": "",
		"agent/run.py":                  "",
	}
	docs := Lint(repo, Options{})
	for _, d := range docs {
		for _, f := range d.Findings {
			if f.Severity == Error {
				t.Errorf("unexpected error: %+v", f)
			}
		}
	}
}

func TestWrongPackageManagerAndLength(t *testing.T) {
	repo := memRepo{
		"CLAUDE.md":      "Install with `npm install`.\n" + strings.Repeat("Keep functions small and well named.\n", 600),
		"pnpm-lock.yaml": "",
		"package.json":   `{}`,
	}
	r := rules(Lint(repo, Options{}))
	if len(r["wrong-package-manager"]) == 0 {
		t.Error("npm used with a pnpm lockfile should be reported")
	}
	if len(r["too-long"]) == 0 {
		t.Error("a ~5,500-token file should be reported as too long")
	}
}

func TestDuplicateFiles(t *testing.T) {
	body := strings.Repeat("Use table-driven tests for every package.\nRun gofmt before committing.\n", 10)
	repo := memRepo{"AGENTS.md": body, "CLAUDE.md": body + "Extra line here.\n"}
	if len(rules(Lint(repo, Options{}))["duplicate"]) == 0 {
		t.Error("near-identical AGENTS.md and CLAUDE.md should be reported")
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}
