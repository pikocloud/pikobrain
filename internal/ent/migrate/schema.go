// Code generated by ent, DO NOT EDIT.

package migrate

import (
	"entgo.io/ent/dialect/sql/schema"
	"entgo.io/ent/schema/field"
)

var (
	// MessagesColumns holds the columns for the "messages" table.
	MessagesColumns = []*schema.Column{
		{Name: "id", Type: field.TypeInt, Increment: true},
		{Name: "thread", Type: field.TypeString, Size: 2147483647},
		{Name: "tool_name", Type: field.TypeString, Nullable: true},
		{Name: "tool_id", Type: field.TypeString, Nullable: true},
		{Name: "role", Type: field.TypeString},
		{Name: "user", Type: field.TypeString, Nullable: true},
		{Name: "mime", Type: field.TypeString, Default: "text/plain"},
		{Name: "content", Type: field.TypeBytes},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
	}
	// MessagesTable holds the schema information for the "messages" table.
	MessagesTable = &schema.Table{
		Name:       "messages",
		Columns:    MessagesColumns,
		PrimaryKey: []*schema.Column{MessagesColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "message_thread",
				Unique:  false,
				Columns: []*schema.Column{MessagesColumns[1]},
			},
			{
				Name:    "message_user",
				Unique:  false,
				Columns: []*schema.Column{MessagesColumns[5]},
			},
		},
	}
	// Tables holds all the tables in the schema.
	Tables = []*schema.Table{
		MessagesTable,
	}
)

func init() {
}