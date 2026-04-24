package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentproxy/agent-proxy/internal/logger"
	"github.com/agentproxy/agent-proxy/internal/proxy"
	"github.com/agentproxy/agent-proxy/internal/telemetry"
	"github.com/agentproxy/agent-proxy/internal/ui"
	"github.com/spf13/cobra"
)

var (
	uiPort      int
	otelEndpoint string
)

func main() {
	root := &cobra.Command{
		Use:   "agent-proxy",
		Short: "Lightweight debugging proxy for MCP, A2A, and ACP protocols",
	}
	root.PersistentFlags().IntVar(&uiPort, "ui-port", 7700, "Port for the web UI and API")
	root.PersistentFlags().StringVar(&otelEndpoint, "otel-endpoint", "", "OTLP HTTP endpoint to export traces (e.g. http://localhost:4318)")

	root.AddCommand(httpCmd(), stdioCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// setupTelemetry initialises OTEL and wires RecordSpan onto the logger.
func setupTelemetry(ctx context.Context, l *logger.Logger) func(context.Context) error {
	shutdown, err := telemetry.Setup(ctx, otelEndpoint)
	if err != nil {
		log.Printf("OTEL setup failed: %v — continuing without traces", err)
		return func(context.Context) error { return nil }
	}
	if otelEndpoint != "" {
		log.Printf("OTEL traces → %s", otelEndpoint)
		l.OnAdd = telemetry.RecordSpan
	}
	return shutdown
}

func httpCmd() *cobra.Command {
	var listenPort int
	var targetURL string

	cmd := &cobra.Command{
		Use:     "http",
		Short:   "HTTP reverse proxy mode (MCP HTTP/SSE, A2A, ACP)",
		Example: `  agent-proxy http --listen 7701 --target http://localhost:8080`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			l := logger.New()
			shutdown := setupTelemetry(ctx, l)
			defer shutdown(ctx)

			p, err := proxy.NewHTTP(targetURL, l)
			if err != nil {
				return err
			}

			uiAddr := fmt.Sprintf(":%d", uiPort)
			proxyAddr := fmt.Sprintf(":%d", listenPort)

			uiMux := http.NewServeMux()
			uiMux.Handle("/ui", ui.Handler())
			uiMux.Handle("/ui/", ui.Handler())
			uiMux.HandleFunc("/api/messages", l.Handler())

			uiSrv := &http.Server{Addr: uiAddr, Handler: uiMux}
			proxySrv := &http.Server{Addr: proxyAddr, Handler: p}

			go func() {
				log.Printf("UI listening on http://localhost%s/ui", uiAddr)
				if err := uiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("UI server error: %v", err)
				}
			}()

			log.Printf("Proxy listening on %s → %s", proxyAddr, targetURL)
			go func() {
				if err := proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Fatalf("Proxy server error: %v", err)
				}
			}()

			waitShutdown(uiSrv, proxySrv)
			return nil
		},
	}

	cmd.Flags().IntVar(&listenPort, "listen", 7701, "Port to listen on for proxied traffic")
	cmd.Flags().StringVar(&targetURL, "target", "", "Upstream target URL (required)")
	cmd.MarkFlagRequired("target")
	return cmd
}

func stdioCmd() *cobra.Command {
	var cmdLine string

	cmd := &cobra.Command{
		Use:     "stdio",
		Short:   "Stdio intercept mode (MCP stdio transport)",
		Example: `  agent-proxy stdio --cmd "python weather_server.py"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			l := logger.New()
			shutdown := setupTelemetry(ctx, l)
			defer shutdown(ctx)

			uiAddr := fmt.Sprintf(":%d", uiPort)
			uiMux := http.NewServeMux()
			uiMux.Handle("/ui", ui.Handler())
			uiMux.Handle("/ui/", ui.Handler())
			uiMux.HandleFunc("/api/messages", l.Handler())

			uiSrv := &http.Server{Addr: uiAddr, Handler: uiMux}
			go func() {
				log.Printf("UI listening on http://localhost%s/ui", uiAddr)
				if err := uiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("UI server error: %v", err)
				}
			}()

			sp := proxy.NewStdio(cmdLine, l)
			return sp.Run()
		},
	}

	cmd.Flags().StringVar(&cmdLine, "cmd", "", "Command to run as the MCP server (required)")
	cmd.MarkFlagRequired("cmd")
	return cmd
}

func waitShutdown(servers ...*http.Server) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		s.Shutdown(ctx)
	}
}
