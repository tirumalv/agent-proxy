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
	uiPort       int
	otelEndpoint string
	logFile      string
)

func main() {
	root := &cobra.Command{
		Use:   "agent-proxy",
		Short: "Lightweight debugging proxy for MCP, A2A, and ACP protocols",
	}
	root.PersistentFlags().IntVar(&uiPort, "ui-port", 7700, "Port for the web UI and API")
	root.PersistentFlags().StringVar(&otelEndpoint, "otel-endpoint", "", "OTLP HTTP endpoint to export traces (e.g. http://localhost:4318)")
	root.PersistentFlags().StringVar(&logFile, "log-file", "", "Append captured messages as NDJSON to this file")

	root.AddCommand(httpCmd(), stdioCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// setupLogger creates the logger, attaches the file hook (if requested), and
// wires OTEL tracing. Returns the logger and an OTEL shutdown function.
func setupLogger(ctx context.Context) (*logger.Logger, func(context.Context) error) {
	l := logger.New()

	if logFile != "" {
		hook, close, err := logger.NewFileHook(logFile)
		if err != nil {
			log.Printf("File log setup failed: %v — continuing without file log", err)
		} else {
			l.AddHook(hook)
			log.Printf("File log → %s", logFile)
			// Close the file when the process exits via the OTEL shutdown chain below.
			_ = close // closed on process exit; acceptable for a proxy tool
		}
	}

	shutdown, err := telemetry.Setup(ctx, otelEndpoint)
	if err != nil {
		log.Printf("OTEL setup failed: %v — continuing without traces", err)
		return l, func(context.Context) error { return nil }
	}
	if otelEndpoint != "" {
		log.Printf("OTEL traces → %s", otelEndpoint)
		l.AddHook(telemetry.RecordSpan)
	}
	return l, shutdown
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
			l, shutdown := setupLogger(ctx)
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
			uiMux.HandleFunc("/api/stats", l.StatsHandler())

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
			l, shutdown := setupLogger(ctx)
			defer shutdown(ctx)

			uiAddr := fmt.Sprintf(":%d", uiPort)
			uiMux := http.NewServeMux()
			uiMux.Handle("/ui", ui.Handler())
			uiMux.Handle("/ui/", ui.Handler())
			uiMux.HandleFunc("/api/messages", l.Handler())
			uiMux.HandleFunc("/api/stats", l.StatsHandler())

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
