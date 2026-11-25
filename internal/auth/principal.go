package auth

import (
	"context"
	"net/netip"
	"slices"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/store"
)

// Principal is the authenticated caller of an API request: a browser
// session or a personal access token.
type Principal struct {
	User    *store.User
	Session *store.Session       // set for cookie sessions
	Token   *store.PersonalToken // set for personal access tokens
}

type clientIPKey struct{}

// WithClientIP records the client address of a request, for the last use
// of access tokens.
func WithClientIP(ctx context.Context, ip netip.Addr) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

func clientIP(ctx context.Context) netip.Addr {
	ip, _ := ctx.Value(clientIPKey{}).(netip.Addr)
	return ip
}

// HasScope reports whether a token grants scope (sessions grant all).
func (p *Principal) HasScope(scope apiv1.TokenScope) bool {
	if p.Token == nil {
		return true
	}
	return slices.Contains(p.Token.Scopes, scope)
}

// Tenant is the organization a request operates in and the caller's role.
type Tenant struct {
	Org  *store.Organization
	Role apiv1.Role
}

// OrgID returns the organization id.
func (t *Tenant) OrgID() uuid.UUID { return t.Org.ID }

type principalKey struct{}
type tenantKey struct{}

// WithPrincipal stores the principal in ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal of ctx, or nil.
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// WithTenant stores the tenant in ctx.
func WithTenant(ctx context.Context, t *Tenant) context.Context {
	return context.WithValue(ctx, tenantKey{}, t)
}

// TenantFrom returns the tenant of ctx, or nil.
func TenantFrom(ctx context.Context) *Tenant {
	t, _ := ctx.Value(tenantKey{}).(*Tenant)
	return t
}

// RoleFromName maps "viewer", "member", "admin", "owner".
func RoleFromName(name string) apiv1.Role {
	switch name {
	case "viewer":
		return apiv1.Role_ROLE_VIEWER
	case "member":
		return apiv1.Role_ROLE_MEMBER
	case "admin":
		return apiv1.Role_ROLE_ADMIN
	case "owner":
		return apiv1.Role_ROLE_OWNER
	}
	return apiv1.Role_ROLE_UNSPECIFIED
}
