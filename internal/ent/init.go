package ent

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib" // driver
	_ "modernc.org/sqlite"             // driver
)

type Config struct {
	URL          string        `long:"url" env:"URL" description:"Database URL" default:"sqlite://data.sqlite?cache=shared&_fk=1&_pragma=foreign_keys(1)"`
	MaxConn      int           `long:"max-conn" env:"MAX_CONN" description:"Maximum number of opened connections to database" default:"10"`
	IdleConn     int           `long:"idle-conn" env:"IDLE_CONN" description:"Maximum number of idle connections to database" default:"1"`
	IdleTimeout  time.Duration `long:"idle-timeout" env:"IDLE_TIMEOUT" description:"Maximum amount of time a connection may be idle" default:"0"`
	ConnLifeTime time.Duration `long:"conn-life-time" env:"CONN_LIFE_TIME" description:"Maximum amount of time a connection may be reused" default:"0"`
}

func New(ctx context.Context, config Config) (*Client, error) {
	u, err := url.Parse(config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}

	client, err := newDBClient(u, config)
	if err != nil {
		return nil, fmt.Errorf("create db client: %w", err)
	}

	if err := client.Schema.Create(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return client, nil
}

func newDBClient(u *url.URL, config Config) (*Client, error) {
	switch u.Scheme {
	case "sqlite", "sqlite3", "file":
		u.Scheme = "file"
		q := u.Query()
		q.Add("_pragma", "foreign_keys(1)")
		u.RawQuery = q.Encode()
		connURL := strings.ReplaceAll(u.String(), "file://", "file:")
		db, err := sql.Open("sqlite", connURL)
		if err != nil {
			return nil, err
		}
		config.applyConfig(db)
		return NewClient(Driver(entsql.OpenDB(dialect.SQLite, db))), nil
	case "postgres":
		db, err := sql.Open("pgx", u.String())
		if err != nil {
			return nil, err
		}
		config.applyConfig(db)
		return NewClient(Driver(entsql.OpenDB(dialect.Postgres, db))), nil
	default:
		return nil, fmt.Errorf("unknown dialect %s", u.Scheme)
	}
}

func (cfg Config) applyConfig(db *sql.DB) {
	db.SetMaxIdleConns(cfg.IdleConn)
	db.SetMaxOpenConns(cfg.MaxConn)
	db.SetConnMaxIdleTime(cfg.IdleTimeout)
	db.SetConnMaxLifetime(cfg.ConnLifeTime)
}
