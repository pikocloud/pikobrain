package loader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/pikocloud/pikobrain/internal/providers/types"
	"github.com/pikocloud/pikobrain/internal/tools/openapi"
)

func LoadFile(file string) ([]types.ToolProviderFunc, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	return Decode(f)
}

// Decode stream assuming that it's multi-document YAML tools definition.
func Decode(src io.Reader) ([]types.ToolProviderFunc, error) {
	var ans []types.ToolProviderFunc
	doc := yaml.NewDecoder(src)
	for {
		fp, err := decodePart(doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		ans = append(ans, fp)
	}
	return ans, nil
}

func decodePart(dec *yaml.Decoder) (types.ToolProviderFunc, error) {
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode document: %w", err)
	}

	var meta Meta
	if err := root.Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	switch meta.Type {
	case OpenAPI:
		var config openapi.Config
		if err := root.Decode(&config); err != nil {
			return nil, fmt.Errorf("decode config: %w", err)
		}
		return func(ctx context.Context) ([]types.Tool, error) {
			return openapi.New(ctx, config)
		}, nil
	}

	return nil, fmt.Errorf("unknown tool type: %s", meta.Type)
}
