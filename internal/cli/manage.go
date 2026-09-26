package cli

import (
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ingest/sentry"
	"github.com/mcihad/kentos-izci/internal/slug"
	"github.com/mcihad/kentos-izci/internal/store"
)

func newOrgCmd(a *cliApp) *cobra.Command {
	cmd := &cobra.Command{Use: "org", Short: "Manage organizations (tenants)"}

	var slugFlag, name string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create an organization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return errors.New("--name is required")
			}
			if slugFlag == "" {
				slugFlag = slug.Make(name, 50)
			}
			if !slug.Valid(slugFlag) {
				return fmt.Errorf("invalid slug %q: use 2–50 lowercase letters, digits or hyphens, starting with a letter", slugFlag)
			}
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			var org *store.Organization
			err = database.InSystem(cmd.Context(), func(tx pgx.Tx) error {
				org, err = store.CreateOrganization(cmd.Context(), tx, slugFlag, name)
				return err
			})
			if store.UniqueViolation(err, "organizations_slug_key") {
				return fmt.Errorf("organization %q already exists", slugFlag)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "organization %q created (id %s)\n", org.Slug, org.ID)
			return nil
		},
	}
	create.Flags().StringVar(&name, "name", "", "display name (required)")
	create.Flags().StringVar(&slugFlag, "slug", "", "URL-safe identifier (default: derived from the name)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List organizations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			rows, err := database.Pool.Query(cmd.Context(), `SELECT slug, name, plan, created_at FROM organizations ORDER BY slug`)
			if err != nil {
				return err
			}
			defer rows.Close()
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "SLUG\tNAME\tPLAN\tCREATED")
			for rows.Next() {
				var s, n, p string
				var at any
				if err := rows.Scan(&s, &n, &p, &at); err != nil {
					return err
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", s, n, p, at)
			}
			return tw.Flush()
		},
	}
	cmd.AddCommand(create, list)
	return cmd
}

func newProjectCmd(a *cliApp) *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Manage projects"}
	var orgSlug, slugFlag, name, platform string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a project and its first API key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if orgSlug == "" || name == "" {
				return errors.New("--org and --name are required")
			}
			if slugFlag == "" {
				slugFlag = slug.Make(name, 50)
			}
			if !slug.Valid(slugFlag) {
				return fmt.Errorf("invalid slug %q", slugFlag)
			}
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			ctx := cmd.Context()
			var org *store.Organization
			if err := database.InSystem(ctx, func(tx pgx.Tx) error {
				org, err = store.GetOrganizationBySlug(ctx, tx, orgSlug)
				return err
			}); err != nil {
				return fmt.Errorf("organization %q: %w", orgSlug, err)
			}
			var project *store.Project
			var token string
			err = database.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
				if project, err = store.CreateProject(ctx, tx, org.ID, slugFlag, name, strings.ToLower(platform)); err != nil {
					return err
				}
				_, token, err = store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{
					OrgID: org.ID, ProjectID: project.ID, Name: "default",
					Scopes: []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST, apiv1.ApiKeyScope_API_KEY_SCOPE_RELEASES},
				})
				return err
			})
			if store.UniqueViolation(err, "projects_org_slug_key") {
				return fmt.Errorf("project %q already exists in %q", slugFlag, orgSlug)
			}
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "project %q created (id %s, sentry id %d)\n", project.Slug, project.ID, project.PublicID)
			fmt.Fprintf(out, "API key (shown once): %s\n", token)
			fmt.Fprintf(out, "Sentry DSN: %s\n", sentry.DSN(a.cfg.PublicURL, token, project.PublicID))
			return nil
		},
	}
	create.Flags().StringVar(&orgSlug, "org", "", "organization slug (required)")
	create.Flags().StringVar(&name, "name", "", "display name (required)")
	create.Flags().StringVar(&slugFlag, "slug", "", "URL-safe identifier (default: derived from the name)")
	create.Flags().StringVar(&platform, "platform", "", "main platform, e.g. dotnet, go, javascript")
	cmd.AddCommand(create)
	return cmd
}

func newKeyCmd(a *cliApp) *cobra.Command {
	cmd := &cobra.Command{Use: "key", Short: "Manage project API keys"}
	var orgSlug, projectSlug, name string
	var origins []string
	var rate int32
	var releases bool
	create := &cobra.Command{
		Use:   "create",
		Short: "Create an ingest API key for a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if orgSlug == "" || projectSlug == "" {
				return errors.New("--org and --project are required")
			}
			database, err := a.openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer database.Close()
			ctx := cmd.Context()
			var org *store.Organization
			if err := database.InSystem(ctx, func(tx pgx.Tx) error {
				org, err = store.GetOrganizationBySlug(ctx, tx, orgSlug)
				return err
			}); err != nil {
				return fmt.Errorf("organization %q: %w", orgSlug, err)
			}
			scopes := []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST}
			if releases {
				scopes = append(scopes, apiv1.ApiKeyScope_API_KEY_SCOPE_RELEASES)
			}
			var token string
			var project *store.Project
			err = database.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
				if project, err = store.GetProjectBySlug(ctx, tx, org.ID, projectSlug); err != nil {
					return fmt.Errorf("project %q: %w", projectSlug, err)
				}
				_, token, err = store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{
					OrgID: org.ID, ProjectID: project.ID, Name: name, Scopes: scopes,
					AllowedOrigins: origins, RateLimitPerMinute: rate,
				})
				return err
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "API key (shown once): %s\nSentry DSN: %s\n", token, sentry.DSN(a.cfg.PublicURL, token, project.PublicID))
			return nil
		},
	}
	create.Flags().StringVar(&orgSlug, "org", "", "organization slug (required)")
	create.Flags().StringVar(&projectSlug, "project", "", "project slug (required)")
	create.Flags().StringVar(&name, "name", "cli", "descriptive name")
	create.Flags().StringSliceVar(&origins, "origin", nil, "allowed browser origin (repeatable), e.g. https://app.example.com")
	create.Flags().Int32Var(&rate, "rate-limit", 0, "events per minute for this key (0 = project limit)")
	create.Flags().BoolVar(&releases, "releases", true, "also allow registering releases")
	cmd.AddCommand(create)
	return cmd
}
