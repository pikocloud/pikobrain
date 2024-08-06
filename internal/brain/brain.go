package brain

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/pikocloud/pikobrain/internal/providers/types"
)

type Brain struct {
	prompt     *template.Template
	config     types.Config
	iterations int
	provider   types.Provider
	toolbox    types.Toolbox
}

func (m *Brain) Run(ctx context.Context, messages []types.Message) (Response, error) {
	// generate prompt
	var prompt bytes.Buffer

	if err := m.prompt.Execute(&prompt, promptContext{
		Messages: messages,
	}); err != nil {
		return nil, fmt.Errorf("render prompt: %w", err)
	}

	cfg := m.config
	cfg.Prompt = prompt.String() // replace prompt to rendered template

	tools := m.toolbox.Snapshot()
	toolSet := tools.Definitions()

	var ans Response
	for range m.iterations {
		res, err := m.provider.Invoke(ctx, cfg, messages, toolSet)
		if err != nil {
			return ans, fmt.Errorf("invoke provider: %w", err)
		}
		ans = append(ans, res)

		calls := res.ToolCalls()
		if len(calls) == 0 {
			break
		}

		for _, msg := range res.Output {
			slog.Debug("output message", "message", msg)
		}

		messages = append(messages, res.Output...)

		for _, call := range calls {
			slog.Debug("calling tool", "tool", call.ToolName, "id", call.ToolID, "input", call.Content.String())
			started := time.Now()
			result, err := tools.Call(ctx, call.ToolName, call.Content.Data)
			if err != nil {
				return ans, fmt.Errorf("call tool %q: %w", call.ToolName, err)
			}
			duration := time.Since(started)
			slog.Debug("call result", "tool", call.ToolName, "id", call.ToolID, "result", result.String(), "input", call.Content.String(), "duration", duration)
			messages = append(messages, types.Message{
				ToolID:   call.ToolID,
				ToolName: call.ToolName,
				Role:     types.RoleToolResult,
				Content:  result,
			})
		}

	}
	return ans, nil

}

type promptContext struct {
	Messages []types.Message
}

type Response []*types.Invoke

// TotalInputTokens returns sum of all used input tokens.
func (r Response) TotalInputTokens() int {
	var sum int
	for _, msg := range r {
		sum += msg.InputToken
	}
	return sum
}

// TotalOutputTokens returns sum of all used output tokens.
func (r Response) TotalOutputTokens() int {
	var sum int
	for _, msg := range r {
		sum += msg.OutputToken
	}
	return sum
}

// TotalTokens returns sum of all  tokens.
func (r Response) TotalTokens() int {
	var sum int
	for _, msg := range r {
		sum += msg.TotalToken
	}
	return sum
}

// Reply returns first non-tool calling model response.
// If nothing found, empty text content returned.
func (r Response) Reply() types.Content {
	for _, m := range r {
		for _, c := range m.Output {
			if c.Role == types.RoleAssistant {
				return c.Content
			}
		}
	}
	return types.Text("")
}

// Called returns how many times function (tool) with specified name has been called.
func (r Response) Called(name string) int {
	var count int
	for _, m := range r {
		for _, c := range m.Output {
			if c.Role == types.RoleToolCall {
				if c.ToolName == name {
					count++
				}
			}
		}
	}
	return count
}
