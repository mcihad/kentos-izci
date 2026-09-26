package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/testutil"
)

func TestOrganizationProjectAndKey(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()

	var (
		org     *store.Organization
		project *store.Project
		key     *store.APIKey
		token   string
	)
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		org, err = store.CreateOrganization(ctx, tx, "kentos", "KentOS")
		return err
	})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	err = database.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := store.CreateOrganization(ctx, tx, "kentos", "Duplicate")
		return err
	})
	if !store.UniqueViolation(err, "organizations_slug_key") {
		t.Errorf("duplicate slug must violate organizations_slug_key, got %v", err)
	}
	if org.Settings.GetTimezone() != "Europe/Istanbul" {
		t.Errorf("default timezone = %q", org.Settings.GetTimezone())
	}

	err = database.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		var err error
		if project, err = store.CreateProject(ctx, tx, org.ID, "kalem", "KentOS Kalem", "dotnet"); err != nil {
			return err
		}
		key, token, err = store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{
			OrgID: org.ID, ProjectID: project.ID, Name: "backend",
			Scopes: []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST, apiv1.ApiKeyScope_API_KEY_SCOPE_RELEASES},
		})
		return err
	})
	if err != nil {
		t.Fatalf("create project/key: %v", err)
	}
	if project.PublicID <= 0 {
		t.Errorf("public id must be assigned, got %d", project.PublicID)
	}
	if project.Settings.GetAi().GetAutoAnalyze() != apiv1.AutoAnalyze_AUTO_ANALYZE_OFF {
		t.Error("new projects must have AI automation off")
	}
	if key.Prefix != token[:10] || !key.HasScope(apiv1.ApiKeyScope_API_KEY_SCOPE_RELEASES) {
		t.Errorf("unexpected key %+v", key)
	}

	// Keys resolve in system scope (no tenant known yet) …
	var resolved *store.ResolvedKey
	err = database.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		resolved, err = store.ResolveAPIKey(ctx, tx, token)
		if err != nil {
			return err
		}
		if _, err := store.ResolveAPIKey(ctx, tx, token+"x"); err != store.ErrNotFound {
			t.Errorf("unknown key must be ErrNotFound, got %v", err)
		}
		if _, err := store.ResolveAPIKeyForProject(ctx, tx, token, project.PublicID+1); err != store.ErrNotFound {
			t.Errorf("key used with another project's DSN must be ErrNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Key.ProjectID != project.ID || resolved.ProjectSlug != "kalem" || resolved.OrgSlug != "kentos" || !resolved.Key.Usable() {
		t.Fatalf("unexpected resolution %+v", resolved)
	}

	// … but not from an unscoped query.
	if _, err := store.ResolveAPIKey(ctx, database.Pool, token); err != store.ErrNotFound {
		t.Fatalf("unscoped resolution must see nothing, got %v", err)
	}

	err = database.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		if err := store.RevokeAPIKey(ctx, tx, project.ID, key.ID); err != nil {
			return err
		}
		keys, err := store.ListAPIKeys(ctx, tx, project.ID, false)
		if err != nil {
			return err
		}
		if len(keys) != 0 {
			t.Errorf("revoked key must be hidden, got %d", len(keys))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
}
