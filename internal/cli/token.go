package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/store"
)

var scopeNames = map[string]apiv1.TokenScope{
	"read": apiv1.TokenScope_TOKEN_SCOPE_READ, "write": apiv1.TokenScope_TOKEN_SCOPE_WRITE, "ai": apiv1.TokenScope_TOKEN_SCOPE_AI,
}

// parseExpiry reads lifetimes such as "90d", "12h" or "never".
func parseExpiry(s string) (*time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "never" || s == "0" {
		return nil, nil
	}
	var d time.Duration
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil || days <= 0 {
			return nil, fmt.Errorf("invalid expiry %q", s)
		}
		d = time.Duration(days) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil || d <= 0 {
			return nil, fmt.Errorf("invalid expiry %q (use e.g. 90d, 12h or never)", s)
		}
	}
	if d > 366*24*time.Hour {
		return nil, errors.New("tokens may live at most one year")
	}
	t := time.Now().Add(d)
	return &t, nil
}

func newTokenCmd(a *cliApp) *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage personal access tokens (API, CLI and MCP clients)"}

	var email, name, org, scopes, expires string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a personal access token for a user",
		Long: `Creates a personal access token (izp_…) that acts as the user, limited to the
given scopes and optionally to one organization. The token is printed alone on
standard output, so it can be redirected into a secret store:

  izci token create --user bot@kentos.local --name ci --org kentos --scopes read > token.txt`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" || name == "" {
				return errors.New("--user and --name are required")
			}
			var list []apiv1.TokenScope
			for _, sc := range strings.Split(scopes, ",") {
				v, ok := scopeNames[strings.TrimSpace(sc)]
				if !ok {
					return fmt.Errorf("unknown scope %q (use read, write, ai)", sc)
				}
				list = append(list, v)
			}
			exp, err := parseExpiry(expires)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			database, err := a.openDB(ctx)
			if err != nil {
				return err
			}
			defer database.Close()
			u, err := store.GetUserByEmail(ctx, database.Pool, strings.ToLower(email))
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("user %s not found", email)
			}
			if err != nil {
				return err
			}
			var orgID *uuid.UUID
			if org != "" {
				o, err := store.GetOrganizationBySlug(ctx, database.Pool, strings.ToLower(org))
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("organization %s not found", org)
				}
				if err != nil {
					return err
				}
				if _, err := store.MemberRole(ctx, database.Pool, o.ID, u.ID); err != nil && !u.Superadmin {
					return fmt.Errorf("%s is not a member of %s", email, org)
				}
				if limit := o.Settings.GetTokenMaxDays(); limit > 0 && (exp == nil || time.Until(*exp) > time.Duration(limit)*24*time.Hour) {
					return fmt.Errorf("%s accepts access tokens for at most %d days; use --expires %dd or less", org, limit, limit)
				}
				orgID = &o.ID
			}
			tok, secret, err := store.CreatePersonalToken(ctx, database.Pool, u.ID, orgID, name, list, exp)
			if err != nil {
				return err
			}
			until := "never expires"
			if exp != nil {
				until = "expires " + exp.Local().Format("2006-01-02")
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "token %s created for %s (%s, %s); it is shown only once:\n", tok.ID, u.Email, scopes, until)
			fmt.Fprintln(cmd.OutOrStdout(), secret)
			return nil
		},
	}
	create.Flags().StringVar(&email, "user", "", "email of the user the token acts as (required)")
	create.Flags().StringVar(&name, "name", "", "descriptive name, e.g. ci or claude-code (required)")
	create.Flags().StringVar(&org, "org", "", "limit the token to one organization")
	create.Flags().StringVar(&scopes, "scopes", "read", "comma-separated scopes: read, write, ai")
	create.Flags().StringVar(&expires, "expires", "90d", "lifetime such as 90d or 12h, or never")

	var listUser string
	list := &cobra.Command{
		Use:   "list",
		Short: "List a user's tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			database, err := a.openDB(ctx)
			if err != nil {
				return err
			}
			defer database.Close()
			u, err := store.GetUserByEmail(ctx, database.Pool, strings.ToLower(listUser))
			if err != nil {
				return fmt.Errorf("user %s: %w", listUser, err)
			}
			tokens, err := store.ListPersonalTokens(ctx, database.Pool, u.ID)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tPREFIX\tSCOPES\tLAST USED\tEXPIRES\tSTATE")
			for _, t := range tokens {
				var sc []string
				for _, s := range t.Scopes {
					sc = append(sc, strings.ToLower(strings.TrimPrefix(s.String(), "TOKEN_SCOPE_")))
				}
				state := "active"
				switch {
				case t.RevokedAt != nil:
					state = "revoked"
				case t.ExpiresAt != nil && t.ExpiresAt.Before(time.Now()):
					state = "expired"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", t.ID, t.Name, t.Prefix, strings.Join(sc, ","), day(t.LastUsedAt), day(t.ExpiresAt), state)
			}
			return tw.Flush()
		},
	}
	list.Flags().StringVar(&listUser, "user", "", "email of the user (required)")
	_ = list.MarkFlagRequired("user")

	var revokeUser string
	revoke := &cobra.Command{
		Use:   "revoke ID",
		Short: "Revoke a token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := uuid.Parse(args[0])
			if err != nil {
				return errors.New("give the token id shown by izci token list")
			}
			ctx := cmd.Context()
			database, err := a.openDB(ctx)
			if err != nil {
				return err
			}
			defer database.Close()
			u, err := store.GetUserByEmail(ctx, database.Pool, strings.ToLower(revokeUser))
			if err != nil {
				return fmt.Errorf("user %s: %w", revokeUser, err)
			}
			if err := store.RevokePersonalToken(ctx, database.Pool, u.ID, id); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "token %s revoked\n", id)
			return nil
		},
	}
	revoke.Flags().StringVar(&revokeUser, "user", "", "email of the token's user (required)")
	_ = revoke.MarkFlagRequired("user")

	cmd.AddCommand(create, list, revoke)
	return cmd
}

func day(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Local().Format("2006-01-02")
}
