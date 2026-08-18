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

// isBroadSearch reports whether a completed tool call was a wide repository
// search worth reminding about: the dedicated grep/glob tools always are, and a
// bash call is when its command shells out to a tree-search utility.
function isBroadSearch(toolName: string, input: Record<string, unknown>): boolean {
  if (toolName === "grep" || toolName === "glob") return true;
  if (toolName === "bash") return TREE_SEARCH.test(String(input?.command ?? ""));
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
