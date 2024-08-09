package types

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
)

//go:generate go run github.com/abice/go-enum@v0.6.0 --values --sql

type Toolbox interface {
	Snapshot() Snapshot
}

type Snapshot map[string]Tool

func (s Snapshot) Definitions() []ToolDefinition {
	var ans = make([]ToolDefinition, 0, len(s))
	for _, tool := range s {
		ans = append(ans, tool)
	}
	return ans
}

func (s Snapshot) Call(ctx context.Context, name string, args json.RawMessage) (Content, error) {
	f, ok := s[name]
	if !ok {
		return Content{}, fmt.Errorf("no such tool %q", name)
	}
	return f.Call(ctx, args)
}

type Tool interface {
	Name() string
	Description() string
	Input() *jsonschema.Schema
	Call(ctx context.Context, args json.RawMessage) (Content, error)
}

// Role for each message.
// ENUM(user,assistant,toolCall,toolResult)
type Role string

// MIME for each message.
// ENUM(
// text = text/plain,
// json = application/json
// png = image/png
// jpeg = image/jpeg
// jpg = image/jpg
// webp = image/webp
// gif = image/gif
// )
type MIME string

func (M MIME) IsText() bool {
	return strings.HasPrefix(string(M), "text/") || M == MIMEJson
}

func (M MIME) IsImage() bool {
	return strings.HasPrefix(string(M), "image/")
}

func (M MIME) ImageFormat() string {
	value := strings.TrimPrefix(string(M), "image/")
	if value == "jpg" {
		value = "jpeg"
	}
	return value
}

type Content struct {
	Data []byte
	Mime MIME
}

func (msg *Content) String() string {
	if msg.Mime.IsText() {
		return string(msg.Data)
	}
	return msg.DataURL()
}

func (msg *Content) DataURL() string {
	return "data:" + string(msg.Mime) + ";base64," + base64.StdEncoding.EncodeToString(msg.Data)
}

func Text(content string) Content {
	return Content{
		Data: []byte(content),
		Mime: MIMEText,
	}
}

func ParseDataURL(data string) Content {
	parts := strings.SplitN(data, ",", 2)
	if len(parts) != 2 {
		return Content{
			Data: []byte(data),
		}
	}
	_, mime, _ := strings.Cut(parts[0], ":")
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		raw = []byte(data)
	}
	return Content{
		Data: raw,
		Mime: MIME(mime),
	}
}

type Message struct {
	ToolID   string
	ToolName string
	Role     Role
	User     string
	Content  Content
}

type ToolCall struct {
	ID       string
	ToolName string
	Started  time.Time
	Duration time.Duration
	Input    json.RawMessage
	Output   Content
}

type ModelMessage struct {
	Started     time.Time
	Duration    time.Duration
	InputToken  int
	OutputToken int
	TotalToken  int
	Input       json.RawMessage
	Output      json.RawMessage
	ToolCalls   []ToolCall
	Content     []Content
}

type Config struct {
	Model     string `json:"model" yaml:"model"`
	Prompt    string `json:"prompt" yaml:"prompt"`
	MaxTokens int    `json:"max_tokens" yaml:"maxTokens"`
	ForceJSON bool   `json:"force_json" yaml:"forceJSON"`
}

type Request struct {
	Config  Config
	History []Message
	Tools   Toolbox
}

type Response struct {
	Request  *Request
	Started  time.Time
	Duration time.Duration
	Messages []ModelMessage
}

type ToolDefinition interface {
	Name() string
	Description() string
	Input() *jsonschema.Schema
}

type Invoke struct {
	Output      []Message
	InputToken  int
	OutputToken int
	TotalToken  int
}

func (inv *Invoke) ToolCalls() []Message {
	var ans []Message
	for _, m := range inv.Output {
		if m.Role == RoleToolCall {
			ans = append(ans, m)
		}
	}
	return ans
}

// Provider to LLM. Should NOT call tools by it self.
type Provider interface {
	Invoke(ctx context.Context, config Config, messages []Message, tools []ToolDefinition) (*Invoke, error)
}
