package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	types2 "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/pikocloud/pikobrain/internal/providers/types"
)

var ErrUnknownBlockType = errors.New("unknown block type: %T")

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

func (bed *Bedrock) Execute(ctx context.Context, req *types.Request) (*types.Response, error) {
	var input = &bedrockruntime.ConverseInput{
		ToolConfig: &types2.ToolConfiguration{},
		ModelId:    aws.String(req.Config.Model),
		InferenceConfig: &types2.InferenceConfiguration{
			MaxTokens: aws.Int32(int32(req.Config.MaxTokens)),
		},
	}

	if req.Config.Prompt != "" {
		input.System = []types2.SystemContentBlock{
			&types2.SystemContentBlockMemberText{Value: req.Config.Prompt},
		}
	}

	var prev *types2.Message

	for _, msg := range req.History {
		content, err := mapContent(msg.Content)
		if err != nil {
			return nil, fmt.Errorf("map content: %w", err)
		}
		next := types2.Message{
			Content: []types2.ContentBlock{content},
			Role:    types2.ConversationRole(msg.Role),
		}
		// squash user messages in one
		// ugly dirty workaround because AWS requires always jump between user and assistant
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

	var toolIndex = make(map[string]types.Tool)
	for _, tool := range req.Tools.All() {
		toolIndex[tool.Name()] = tool

		// workaround since AWS serializer doesn't support encoding/json contract

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

	var response = &types.Response{
		Request: req,
		Started: time.Now(),
	}

	for i := 0; i < req.Config.MaxIterations; i++ {

		rawInput, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("marshal input: %w", err)
		}
		var modelMessage = types.ModelMessage{
			Started: time.Now(),
			Input:   rawInput,
		}

		resp, err := bed.client.Converse(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("converse: %w", err)
		}

		modelMessage.Duration = time.Since(modelMessage.Started)

		rawOutput, err := json.Marshal(resp)
		if err != nil {
			return nil, fmt.Errorf("marshal output: %w", err)
		}
		modelMessage.Output = rawOutput

		modelMessage.TotalToken = int(*resp.Usage.TotalTokens)
		modelMessage.InputToken = int(*resp.Usage.InputTokens)
		modelMessage.OutputToken = int(*resp.Usage.OutputTokens)

		v, ok := resp.Output.(*types2.ConverseOutputMemberMessage)
		if !ok {
			continue
		}

		out, err := callTools(ctx, v, toolIndex)
		if err != nil {
			return nil, fmt.Errorf("call tools: %w", err)
		}

		modelMessage.ToolCalls = out
		input.Messages = append(input.Messages, v.Value)

		for _, block := range v.Value.Content {
			value, err := parseContent(block)
			if errors.Is(err, ErrUnknownBlockType) {
				continue // skip
			}
			if err != nil {
				return nil, fmt.Errorf("parse content: %w", err)
			}
			modelMessage.Content = append(modelMessage.Content, value)
		}

		response.Messages = append(response.Messages, modelMessage)
		if len(out) == 0 {
			break // no need more iterations
		}

		var reply = types2.Message{Role: types2.ConversationRoleUser}
		for _, toolResult := range out {
			outContent, err := mapResult(toolResult.Output)
			if err != nil {
				return nil, fmt.Errorf("map content: %w", err)
			}
			reply.Content = append(reply.Content, &types2.ContentBlockMemberToolResult{
				Value: types2.ToolResultBlock{
					Content:   []types2.ToolResultContentBlock{outContent},
					ToolUseId: aws.String(toolResult.ID),
				},
			})
		}

		// add responses to context
		input.Messages = append(input.Messages, reply)
	}

	response.Duration = time.Since(response.Started)
	return response, nil
}

func mapContent(content types.Content) (types2.ContentBlock, error) {
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
	return nil, fmt.Errorf("unknown mime: %s", content.Mime)
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

func parseContent(block types2.ContentBlock) (types.Content, error) {
	switch item := block.(type) {
	case *types2.ContentBlockMemberText:
		return types.Text(item.Value), nil
	case *types2.ContentBlockMemberImage:
		return types.Content{
			Data: item.Value.Source.(*types2.ImageSourceMemberBytes).Value,
			Mime: types.MIME("image/" + item.Value.Format),
		}, nil
	}
	return types.Content{}, fmt.Errorf("block type: %T: %w", block, ErrUnknownBlockType)
}

func callTools(ctx context.Context, message *types2.ConverseOutputMemberMessage, tools map[string]types.Tool) ([]types.ToolCall, error) {
	var ans []types.ToolCall
	for _, block := range message.Value.Content {
		it, ok := block.(*types2.ContentBlockMemberToolUse)
		if !ok {
			continue
		}

		tool, ok := tools[*it.Value.Name]
		if !ok {
			return nil, fmt.Errorf("tool %q not found", *it.Value.Name)
		}

		payload, err := it.Value.Input.MarshalSmithyDocument()
		if err != nil {
			return nil, fmt.Errorf("unmarshal input for tool %q: %w", *it.Value.Name, err)
		}

		s := time.Now()
		res, err := tool.Call(ctx, payload)
		if err != nil {
			return nil, fmt.Errorf("call tool %q: %w", *it.Value.Name, err)
		}

		ans = append(ans, types.ToolCall{
			ID:       *it.Value.ToolUseId,
			ToolName: *it.Value.Name,
			Started:  s,
			Duration: time.Since(s),
			Input:    payload,
			Output:   res,
		})
	}

	return ans, nil
}
