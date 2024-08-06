package openai

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"

	"github.com/pikocloud/pikobrain/internal/providers/types"
)

var _ types.Provider = &OpenAI{}

func New(url string, token string) *OpenAI {
	cfg := openai.DefaultConfig(token)
	cfg.BaseURL = url
	return &OpenAI{
		client: openai.NewClientWithConfig(cfg),
	}
}

type OpenAI struct {
	client *openai.Client
}

func (provider *OpenAI) Invoke(ctx context.Context, config types.Config, messages []types.Message, tools []types.ToolDefinition) (*types.Invoke, error) {
	var input = make([]openai.ChatCompletionMessage, 0, 1+len(messages))
	input = append(input, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: config.Prompt,
	})

	//var prevTool *openai.ChatCompletionMessage
	for _, message := range messages {
		input = append(input, mapMessage(message))

		//	prevTool = &openai.ChatCompletionMessage{
		//		Role:         openai.ChatMessageRoleTool,
		//		ToolCalls:    nil,
		//		ToolCallID:   "",
		//	}
		//}
		//// aggregate tool calls
		//prevTool.ToolCalls = append(prevTool.ToolCalls, openai.ToolCall{
		//	ID:       "",
		//	Type:     "",
		//	Function: openai.FunctionCall{},
		//})

	}

	var openTools = make([]openai.Tool, 0, len(tools))
	for _, tool := range tools {
		openTools = append(openTools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.Input(),
			},
		})
	}

	var format = &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeText}
	if config.ForceJSON {
		format.Type = openai.ChatCompletionResponseFormatTypeJSONObject
	}

	req := openai.ChatCompletionRequest{
		Model:          config.Model,
		Messages:       input,
		MaxTokens:      config.MaxTokens,
		ResponseFormat: format,
		Tools:          openTools,
	}

	res, err := provider.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create chat completion: %w", err)
	}

	var output = make([]types.Message, 0, len(res.Choices))
	for _, choice := range res.Choices {
		// add function calls
		// function call can generate multiple calls (parallel)
		// but response should be per-message, therefore we are flattening them.
		for _, call := range choice.Message.ToolCalls {
			output = append(output, types.Message{
				ToolID:   call.ID,
				ToolName: call.Function.Name,
				Role:     types.RoleToolCall,
				User:     choice.Message.Name,
				Content: types.Content{
					Data: []byte(call.Function.Arguments),
					Mime: types.MIMEJson,
				},
			})
		}

		// add direct messages
		for _, assistantContent := range parseMessage(choice.Message) {
			output = append(output, types.Message{
				Role:    types.RoleAssistant,
				Content: assistantContent,
				User:    choice.Message.Name,
			})
		}
	}

	return &types.Invoke{
		Output:      output,
		InputToken:  res.Usage.PromptTokens,
		OutputToken: res.Usage.CompletionTokens,
		TotalToken:  res.Usage.TotalTokens,
	}, nil
}

func mapMessage(message types.Message) openai.ChatCompletionMessage {
	var role = openai.ChatMessageRoleUser

	switch message.Role {
	case types.RoleAssistant:
		role = openai.ChatMessageRoleAssistant
	case types.RoleUser:
		role = openai.ChatMessageRoleUser
	case types.RoleToolCall:
		return openai.ChatCompletionMessage{
			Role:         openai.ChatMessageRoleAssistant,
			MultiContent: []openai.ChatMessagePart{mapContent(message.Content)},
			Name:         message.ToolName,
			ToolCalls: []openai.ToolCall{
				{
					ID:   message.ToolID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      message.ToolName,
						Arguments: string(message.Content.Data),
					},
				},
			},
			ToolCallID: message.ToolID,
		}
	case types.RoleToolResult:
		role = openai.ChatMessageRoleTool
	}

	return openai.ChatCompletionMessage{
		ToolCallID:   message.ToolID,
		Role:         role,
		MultiContent: []openai.ChatMessagePart{mapContent(message.Content)},
		Name:         message.User,
	}
}

func mapContent(data types.Content) openai.ChatMessagePart {
	switch {
	case data.Mime.IsImage():
		return openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL: data.DataURL(),
			},
		}
	default:
		fallthrough
	case data.Mime.IsText():
		return openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeText,
			Text: string(data.Data),
		}
	}
}

func parseMessage(message openai.ChatCompletionMessage) []types.Content {
	var out []types.Content
	if message.Content != "" {
		out = append(out, types.Content{
			Data: []byte(message.Content),
			Mime: types.MIMEText,
		})
	}

	for _, m := range message.MultiContent {
		switch m.Type {
		case openai.ChatMessagePartTypeText:
			out = append(out, types.Content{
				Data: []byte(m.Text),
				Mime: types.MIMEText,
			})
		case openai.ChatMessagePartTypeImageURL:
			// data
			out = append(out, types.ParseDataURL(m.ImageURL.URL))
		}
	}

	return out
}
