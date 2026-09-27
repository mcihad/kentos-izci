package api_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/store"
)

func TestSymbols(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	org, err := owner.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	orgID := uuid.MustParse(org.Msg.GetOrganization().GetId())
	debugID := uuid.New()
	// Uploads go through the Sentry-compatible endpoints (internal/sentryapi);
	// here the rows are stored directly.
	err = e.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		p, err := store.GetProjectBySlug(ctx, tx, orgID, "kalem")
		if err != nil {
			return err
		}
		for i, rel := range []string{"kalem-web@1.0.0", "kalem-web@1.1.0"} {
			b := &store.ArtifactBundle{OrgID: orgID, ProjectID: p.ID, Checksum: string(rune('a' + i)), Release: rel, FileCount: 2, SizeBytes: 300}
			if _, err := store.InsertArtifactBundle(ctx, tx, b, []store.ArtifactFile{
				{URL: "~/assets/app.js", Kind: store.ArtifactMinified, Data: []byte("x")},
				{URL: "~/assets/app.js.map", Kind: store.ArtifactSourceMap, DebugID: &debugID, Data: []byte("{}")},
			}); err != nil {
				return err
			}
		}
		_, _, err = store.InsertDebugFile(ctx, tx, &store.DebugFile{OrgID: orgID, ProjectID: p.ID, DebugID: debugID, Kind: "elf",
			Name: "app.android-arm64.symbols", Arch: "arm64", Features: []string{"debug"}, Checksum: "c1", Data: []byte("elf")})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	page, err := owner.symbol.ListArtifactBundles(ctx, req(&apiv1.ListArtifactBundlesRequest{Project: "kalem", PageSize: 1}))
	if err != nil {
		t.Fatal(err)
	}
	first := page.Msg.GetBundles()
	if len(first) != 1 || first[0].GetRelease() != "kalem-web@1.1.0" || len(first[0].GetFiles()) != 2 || page.Msg.GetNextPageToken() == "" {
		t.Fatalf("page 1: %v", page.Msg)
	}
	if first[0].GetFiles()[0].GetKind() != apiv1.ArtifactFileKind_ARTIFACT_FILE_KIND_MINIFIED_SOURCE ||
		first[0].GetFiles()[1].GetDebugId() != debugID.String() {
		t.Fatalf("files: %v", first[0].GetFiles())
	}
	page, err = owner.symbol.ListArtifactBundles(ctx, req(&apiv1.ListArtifactBundlesRequest{Project: "kalem", PageSize: 1,
		PageToken: page.Msg.GetNextPageToken()}))
	if err != nil || len(page.Msg.GetBundles()) != 1 || page.Msg.GetBundles()[0].GetRelease() != "kalem-web@1.0.0" || page.Msg.GetNextPageToken() != "" {
		t.Fatalf("page 2: %v %v", page, err)
	}
	files, err := owner.symbol.ListDebugFiles(ctx, req(&apiv1.ListDebugFilesRequest{Project: "kalem"}))
	if err != nil || len(files.Msg.GetFiles()) != 1 || files.Msg.GetFiles()[0].GetKind() != apiv1.DebugFileKind_DEBUG_FILE_KIND_ELF ||
		files.Msg.GetFiles()[0].GetDebugId() != debugID.String() {
		t.Fatalf("debug files: %v %v", files, err)
	}

	// Viewers read; only admins delete.
	viewer := joinAs(t, e, owner, "izleyici@example.com", apiv1.Role_ROLE_VIEWER)
	if _, err := viewer.symbol.ListDebugFiles(ctx, req(&apiv1.ListDebugFilesRequest{Project: "kalem"})); err != nil {
		t.Fatalf("viewer list: %v", err)
	}
	if _, err := viewer.symbol.DeleteDebugFile(ctx, req(&apiv1.DeleteDebugFileRequest{Project: "kalem",
		Id: files.Msg.GetFiles()[0].GetId()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("viewer delete: %v", err)
	}
	if _, err := owner.symbol.DeleteDebugFile(ctx, req(&apiv1.DeleteDebugFileRequest{Project: "kalem",
		Id: files.Msg.GetFiles()[0].GetId()})); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.symbol.DeleteArtifactBundle(ctx, req(&apiv1.DeleteArtifactBundleRequest{Project: "kalem",
		Id: first[0].GetId()})); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.symbol.DeleteArtifactBundle(ctx, req(&apiv1.DeleteArtifactBundleRequest{Project: "kalem",
		Id: first[0].GetId()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("second delete: %v", err)
	}
	left, _ := owner.symbol.ListArtifactBundles(ctx, req(&apiv1.ListArtifactBundlesRequest{Project: "kalem"}))
	if len(left.Msg.GetBundles()) != 1 {
		t.Fatalf("after delete: %v", left.Msg)
	}
	var files2 int
	_ = e.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM artifact_files`).Scan(&files2)
	})
	if files2 != 2 {
		t.Fatalf("files of the deleted bundle must go with it: %d left", files2)
	}
	log, _ := owner.org.ListAuditLog(ctx, req(&apiv1.ListAuditLogRequest{}))
	actions := map[string]bool{}
	for _, entry := range log.Msg.GetEntries() {
		actions[entry.GetAction()] = true
	}
	if !actions["symbols.bundle.delete"] || !actions["symbols.debug_file.delete"] {
		t.Fatalf("audit: %v", actions)
	}
	if _, err := owner.symbol.ListDebugFiles(ctx, req(&apiv1.ListDebugFilesRequest{Project: "yok"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("unknown project: %v", err)
	}
}
