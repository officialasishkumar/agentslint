# agentslint

Do your agent instructions still match your repository?

`AGENTS.md`, `CLAUDE.md`, `.github/copilot-instructions.md` and Cursor rules tell
coding agents how to build, test and navigate a project. They are written once
and rarely checked again, while the code around them moves. When a file says
`npm run typecheck` and that script was renamed, or points to `src/config/` and
that directory moved, the agent follows the instruction anyway, fails, and
spends tokens finding its own way.

`agentslint` checks every instruction file in a repository against the
repository itself:

| rule | what it catches |
|---|---|
| `missing-path` | a file, directory, script or glob the instructions mention that exists nowhere in the repository |
| `missing-script` | `npm/pnpm/yarn/bun run X` where no package.json defines `X` |
| `missing-make-target` | `make X` where no Makefile has target `X` |
| `missing-just-recipe` | `just X` where no justfile has recipe `X` |
| `wrong-package-manager` | `npm install` in a repo that commits only `pnpm-lock.yaml` (and similar) |
| `too-long` | files over ~4,000 tokens, which are loaded into context on every request (root) or whenever an agent works in that directory (nested) |
| `duplicate` | an `AGENTS.md` and `CLAUDE.md` that are near-copies of each other and will drift apart (use a symlink or an import) |

A real run, on cline/cline at commit 34c104e (23 Nov 2026):

```
$ agentslint --warn-ok
.github/copilot-instructions.md  ~951 tokens, 6 commands and 21 paths checked
  .github/copilot-instructions.md:14  error missing-path  no such file or directory anywhere in the repository  `src/shared/proto/`
  .github/copilot-instructions.md:30  error missing-path  no such file or directory anywhere in the repository  `src/shared/providers/providers.json`
  .github/copilot-instructions.md:36  error missing-path  no such file or directory anywhere in the repository  `src/core/prompts/system-prompt/tools/`
  .github/copilot-instructions.md:38  warn  missing-path  no file matches this pattern  `src/core/prompts/system-prompt/variants/*/config.ts`
  .github/copilot-instructions.md:39  error missing-path  no such file or directory anywhere in the repository  `src/core/task/tools/handlers/`
  ...
AGENTS.md  ~1363 tokens, 19 commands and 1 paths checked
sdk/AGENTS.md  ~1399 tokens, 12 commands and 6 paths checked

4 files, 7 errors, 1 warnings
```

Git history shows `src/core/prompts/system-prompt/tools/` existed until May 2026:
the Copilot instructions still describe the old layout.

## The census: GitHub's most popular repositories

We took the 2,000 most-starred GitHub repositories pushed since 1 Aug 2026
(18,000+ stars each) and linted every one that has agent instructions
(24 Nov 2026; data in [results/](results/)).

- **735 of 2,000 (37%)** have an `AGENTS.md` at the root; 806 have `AGENTS.md`
  or `CLAUDE.md`. Together they hold **4,564** instruction files (3,200
  `AGENTS.md`, 1,041 `CLAUDE.md`, 112 Cursor rules, 97 Copilot instruction files, 88 `GEMINI.md`).
- **281 of 804 repositories (35%)** have at least one instruction that refers to
  a path, script, make target or recipe that does not exist anywhere in the
  repository; **833 files (18%)** have at least one such error.
- **Size:** median 868 tokens; **517 files (11%) are over 4,000 tokens**; the
  largest is 72,526 tokens.
- **62 repositories** tell agents to use a package manager other than the one
  whose lockfile they commit (`npm install` in a pnpm repository, and so on).

| rule | findings | repositories |
|---|---:|---:|
| missing-path | 1,569 | 268 |
| too-long | 517 | 211 |
| wrong-package-manager | 168 | 62 |
| missing-script | 124 | 48 |
| duplicate | 35 | 24 |
| missing-make-target | 23 | 13 |
| missing-just-recipe | 8 | 4 |

**How many findings are real?** We drew 200 `missing-path` errors at random
and checked each one by hand and against git history
([results/verify-sample.tsv](results/verify-sample.tsv)):

| verdict | share |
|---|---:|
| the path existed and was later moved or deleted (git history) | 28% |
| broken in another way: wrong relative path, file never written, link copied into a machine translation | ~55% |
| false alarm: runtime state files, placeholders, paths in another repository | ~17% |

So about five in six flagged paths are real, and more than a quarter are
instructions that were correct once and drifted as the code moved.


## It is deliberately conservative

A linter that cries wolf gets turned off. agentslint only reports what it can
show is wrong:

- An inline code span counts as a path only if it has a file extension, a
  trailing slash, or starts with `./` or `../`; bare names like `utils.py` are
  left alone, because they often describe a kind of file.
- A path is accepted if it exists relative to the instruction file, to the
  repository root, **or as the tail of any path in the repository**, so
  `workbench/browser/` matches `src/vs/workbench/browser/`.
- A script, make target or just recipe is accepted if **any** manifest in the
  repository defines it: monorepo instructions often mean "run this in that
  package".
- Placeholders (`<name>`, `{id}`, `path/to/`, `YourService`, `X.Y`), gitignored
  paths, git submodules, paths outside the repository, per-user agent
  directories (`.claude/`, `.cursor/`, `.agents/`), and Makefiles with includes
  or pattern rules are skipped.

## Install and use

```
go install github.com/officialasishkumar/agentslint@latest

agentslint                 # lint the current repository; exit 1 on errors or warnings
agentslint path/to/repo --warn-ok --json
```

In CI (GitHub Actions):

```yaml
- uses: actions/setup-go@v5
  with: { go-version: stable }
- run: go install github.com/officialasishkumar/agentslint@latest && agentslint --warn-ok
```

Census tooling, to repeat the study:

```
GITHUB_TOKEN=... agentslint discover -min-stars 2000 -limit 2000 -pushed 2026-08-01 -out repos.tsv
agentslint census -repos repos.tsv -cache ./cache -out census.jsonl -workers 12
agentslint census-report census.jsonl
GITHUB_TOKEN=... python3 scripts/verify_sample.py census.jsonl 200 > verify.tsv
```

`census` makes blobless, depth-1 bare clones and reads only the files it needs,
so 806 repositories fit in a few GB.

## License

MIT
