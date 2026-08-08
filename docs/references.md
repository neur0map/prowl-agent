# Reference sources

A running list of external projects worth mining for prowl-agent ideas: skill
authoring, adoption mechanics, hooks, scripts, and token economy. Revisit these
when designing new skills or features. Status: `deep` = read in detail, `scan` =
surveyed via summary, `todo` = not yet reviewed.

## Skills and methodology frameworks

- https://github.com/obra/superpowers  (deep)
  The reference skills framework. SKILL.md = YAML frontmatter (`name`,
  `description`) + body; the description MUST start with "Use when" and state
  triggers only, never workflow (agents follow the description and skip the body
  otherwise). Adoption is a session-start hook that injects a bootstrap skill;
  multi-harness support via per-harness plugin manifests that ship skills+hooks
  through the harness install mechanism, never the user's global config. Skills
  name actions, not tool names. Copy: description discipline, per-harness install,
  the porting invariants in docs/porting-to-a-new-harness.md.

- https://github.com/addyosmani/agent-skills  (scan)
  Skills collection. Mine for extra code-exploration skill ideas and description
  phrasing.

- https://github.com/mattpocock/skills  (scan)
  Skills collection. Same use: authoring patterns and trigger phrasing.

- https://github.com/bmad-code-org/BMAD-METHOD  (scan)
  Agile multi-agent methodology with personas and lifecycle phases. Do NOT adopt
  the persona/lifecycle framework; prowl is a focused context engine. Only mine
  its artifact-continuity idea (structured docs passed between steps), which our
  durable-knowledge skill already covers.

- https://github.com/gsd-build/get-shit-done  (scan)
  Workflow/methodology. Mine only for concrete skill triggers, not the framework.

- https://github.com/garrytan/gstack  (todo)
- https://github.com/affaan-m/ECC  (todo)
- https://github.com/juliusbrussee/caveman  (todo)

## Token economy and minimalism

- https://github.com/rtk-ai/rtk  (deep)
  CLI proxy that compresses bash output (ls/cat/grep/git/test) before the agent
  reads it. Complementary to prowl, not a competitor: rtk compresses shell output,
  prowl indexes code structure. Confirms our per-harness `init` install pattern
  (`rtk init --agent <harness>` writes into `.claude/skills`, `.claude/hooks`,
  etc.). Ideas to consider: a PreToolUse "suggest" hook that nudges toward prowl
  when the agent is about to grep or read many files (harness-specific, defer
  until testable); a `gain`/savings dashboard (prowl already has `status`
  savings). Honest-savings framing matches our TOKENS.md.

- https://github.com/dietrichgebert/ponytail  (scan)
  Minimalism: laziest-solution-that-works, intensity levels (lite/full/ultra).
  Maps to prowl's existing `--mode compact|standard|full` and `--budget-tokens`.
  Ethos to keep: return the least context that answers the question.

- https://github.com/diegosouzapw/OmniRoute  (todo)
  LLM provider router. Out of scope for prowl proper: prowl stays provider-neutral
  and lets the host own model routing (via MCP sampling). Relevant only as
  background for the host-powered rerank feature (route rerank to a cheap model).

## Harness

- https://github.com/can1357/oh-my-pi  (deep)
  The primary target harness. 60+ providers, first-class subagents (`task` into
  isolated worktrees, typed results, `agent://` artifacts), native skill discovery
  from skill directories, MCP sampling. Skills installed into its skill dir surface
  automatically at session start, so no separate bootstrap hook is required there.

## Applied to prowl so far

- Shipped skills with "Use when" descriptions: prowl-repo-exploration,
  prowl-change-safety, prowl-durable-knowledge.
- `init` installs skills per detected harness skill directory; AGENTS.md is the
  fallback for harnesses without a skill system.
- `explore` indexes a foreign repo without polluting it.
- Host-powered rerank via MCP sampling: cheap models fix full-text ranking noise
  (proven on real queries), so semantic-quality relevance needs no local Ollama.
