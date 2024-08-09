package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/pikocloud/pikobrain/internal/providers/types"
)

// Message holds the schema definition for the Message entity.
type Message struct {
	ent.Schema
}

// Fields of the Message.
func (Message) Fields() []ent.Field {
	return []ent.Field{
		field.Text("thread"),
		field.String("tool_name").Optional(),
		field.String("tool_id").Optional(),
		field.String("role").GoType(types.Role("")).NotEmpty(),
		field.String("user").Optional(),
		field.String("mime").GoType(types.MIME("")).Default(string(types.MIMEText)).NotEmpty(),
		field.Bytes("content"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Message.
func (Message) Edges() []ent.Edge {
	return nil
}

func (Message) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("thread"),
		index.Fields("user"),
	}
}
