package cli

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mcihad/kentos-izci/internal/app"
	"github.com/mcihad/kentos-izci/internal/mcp"
	"github.com/mcihad/kentos-izci/internal/version"
)

func newMCPCmd(a *cliApp) *cobra.Command {
	var serverURL, org string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve izci's MCP tools over stdio for AI assistants",
		Long: `Serves the izci Model Context Protocol tools (search_issues, get_issue,
get_event, start_ai_analysis, …) over standard input and output, for AI
assistants that start MCP servers as local processes.

The access token (a personal access token, izp_…) is read from IZCI_TOKEN.
With --url (or IZCI_URL) the tools call that izci server over HTTPS; without
it they run in-process against the database configured for this machine.

Example for Claude Code:
  claude mcp add izci --env IZCI_TOKEN=izp_… -- izci mcp --url https://izci.example.com

A running server also serves the same tools over HTTP at /mcp.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token := strings.TrimSpace(os.Getenv("IZCI_TOKEN"))
			if token == "" {
				return errors.New("set IZCI_TOKEN to a personal access token (izp_…)")
			}
			if serverURL == "" {
				serverURL = os.Getenv("IZCI_URL")
			}
			if org == "" {
				org = os.Getenv("IZCI_ORG")
			}
			ctx := cmd.Context()
			var api *mcp.API
			if serverURL != "" {
				u, err := url.Parse(serverURL)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					return errors.New("--url must be the izci server address, e.g. https://izci.example.com")
				}
				if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
					return errors.New("--url must use https except for localhost: the access token would travel in clear text")
				}
				api = mcp.NewAPI(&http.Client{Timeout: 60 * time.Second}, serverURL)
			} else {
				database, err := a.openDB(ctx)
				if err != nil {
					return err
				}
				defer database.Close()
				h, err := app.APIHandler(a.cfg, a.log, database)
				if err != nil {
					return err
				}
				api = mcp.NewAPI(mcp.InProcess(h), "http://izci.internal")
			}
			srv := mcp.New(mcp.Options{API: api, Version: version.Version, Credentials: mcp.Credentials{Token: token, Org: org}})
			return srv.RunStdio(ctx)
		},
	}
	cmd.Flags().StringVar(&serverURL, "url", "", "izci server to call (env IZCI_URL); in-process against the local database when empty")
	cmd.Flags().StringVar(&org, "org", "", "default organization slug (env IZCI_ORG)")
	return cmd
}
