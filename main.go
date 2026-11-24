// agentslint checks agent instruction files (AGENTS.md, CLAUDE.md,
// copilot-instructions.md, .cursor/rules) against the repository they
// describe: commands that no longer run, paths that no longer exist,
// package managers the repo stopped using, and files too long to send on
// every request.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/officialasishkumar/agentslint/internal/census"
	"github.com/officialasishkumar/agentslint/internal/lint"
)

var version = "dev"

const usage = `agentslint: do your AGENTS.md instructions still match the repository?

Usage:
  agentslint [check] [DIR] [--json] [--max-tokens N] [--warn-ok]
      Lint every agent instruction file under DIR (default .). Exits 1 if
      any error is found (warnings too, unless --warn-ok).
  agentslint discover [-min-stars 1000] [-limit 1000] [-out repos.txt]
      List popular GitHub repositories and whether each has an AGENTS.md
      (needs GITHUB_TOKEN).
  agentslint census -repos repos.txt [-cache DIR] [-out census.jsonl] [-workers 8]
      Blobless-clone each repository and lint it.
  agentslint census-report census.jsonl
      Summarise a census as Markdown.
  agentslint version

Files checked: AGENTS.md (any directory), AGENT.md, CLAUDE.md, GEMINI.md,
.github/copilot-instructions.md, .cursorrules, .windsurfrules, .cursor/rules/*.mdc
`

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentslint:", err)
		os.Exit(2)
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			fmt.Print(usage)
			return 0, nil
		case "version":
			fmt.Println("agentslint", version)
			return 0, nil
		case "check":
			return check(args[1:])
		case "discover":
			return 0, discover(args[1:])
		case "census":
			return 0, runCensus(args[1:])
		case "census-report":
			return 0, censusReport(args[1:])
		}
	}
	return check(args)
}

func check(args []string) (int, error) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	maxTok := fs.Int("max-tokens", 4000, "warn when a file is longer than this many tokens")
	warnOK := fs.Bool("warn-ok", false, "exit 0 when there are only warnings")
	var pos []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			return 2, err
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	dir := "."
	if len(pos) > 0 {
		dir = pos[0]
	}
	r, err := lint.OpenDir(dir)
	if err != nil {
		return 2, err
	}
	docs := lint.Lint(r, lint.Options{MaxTokens: *maxTok})
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(docs); err != nil {
			return 2, err
		}
	} else {
		printText(docs)
	}
	errs, warns := count(docs)
	if errs > 0 || (warns > 0 && !*warnOK) {
		return 1, nil
	}
	return 0, nil
}

func count(docs []lint.Doc) (errs, warns int) {
	for _, d := range docs {
		for _, f := range d.Findings {
			if f.Severity == lint.Error {
				errs++
			} else {
				warns++
			}
		}
	}
	return
}

func printText(docs []lint.Doc) {
	if len(docs) == 0 {
		fmt.Println("no agent instruction files found")
		return
	}
	for _, d := range docs {
		fmt.Printf("%s  ~%d tokens, %d commands and %d paths checked\n", d.Path, d.Tokens, d.Commands, d.Paths)
		for _, f := range d.Findings {
			t := ""
			if f.Text != "" {
				t = "  `" + f.Text + "`"
			}
			fmt.Printf("  %s:%d  %-5s %-22s %s%s\n", f.File, f.Line, f.Severity, f.Rule, f.Message, t)
		}
	}
	errs, warns := count(docs)
	fmt.Printf("\n%d files, %d errors, %d warnings\n", len(docs), errs, warns)
}

func discover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	minStars := fs.Int("min-stars", 1000, "minimum stars")
	limit := fs.Int("limit", 1000, "how many repositories to examine")
	pushed := fs.String("pushed", "2026-09-01", "only repositories pushed since this date")
	out := fs.String("out", "repos.tsv", "output: repo, stars, language, has AGENTS.md, has CLAUDE.md")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tok := token()
	if tok == "" {
		return errors.New("set GITHUB_TOKEN")
	}
	rows, err := census.Discover(tok, *minStars, *limit, *pushed, os.Stderr)
	if err != nil && len(rows) == 0 {
		return err
	}
	f, err2 := os.Create(*out)
	if err2 != nil {
		return err2
	}
	defer f.Close()
	for _, r := range rows {
		fmt.Fprintf(f, "%s\t%d\t%s\t%v\t%v\n", r.Repo, r.Stars, r.Language, r.HasAgents, r.HasClaude)
	}
	return err
}

func token() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GH_TOKEN")
}

func runCensus(args []string) error {
	fs := flag.NewFlagSet("census", flag.ContinueOnError)
	reposFile := fs.String("repos", "", "file with owner/name as the first column")
	cache := fs.String("cache", filepath.Join(os.TempDir(), "agentslint-cache"), "where bare clones go")
	out := fs.String("out", "census.jsonl", "output JSON Lines (appended; finished repos are skipped)")
	workers := fs.Int("workers", 8, "parallel clones")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *reposFile == "" {
		return errors.New("need -repos FILE")
	}
	repos, err := readFirstColumn(*reposFile)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	if old, err := census.Load(*out); err == nil {
		for _, r := range old {
			done[r.Repo] = true
		}
	}
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(*cache, 0o755); err != nil {
		return err
	}
	var mu sync.Mutex
	jobs := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range jobs {
				res := census.Run(repo, *cache)
				mu.Lock()
				_ = json.NewEncoder(f).Encode(res)
				status := fmt.Sprintf("%d files, %d findings", len(res.Docs), res.Errors+res.Warnings)
				if res.Err != "" {
					status = res.Err
				}
				fmt.Fprintf(os.Stderr, "%s: %s\n", repo, status)
				mu.Unlock()
			}
		}()
	}
	for _, r := range repos {
		if !done[r] {
			jobs <- r
		}
	}
	close(jobs)
	wg.Wait()
	return nil
}

func censusReport(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: agentslint census-report census.jsonl")
	}
	res, err := census.Load(args[0])
	if err != nil {
		return err
	}
	census.Markdown(os.Stdout, res)
	return nil
}

func readFirstColumn(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		l := strings.TrimSpace(s.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		cols := strings.Split(l, "\t")
		if len(cols) >= 4 && cols[3] == "false" && (len(cols) < 5 || cols[4] == "false") {
			continue // discover output: no instruction file at the root
		}
		out = append(out, cols[0])
	}
	sort.Strings(out)
	return out, s.Err()
}
