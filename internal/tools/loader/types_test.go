package loader_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/pikocloud/pikobrain/internal/tools/loader"
)

func TestToolType_MarshalText(t *testing.T) {
	var (
		valid   = []byte("type: openapi")
		invalud = []byte("type: xyz")
	)

	var meta loader.Meta

	err := yaml.Unmarshal(valid, &meta)
	require.NoError(t, err)
	assert.Equal(t, loader.OpenAPI, meta.Type)

	var other loader.Meta

	err = yaml.Unmarshal(invalud, &other)
	require.Error(t, err)
	t.Logf("err: %v", err)
}
