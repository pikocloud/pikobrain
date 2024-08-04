package brain

import (
	"errors"
	"fmt"
	"os"
	"text/template"

	"github.com/Masterminds/sprig"
	"gopkg.in/yaml.v3"

	"github.com/pikocloud/pikobrain/internal/providers/openai"
	"github.com/pikocloud/pikobrain/internal/providers/types"
	"github.com/pikocloud/pikobrain/internal/utils"
)

var (
	ErrProviderNotFound = errors.New("provider not found")
)

type Definition struct {
	types.Config `yaml:",inline"`    // model configuration
	Provider     string              `json:"provider" yaml:"provider"` // provider name (openai, bedrock)
	URL          string              `json:"url" yaml:"url"`           // provider URL
	Secret       utils.Value[string] `json:"secret" yaml:"secret"`     // provider secret
}

func Default() Definition {
	return Definition{
		Config: types.Config{
			Model:         "gpt-4o-mini",
			Prompt:        "You are the helpful assistant",
			MaxTokens:     300,
			MaxIterations: 2,
			ForceJSON:     false,
		},
		Provider: "openai",
		URL:      "https://api.openai.com/v1",
		Secret: utils.Value[string]{
			FromEnv: "OPENAI_TOKEN",
		},
	}
}

func New(toolbox types.Toolbox, definition Definition) (*Brain, error) {
	var provider types.Provider

	secret, err := definition.Secret.Get()
	if err != nil {
		return nil, fmt.Errorf("get secret: %w", err)
	}

	switch definition.Provider {
	case "openai":
		provider = openai.New(definition.URL, secret)
	default:
		return nil, fmt.Errorf("provider %q: %w", definition.Provider, ErrProviderNotFound)
	}

	t, err := template.New("").Funcs(sprig.TxtFuncMap()).Parse(definition.Prompt)
	if err != nil {
		return nil, fmt.Errorf("parse prompt: %w", err)
	}

	return &Brain{
		config:   definition.Config,
		provider: provider,
		prompt:   t,
		toolbox:  toolbox,
	}, nil
}

func NewFromFile(toolbox types.Toolbox, file string) (*Brain, error) {
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
	return New(toolbox, def)
}
