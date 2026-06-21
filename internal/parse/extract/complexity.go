package extract

import sitter "github.com/alexaandru/go-tree-sitter-bare"

// jsDecision is the decision-point node set shared by JavaScript, TypeScript,
// and TSX (one grammar family).
var jsDecision = map[string]bool{
	"if_statement": true, "for_statement": true, "for_in_statement": true,
	"while_statement": true, "do_statement": true, "switch_case": true,
	"catch_clause": true, "ternary_expression": true,
}

// decisionNodes lists, per language, the tree-sitter node types that introduce a
// branch (a decision point). A function's cyclomatic-style complexity is 1 plus
// the count of these nodes in its body. Boolean operators (&&, ||) are not
// counted, so this is a slight undercount of strict McCabe but a faithful
// ranking of which functions are most branch-heavy. Names verified per grammar.
var decisionNodes = map[string]map[string]bool{
	"go": {
		"if_statement": true, "for_statement": true,
		"expression_case": true, "type_case": true, "communication_case": true,
	},
	"python": {
		"if_statement": true, "elif_clause": true, "for_statement": true,
		"while_statement": true, "except_clause": true, "case_clause": true,
		"conditional_expression": true,
	},
	"rust": {
		"if_expression": true, "for_expression": true, "while_expression": true,
		"match_arm": true,
	},
	"cpp": {
		"if_statement": true, "for_statement": true, "while_statement": true,
		"do_statement": true, "case_statement": true, "catch_clause": true,
		"conditional_expression": true,
	},
	"java": {
		"if_statement": true, "for_statement": true, "enhanced_for_statement": true,
		"while_statement": true, "do_statement": true, "catch_clause": true,
		"switch_block_statement_group": true, "ternary_expression": true,
	},
	"ruby": {
		"if": true, "elsif": true, "unless": true, "while": true, "until": true,
		"for": true, "when": true, "conditional": true, "rescue": true,
		"if_modifier": true, "unless_modifier": true, "while_modifier": true,
		"until_modifier": true, "rescue_modifier": true,
	},
	"javascript": jsDecision,
	"typescript": jsDecision,
	"tsx":        jsDecision,
}

// complexity returns a cyclomatic-style complexity for the function or method
// whose definition node is n: 1 plus the number of decision-point nodes in its
// subtree. Languages without a decision set return 1.
func complexity(n sitter.Node, lang string) int {
	set := decisionNodes[lang]
	if set == nil {
		return 1
	}
	count := 1
	var walk func(sitter.Node)
	walk = func(node sitter.Node) {
		if set[node.Type()] {
			count++
		}
		for i := uint32(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(n)
	return count
}
