package loader

//go:generate go run github.com/abice/go-enum@v0.6.0 --marshal --noprefix

// ToolType describes which tool should be used.
// ENUM(
// OpenAPI = openapi,
// )
type ToolType string

type Meta struct {
	Type ToolType `yaml:"type"`
}
