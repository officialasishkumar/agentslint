# AGENTS.md census

- Repositories linted: **806** (0 could not be cloned)
- With at least one agent instruction file: **804**, holding **4564** files
- Repositories with at least one instruction that cannot work as written: **281 (35%)**
- Files with at least one such error: **833 of 4564 (18%)**
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
| missing-path | 1569 | 268 |
| too-long | 517 | 211 |
| wrong-package-manager | 168 | 62 |
| missing-script | 124 | 48 |
| duplicate | 35 | 24 |
| missing-make-target | 23 | 13 |
| missing-just-recipe | 8 | 4 |

## Examples (errors)

- `` CLAUDE.md:66 missing-path: no such file or directory anywhere in the repository `packages/server/src/tests/TestConfiguration.ts`
- `claude-code-best/claude-code` CLAUDE.md:280 missing-path: no such file or directory anywhere in the repository `packages/swarm/`
- `conductor-oss/conductor` CLAUDE.md:54 missing-path: no such file or directory anywhere in the repository `conductor-cli/cmd/workflow.go`
- `PostHog/posthog` .cursor/rules/django-python.mdc:113 missing-path: requirements file does not exist `pip install -r ...`
- `cline/cline` .github/copilot-instructions.md:14 missing-path: no such file or directory anywhere in the repository `src/shared/proto/`
- `dataease/dataease` AGENTS.md:48 missing-path: no such file or directory anywhere in the repository `resources/static/`
- `dataelement/dsh-desktop` AGENTS.md:116 missing-script: no package.json in the repository defines a "lint" script (nearest, package.json, has: build, dev, icons:generate, loader:generate, office:prepare, package:dev:dir, package:dev:linux:x64, package:dev:mac:arm64, …) `npm run lint`
- `datawhalechina/easy-vibe` CLAUDE.md:92 missing-path: no such file or directory anywhere in the repository `stage-2/backend/what-is-api/extra2/extra2-what-is-api.md`
- `decolua/9router` CLAUDE.md:45 missing-path: no such file or directory anywhere in the repository `cloud/src/handlers/embeddings.js`
- `bytedance/deer-flow` .github/copilot-instructions.md:12 missing-make-target: Makefile has no target "docker-*", and no other Makefile does `make docker-*`
- `davila7/claude-code-templates` cli-tool/components/skills/ai-research/loki-mode/CLAUDE.md:59 missing-path: no such file or directory anywhere in the repository `.loki/memory/episodic/`
- `docker-mailserver/docker-mailserver` AGENTS.md:36 missing-make-target: Makefile has no target "tests/parallel/set1", and no other Makefile does `make clean generate-accounts tests/parallel/set1`
- `docmirror/dev-sidecar` .github/copilot-instructions.md:35 missing-path: no such file or directory anywhere in the repository `packages/core/src/index.js`
- `doocs/leetcode` AGENTS.md:28 missing-path: file passed to python3 does not exist `python3 build_site.py`
