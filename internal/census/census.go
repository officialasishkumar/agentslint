// Package census lints many public repositories and summarises the results.
package census

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/officialasishkumar/agentslint/internal/lint"
)

// Result is one repository's census row.
type Result struct {
	Repo     string     `json:"repo"`
	Commit   string     `json:"commit,omitempty"`
	Date     string     `json:"date,omitempty"` // commit date of HEAD
	Docs     []lint.Doc `json:"docs"`
	Errors   int        `json:"errors"`
	Warnings int        `json:"warnings"`
	Err      string     `json:"err,omitempty"`
}

// Run blobless-clones repo (or reuses the clone) and lints HEAD.
func Run(repo, cache string) Result {
	res := Result{Repo: repo}
	dir := filepath.Join(cache, strings.ReplaceAll(repo, "/", "__")+".git")
	if _, err := os.Stat(dir); err != nil {
		cmd := exec.Command("git", "clone", "--quiet", "--bare", "--depth", "1", "--filter=blob:none", "https://github.com/"+repo+".git", dir)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			os.RemoveAll(dir)
			res.Err = "clone: " + strings.TrimSpace(string(out))
			return res
		}
	}
	if out, err := exec.Command("git", "--git-dir", dir, "log", "-1", "--format=%H %cs").Output(); err == nil {
		f := strings.Fields(string(out))
		if len(f) == 2 {
			res.Commit, res.Date = f[0], f[1]
		}
	}
	g, err := lint.OpenGit(dir, "HEAD")
	if err != nil {
		res.Err = "ls-tree: " + err.Error()
		return res
	}
	res.Docs = lint.Lint(g, lint.Options{})
	for _, d := range res.Docs {
		for _, f := range d.Findings {
			if f.Severity == lint.Error {
				res.Errors++
			} else {
				res.Warnings++
			}
		}
	}
	return res
}

// Load reads census JSON Lines.
func Load(path string) ([]Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Result
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<26)
	for s.Scan() {
		var r Result
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, s.Err()
}

// Row is one discovered repository.
type Row struct {
	Repo      string
	Stars     int
	Language  string
	HasAgents bool
	HasClaude bool
}

// Discover pages GitHub search by descending star ranges (search returns at
// most 1,000 results per query) and checks each repository's root for
// AGENTS.md and CLAUDE.md in the same GraphQL request.
func Discover(token string, minStars, limit int, pushed string, log io.Writer) ([]Row, error) {
	var rows []Row
	seen := map[string]bool{}
	upper := 0 // 0: no upper bound
	for len(rows) < limit {
		rng := fmt.Sprintf("stars:>=%d", minStars)
		if upper > 0 {
			rng = fmt.Sprintf("stars:%d..%d", minStars, upper)
		}
		q := fmt.Sprintf("%s pushed:>=%s fork:false archived:false sort:stars-desc", rng, pushed)
		var after any
		lowest := 0
		got := 0
		for len(rows) < limit {
			var resp struct {
				Data struct {
					Search struct {
						PageInfo struct {
							HasNextPage bool
							EndCursor   string
						}
						Nodes []struct {
							NameWithOwner   string
							StargazerCount  int
							PrimaryLanguage *struct{ Name string }
							Agents          *struct{ Oid string } `json:"agents"`
							Claude          *struct{ Oid string } `json:"claude"`
						}
					}
				}
				Errors []struct{ Message string }
			}
			err := graphql(token, `query($q:String!,$after:String){search(query:$q,type:REPOSITORY,first:50,after:$after){
				pageInfo{hasNextPage endCursor}
				nodes{... on Repository{nameWithOwner stargazerCount primaryLanguage{name}
					agents:object(expression:"HEAD:AGENTS.md"){oid} claude:object(expression:"HEAD:CLAUDE.md"){oid}}}}}`,
				map[string]any{"q": q, "after": after}, &resp)
			if err != nil {
				return rows, err
			}
			for _, n := range resp.Data.Search.Nodes {
				if n.NameWithOwner == "" || seen[n.NameWithOwner] {
					continue
				}
				seen[n.NameWithOwner] = true
				r := Row{Repo: n.NameWithOwner, Stars: n.StargazerCount, HasAgents: n.Agents != nil, HasClaude: n.Claude != nil}
				if n.PrimaryLanguage != nil {
					r.Language = n.PrimaryLanguage.Name
				}
				rows = append(rows, r)
				got++
				lowest = n.StargazerCount
			}
			fmt.Fprintf(log, "discovered %d repositories (down to %d stars)\n", len(rows), lowest)
			if !resp.Data.Search.PageInfo.HasNextPage {
				break
			}
			after = resp.Data.Search.PageInfo.EndCursor
		}
		if got == 0 || lowest <= minStars {
			break
		}
		upper = lowest // next window: everything at or below the lowest seen
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Stars > rows[j].Stars })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func graphql(token, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	for attempt := 0; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://api.github.com/graphql", bytes.NewReader(body))
		req.Header.Set("Authorization", "bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == 200 {
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(out)
		}
		if resp != nil {
			resp.Body.Close()
		}
		if attempt >= 4 {
			if err == nil {
				err = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Second)
	}
}
