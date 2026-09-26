package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mcihad/kentos-izci/internal/db"
)

func newMigrateCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage database schema migrations",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Apply all pending migrations",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				database, err := a.openDB(cmd.Context())
				if err != nil {
					return err
				}
				defer database.Close()
				if _, err := database.CheckServer(cmd.Context(), a.cfg.IsProduction(), a.cfg.DB.AllowPrivilegedRole); err != nil {
					return err
				}
				return database.MigrateUp(cmd.Context())
			},
		},
		&cobra.Command{
			Use:   "down",
			Short: "Roll back the most recent migration",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				database, err := a.openDB(cmd.Context())
				if err != nil {
					return err
				}
				defer database.Close()
				return database.MigrateDown(cmd.Context())
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "List migrations and whether they are applied",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				database, err := a.openDB(cmd.Context())
				if err != nil {
					return err
				}
				defer database.Close()
				st, err := database.MigrateStatus(cmd.Context())
				if err != nil {
					return err
				}
				tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "VERSION\tSTATE\tFILE")
				for _, s := range st {
					state := "pending"
					if s.Applied {
						state = "applied"
					}
					fmt.Fprintf(tw, "%d\t%s\t%s\n", s.Version, state, s.File)
				}
				return tw.Flush()
			},
		},
	)
	return cmd
}

func newDBCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database administration helpers",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "bootstrap",
		Short: "Create the izci role and databases using a superuser connection (DB_ADMIN_URL)",
		Long: `Create (or repair) the dedicated izci login role and its databases.

The role and password come from DATABASE_URL; IZCI_TEST_DATABASE_URL, when set,
adds the integration-test database. DB_ADMIN_URL must be a superuser
connection. The role is created without SUPERUSER and BYPASSRLS so that
row-level security is enforced. The command is idempotent.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.load(); err != nil {
				return err
			}
			if err := a.cfg.RequireDatabase(); err != nil {
				return err
			}
			res, err := db.Bootstrap(cmd.Context(), a.cfg.DB.AdminURL, a.cfg.DB.URL, a.cfg.DB.TestURL)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if res.RoleCreated {
				fmt.Fprintf(out, "role %q created\n", res.Role)
			} else {
				fmt.Fprintf(out, "role %q updated (password reset, SUPERUSER/BYPASSRLS removed)\n", res.Role)
			}
			for _, name := range res.DatabasesCreated {
				fmt.Fprintf(out, "database %q created\n", name)
			}
			for _, name := range res.DatabasesExisted {
				fmt.Fprintf(out, "database %q already exists (owner ensured)\n", name)
			}
			return nil
		},
	})
	return cmd
}
