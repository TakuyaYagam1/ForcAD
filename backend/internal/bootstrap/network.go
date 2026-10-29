package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/nftables"
)

const (
	defaultNetworkMetricsListen = "127.0.0.1:9109"
	networkServerShutdownLimit  = 5 * time.Second
)

type networkOptions struct {
	configPath string
	listen     string
}

// RunNetworkConfigCommand writes the sanitized team network inventory as JSON.
func RunNetworkConfigCommand(ctx context.Context, args []string, out io.Writer) error {
	const location = "bootstrap.RunNetworkConfigCommand"
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - command canceled: %w", location, err)
	}
	out = networkOutput(out)
	options, err := parseNetworkOptions("network-config", args, out, false)
	if err != nil {
		return fmt.Errorf("%s - parse arguments: %w", location, err)
	}
	document, err := config.LoadDocument(options.configPath)
	if err != nil {
		return err
	}
	inventory, err := nftables.InventoryFromDocument(document)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(inventory); err != nil {
		return fmt.Errorf("%s - write sanitized inventory: %w", location, err)
	}
	return nil
}

// RunNetworkRulesCommand writes an nftables script for a sanitized inventory.
func RunNetworkRulesCommand(ctx context.Context, args []string, out io.Writer) error {
	const location = "bootstrap.RunNetworkRulesCommand"
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - command canceled: %w", location, err)
	}
	out = networkOutput(out)
	options, err := parseNetworkOptions("network-rules", args, out, false)
	if err != nil {
		return fmt.Errorf("%s - parse arguments: %w", location, err)
	}
	inventory, err := nftables.ReadInventory(options.configPath)
	if err != nil {
		return err
	}
	rules, err := nftables.RenderRules(inventory)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(out, rules); err != nil {
		return fmt.Errorf("%s - write nftables rules: %w", location, err)
	}
	return nil
}

// RunNetworkMetricsCommand serves Prometheus metrics for the monitoring table.
func RunNetworkMetricsCommand(ctx context.Context, args []string, out io.Writer) error {
	const location = "bootstrap.RunNetworkMetricsCommand"
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - command canceled: %w", location, err)
	}
	out = networkOutput(out)
	options, err := parseNetworkOptions("network-metrics", args, out, true)
	if err != nil {
		return fmt.Errorf("%s - parse arguments: %w", location, err)
	}
	inventory, err := nftables.ReadInventory(options.configPath)
	if err != nil {
		return err
	}
	handler, err := nftables.NewMetricsHandler(ctx, inventory, nftables.ExecCounterLister{})
	if err != nil {
		return err
	}
	return serveNetworkMetrics(ctx, options.listen, handler, out)
}

func parseNetworkOptions(command string, args []string, out io.Writer, withListen bool) (networkOptions, error) {
	options := flag.NewFlagSet(command, flag.ContinueOnError)
	options.SetOutput(out)
	configPath := options.String("config", "", "configuration file")
	listen := ""
	if withListen {
		listen = defaultNetworkMetricsListen
		options.StringVar(&listen, "listen", listen, "metrics listen address")
	}
	if err := options.Parse(args); err != nil {
		return networkOptions{}, err
	}
	if options.NArg() != 0 {
		return networkOptions{}, fmt.Errorf("unexpected command arguments")
	}
	if *configPath == "" {
		return networkOptions{}, fmt.Errorf("--config is required")
	}
	return networkOptions{configPath: *configPath, listen: listen}, nil
}

func networkOutput(out io.Writer) io.Writer {
	if out == nil {
		return io.Discard
	}
	return out
}

func serveNetworkMetrics(ctx context.Context, address string, handler http.Handler, out io.Writer) error {
	const location = "bootstrap.serveNetworkMetrics"
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("%s - listen for network metrics: %w", location, err)
	}
	return serveNetworkMetricsWithListener(ctx, listener, handler, out)
}

func serveNetworkMetricsWithListener(ctx context.Context, listener net.Listener, handler http.Handler, out io.Writer) error {
	const location = "bootstrap.serveNetworkMetricsWithListener"
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	if _, err := fmt.Fprintf(out, "Serving network metrics on %s\n", listener.Addr()); err != nil {
		_ = server.Close()
		if serveErr := <-serveResult; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return errors.Join(fmt.Errorf("%s - write server status: %w", location, err), fmt.Errorf("%s - serve network metrics: %w", location, serveErr))
		}
		return fmt.Errorf("%s - write server status: %w", location, err)
	}
	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("%s - serve network metrics: %w", location, err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), networkServerShutdownLimit)
		shutdownErr := server.Shutdown(shutdownContext)
		cancel()
		if shutdownErr != nil {
			_ = server.Close()
			if serveErr := <-serveResult; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				return errors.Join(fmt.Errorf("%s - shut down network metrics server: %w", location, shutdownErr), fmt.Errorf("%s - serve network metrics: %w", location, serveErr))
			}
			return fmt.Errorf("%s - shut down network metrics server: %w", location, shutdownErr)
		}
		if serveErr := <-serveResult; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("%s - serve network metrics: %w", location, serveErr)
		}
		return nil
	}
}
