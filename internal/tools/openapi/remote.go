package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/invopop/jsonschema"

	"github.com/pikocloud/pikobrain/internal/providers/types"
	"github.com/pikocloud/pikobrain/internal/utils"
)

const (
	DefaultTimeout = 30 * time.Second
	DefaultLimit   = 1024 * 1024
	DefaultDepth   = 10
)

type Config struct {
	Namespace               string               `json:"namespace" yaml:"namespace,omitempty"`                     // Prefix all operation with value and underscore
	KeepRefs                bool                 `json:"keep_refs" yaml:"keepRefs,omitempty"`                      // do not flatten schema refs
	URL                     string               `json:"url" yaml:"url"`                                           // OpenAPI url.
	Timeout                 time.Duration        `json:"timeout" yaml:"timeout"`                                   // Request timeout.
	MaxResponse             int                  `json:"max_response" yaml:"maxResponse"`                          // Maximum response body size in bytes.
	Headers                 []utils.Pair[string] `json:"headers,omitempty" yaml:"headers,omitempty"`               // Extra outgoing headers.
	IgnoreInvalidOperations bool                 `json:"ignore_invalid_operations" yaml:"ignoreInvalidOperations"` // Do not fail if some operations can not be used. Just print error and skip.
	AcceptJSON              bool                 `json:"accept_json" yaml:"acceptJSON"`                            // Set Accept: application/json headers
	BaseURL                 string               `json:"base_url" yaml:"baseURL"`                                  // use another base URL.
	Exclude                 []string             `json:"exclude,omitempty" yaml:"exclude,omitempty"`               // Exclude specific operations IDs (ex: health checks or readiness)
}

// New tools from OpenAPI schema.
// Schema should be OAS 3+.
// Complex structures are not supported (allOf, anyOf, ...).
func New(ctx context.Context, config Config) ([]types.Tool, error) {
	doc, err := parseRemote(ctx, config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse remote document: %w", err)
	}

	var rawURL = config.URL
	if config.BaseURL != "" {
		rawURL = config.BaseURL
	} else if strings.HasPrefix(rawURL, "file:") {
		rawURL = ""
	}
	baseURL, err := doc.BaseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("base url: %w", err)
	}

	var staticHeaders = make(http.Header)
	for _, h := range config.Headers {
		value, err := h.Get()
		if err != nil {
			return nil, fmt.Errorf("get header %q value: %w", h.Name, err)
		}
		staticHeaders.Add(h.Name, value)
	}

	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}

	if config.MaxResponse <= 0 {
		config.MaxResponse = DefaultLimit
	}

	httpClient := &http.Client{
		Timeout: config.Timeout,
	}

	var client httpClientFunc = func(req *http.Request) (*http.Response, error) {
		for k, v := range staticHeaders {
			req.Header[k] = v
		}
		if config.AcceptJSON {
			req.Header.Set("Accept", "application/json")
		}
		res, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http request: %w", err)
		}

		res.Body = utils.ReadCloser(utils.NewLimitedReader(res.Body, config.MaxResponse), res.Body)
		return res, nil
	}

	excluded := utils.NewSet(config.Exclude...)

	var ans []types.Tool
	for p, def := range doc.Paths {
		for method, operation := range def.Operations {
			if excluded.Contains(operation.OperationID) {
				slog.Debug("operation excluded by config", "operation", operation.OperationID)
				continue
			}

			var description = utils.Concat(". ", operation.Summary, operation.Description)

			input, err := operation.toolInput()
			if err != nil {
				if config.IgnoreInvalidOperations {
					slog.Warn("ignoring invalid operation", "path", p, "method", method, "operation", operation.OperationID, "error", err)
					continue
				}
				return nil, fmt.Errorf("create tool definition for %q: %w", operation.OperationID, err)
			}

			if !config.KeepRefs {
				resolved, err := flatten(input.Definitions, input, DefaultDepth)
				if err != nil {
					return nil, fmt.Errorf("flatten definition %q: %w", operation.OperationID, err)
				}
				input = resolved
				input.Definitions = nil
			}

			name := utils.Concat("_", config.Namespace, operation.OperationID)

			ans = append(ans, &openAPITool{
				name:         name,
				input:        input,
				description:  description,
				client:       client,
				method:       method,
				baseURL:      baseURL,
				pathTemplate: p,
			})
		}
	}

	return ans, nil
}

type openAPITool struct {
	input       *jsonschema.Schema
	name        string
	description string

	client       httpClientFunc
	method       string
	baseURL      *url.URL
	pathTemplate string
}

func (tool *openAPITool) Name() string {
	return tool.name
}

func (tool *openAPITool) Description() string {
	return tool.description
}

func (tool *openAPITool) Input() *jsonschema.Schema {
	return tool.input
}

func (tool *openAPITool) Call(ctx context.Context, message json.RawMessage) (types.Content, error) {
	var req toolRequest
	if err := json.Unmarshal(message, &req); err != nil {
		return types.Content{}, fmt.Errorf("parse request: %w", err)
	}

	u := tool.pathTemplate

	for k, v := range req.Path {
		u = strings.ReplaceAll(u, "{"+k+"}", url.PathEscape(fmt.Sprint(v)))
	}

	link := *tool.baseURL
	link.Path = path.Join(link.Path, u)

	var q = make(url.Values)
	for k, v := range req.Query {
		q.Add(k, fmt.Sprint(v))
	}

	link.RawQuery = q.Encode()

	out, err := http.NewRequestWithContext(ctx, strings.ToUpper(tool.method), link.String(), bytes.NewReader(req.Body))
	if err != nil {
		return types.Content{}, fmt.Errorf("create request: %w", err)
	}
	if len(req.Body) > 0 {
		out.Header.Set("Content-Type", "application/json")
	}

	for k, v := range req.Headers {
		out.Header.Set(k, fmt.Sprint(v))
	}

	res, err := tool.client(out)
	if err != nil {
		return types.Content{}, fmt.Errorf("do request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode/100 != 2 {
		return types.Content{}, fmt.Errorf("invalid response status code: %d", res.StatusCode)
	}

	content, err := io.ReadAll(res.Body)
	if err != nil {
		return types.Content{}, fmt.Errorf("read response body: %w", err)
	}
	contentType := utils.ContentType(res.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/plain"
	}

	mime, err := types.ParseMIME(contentType)
	if err != nil {
		return types.Content{}, fmt.Errorf("parse MIME: %w", err)
	}

	return types.Content{
		Data: content,
		Mime: mime,
	}, nil

}
