import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";

// The shared routing reminder. It restates the same boundary the code-search
// skill defines: structural questions go to the read-only prowl-agent CLI;
// grep and glob stay reserved for exact text and filename lookups.
const ROUTING_REMINDER =
  "Prowl routing reminder: for structural repository questions -- where code is, " +
  "what a symbol does, who calls it, a file's shape, or a change's blast radius -- " +
  "the read-only `prowl-agent` CLI (search, find, def, outline, references, impact) " +
  "answers in one cited call instead of scanning many files. Reserve grep for an " +
  "exact literal or regex match and glob for filename patterns.";

// Shell searches that fan across the tree; the structural questions they
// approximate are the ones prowl-agent answers in a single cited call.
const TREE_SEARCH = /(^|[|&;\s])(rg|grep|egrep|fgrep|ag|ack|find|fd|fdfind)\b/;

function repoWidePath(input: Record<string, unknown>): boolean {
  const path = String(input?.path ?? "").trim();
  return path === "" || path === "." || path === "./" || path === "/";
}

// Be conservative: ambiguity suppresses the advisory. Grep-family commands are
// wide only when they have no operand after the pattern; find/fd are wide only
// when their search root is absent or the repository root.
function bashIsRepoWide(command: string): boolean {
  for (const segment of command.split(/[|&;\n]+/)) {
    const match = TREE_SEARCH.exec(segment);
    if (!match) continue;
    const utility = match[2];
    const words =
      segment
        .slice((match.index ?? 0) + match[0].length)
        .match(/"[^"]*"|'[^']*'|[^\s]+/g)
        ?.filter((word) => !word.startsWith("-")) ?? [];
    if (utility === "find") {
      if (words.length === 0 || [".", "./", "/"].includes(words[0])) return true;
      continue;
    }
    if (utility === "fd" || utility === "fdfind") {
      if (words.length <= 1 || [".", "./", "/"].includes(words[1])) return true;
      continue;
    }
    if (words.length <= 1) return true;
  }
  return false;
}

// isBroadSearch reports whether a completed tool call was a repository-wide
// search worth reminding about. File- and directory-bounded native controls
// remain untouched.
function isBroadSearch(toolName: string, input: Record<string, unknown>): boolean {
  if (toolName === "grep" || toolName === "glob") return repoWidePath(input);
  if (toolName === "bash") {
    const command = String(input?.command ?? "");
    return TREE_SEARCH.test(command) && bashIsRepoWide(command);
  }
  return false;
}

// prowl-routing is advisory only. It observes tool_result (never tool_call), so
// it can neither block a tool nor rewrite its input; after a broad search it
// appends one reminder text chunk to the result and leaves everything else --
// existing content, details, error state -- untouched.
export default function prowlRouting(pi: ExtensionAPI) {
  pi.on("tool_result", async (event) => {
    if (event.isError) return;
    if (!isBroadSearch(event.toolName, event.input ?? {})) return;
    return {
      content: [
        ...event.content,
        { type: "text", text: `\n\n${ROUTING_REMINDER}` },
      ],
    };
  });
}
