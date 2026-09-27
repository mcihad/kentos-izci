package api

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/symbols"
)

// SymbolServer implements izci.api.v1.SymbolService.
type SymbolServer struct {
	*base
	apiv1connect.UnimplementedSymbolServiceHandler
	// symbols forgets cached lookups after deletions (optional in tests).
	symbols *symbols.Service
}

var _ apiv1connect.SymbolServiceHandler = (*SymbolServer)(nil)

// bundleFiles is how many files ListArtifactBundles returns per bundle.
const bundleFiles = 50

func (s *SymbolServer) project(ctx context.Context, tx pgx.Tx, t *auth.Tenant, slug string) (*store.Project, error) {
	p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), strings.ToLower(slug))
	if errors.Is(err, store.ErrNotFound) {
		return nil, errNotFound("project")
	}
	return p, err
}

// idCursor pages lists ordered by UUIDv7 ids, newest first.
func idCursor(token string) (*uuid.UUID, error) {
	if token == "" {
		return nil, nil
	}
	id, err := uuid.Parse(token)
	if err != nil {
		return nil, fieldError("page_token", "invalid page token")
	}
	return &id, nil
}

func listSize(n uint32) int {
	if n == 0 {
		return 20
	}
	return int(min(n, 100))
}

// ListArtifactBundles implements SymbolService.ListArtifactBundles.
func (s *SymbolServer) ListArtifactBundles(ctx context.Context, req *connect.Request[apiv1.ListArtifactBundlesRequest]) (*connect.Response[apiv1.ListArtifactBundlesResponse], error) {
	before, err := idCursor(req.Msg.GetPageToken())
	if err != nil {
		return nil, err
	}
	limit := listSize(req.Msg.GetPageSize())
	var out apiv1.ListArtifactBundlesResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		bundles, err := store.ListArtifactBundles(ctx, tx, p.ID, before, limit+1)
		if err != nil {
			return err
		}
		if len(bundles) > limit {
			bundles = bundles[:limit]
			out.NextPageToken = bundles[limit-1].ID.String()
		}
		ids := make([]uuid.UUID, len(bundles))
		byID := map[uuid.UUID]*apiv1.ArtifactBundle{}
		for i, b := range bundles {
			ids[i] = b.ID
			pb := &apiv1.ArtifactBundle{Id: b.ID.String(), Release: b.Release, Dist: b.Dist, FileCount: uint32(b.FileCount),
				SizeBytes: uint64(max(b.SizeBytes, 0)), CreatedAt: ts(b.CreatedAt), LastUsedAt: tsp(b.LastUsedAt)}
			byID[b.ID] = pb
			out.Bundles = append(out.Bundles, pb)
		}
		if len(ids) == 0 {
			return nil
		}
		files, err := store.ListArtifactFiles(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, f := range files {
			pb := byID[f.BundleID]
			if pb == nil || len(pb.Files) >= bundleFiles {
				continue
			}
			af := &apiv1.ArtifactFile{Url: f.URL, Kind: apiv1.ArtifactFileKind(f.Kind), SizeBytes: uint32(max(f.SizeBytes, 0))}
			if f.DebugID != nil {
				af.DebugId = f.DebugID.String()
			}
			pb.Files = append(pb.Files, af)
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list artifact bundles", err)
	}
	return connect.NewResponse(&out), nil
}

// DeleteArtifactBundle implements SymbolService.DeleteArtifactBundle.
func (s *SymbolServer) DeleteArtifactBundle(ctx context.Context, req *connect.Request[apiv1.DeleteArtifactBundleRequest]) (*connect.Response[apiv1.DeleteArtifactBundleResponse], error) {
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, fieldError("id", "must be a UUID")
	}
	var projectID uuid.UUID
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		projectID = p.ID
		b, err := store.DeleteArtifactBundle(ctx, tx, p.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound("bundle")
		}
		if err != nil {
			return err
		}
		name := b.Release
		if name == "" {
			name = b.Checksum[:12]
		}
		return s.audit(ctx, tx, "symbols.bundle.delete", "project", p.ID.String(), p.Name+": "+name,
			map[string]string{"release": b.Release, "files": strconv.Itoa(b.FileCount)})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "delete artifact bundle", err)
	}
	if s.symbols != nil {
		s.symbols.Invalidate(projectID)
	}
	return connect.NewResponse(&apiv1.DeleteArtifactBundleResponse{}), nil
}

var debugFileKinds = map[string]apiv1.DebugFileKind{
	"elf":                     apiv1.DebugFileKind_DEBUG_FILE_KIND_ELF,
	"macho":                   apiv1.DebugFileKind_DEBUG_FILE_KIND_MACHO,
	symbols.KindDartSymbolMap: apiv1.DebugFileKind_DEBUG_FILE_KIND_DART_SYMBOL_MAP,
}

// ListDebugFiles implements SymbolService.ListDebugFiles.
func (s *SymbolServer) ListDebugFiles(ctx context.Context, req *connect.Request[apiv1.ListDebugFilesRequest]) (*connect.Response[apiv1.ListDebugFilesResponse], error) {
	before, err := idCursor(req.Msg.GetPageToken())
	if err != nil {
		return nil, err
	}
	limit := listSize(req.Msg.GetPageSize())
	var out apiv1.ListDebugFilesResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		files, err := store.ListDebugFiles(ctx, tx, p.ID, before, limit+1)
		if err != nil {
			return err
		}
		if len(files) > limit {
			files = files[:limit]
			out.NextPageToken = files[limit-1].ID.String()
		}
		for _, f := range files {
			out.Files = append(out.Files, &apiv1.DebugFile{Id: f.ID.String(), DebugId: f.DebugID.String(), CodeId: f.CodeID,
				Kind: debugFileKinds[f.Kind], Name: f.Name, Arch: f.Arch, Features: f.Features, SizeBytes: uint64(max(f.SizeBytes, 0)),
				CreatedAt: ts(f.CreatedAt), LastUsedAt: tsp(f.LastUsedAt)})
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list debug files", err)
	}
	return connect.NewResponse(&out), nil
}

// DeleteDebugFile implements SymbolService.DeleteDebugFile.
func (s *SymbolServer) DeleteDebugFile(ctx context.Context, req *connect.Request[apiv1.DeleteDebugFileRequest]) (*connect.Response[apiv1.DeleteDebugFileResponse], error) {
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, fieldError("id", "must be a UUID")
	}
	var projectID uuid.UUID
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		projectID = p.ID
		f, err := store.DeleteDebugFile(ctx, tx, p.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound("debug file")
		}
		if err != nil {
			return err
		}
		return s.audit(ctx, tx, "symbols.debug_file.delete", "project", p.ID.String(), p.Name+": "+f.Name,
			map[string]string{"debug_id": f.DebugID.String(), "kind": f.Kind})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "delete debug file", err)
	}
	if s.symbols != nil {
		s.symbols.Invalidate(projectID)
	}
	return connect.NewResponse(&apiv1.DeleteDebugFileResponse{}), nil
}
