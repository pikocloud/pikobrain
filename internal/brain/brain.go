package brain

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"text/template"

	"github.com/pikocloud/pikobrain/internal/providers/types"
)

type Brain struct {
	prompt   *template.Template
	config   types.Config
	provider types.Provider
	toolbox  types.Toolbox
}

func (m *Brain) Run(ctx context.Context, messages []types.Message, params url.Values) (*types.Response, error) {
	// generate prompt
	var prompt bytes.Buffer

	if err := m.prompt.Execute(&prompt, promptContext{
		Messages: messages,
		Params:   params,
	}); err != nil {
		return nil, fmt.Errorf("render prompt: %w", err)
	}

	cfg := m.config
	cfg.Prompt = prompt.String() // replace prompt to rendered template

	return m.provider.Execute(ctx, &types.Request{
		Config:  cfg,
		History: messages,
		Tools:   m.toolbox,
	})

}

type promptContext struct {
	Messages []types.Message
	Params   url.Values
}
