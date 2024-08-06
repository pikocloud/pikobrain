package brain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/template"

	"github.com/Masterminds/sprig"
	"gopkg.in/yaml.v3"

	"github.com/pikocloud/pikobrain/internal/providers/bedrock"
	"github.com/pikocloud/pikobrain/internal/providers/openai"
	"github.com/pikocloud/pikobrain/internal/providers/types"
	"github.com/pikocloud/pikobrain/internal/utils"
)

var (
	ErrProviderNotFound = errors.New("provider not found")
)

type Definition struct {
	types.Config  `yaml:",inline"`    // model configuration
	MaxIterations int                 `json:"max_iterations" yaml:"maxIterations"`
	Provider      string              `json:"provider" yaml:"provider"` // provider name (openai, bedrock)
	URL           string              `json:"url" yaml:"url"`           // provider URL
	Secret        utils.Value[string] `json:"secret" yaml:"secret"`     // provider secret
}

func Default() Definition {
	return Definition{
		Config: types.Config{
			Model:     "gpt-4o-mini",
			Prompt:    "You are the helpful assistant",
			MaxTokens: 300,
			ForceJSON: false,
		},
		MaxIterations: 2,
		Provider:      "openai",
		URL:           "https://api.openai.com/v1",
		Secret: utils.Value[string]{
			FromEnv: "OPENAI_TOKEN",
		},
	}
}

func New(ctx context.Context, toolbox types.Toolbox, definition Definition) (*Brain, error) {
	var provider types.Provider

	secret, err := definition.Secret.Get()
	if err != nil {
		return nil, fmt.Errorf("get secret: %w", err)
	}

	switch definition.Provider {
	case "openai":
		provider = openai.New(definition.URL, secret)
	case "bedrock":
		p, err := bedrock.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("new bedrock provider: %w", err)
		}
		provider = p
	default:
		return nil, fmt.Errorf("provider %q: %w", definition.Provider, ErrProviderNotFound)
	}

	t, err := template.New("").Funcs(sprig.TxtFuncMap()).Parse(definition.Prompt)
	if err != nil {
		return nil, fmt.Errorf("parse prompt: %w", err)
	}

	return &Brain{
		config:     definition.Config,
		iterations: definition.MaxIterations,
		provider:   provider,
		prompt:     t,
		toolbox:    toolbox,
	}, nil
}

func NewFromFile(ctx context.Context, toolbox types.Toolbox, file string) (*Brain, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	var def Definition
	err = yaml.NewDecoder(f).Decode(&def)
	if err != nil {
		return nil, fmt.Errorf("decode file: %w", err)
	}
	_ = f.Close()
	return New(ctx, toolbox, def)
}
