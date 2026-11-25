package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/slug"
	"github.com/mcihad/kentos-izci/internal/store"
)

// readPassword takes the password from a flag, IZCI_PASSWORD or stdin.
func readPassword(cmd *cobra.Command, flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if v := os.Getenv("IZCI_PASSWORD"); v != "" {
		return v, nil
	}
	fmt.Fprint(cmd.ErrOrStderr(), "password: ")
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no password given (use --password, IZCI_PASSWORD or stdin)")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func newSetupCmd(a *cliApp) *cobra.Command {
	var email, name, password, orgName, orgSlug, projectName, platform string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create the first administrator, organization and project (non-interactive first-run setup)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" || orgName == "" {
				return errors.New("--email and --org-name are required")
			}
			pw, err := readPassword(cmd, password)
			if err != nil {
				return err
			}
			if err := auth.ValidatePassword(pw); err != nil {
				return err
			}
			hash, err := auth.HashPassword(pw)
			if err != nil {
				return err
			}
			if orgSlug == "" {
				orgSlug = slug.Make(orgName, 50)
			}
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			ctx := cmd.Context()
			if err := database.MigrateUp(ctx); err != nil {
				return err
			}
			var token string
			err = database.InSystem(ctx, func(tx pgx.Tx) error {
				if n, err := store.CountUsers(ctx, tx); err != nil {
					return err
				} else if n > 0 {
					return errors.New("setup already completed: users exist (use `izci user create`)")
				}
				if name == "" {
					name = strings.Split(email, "@")[0]
				}
				u, err := store.CreateUser(ctx, tx, store.NewUserInput{Email: strings.ToLower(email), Name: name, PasswordHash: &hash, Superadmin: true})
				if err != nil {
					return err
				}
				org, err := store.CreateOrganization(ctx, tx, orgSlug, orgName)
				if err != nil {
					return err
				}
				if err := store.AddMember(ctx, tx, org.ID, u.ID, apiv1.Role_ROLE_OWNER); err != nil {
					return err
				}
				if projectName == "" {
					return nil
				}
				p, err := store.CreateProject(ctx, tx, org.ID, slug.Make(projectName, 50), projectName, platform)
				if err != nil {
					return err
				}
				_, token, err = store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{OrgID: org.ID, ProjectID: p.ID, Name: "default",
					Scopes: []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST}, CreatedBy: &u.ID})
				return err
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "administrator %s and organization %q created\n", email, orgSlug)
			if token != "" {
				fmt.Fprintf(out, "project API key (shown once): %s\n", token)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "administrator email (required)")
	cmd.Flags().StringVar(&name, "name", "", "administrator display name")
	cmd.Flags().StringVar(&password, "password", "", "administrator password (or IZCI_PASSWORD / stdin)")
	cmd.Flags().StringVar(&orgName, "org-name", "", "organization name (required)")
	cmd.Flags().StringVar(&orgSlug, "org", "", "organization slug (default: derived from the name)")
	cmd.Flags().StringVar(&projectName, "project", "", "first project name (optional)")
	cmd.Flags().StringVar(&platform, "platform", "", "first project platform, e.g. dotnet")
	return cmd
}

func newUserCmd(a *cliApp) *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage user accounts"}

	var email, name, password, orgSlug, role string
	var superadmin bool
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a user (optionally adding them to an organization)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" {
				return errors.New("--email is required")
			}
			pw, err := readPassword(cmd, password)
			if err != nil {
				return err
			}
			if err := auth.ValidatePassword(pw); err != nil {
				return err
			}
			hash, _ := auth.HashPassword(pw)
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			ctx := cmd.Context()
			err = database.InSystem(ctx, func(tx pgx.Tx) error {
				if name == "" {
					name = strings.Split(email, "@")[0]
				}
				u, err := store.CreateUser(ctx, tx, store.NewUserInput{Email: strings.ToLower(email), Name: name, PasswordHash: &hash, Superadmin: superadmin})
				if err != nil {
					return err
				}
				if orgSlug == "" {
					return nil
				}
				org, err := store.GetOrganizationBySlug(ctx, tx, orgSlug)
				if err != nil {
					return fmt.Errorf("organization %q: %w", orgSlug, err)
				}
				r := auth.RoleFromName(role)
				if r == apiv1.Role_ROLE_UNSPECIFIED {
					return fmt.Errorf("invalid role %q (viewer, member, admin, owner)", role)
				}
				return store.AddMember(ctx, tx, org.ID, u.ID, r)
			})
			if store.UniqueViolation(err, "users_email_key") {
				return fmt.Errorf("a user with email %s already exists", email)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "user %s created\n", email)
			return nil
		},
	}
	create.Flags().StringVar(&email, "email", "", "email (required)")
	create.Flags().StringVar(&name, "name", "", "display name")
	create.Flags().StringVar(&password, "password", "", "password (or IZCI_PASSWORD / stdin)")
	create.Flags().StringVar(&orgSlug, "org", "", "add to this organization")
	create.Flags().StringVar(&role, "role", "member", "role in --org: viewer, member, admin or owner")
	create.Flags().BoolVar(&superadmin, "superadmin", false, "make the user an instance administrator")

	list := &cobra.Command{
		Use:   "list",
		Short: "List users",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			users, err := store.ListUsers(cmd.Context(), database.Pool, "", 1000, "")
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "EMAIL\tNAME\tSUPERADMIN\tDISABLED\t2FA\tLAST LOGIN")
			for _, u := range users {
				last := "-"
				if u.LastLoginAt != nil {
					last = u.LastLoginAt.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(tw, "%s\t%s\t%v\t%v\t%v\t%s\n", u.Email, u.Name, u.Superadmin, u.Disabled, u.TwoFactor(), last)
			}
			return tw.Flush()
		},
	}

	var resetPassword string
	reset := &cobra.Command{
		Use:   "reset-password EMAIL",
		Short: "Set a new password and end the user's sessions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := readPassword(cmd, resetPassword)
			if err != nil {
				return err
			}
			if err := auth.ValidatePassword(pw); err != nil {
				return err
			}
			hash, _ := auth.HashPassword(pw)
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			u, err := store.GetUserByEmail(cmd.Context(), database.Pool, args[0])
			if err != nil {
				return fmt.Errorf("user %s: %w", args[0], err)
			}
			if err := store.UpdateUserPassword(cmd.Context(), database.Pool, u.ID, hash); err != nil {
				return err
			}
			if _, err := database.Pool.Exec(cmd.Context(), `DELETE FROM sessions WHERE user_id = $1`, u.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "password of %s changed; sessions ended\n", u.Email)
			return nil
		},
	}
	reset.Flags().StringVar(&resetPassword, "password", "", "new password (or IZCI_PASSWORD / stdin)")

	reset2FA := &cobra.Command{
		Use:   "reset-2fa EMAIL",
		Short: "Turn off two-factor authentication for a user who lost their authenticator and recovery codes",
		Long: `Turn off two-factor authentication and delete the recovery codes of a user
who lost both. They sign in with their password alone and can set it up
again; organizations requiring two-factor authentication turn them away
until they do.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			u, err := store.GetUserByEmail(cmd.Context(), database.Pool, args[0])
			if err != nil {
				return fmt.Errorf("user %s: %w", args[0], err)
			}
			if !u.TwoFactor() {
				fmt.Fprintf(cmd.OutOrStdout(), "two-factor authentication of %s is already off\n", u.Email)
				return nil
			}
			if err := store.DisableTwoFactor(cmd.Context(), database.Pool, u.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "two-factor authentication of %s turned off\n", u.Email)
			return nil
		},
	}

	var revoke bool
	admin := &cobra.Command{
		Use:   "superadmin EMAIL",
		Short: "Grant (or with --revoke, remove) instance administrator rights",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			u, err := store.GetUserByEmail(cmd.Context(), database.Pool, args[0])
			if err != nil {
				return fmt.Errorf("user %s: %w", args[0], err)
			}
			v := !revoke
			if _, err := store.UpdateUserFlags(cmd.Context(), database.Pool, u.ID, &v, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "superadmin=%v for %s\n", v, u.Email)
			return nil
		},
	}
	admin.Flags().BoolVar(&revoke, "revoke", false, "remove instead of grant")

	cmd.AddCommand(create, list, reset, reset2FA, admin)
	return cmd
}
