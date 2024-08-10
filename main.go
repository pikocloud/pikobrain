package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/rs/cors"
	"github.com/sourcegraph/conc/pool"

	"github.com/pikocloud/pikobrain/internal/brain"
	"github.com/pikocloud/pikobrain/internal/ent"
	"github.com/pikocloud/pikobrain/internal/providers/types"
	"github.com/pikocloud/pikobrain/internal/server"
	"github.com/pikocloud/pikobrain/internal/tools/loader"
	"github.com/pikocloud/pikobrain/internal/web"
)

type Config struct {
	Debug struct {
		Enable bool `long:"enable" env:"ENABLE" description:"Enable debug mode"`
	} `group:"Debug" namespace:"debug" env-namespace:"DEBUG"`
	DB      ent.Config    `group:"Database configuration" namespace:"db" env-namespace:"DB"`
	Timeout time.Duration `long:"timeout" env:"TIMEOUT" description:"LLM timeout" default:"30s"`
	Refresh time.Duration `long:"refresh" env:"REFRESH" description:"Refresh interval for tools" default:"30s"`
	Config  string        `long:"config" env:"CONFIG" description:"Config file" default:"brain.yaml"`
	Tools   string        `long:"tools" env:"TOOLS" description:"Tool file"`
	BaseURL string        `long:"base-url" env:"BASE_URL" description:"Base URL for UI"`
	Server  struct {
		Bind              string        `long:"bind" env:"BIND" description:"Bind address" default:":8080"`
		TLS               bool          `long:"tls" env:"TLS" description:"Enable TLS"`
		CA                string        `long:"ca" env:"CA" description:"Path to CA files. Optional unless IGNORE_SYSTEM_CA set" default:"ca.pem"`
		Cert              string        `long:"cert" env:"CERT" description:"Server certificate" default:"cert.pem"`
		Key               string        `long:"key" env:"KEY" description:"Server private key" default:"key.pem"`
		Mutual            bool          `long:"mutual" env:"MUTUAL" description:"Enable mutual TLS"`
		IgnoreSystemCA    bool          `long:"ignore-system-ca" env:"IGNORE_SYSTEM_CA" description:"Do not load system-wide CA"`
		ReadHeaderTimeout time.Duration `long:"read-header-timeout" env:"READ_HEADER_TIMEOUT" description:"How long to read header from the request" default:"3s"`
		Graceful          time.Duration `long:"graceful" env:"GRACEFUL" description:"Graceful shutdown timeout" default:"5s"`
		Timeout           time.Duration `long:"timeout" env:"TIMEOUT" description:"Any request timeout" default:"30s"`
		MaxBodySize       int64         `long:"max-body-size" env:"MAX_BODY_SIZE" description:"Maximum payload size in bytes" default:"1048576"` // 1 MiB
	} `group:"HTTP server configuration" namespace:"http" env-namespace:"HTTP"`
}

func main() {
	var app Config
	parser := flags.NewParser(&app, flags.Default)
	parser.ShortDescription = `PikoBrain`
	parser.LongDescription = `Server for orchestrating LLM providers and agents`

	_, err := parser.Parse()
	if err != nil {
		os.Exit(1)
	}

	if err := app.Execute(nil); err != nil {
		slog.Error("failed run", "error", err)
		os.Exit(2)
	}
}

func (config *Config) Execute([]string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	config.setupLogging()
	store, err := ent.New(ctx, config.DB)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}
	defer store.Close()

	var toolBox types.DynamicToolbox

	if config.Tools != "" {
		tools, err := loader.LoadFile(config.Tools)
		if err != nil {
			return fmt.Errorf("load tools: %w", err)
		}

		toolBox.Provider(tools...)
	}

	slog.Info("loading initial tools state...")
	if err := toolBox.Update(ctx, true); err != nil {
		return fmt.Errorf("load tools: %w", err)
	}

	slog.Info("loading brain config")
	mind, err := brain.NewFromFile(ctx, store, &toolBox, config.Config)
	if err != nil {
		return fmt.Errorf("load brain config: %w", err)
	}

	slog.Info("configuration loaded")

	// setup backend
	srv := &server.Server{
		Brain:   mind,
		Timeout: config.Timeout,
	}
	router := http.NewServeMux()
	router.HandleFunc("PUT /{thread}", srv.Append)
	router.HandleFunc("POST /{thread}", srv.Chat)
	router.HandleFunc("POST /{thread}/", srv.Chat)
	router.HandleFunc("POST /", srv.Run)
	router.HandleFunc("GET /ready", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})

	// frontend
	front, err := web.New(store, mind, config.BaseURL)
	if err != nil {
		return fmt.Errorf("create frontend: %w", err)
	}
	router.HandleFunc("GET /", front.Index)
	router.HandleFunc("GET /threads/", front.Threads)
	router.HandleFunc("GET /threads/{thread}/", front.Thread)
	router.HandleFunc("DELETE /messages/{message}/", front.DeleteMessage)
	router.Handle("GET /static/", http.StripPrefix("/static", http.FileServerFS(web.MustStatic())))

	// setup HTTP server
	httpServer := &http.Server{
		Addr:              config.Server.Bind,
		Handler:           config.httpLimiter(ctx, router),
		ReadHeaderTimeout: config.Server.ReadHeaderTimeout,
	}

	tlsConfig, err := config.tlsConfig()
	if err != nil {
		return fmt.Errorf("create TLS config: %w", err)
	}
	httpServer.TLSConfig = tlsConfig

	wg := pool.New().WithContext(ctx).WithCancelOnError()

	// run HTTP server in background
	wg.Go(func(_ context.Context) error {
		var err error
		if config.Server.TLS {
			slog.Info("starting TLS server", "bind", config.Server.Bind)
			err = httpServer.ListenAndServeTLS(config.Server.Cert, config.Server.Key)
		} else {
			slog.Info("starting plain server", "bind", config.Server.Bind)
			err = httpServer.ListenAndServe()
		}
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		return err
	})

	// watch signal ad stop HTTP server gracefully
	wg.Go(func(ctx context.Context) error {
		<-ctx.Done()
		slog.Info("stopping server")
		tctx, tcancel := context.WithTimeout(context.Background(), config.Server.Graceful)
		defer tcancel()
		return httpServer.Shutdown(tctx)
	})

	// periodically update
	wg.Go(func(ctx context.Context) error {
		t := time.NewTicker(config.Refresh)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
			}

			slog.Debug("updating tools cache")
			err := toolBox.Update(ctx, false)
			if err != nil {
				slog.Warn("failed update tools cache", "error", err)
			}
		}
	})

	return wg.Wait()
}

func (config *Config) setupLogging() {
	if !config.Debug.Enable {
		return
	}
	lvl := new(slog.LevelVar)
	lvl.Set(slog.LevelDebug)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	}))
	slog.SetDefault(logger)
}

func (config *Config) tlsConfig() (*tls.Config, error) {
	if !config.Server.TLS {
		return nil, nil //nolint:nilnil
	}
	var ca *x509.CertPool
	// create system-based CA or completely independent
	if config.Server.IgnoreSystemCA {
		ca = x509.NewCertPool()
	} else if roots, err := x509.SystemCertPool(); err == nil {
		ca = roots
	} else {
		return nil, fmt.Errorf("read system certs: %w", err)
	}

	// attach custom CA (if required)
	if err := config.loadCA(ca); err != nil {
		return nil, fmt.Errorf("load CA: %w", err)
	}

	// read key
	cert, err := tls.LoadX509KeyPair(config.Server.Cert, config.Server.Key)
	if err != nil {
		return nil, fmt.Errorf("load cert and key: %w", err)
	}

	// enable mTLS if needed
	var clientAuth = tls.NoClientCert
	if config.Server.Mutual {
		clientAuth = tls.RequireAndVerifyClientCert
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      ca,
		ClientCAs:    ca,
		ClientAuth:   clientAuth,
	}, nil
}

func (config *Config) loadCA(ca *x509.CertPool) error {
	caCert, err := os.ReadFile(config.Server.CA)

	if err != nil {
		if config.Server.IgnoreSystemCA {
			// no system, no custom
			return fmt.Errorf("read CA: %w", err)
		}
		slog.Warn("failed read custom CA", "error", err)
		return nil
	}

	if !ca.AppendCertsFromPEM(caCert) {
		if config.Server.IgnoreSystemCA {
			return errors.New("CA certs failed to load")
		}
		slog.Warn("failed add custom CA to pool")
	}
	return nil
}

func (config *Config) httpLimiter(parent context.Context, next http.Handler) http.Handler {
	if config.Debug.Enable {
		slog.Warn("CORS allowed")
		next = cors.AllowAll().Handler(next)
	}
	next = http.MaxBytesHandler(next, config.Server.MaxBodySize)

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(parent, config.Server.Timeout)
		defer cancel()
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}
