package mcp

import (
	"context"
	"embed"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed promptdata/*.md
var promptFS embed.FS

type promptSpec struct {
	name, title, description, file, argument, argumentDescription string
	required                                                      bool
}

func registerPrompts(server *sdk.Server) {
	specifications := []promptSpec{
		{name: "understand-project", title: "Understand project", description: "Build a progressive, cited project orientation.", file: "understand-project.md", argument: "focus", argumentDescription: "Optional area to emphasize."},
		{name: "research-topic", title: "Research topic", description: "Research a topic from local knowledge and evidence.", file: "research-topic.md", argument: "topic", argumentDescription: "Topic to research.", required: true},
		{name: "review-change", title: "Review change", description: "Review a changed path with structural impact.", file: "review-change.md", argument: "path", argumentDescription: "Project-relative changed path.", required: true},
		{name: "update-knowledge", title: "Update knowledge", description: "Prepare a cited, reviewable OKF update.", file: "update-knowledge.md", argument: "objective", argumentDescription: "Knowledge objective.", required: true},
		{name: "prepare-implementation", title: "Prepare implementation", description: "Prepare bounded implementation context and steps.", file: "prepare-implementation.md", argument: "task", argumentDescription: "Implementation task.", required: true},
	}
	for _, specification := range specifications {
		specification := specification
		prompt := &sdk.Prompt{Name: specification.name, Title: specification.title, Description: specification.description, Arguments: []*sdk.PromptArgument{{Name: specification.argument, Description: specification.argumentDescription, Required: specification.required}}}
		server.AddPrompt(prompt, func(_ context.Context, request *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			value := strings.TrimSpace(request.Params.Arguments[specification.argument])
			if specification.required && value == "" {
				return nil, fmt.Errorf("prompt argument %q is required", specification.argument)
			}
			data, err := promptFS.ReadFile("promptdata/" + specification.file)
			if err != nil {
				return nil, err
			}
			text := strings.ReplaceAll(string(data), "{{"+specification.argument+"}}", value)
			return &sdk.GetPromptResult{Description: specification.description, Messages: []*sdk.PromptMessage{{Role: sdk.Role("user"), Content: &sdk.TextContent{Text: text}}}}, nil
		})
	}
}
