package types

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
)

//go:generate go run github.com/abice/go-enum@v0.6.0

type Toolbox interface {
	All() []Tool
}

type Tool interface {
	Name() string
	Description() string
	Input() *jsonschema.Schema
	Call(ctx context.Context, args json.RawMessage) (Content, error)
}

// Role for each message.
// ENUM(user,assistant)
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
	Role Role
	User string
	Content
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
	Model         string `json:"model" yaml:"model"`
	Prompt        string `json:"prompt" yaml:"prompt"`
	MaxTokens     int    `json:"max_tokens" yaml:"maxTokens"`
	MaxIterations int    `json:"max_iterations" yaml:"maxIterations"`
	ForceJSON     bool   `json:"force_json" yaml:"forceJSON"`
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

// Reply returns first non-tool calling model response.
// If nothing found, empty text content returned.
func (r *Response) Reply() Content {
	for _, m := range r.Messages {
		if len(m.ToolCalls) > 0 {
			continue
		}
		for _, c := range m.Content {
			return c
		}
	}
	return Text("")
}

// TotalInputTokens returns sum of all used input tokens.
func (r *Response) TotalInputTokens() int {
	var sum int
	for _, msg := range r.Messages {
		sum += msg.InputToken
	}
	return sum
}

// TotalOutputTokens returns sum of all used output tokens.
func (r *Response) TotalOutputTokens() int {
	var sum int
	for _, msg := range r.Messages {
		sum += msg.OutputToken
	}
	return sum
}

// TotalTokens returns sum of all  tokens.
func (r *Response) TotalTokens() int {
	var sum int
	for _, msg := range r.Messages {
		sum += msg.TotalToken
	}
	return sum
}

// Called returns how many times function (tool) with specified name has been called.
func (r *Response) Called(name string) int {
	var count int
	for _, m := range r.Messages {
		for _, t := range m.ToolCalls {
			if t.ToolName == name {
				count++
			}
		}
	}
	return count
}

// Provider to LLM. Should loop over tools by itself.
type Provider interface {
	Execute(ctx context.Context, req *Request) (*Response, error)
}
