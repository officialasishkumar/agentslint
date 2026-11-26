# AGENTS.md census

- Repositories linted: **806** (0 could not be cloned)
- With at least one agent instruction file: **804**, holding **4564** files
- Repositories with at least one instruction that cannot work as written: **280 (35%)**
- Files with at least one such error: **832 of 4564 (18%)**
- Size: median **868** tokens, 90th percentile **4378**, largest **72526**; 517 files (11%) over 4,000 tokens

## Files by name

| file | count |
|---|---:|
| AGENTS.md | 3200 |
| CLAUDE.md | 1041 |
| .cursor/rules/*.mdc | 112 |
| copilot-instructions.md | 97 |
| GEMINI.md | 88 |
| .cursorrules | 18 |
| AGENT.md | 5 |
| .windsurfrules | 3 |

## Findings by rule

| rule | findings | repositories |
|---|---:|---:|
| missing-path | 1568 | 267 |
| too-long | 517 | 211 |
| wrong-package-manager | 168 | 62 |
| missing-script | 124 | 48 |
| duplicate | 35 | 24 |
| missing-make-target | 23 | 13 |
| missing-just-recipe | 8 | 4 |

## Examples (errors)

- `` AGENTS.md:114 missing-path: no such file or directory anywhere in the repository `src/third_party/browser.js`
- `bytedance/deer-flow` AGENTS.md:50 missing-path: no such file or directory anywhere in the repository `skills/custom/`
- `ccxt/ccxt` CLAUDE.md:30 missing-path: no such file or directory anywhere in the repository `cs/ccxt/wrappers/`
- `charmbracelet/crush` AGENTS.md:106 missing-path: Go package path does not exist `go test ./internal/llm/prompt -run TestGetContextFromPaths`
- `chakra-ui/chakra-ui` CLAUDE.md:176 missing-path: no such file or directory anywhere in the repository `.changeset/fix-recipe-bracket-syntax.md`
- `cjpais/Handy` AGENTS.md:93 missing-path: no such file or directory anywhere in the repository `lib/types.ts`
- `claude-code-best/claude-code` AGENTS.md:37 missing-script: no package.json in the repository defines a "src/entrypoints/cli.tsx" script (nearest, package.json, has: build, build:bun, build:vite, build:vite:only, check, check:bundle, check:fix, check:unused, …) `bun run src/entrypoints/cli.tsx -p`
- `cline/cline` .github/copilot-instructions.md:14 missing-path: no such file or directory anywhere in the repository `src/shared/proto/`
- `biomejs/biome` AGENTS.md:35 missing-just-recipe: justfile has no recipe "gen-grammar", and no other justfile does `just gen-grammar <lang>`
- `cheaper-inference/OmniRoute` AGENTS.md:177 missing-path: no such file or directory anywhere in the repository `path/to/file.ts`
