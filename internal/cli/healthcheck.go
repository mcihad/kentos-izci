package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mcihad/kentos-izci/internal/config"
)

func newHealthcheckCmd(a *cliApp) *cobra.Command {
	var (
		ready   bool
		timeout time.Duration
		target  string
	)
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Check that the local izci server answers (container health checks)",
		Long: `Request /healthz from the server listening on IZCI_HTTP_ADDR and exit with
status 0 when it answers 200 OK. With --ready, /readyz is checked instead,
which also needs the database. The Docker image uses this command as its
HEALTHCHECK, since it ships without curl.`,
		Example: `  izci healthcheck
  izci healthcheck --ready --timeout 5s
  izci healthcheck --url https://izci.example.com`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := &http.Client{}
			if target == "" {
				if err := a.load(); err != nil {
					return err
				}
				var err error
				if target, err = localURL(a.cfg); err != nil {
					return err
				}
				if a.cfg.TLSEnabled() {
					// The certificate names the public host, not 127.0.0.1.
					pub, _ := url.Parse(a.cfg.PublicURL)
					client.Transport = &http.Transport{TLSClientConfig: &tls.Config{
						ServerName: pub.Hostname(), MinVersion: tls.VersionTLS12,
					}}
				}
			}
			path := "/healthz"
			if ready {
				path = "/readyz"
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(target, "/")+path, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("health check failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("health check failed: %s answered %s", path, resp.Status)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return nil
		},
	}
	cmd.Flags().BoolVar(&ready, "ready", false, "check /readyz (database reachable) instead of /healthz")
	cmd.Flags().DurationVar(&timeout, "timeout", 3*time.Second, "give up after this long")
	cmd.Flags().StringVar(&target, "url", "", "base URL of the server (default: derived from IZCI_HTTP_ADDR)")
	return cmd
}

// localURL is the address a process on the same host reaches the listener
// at: wildcard addresses become loopback.
func localURL(cfg *config.Config) (string, error) {
	host, port, err := net.SplitHostPort(cfg.HTTP.Addr)
	if err != nil {
		return "", fmt.Errorf("IZCI_HTTP_ADDR %q: %w", cfg.HTTP.Addr, err)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	scheme := "http"
	if cfg.TLSEnabled() {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, port), nil
}
