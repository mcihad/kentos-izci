// Package cli implements the `izci` command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/logging"
)

// app carries state shared by all commands.
type cliApp struct {
	envFile string
	cfg     *config.Config
	log     *slog.Logger
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	a := &cliApp{}
	root := &cobra.Command{
		Use:   "izci",
		Short: "izci — error tracking and aggregation for KentOS applications",
		Long: `izci collects errors from applications over gRPC/Connect and the Sentry
protocol, groups them by signature, notifies developers on Telegram and can
analyze issues with AI (opencode) to open fix branches or pull requests.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&a.envFile, "env-file", config.LookupEnvFile(),
		"path to a .env file (default: ./.env when present; env: IZCI_ENV_FILE)")

	root.AddCommand(
		newServeCmd(a),
		newMigrateCmd(a),
		newDBCmd(a),
		newOrgCmd(a),
		newProjectCmd(a),
		newKeyCmd(a),
		newEventCmd(),
		newBenchCmd(),
		newVersionCmd(),
	)
	return root
}

// load reads configuration and initializes logging once.
func (a *cliApp) load() error {
	if a.cfg != nil {
		return nil
	}
	cfg, err := config.Load(a.envFile)
	if err != nil {
		return err
	}
	a.cfg = cfg
	a.log = logging.New(cfg.Log)
	return nil
}

// openDB loads config and connects to the database.
func (a *cliApp) openDB(ctx context.Context) (*db.DB, error) {
	if err := a.load(); err != nil {
		return nil, err
	}
	if err := a.cfg.RequireDatabase(); err != nil {
		return nil, err
	}
	return db.Open(ctx, a.cfg.DB, a.log)
}
