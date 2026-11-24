package census

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/officialasishkumar/agentslint/internal/lint"
)

// Markdown writes the census summary.
func Markdown(w io.Writer, res []Result) {
	var ok []Result
	failed := 0
	for _, r := range res {
		if r.Err != "" {
			failed++
			continue
		}
		ok = append(ok, r)
	}
	withDocs, withErr, files, filesErr := 0, 0, 0, 0
	byName := map[string]int{}
	ruleCount := map[string]int{}
	ruleRepos := map[string]map[string]bool{}
	var tokens []int
	over := 0
	var examples []lint.Finding
	exampleRepo := map[*lint.Finding]string{}
	for _, r := range ok {
		if len(r.Docs) == 0 {
			continue
		}
		withDocs++
		if r.Errors > 0 {
			withErr++
		}
		for _, d := range r.Docs {
			files++
			name := path.Base(d.Path)
			if strings.HasPrefix(d.Path, ".cursor/rules/") {
				name = ".cursor/rules/*.mdc"
			}
			byName[name]++
			tokens = append(tokens, d.Tokens)
			if d.Tokens > 4000 {
				over++
			}
			hasErr := false
			for i := range d.Findings {
				f := d.Findings[i]
				ruleCount[f.Rule]++
				if ruleRepos[f.Rule] == nil {
					ruleRepos[f.Rule] = map[string]bool{}
				}
				ruleRepos[f.Rule][r.Repo] = true
				if f.Severity == lint.Error {
					hasErr = true
					if len(examples) < 400 {
						examples = append(examples, f)
						exampleRepo[&examples[len(examples)-1]] = r.Repo
					}
				}
			}
			if hasErr {
				filesErr++
			}
		}
	}
	pct := func(a, b int) string {
		if b == 0 {
			return "0%"
		}
		return fmt.Sprintf("%.0f%%", 100*float64(a)/float64(b))
	}
	fmt.Fprintf(w, "# AGENTS.md census\n\n")
	fmt.Fprintf(w, "- Repositories linted: **%d** (%d could not be cloned)\n", len(ok), failed)
	fmt.Fprintf(w, "- With at least one agent instruction file: **%d**, holding **%d** files\n", withDocs, files)
	fmt.Fprintf(w, "- Repositories with at least one instruction that cannot work as written: **%d (%s)**\n", withErr, pct(withErr, withDocs))
	fmt.Fprintf(w, "- Files with at least one such error: **%d of %d (%s)**\n", filesErr, files, pct(filesErr, files))
	if len(tokens) > 0 {
		sort.Ints(tokens)
		fmt.Fprintf(w, "- Size: median **%d** tokens, 90th percentile **%d**, largest **%d**; %d files (%s) over 4,000 tokens\n",
			tokens[len(tokens)/2], tokens[len(tokens)*9/10], tokens[len(tokens)-1], over, pct(over, files))
	}
	fmt.Fprintf(w, "\n## Files by name\n\n| file | count |\n|---|---:|\n")
	for _, k := range sortedKeys(byName) {
		fmt.Fprintf(w, "| %s | %d |\n", k, byName[k])
	}
	fmt.Fprintf(w, "\n## Findings by rule\n\n| rule | findings | repositories |\n|---|---:|---:|\n")
	for _, k := range sortedKeys(ruleCount) {
		fmt.Fprintf(w, "| %s | %d | %d |\n", k, ruleCount[k], len(ruleRepos[k]))
	}
	fmt.Fprintf(w, "\n## Examples (errors)\n\n")
	shown := map[string]int{}
	n := 0
	for i := range examples {
		f := &examples[i]
		repo := exampleRepo[f]
		if shown[repo] >= 1 || n >= 40 {
			continue
		}
		shown[repo]++
		n++
		fmt.Fprintf(w, "- `%s` %s:%d %s: %s `%s`\n", repo, f.File, f.Line, f.Rule, f.Message, f.Text)
	}
}

func sortedKeys(m map[string]int) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		if m[ks[i]] != m[ks[j]] {
			return m[ks[i]] > m[ks[j]]
		}
		return ks[i] < ks[j]
	})
	return ks
}
