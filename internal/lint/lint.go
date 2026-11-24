package lint

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Severity of a finding.
type Severity string

const (
	Error Severity = "error" // the instruction cannot work as written
	Warn  Severity = "warn"  // probably wrong or costly
)

// Finding is one problem in an instruction file.
type Finding struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Text     string   `json:"text,omitempty"` // the snippet that triggered it
}

// Doc is one instruction file and what was checked in it.
type Doc struct {
	Path     string    `json:"path"`
	Bytes    int       `json:"bytes"`
	Tokens   int       `json:"tokens"` // estimate: bytes/4
	Commands int       `json:"commands"`
	Paths    int       `json:"paths"`
	Findings []Finding `json:"findings"`
}

// Names of instruction files, in the order agents look for them.
var InstructionFiles = []string{
	"AGENTS.md", "AGENT.md", "CLAUDE.md", "GEMINI.md", ".github/copilot-instructions.md",
	".cursorrules", ".windsurfrules", "CONVENTIONS.md",
}

// isInstruction reports whether a repo path is an agent instruction file.
func isInstruction(p string) bool {
	for _, part := range strings.Split(path.Dir(p), "/") {
		switch part {
		case "vendor", "third_party", "third-party", "thirdparty", "external", "deps", "node_modules", "testdata", "fixtures":
			return false // instructions that belong to vendored code or test data
		}
	}
	b := path.Base(p)
	switch b {
	case "AGENTS.md", "AGENT.md", "CLAUDE.md", "GEMINI.md", ".cursorrules", ".windsurfrules":
		return true
	}
	if p == ".github/copilot-instructions.md" {
		return true
	}
	return strings.HasPrefix(p, ".cursor/rules/") && strings.HasSuffix(p, ".mdc")
}

// Options tune the checks.
type Options struct {
	MaxTokens int // warn above this many tokens per file (0: 4000)
}

// Lint finds every instruction file in r and checks it.
func Lint(r Repo, o Options) []Doc {
	if o.MaxTokens == 0 {
		o.MaxTokens = 4000
	}
	ix := newIndex(r)
	var docs []Doc
	bodies := map[string]string{}
	for _, f := range ix.list {
		if strings.HasSuffix(f, "/") || !isInstruction(f) {
			continue
		}
		if t, ok := r.IsLink(f); ok {
			// A symlink to another instruction file is the recommended way to
			// share one file between agents; lint the target only.
			_ = t
			continue
		}
		b, err := r.Read(f)
		if err != nil {
			continue
		}
		bodies[f] = string(b)
		docs = append(docs, check(r, ix, f, string(b), o))
	}
	docs = append(docs[:0:0], dedupe(docs, bodies)...)
	return docs
}

var (
	fenceRe  = regexp.MustCompile("^\\s*(```|~~~)")
	inlineRe = regexp.MustCompile("`([^`\\n]+)`")
	linkRe   = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	importRe = regexp.MustCompile(`(?:^|\s)@([\w./-]+\.\w+)`) // CLAUDE.md @path imports
)

func check(r Repo, ix *index, file, body string, o Options) Doc {
	d := Doc{Path: file, Bytes: len(body), Tokens: (len(body) + 3) / 4}
	c := &checker{r: r, ix: ix, file: file, dir: path.Dir(file)}
	inFence, fenceLang := false, ""
	for i, line := range strings.Split(body, "\n") {
		n := i + 1
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			if !inFence {
				inFence = true
				fenceLang = strings.ToLower(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "`~")))
			} else {
				inFence = false
			}
			continue
		}
		if inFence {
			if fenceLang == "" || shellLang[fenceLang] {
				c.command(n, line)
			}
			continue
		}
		for _, m := range inlineRe.FindAllStringSubmatch(line, -1) {
			s := strings.TrimSpace(m[1])
			if looksLikeCommand(s) {
				c.command(n, s)
			} else {
				c.pathRef(n, s, false)
			}
		}
		for _, m := range linkRe.FindAllStringSubmatch(line, -1) {
			c.pathRef(n, m[1], true)
		}
		if strings.HasSuffix(file, "CLAUDE.md") {
			for _, m := range importRe.FindAllStringSubmatch(line, -1) {
				c.pathRef(n, m[1], true)
			}
		}
	}
	if d.Tokens > o.MaxTokens {
		where := "with every request"
		if path.Dir(file) != "." && file != ".github/copilot-instructions.md" {
			where = "whenever an agent works in " + path.Dir(file) + "/"
		}
		c.add(1, "too-long", Warn, fmt.Sprintf("about %d tokens, loaded %s; over the %d-token budget", d.Tokens, where, o.MaxTokens), "")
	}
	c.packageManager()
	d.Commands, d.Paths, d.Findings = c.commands, c.paths, c.findings
	sort.SliceStable(d.Findings, func(i, j int) bool { return d.Findings[i].Line < d.Findings[j].Line })
	return d
}

var shellLang = map[string]bool{"sh": true, "bash": true, "shell": true, "console": true, "zsh": true, "shell-session": true, "terminal": true, "cmd": true, "powershell": false}

type checker struct {
	r        Repo
	ix       *index
	file     string
	dir      string
	findings []Finding
	commands int
	paths    int
	pms      map[string]int // package managers mentioned, with first line
	seen     map[string]bool
	// lazily built from every package.json / Makefile in the repository
	allScripts map[string]string
	allTargets map[string]bool
	gitignore  []string
	submodules []string
	deps       map[string]bool
}

func (c *checker) add(line int, rule string, sev Severity, msg, text string) {
	key := rule + "\x00" + text
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	if text != "" && c.seen[key] {
		return
	}
	c.seen[key] = true
	c.findings = append(c.findings, Finding{File: c.file, Line: line, Rule: rule, Severity: sev, Message: msg, Text: text})
}

// MarshalJSON keeps nil findings as [].
func (d Doc) MarshalJSON() ([]byte, error) {
	type alias Doc
	if d.Findings == nil {
		d.Findings = []Finding{}
	}
	return json.Marshal(alias(d))
}

// dedupe flags instruction files that are near-copies of each other.
func dedupe(docs []Doc, bodies map[string]string) []Doc {
	for i := range docs {
		for j := i + 1; j < len(docs); j++ {
			a, b := bodies[docs[i].Path], bodies[docs[j].Path]
			if path.Dir(docs[i].Path) != path.Dir(docs[j].Path) || len(a) < 200 || len(b) < 200 {
				continue
			}
			if s := similarity(a, b); s >= 0.6 {
				docs[j].Findings = append(docs[j].Findings, Finding{File: docs[j].Path, Line: 1, Rule: "duplicate", Severity: Warn,
					Message: fmt.Sprintf("%.0f%% of its lines also appear in %s; two copies drift apart, use a symlink or an import", s*100, docs[i].Path)})
			}
		}
	}
	return docs
}

func similarity(a, b string) float64 {
	set := map[string]bool{}
	for _, l := range strings.Split(a, "\n") {
		if l = strings.TrimSpace(l); len(l) > 3 {
			set[l] = true
		}
	}
	shared, total := 0, 0
	for _, l := range strings.Split(b, "\n") {
		if l = strings.TrimSpace(l); len(l) > 3 {
			total++
			if set[l] {
				shared++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(shared) / float64(total)
}
