package google

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/generative-ai-go/genai"
	"github.com/invopop/jsonschema"
	"google.golang.org/api/option"

	"github.com/pikocloud/pikobrain/internal/providers/types"
)

func New(ctx context.Context, token string) (*Google, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(token))
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	return &Google{client}, nil
}

type Google struct {
	client *genai.Client
}

func (srv *Google) Invoke(ctx context.Context, config types.Config, messages []types.Message, tools []types.ToolDefinition) (*types.Invoke, error) {
	var tokens = int32(config.MaxTokens)
	model := srv.client.GenerativeModel(config.Model)

	model.GenerationConfig = genai.GenerationConfig{
		MaxOutputTokens: &tokens,
	}
	if config.ForceJSON {
		model.GenerationConfig.ResponseMIMEType = "application/json"
	}
	if config.Prompt != "" {
		model.SystemInstruction = &genai.Content{
			Parts: []genai.Part{
				genai.Text(config.Prompt),
			},
		}
	}

	var funcDefs []*genai.FunctionDeclaration
	for _, tool := range tools {
		funcDefs = append(funcDefs, &genai.FunctionDeclaration{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  schemaConverter(tool.Input()),
		})
	}

	if len(funcDefs) > 0 {
		model.Tools = append(model.Tools, &genai.Tool{
			FunctionDeclarations: funcDefs,
		})
	}

	chat := model.StartChat()

	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages")
	}

	for _, msg := range messages {
		part, err := mapContent(msg)
		if err != nil {
			return nil, fmt.Errorf("map content: %w", err)
		}
		var role string
		switch msg.Role {
		case types.RoleUser:
			role = "user"
		case types.RoleAssistant:
			role = "model"
		}

		chat.History = append(chat.History, &genai.Content{
			Parts: []genai.Part{part},
			Role:  role,
		})
	}
	last := chat.History[len(chat.History)-1]
	chat.History = chat.History[:len(chat.History)-1]

	result, err := chat.SendMessage(ctx, last.Parts...)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	var out = &types.Invoke{
		InputToken:  int(result.UsageMetadata.PromptTokenCount),
		OutputToken: int(result.UsageMetadata.CandidatesTokenCount + result.UsageMetadata.CachedContentTokenCount),
		TotalToken:  int(result.UsageMetadata.TotalTokenCount),
	}

	if len(result.Candidates) == 0 {
		slog.Warn("no candidates")
		return out, nil
	}

	candidate := result.Candidates[0]
	if candidate.Content == nil {
		slog.Warn("candidate content is empty")
		return out, nil
	}

	var output []types.Message
	for _, part := range candidate.Content.Parts {
		switch v := part.(type) {
		case genai.FunctionCall:
			data, err := json.Marshal(v.Args)
			if err != nil {
				return nil, fmt.Errorf("marshal call part: %w", err)
			}
			output = append(output, types.Message{
				ToolName: v.Name,
				Role:     types.RoleToolCall,
				Content: types.Content{
					Data: data,
					Mime: types.MIMEJson,
				},
			})
		case genai.Text:
			output = append(output, types.Message{
				Role: types.RoleAssistant,
				Content: types.Content{
					Data: []byte(v),
					Mime: types.MIMEText,
				},
			})
		case genai.Blob:
			tp, err := types.ParseMIME(v.MIMEType)
			if err != nil {
				return nil, fmt.Errorf("parse mime type: %w", err)
			}
			output = append(output, types.Message{
				Role: types.RoleAssistant,
				Content: types.Content{
					Data: v.Data,
					Mime: tp,
				},
			})
		}
	}
	out.Output = output
	return out, nil
}

func mapContent(msg types.Message) (genai.Part, error) {
	switch {
	case msg.Role == types.RoleToolCall:
		var out map[string]any
		if err := json.Unmarshal(msg.Content.Data, &out); err != nil {
			return nil, fmt.Errorf("unmarshal data: %w", err)
		}
		return genai.FunctionCall{
			Name: msg.ToolName,
			Args: out,
		}, nil
	case msg.Role == types.RoleToolResult:
		var out map[string]any
		// workaround to support most types
		switch {
		case msg.Content.Mime == types.MIMEText:
			out = map[string]any{"content": string(msg.Content.Data)}
		case msg.Content.Mime == types.MIMEJson:
			var src = string(msg.Content.Data)
			if len(msg.Content.Data) > 0 && msg.Content.Data[0] != '{' {
				src = "{\"content\": " + src + "}"
			}
			if err := json.Unmarshal(msg.Content.Data, &out); err != nil {
				return nil, fmt.Errorf("unmarshal data: %w", err)
			}
		default:
			return nil, fmt.Errorf("unsupported mime type for function call: %s", msg.Content.Mime)
		}
		return genai.FunctionResponse{
			Name:     msg.ToolName,
			Response: out,
		}, nil
	case msg.Content.Mime.IsImage():
		return genai.Blob{
			MIMEType: msg.Content.Mime.String(),
			Data:     msg.Content.Data,
		}, nil
	case msg.Content.Mime.IsText():
		return genai.Text(msg.Content.Data), nil
	default:
		return nil, fmt.Errorf("unknown mime: %s", msg.Content.Mime)
	}
}

func schemaConverter(input *jsonschema.Schema) *genai.Schema {
	var out = &genai.Schema{
		Type:        schemaType(input.Type),
		Format:      input.Format,
		Description: input.Description,
		Enum:        schemaEnum(input.Enum),
		Required:    slices.Clone(input.Required),
	}
	if n := input.Properties.Len(); n > 0 {
		out.Properties = make(map[string]*genai.Schema, n)
		for item := input.Properties.Oldest(); item != nil; item = item.Next() {
			out.Properties[item.Key] = schemaConverter(item.Value)
		}
	}
	if input.Items != nil {
		out.Items = schemaConverter(input.Items)
	}
	return out
}

func schemaType(input string) genai.Type {
	switch input {
	case "object":
		return genai.TypeObject
	case "array":
		return genai.TypeArray
	case "boolean":
		return genai.TypeBoolean
	case "number", "integer":
		return genai.TypeNumber
	case "string":
		return genai.TypeString
	default:
		return genai.TypeUnspecified
	}
}

func schemaEnum(input []any) []string {
	var out = make([]string, 0, len(input))
	for _, item := range input {
		out = append(out, fmt.Sprint(item))
	}
	return out
}
