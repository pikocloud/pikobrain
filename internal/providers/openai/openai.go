package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

func (provider *OpenAI) Execute(ctx context.Context, request *types.Request) (*types.Response, error) {
	var messages = make([]openai.ChatCompletionMessage, 0, 1+len(request.History))
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: request.Config.Prompt,
	})

	for _, message := range request.History {
		messages = append(messages, mapMessage(message))
	}

	var format = &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeText}
	if request.Config.ForceJSON {
		format.Type = openai.ChatCompletionResponseFormatTypeJSONObject
	}

	allTools := request.Tools.All()
	var tools = make([]openai.Tool, 0, len(allTools))
	var toolIndex = make(map[string]types.Tool, len(allTools))

	for _, tool := range allTools {
		toolIndex[tool.Name()] = tool
		tools = append(tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.Input(),
			},
		})
	}
	req := openai.ChatCompletionRequest{
		Model:          request.Config.Model,
		Messages:       messages,
		MaxTokens:      request.Config.MaxTokens,
		ResponseFormat: format,
		Tools:          tools,
	}

	var out = types.Response{Request: request, Started: time.Now()}

	for i := 0; i < request.Config.MaxIterations; i++ {
		resp, err := provider.iterate(ctx, toolIndex, req)
		if err != nil {
			return nil, fmt.Errorf("iterate: %w", err)
		}
		out.Messages = append(out.Messages, *resp.out)
		if !resp.needMore {
			break
		}
		// add responses to context
		req.Messages = append(req.Messages, resp.messages...)
	}
	out.Duration = time.Since(out.Started)
	return &out, nil
}

type aiResponse struct {
	out      *types.ModelMessage
	messages []openai.ChatCompletionMessage
	needMore bool
}

func (provider *OpenAI) iterate(ctx context.Context, tools map[string]types.Tool, req openai.ChatCompletionRequest) (*aiResponse, error) {
	input, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	started := time.Now()
	res, err := provider.client.CreateChatCompletion(ctx, req)
	duration := time.Since(started)
	if err != nil {
		return nil, fmt.Errorf("create chat completion: %w", err)
	}

	output, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}

	msg := &types.ModelMessage{
		Started:     started,
		Duration:    duration,
		InputToken:  res.Usage.PromptTokens,
		OutputToken: res.Usage.CompletionTokens,
		TotalToken:  res.Usage.TotalTokens,
		Input:       input,
		Output:      output,
	}

	var answers []openai.ChatCompletionMessage

	// TODO: account if failed
	for _, choice := range res.Choices {
		switch choice.FinishReason {
		case openai.FinishReasonToolCalls:
			responses, err := callTools(ctx, choice.Message, tools)
			if err != nil {
				return nil, fmt.Errorf("call tools: %w", err)
			}
			msg.ToolCalls = responses
			answers = append(answers, choice.Message)
			for _, response := range responses {
				answers = append(answers, openai.ChatCompletionMessage{
					Role:         openai.ChatMessageRoleTool,
					MultiContent: []openai.ChatMessagePart{mapContent(response.Output)},
					ToolCallID:   response.ID,
				})
			}
		default:
			msg.Content = append(msg.Content, parseMessage(choice.Message)...)
		}
	}
	return &aiResponse{
		out:      msg,
		needMore: len(answers) > 0,
		messages: answers,
	}, nil
}

func callTools(ctx context.Context, message openai.ChatCompletionMessage, tools map[string]types.Tool) ([]types.ToolCall, error) {
	var ans = make([]types.ToolCall, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		if call.Type != openai.ToolTypeFunction {
			return nil, fmt.Errorf("invalid tool type: %q", call.Type)
		}

		var input json.RawMessage
		if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
			return nil, fmt.Errorf("unmarshal input: %w", err)
		}

		tool, ok := tools[call.Function.Name]
		if !ok {
			return nil, fmt.Errorf("unknown tool name: %q", call.Function.Name)
		}

		started := time.Now()
		result, err := tool.Call(ctx, input)
		duration := time.Since(started)

		if err != nil {
			return nil, fmt.Errorf("call tool %q: %w", call.Function.Name, err)
		}

		ans = append(ans, types.ToolCall{
			ID:       call.ID,
			ToolName: call.Function.Name,
			Started:  started,
			Duration: duration,
			Input:    input,
			Output:   result,
		})
	}
	return ans, nil
}

func mapMessage(message types.Message) openai.ChatCompletionMessage {
	var role = openai.ChatMessageRoleUser

	switch message.Role {
	case types.RoleAssistant:
		role = openai.ChatMessageRoleAssistant
	}

	return openai.ChatCompletionMessage{
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
