package types

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/invopop/jsonschema"
	"github.com/sourcegraph/conc/pool"
)

type ToolProviderFunc func(ctx context.Context) ([]Tool, error)

type ToolFunc func(context.Context, json.RawMessage) (Content, error)

func MustTool[In any](name string, description string, callable func(ctx context.Context, payload In) (Content, error)) Tool {
	var inp In

	sch := (&jsonschema.Reflector{
		Anonymous:      true,
		DoNotReference: true,
	}).Reflect(&inp)
	sch.Version = ""
	sch.AdditionalProperties = nil
	return &simpleTool{
		name:        name,
		description: description,
		input:       sch,
		handler: func(ctx context.Context, s json.RawMessage) (Content, error) {
			var arg In
			if err := json.Unmarshal(s, &arg); err != nil {
				return Content{}, fmt.Errorf("unmarshal: %w", err)
			}

			return callable(ctx, arg)
		},
	}
}

type simpleTool struct {
	name        string
	description string
	input       *jsonschema.Schema
	handler     ToolFunc
}

func (st *simpleTool) Name() string {
	return st.name
}

func (st *simpleTool) Description() string {
	return st.description
}

func (st *simpleTool) Input() *jsonschema.Schema {
	return st.input
}

func (st *simpleTool) Call(ctx context.Context, args json.RawMessage) (Content, error) {
	return st.handler(ctx, args)
}

type ToolProvider struct {
	provider ToolProviderFunc
	state    atomic.Pointer[[]Tool]
}

func (tp *ToolProvider) Update(ctx context.Context) error {
	data, err := tp.provider(ctx)
	if err != nil {
		return err
	}
	tp.state.Store(&data)
	return nil
}

type DynamicToolbox struct {
	providers []*ToolProvider
}

func (st *DynamicToolbox) Snapshot() Snapshot {
	var snapshot = make(Snapshot)

	for _, p := range st.providers {
		state := p.state.Load()
		if state == nil {
			continue
		}
		for _, tool := range *state {
			snapshot[tool.Name()] = tool
		}
	}

	return snapshot
}

// Update all providers. If strict enabled, stops on a first error.
func (st *DynamicToolbox) Update(ctx context.Context, strict bool) error {
	wg := pool.New().WithContext(ctx)
	if strict {
		wg = wg.WithCancelOnError()
	}
	for _, p := range st.providers {
		p := p
		wg.Go(func(ctx context.Context) error {
			return p.Update(ctx)
		})
	}
	return wg.Wait()
}

// Provider registers provider which dynamically can Update internal set of tools. Should not be called after start.
func (st *DynamicToolbox) Provider(providerFunc ...ToolProviderFunc) {
	for _, p := range providerFunc {
		st.providers = append(st.providers, &ToolProvider{
			provider: p,
		})
	}
}

// Add tools. Basic wrapper around Provider. Should not be called after start.
func (st *DynamicToolbox) Add(tools ...Tool) {
	if len(tools) == 0 {
		return
	}
	st.Provider(func(ctx context.Context) ([]Tool, error) {
		return tools, nil
	})
}
