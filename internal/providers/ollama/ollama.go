package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ollama/ollama/api"

	"github.com/pikocloud/pikobrain/internal/providers/types"
)

func New(u string) (*Ollama, error) {
	link, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("parse URL %s: %w", u, err)
	}

	return &Ollama{client: api.NewClient(link, http.DefaultClient)}, nil
}

type Ollama struct {
	client *api.Client
}

func (olm *Ollama) Invoke(ctx context.Context, config types.Config, messages []types.Message, tools []types.ToolDefinition) (*types.Invoke, error) {
	var req = api.ChatRequest{
		Model:  config.Model,
		Stream: new(bool),
	}
	if config.ForceJSON {
		req.Format = "json"
	}
	if config.Prompt != "" {
		req.Messages = append(req.Messages, api.Message{
			Role:    "system",
			Content: config.Prompt,
		})
	}

	for _, msg := range messages {
		mapped, err := mapMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("map message: %w", err)
		}
		req.Messages = append(req.Messages, mapped)
	}

	for _, tool := range tools {
		var params = api.ToolFunction{
			Name:        tool.Name(),
			Description: tool.Description(),
		}
		// TODO: inline definitions
		// downcast input schema
		raw, err := json.Marshal(tool.Input())
		if err != nil {
			return nil, fmt.Errorf("marshal tool input: %w", err)
		}
		if err := json.Unmarshal(raw, &params.Parameters); err != nil {
			return nil, fmt.Errorf("unmarshal tool input: %w", err)
		}
		req.Tools = append(req.Tools, api.Tool{
			Type:     "function",
			Function: params,
		})
	}

	var responses []api.ChatResponse
	err := olm.client.Chat(ctx, &req, func(response api.ChatResponse) error {
		responses = append(responses, response)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}
	var (
		inpToken int
		outToken int
		totToken int
	)
	var output = make([]types.Message, 0, len(responses))
	for _, response := range responses {
		inpToken += response.PromptEvalCount
		outToken += response.EvalCount
		totToken += inpToken + outToken // no difference here

		for _, image := range response.Message.Images {
			output = append(output, types.Message{
				Role: types.RoleAssistant,
				Content: types.Content{
					Data: image,
					Mime: types.MIMEJpg,
				},
			})
		}

		if response.Message.Content != "" {
			output = append(output, types.Message{
				Role:    types.RoleAssistant,
				Content: types.Text(response.Message.Content),
			})
		}

		for _, toolCall := range response.Message.ToolCalls {

			in, err := json.Marshal(toolCall.Function.Arguments)
			if err != nil {
				return nil, fmt.Errorf("marshal tool call arguments: %w", err)
			}
			output = append(output, types.Message{
				ToolID:   toolCall.Function.Name,
				ToolName: toolCall.Function.Name,
				Role:     types.RoleToolCall,
				Content: types.Content{
					Data: in,
					Mime: types.MIMEJson,
				},
			})
		}
	}

	return &types.Invoke{
		Output:      output,
		InputToken:  inpToken,
		OutputToken: outToken,
		TotalToken:  totToken,
	}, nil
}

func mapMessage(msg types.Message) (api.Message, error) {
	var role = "user"
	switch msg.Role {
	case types.RoleUser:
		role = "user"
	case types.RoleAssistant:
		role = "assistant"
	case types.RoleToolCall:
		var arg api.ToolCallFunctionArguments
		if err := json.Unmarshal(msg.Content.Data, &arg); err != nil {
			return api.Message{}, fmt.Errorf("unmarshal tool call arguments: %w", err)
		}
		return api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{{
				Function: api.ToolCallFunction{
					Name:      msg.ToolName,
					Arguments: arg,
				},
			}},
		}, nil
	case types.RoleToolResult:
		role = "tool"
	}

	res := api.Message{
		Role: role,
	}
	if msg.Content.Mime.IsImage() {
		res.Images = append(res.Images, api.ImageData(msg.Content.Data))
	} else if msg.Content.Data != nil {
		res.Content = string(msg.Content.Data)
	}

	return res, nil
}
