package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/Cali0707/mcpskills/internal/catalog"
	"github.com/Cali0707/mcpskills/internal/config"
	"github.com/Cali0707/mcpskills/internal/policy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/propagation"
)

const (
	serverName    = "mcpskills"
	serverVersion = "0.1.0"
)

func newMCPServer(source catalog.Catalog, logger *slog.Logger) (*mcp.Server, error) {
	return buildMCPServer(source, nil, logger)
}

func buildMCPServer(source catalog.Catalog, access *accessControl, logger *slog.Logger) (*mcp.Server, error) {
	capabilities := &mcp.ServerCapabilities{Resources: &mcp.ResourceCapabilities{}}
	capabilities.AddExtension(skillsExtension, map[string]any{"directoryRead": true})
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, &mcp.ServerOptions{
		Logger:       logger,
		Capabilities: capabilities,
	})
	if access != nil {
		server.AddReceivingMiddleware(access.middleware)
	}
	if err := registerSkills(server, source, access); err != nil {
		return nil, err
	}
	return server, nil
}

func protectedHTTPHandler(server *mcp.Server, cfg config.Config, verifier *tokenVerifier, logger *slog.Logger) http.Handler {
	resource, _ := url.Parse(cfg.ResourceURL)
	metadataURL := resource.Scheme + "://" + resource.Host + "/.well-known/oauth-protected-resource" + cfg.EndpointPath
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-protected-resource"+cfg.EndpointPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              cfg.ResourceURL,
			"authorization_servers": []string{cfg.Issuer},
			"scopes_supported":      []string{"mcp:skills"},
		})
	})
	streamable := newHTTPHandler(server, cfg.EndpointPath, logger)
	mux.Handle(cfg.EndpointPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		challenge := "Bearer resource_metadata=\"" + metadataURL + "\", scope=\"mcp:skills\""
		auth := r.Header.Get("Authorization")
		if len(r.Header.Values("Authorization")) != 1 || !strings.HasPrefix(auth, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == "" {
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}
		p, err := verifier.verify(r.Context(), strings.TrimPrefix(auth, "Bearer "))
		if err != nil {
			w.Header().Set("WWW-Authenticate", challenge + ", error=\"invalid_token\"")
			http.Error(w, "invalid access token", http.StatusUnauthorized)
			return
		}
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		streamable.ServeHTTP(w, r.WithContext(context.WithValue(ctx, principalKey{}, p)))
	}))
	return mux
}

func newHTTPHandler(server *mcp.Server, endpointPath string, logger *slog.Logger) http.Handler {
	streamableHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, Logger: logger},
	)

	mux := http.NewServeMux()
	mux.Handle(endpointPath, streamableHandler)
	return mux
}

// Run serves the MCP endpoint until the context is canceled or the server fails.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	source, err := catalog.NewDirectorySource(cfg.SkillsDir)
	if err != nil {
		return fmt.Errorf("load skills catalog: %w", err)
	}
	skillsDir, err := filepath.Abs(cfg.SkillsDir)
	if err != nil {
		return err
	}
	policyPath, err := filepath.Abs(cfg.PolicyFile)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(skillsDir, policyPath)
	if err != nil {
		return err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("policy file must be outside skills directory")
	}
	rules, err := policy.Load(cfg.PolicyFile, source)
	if err != nil {
		return err
	}
	access, err := newAccessControl(source, rules)
	if err != nil {
		return err
	}
	metrics, err := newTelemetry(ctx)
	if err != nil {
		return fmt.Errorf("configure telemetry: %w", err)
	}
	defer metrics.shutdown()
	access.telemetry = metrics
	mcpServer, err := buildMCPServer(source, access, logger)
	if err != nil {
		return fmt.Errorf("configure skills server: %w", err)
	}
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           protectedHTTPHandler(mcpServer, cfg, newTokenVerifier(cfg.Issuer, cfg.ResourceURL, cfg.GroupsClaim), logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.ListenAndServe()
	}()
	logger.Info("MCP server listening", "address", cfg.ListenAddr, "endpoint", cfg.EndpointPath)

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down MCP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = httpServer.Close()
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}
		return nil
	}
}
