package cli

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/mcihad/kentos-izci/internal/server"
	"github.com/mcihad/kentos-izci/internal/version"
	"github.com/mcihad/kentos-izci/web"
)

func newServeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the izci server (ingest, API, UI, MCP) on a single port",
		Long: `Run the izci server. One listener serves native gRPC (h2c or TLS),
gRPC-Web, Connect, the Sentry-compatible ingest endpoints, the MCP endpoint
and the embedded web UI.

Pending database migrations are applied on startup unless DB_AUTO_MIGRATE=false.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := a.load(); err != nil {
				return err
			}
			info := version.Get()
			a.log.Info("starting izci",
				slog.String("version", info.Version),
				slog.String("commit", info.ShortCommit()),
				slog.String("env", string(a.cfg.Env)))
			if a.cfg.SecretGenerated() {
				a.log.Warn("IZCI_SECRET_KEY is not set: using an ephemeral key, sessions and encrypted secrets will not survive a restart")
			}

			database, err := a.openDB(ctx)
			if err != nil {
				return err
			}
			defer database.Close()

			srvInfo, err := database.CheckServer(ctx, a.cfg.IsProduction(), a.cfg.DB.AllowPrivilegedRole)
			if err != nil {
				return err
			}
			a.log.Info("database connected",
				slog.String("server", srvInfo.Version),
				slog.String("database", srvInfo.Database),
				slog.String("role", srvInfo.Role))

			if a.cfg.DB.AutoMigrate {
				if err := database.MigrateUp(ctx); err != nil {
					return err
				}
			}

			srv := server.New(server.Options{
				Config: a.cfg,
				Logger: a.log,
				DB:     database,
				UI:     web.FS(),
			})
			return srv.Run(ctx)
		},
	}
}
