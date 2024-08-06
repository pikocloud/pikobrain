package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	types2 "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/pikocloud/pikobrain/internal/providers/types"
)

var ErrUnknownBlockType = errors.New("unknown block type")

func New(ctx context.Context) (*Bedrock, error) {
	sdkConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load default AWS config: %w", err)
	}
	bedrockClient := bedrockruntime.NewFromConfig(sdkConfig)

	return &Bedrock{client: bedrockClient}, nil
}

type Bedrock struct {
	client *bedrockruntime.Client
}

func (bed *Bedrock) Invoke(ctx context.Context, config types.Config, messages []types.Message, tools []types.ToolDefinition) (*types.Invoke, error) {
	var input = &bedrockruntime.ConverseInput{
		ToolConfig: &types2.ToolConfiguration{},
		ModelId:    aws.String(config.Model),
		InferenceConfig: &types2.InferenceConfiguration{
			MaxTokens: aws.Int32(int32(config.MaxTokens)),
		},
	}

	if config.Prompt != "" {
		input.System = []types2.SystemContentBlock{
			&types2.SystemContentBlockMemberText{Value: config.Prompt},
		}
	}
	// convert input
	var mappedMessages []types2.Message
	for _, msg := range messages {
		var role types2.ConversationRole
		switch msg.Role {
		case types.RoleAssistant, types.RoleToolCall:
			role = types2.ConversationRoleAssistant
		case types.RoleUser, types.RoleToolResult:
			role = types2.ConversationRoleUser
		}

		var ct types2.ContentBlock

		switch msg.Role {

		case types.RoleUser, types.RoleAssistant:
			value, err := mapUserBlock(msg.Content)
			if err != nil {
				return nil, fmt.Errorf("map data content: %w", err)
			}
			ct = value
		case types.RoleToolCall:
			var pd any
			if err := json.Unmarshal(msg.Content.Data, &pd); err != nil {
				return nil, fmt.Errorf("unmarshal tool use content: %w", err)
			}
			ct = &types2.ContentBlockMemberToolUse{
				Value: types2.ToolUseBlock{
					Input:     document.NewLazyDocument(pd),
					Name:      aws.String(msg.ToolName),
					ToolUseId: aws.String(msg.ToolID),
				},
			}
		case types.RoleToolResult:
			value, err := mapToolResult(msg.ToolID, msg.Content)
			if err != nil {
				return nil, fmt.Errorf("map result content: %w", err)
			}
			ct = value
		}

		mappedMessages = append(mappedMessages, types2.Message{
			Content: []types2.ContentBlock{ct},
			Role:    role,
		})

	}

	// merge messages by role
	// ugly dirty workaround because AWS requires always jump between user and assistant
	var prev *types2.Message

	for _, next := range mappedMessages {
		next := next
		if prev != nil {
			if prev.Role != next.Role {
				// role changed - append
				input.Messages = append(input.Messages, *prev)
				prev = &next
			} else {
				// squash
				prev.Content = append(prev.Content, next.Content...)
			}
		} else {
			prev = &next
		}
	}

	if prev != nil {
		input.Messages = append(input.Messages, *prev)
	}

	// set tools
	for _, tool := range tools {
		// Workaround since AWS serializer doesn't support encoding/json contract.
		// So we need to marshal it to json, then back from json to map[string]any
		schema, err := json.Marshal(tool.Input())
		if err != nil {
			return nil, fmt.Errorf("marshal tool schema: %w", err)
		}

		var raw map[string]any
		if err := json.Unmarshal(schema, &raw); err != nil {
			return nil, fmt.Errorf("unmarshal tool schema: %w", err)
		}

		input.ToolConfig.Tools = append(input.ToolConfig.Tools, &types2.ToolMemberToolSpec{
			Value: types2.ToolSpecification{
				InputSchema: &types2.ToolInputSchemaMemberJson{
					Value: document.NewLazyDocument(raw),
				},
				Name:        aws.String(tool.Name()),
				Description: aws.String(tool.Description()),
			},
		})
	}

	// call
	resp, err := bed.client.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("converse: %w", err)
	}

	// map output
	output, err := mapOutput(resp)
	if err != nil {
		return nil, fmt.Errorf("map output: %w", err)
	}
	return &types.Invoke{
		Output:      output,
		TotalToken:  int(*resp.Usage.TotalTokens),
		InputToken:  int(*resp.Usage.InputTokens),
		OutputToken: int(*resp.Usage.OutputTokens),
	}, nil
}

func mapOutput(resp *bedrockruntime.ConverseOutput) ([]types.Message, error) {
	switch v := resp.Output.(type) {
	case *types2.ConverseOutputMemberMessage:
		if v == nil {
			return nil, nil
		}
		var ct = make([]types.Message, 0, len(v.Value.Content))
		for _, block := range v.Value.Content {
			switch item := block.(type) {
			case *types2.ContentBlockMemberToolUse:
				payload, err := item.Value.Input.MarshalSmithyDocument()
				if err != nil {
					return nil, fmt.Errorf("unmarshal input for tool %q: %w", *item.Value.Name, err)
				}
				ct = append(ct, types.Message{
					Role:     types.RoleToolCall,
					ToolName: *item.Value.Name,
					ToolID:   *item.Value.ToolUseId,
					Content: types.Content{
						Data: payload,
						Mime: types.MIMEJson,
					},
				})
			case *types2.ContentBlockMemberText:
				ct = append(ct, types.Message{
					Role:    types.RoleAssistant,
					Content: types.Text(item.Value),
				})
			case *types2.ContentBlockMemberImage:
				ct = append(ct, types.Message{
					Role: types.RoleAssistant,
					Content: types.Content{
						Data: item.Value.Source.(*types2.ImageSourceMemberBytes).Value,
						Mime: types.MIME("image/" + item.Value.Format),
					},
				})
			}
		}
		return ct, nil
	default:
		return nil, fmt.Errorf("%T: %w", v, ErrUnknownBlockType)
	}
}

func mapToolResult(id string, content types.Content) (types2.ContentBlock, error) {
	value, err := mapResult(content)
	if err != nil {
		return nil, fmt.Errorf("map result content: %w", err)
	}
	return &types2.ContentBlockMemberToolResult{
		Value: types2.ToolResultBlock{
			Content:   []types2.ToolResultContentBlock{value},
			ToolUseId: aws.String(id),
		},
	}, nil
}

func mapUserBlock(content types.Content) (types2.ContentBlock, error) {
	switch {
	case content.Mime.IsText():
		return &types2.ContentBlockMemberText{Value: string(content.Data)}, nil
	case content.Mime.IsImage():
		var img = types2.ImageFormat(content.Mime.ImageFormat())

		return &types2.ContentBlockMemberImage{Value: types2.ImageBlock{
			Format: img,
			Source: &types2.ImageSourceMemberBytes{Value: content.Data},
		}}, nil
	}
	return nil, fmt.Errorf("unknown mime type: %s", content.Mime)
}

func mapResult(content types.Content) (types2.ToolResultContentBlock, error) {
	switch {
	case content.Mime == types.MIMEJson:
		return &types2.ToolResultContentBlockMemberJson{Value: document.NewLazyDocument(json.RawMessage(content.Data))}, nil
	case content.Mime.IsText():
		return &types2.ToolResultContentBlockMemberText{Value: string(content.Data)}, nil
	case content.Mime.IsImage():
		var img = types2.ImageFormat(content.Mime.ImageFormat())
		return &types2.ToolResultContentBlockMemberImage{Value: types2.ImageBlock{
			Format: img,
			Source: &types2.ImageSourceMemberBytes{Value: content.Data},
		}}, nil
	}
	return nil, fmt.Errorf("unknown mime: %s", content.Mime)
}
