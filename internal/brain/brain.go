package brain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"text/template"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/pikocloud/pikobrain/internal/ent"
	"github.com/pikocloud/pikobrain/internal/ent/message"
	"github.com/pikocloud/pikobrain/internal/providers/types"
)

type Brain struct {
	iterations int
	parallel   bool
	depth      int
	db         *ent.Client
	vision     *Vision
	prompt     *template.Template
	config     types.Config
	provider   types.Provider
	toolbox    types.Toolbox
	definition Definition
}

func (m *Brain) Definition() Definition {
	return m.definition
}

// Run model using only provided state.
func (m *Brain) Run(ctx context.Context, messages []types.Message, thread string) (Response, error) {
	// generate prompt
	var prompt bytes.Buffer

	if err := m.prompt.Execute(&prompt, promptContext{
		Messages: messages,
		Thread:   thread,
	}); err != nil {
		return nil, fmt.Errorf("render prompt: %w", err)
	}

	cfg := m.config
	cfg.Prompt = prompt.String() // replace prompt to rendered template

	tools := m.toolbox.Snapshot()
	toolSet := tools.Definitions()
	var ans Response

	// if vision model set - replace all images with results from vision
	if m.vision != nil {
		responses, err := m.replaceImagesByDescription(ctx, messages)
		ans = append(ans, responses...)
		if err != nil {
			return ans, err
		}
	}

	slog.Debug("running model", "messages", len(messages), "tools", len(tools), "prompt", prompt.String())

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
		// TODO: parallel call
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

func (m *Brain) Chat(ctx context.Context, thread string, messages ...types.Message) (Response, error) {
	res, err := m.Append(ctx, thread, messages)
	if err != nil {
		return res, fmt.Errorf("append to thread %q: %w", thread, err)
	}

	rawHistory, err := m.db.Message.Query().Where(message.Thread(thread)).Order(message.ByID(sql.OrderDesc())).Limit(m.depth).All(ctx)
	if err != nil {
		return res, fmt.Errorf("query history: %w", err)
	}

	slices.Reverse(rawHistory) // from oldest to newest

	// history must start from user role
	for i, msg := range rawHistory {
		if msg.Role == types.RoleUser {
			rawHistory = rawHistory[i:]
			break
		}
	}

	var history = make([]types.Message, 0, len(rawHistory))
	for _, msg := range rawHistory {
		history = append(history, types.Message{
			ToolID:   msg.ToolID,
			ToolName: msg.ToolName,
			Role:     msg.Role,
			User:     msg.User,
			Content: types.Content{
				Data: msg.Content,
				Mime: msg.Mime,
			},
		})
	}
	slog.Debug("running chat", "thread", thread, "raw_history", len(rawHistory), "filtered", len(history), "depth", m.depth)

	exec, err := m.Run(ctx, history, thread)
	if err != nil {
		return res, fmt.Errorf("run: %w", err)
	}

	saved, err := m.Append(ctx, thread, exec.Messages())
	if err != nil {
		return append(res, exec...), fmt.Errorf("append response to thread %q: %w", thread, err)
	}

	return slices.Concat(res, exec, saved), nil
}

// Append messages to thread.
func (m *Brain) Append(ctx context.Context, thread string, messages []types.Message) (Response, error) {
	messages = withoutEmptyMessages(messages)
	if len(messages) == 0 {
		return nil, nil
	}
	var res Response
	// if vision model set - replace all images with results from vision
	if m.vision != nil {
		v, err := m.replaceImagesByDescription(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("replace images by description: %w", err)
		}
		res = v
	}
	tx, err := m.db.Tx(ctx)
	if err != nil {
		return res, fmt.Errorf("begin transaction: %w", err)
	}

	err = tx.Message.MapCreateBulk(messages, func(create *ent.MessageCreate, i int) {
		msg := messages[i]
		create.SetThread(thread).SetMime(msg.Content.Mime).SetContent(msg.Content.Data).SetRole(msg.Role)
		if msg.User != "" {
			create.SetUser(msg.User)
		}
		if msg.ToolName != "" {
			create.SetToolName(msg.ToolName)
		}
		if msg.ToolID != "" {
			create.SetToolID(msg.ToolID)
		}
	}).Exec(ctx)

	if err != nil {
		return res, errors.Join(tx.Rollback(), err)
	}

	return res, tx.Commit()
}

func (m *Brain) replaceImagesByDescription(ctx context.Context, messages []types.Message) (Response, error) {
	var ans Response
	for i, msg := range messages {
		if msg.Role == types.RoleUser && msg.Content.Mime.IsImage() {
			result, err := m.provider.Invoke(ctx, types.Config{
				Model:     m.vision.Model,
				MaxTokens: m.config.MaxTokens,
			}, []types.Message{msg}, nil)
			if err != nil {
				return ans, fmt.Errorf("invoke vision model: %w", err)
			}
			ans = append(ans, result)
			for _, out := range result.Output {
				if out.Role == types.RoleAssistant {
					out.Role = types.RoleUser
					messages[i] = out
					slog.Debug("message replaced by vision model", "model", m.vision.Model, "messageIdx", i, "value", out.Content.String())
					break
				}
			}
		}
	}
	return ans, nil
}

type promptContext struct {
	Messages []types.Message
	Thread   string
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

// Messages of all responses.
func (r Response) Messages() []types.Message {
	var ans = make([]types.Message, 0, len(r))
	for _, inv := range r {
		ans = append(ans, inv.Output...)
	}
	return ans
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

func withoutEmptyMessages(messages []types.Message) []types.Message {
	// filter empty messages
	var out = make([]types.Message, 0, len(messages))
	for _, msg := range messages {
		if len(msg.Content.Data) != 0 {
			out = append(out, msg)
		}
	}
	return out
}
