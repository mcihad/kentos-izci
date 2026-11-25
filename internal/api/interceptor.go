package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/clientip"
	"github.com/mcihad/kentos-izci/internal/config"
)

// Interceptor authenticates, authorizes (see rules) and validates every
// izci.api.v1 call.
type Interceptor struct {
	auth      *auth.Service
	log       *slog.Logger
	validator protovalidate.Validator
	trusted   config.PrefixList
}

// NewInterceptor creates the interceptor.
func NewInterceptor(a *auth.Service, log *slog.Logger, trusted config.PrefixList) (*Interceptor, error) {
	v, err := protovalidate.New()
	if err != nil {
		return nil, err
	}
	return &Interceptor{auth: a, log: log, validator: v, trusted: trusted}, nil
}

// requestInfo carries transport details handlers need (sessions, audit).
type requestInfo struct {
	IP        netip.Addr
	UserAgent string
}

type requestInfoKey struct{}

func infoFrom(ctx context.Context) requestInfo {
	ri, _ := ctx.Value(requestInfoKey{}).(requestInfo)
	return ri
}

// WrapUnary implements connect.Interceptor.
func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, err := i.authorize(ctx, req.Spec().Procedure, req.Header(), req.Peer().Addr)
		if err != nil {
			return nil, err
		}
		if msg, ok := req.Any().(proto.Message); ok {
			if err := i.validate(msg); err != nil {
				return nil, err
			}
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient implements connect.Interceptor (unused on servers).
func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler implements connect.Interceptor.
func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := i.authorize(ctx, conn.Spec().Procedure, conn.RequestHeader(), conn.Peer().Addr)
		if err != nil {
			return err
		}
		return next(ctx, &validatingConn{StreamingHandlerConn: conn, i: i})
	}
}

type validatingConn struct {
	connect.StreamingHandlerConn
	i *Interceptor
}

func (c *validatingConn) Receive(msg any) error {
	if err := c.StreamingHandlerConn.Receive(msg); err != nil {
		return err
	}
	if m, ok := msg.(proto.Message); ok {
		return c.i.validate(m)
	}
	return nil
}

func (i *Interceptor) validate(msg proto.Message) error {
	err := i.validator.Validate(msg)
	if err == nil {
		return nil
	}
	var verr *protovalidate.ValidationError
	if errors.As(err, &verr) && len(verr.Violations) > 0 {
		v := verr.Violations[0]
		field := protovalidate.FieldPathString(v.Proto.GetField())
		cerr := fieldError(field, field+": "+v.Proto.GetMessage())
		return cerr
	}
	return connect.NewError(connect.CodeInvalidArgument, err)
}

func (i *Interceptor) authorize(ctx context.Context, procedure string, h http.Header, peer string) (context.Context, error) {
	r, ok := rules[procedure]
	if !ok {
		return ctx, errPermission("procedure is not authorized")
	}
	ip := clientip.Resolve(peer, h, i.trusted)
	ctx = context.WithValue(ctx, requestInfoKey{}, requestInfo{IP: ip, UserAgent: h.Get("User-Agent")})
	ctx = auth.WithClientIP(ctx, ip)
	p, err := i.auth.Authenticate(ctx, h)
	if err != nil {
		return ctx, internal(ctx, i.log, "authenticate", err)
	}
	if p != nil {
		ctx = auth.WithPrincipal(ctx, p)
	}
	if r.public || (p == nil && r.optional) {
		return ctx, nil
	}
	if p == nil {
		return ctx, errUnauthenticated()
	}
	if r.scope != apiv1.TokenScope_TOKEN_SCOPE_UNSPECIFIED && !p.HasScope(r.scope) {
		return ctx, errPermission("access token lacks the " + strings.TrimPrefix(r.scope.String(), "TOKEN_SCOPE_") + " scope")
	}
	if r.superadmin && !p.User.Superadmin {
		return ctx, errPermission("instance administrators only")
	}
	if r.superadmin && i.auth.SuperadminNeedsTwoFactor(p.User) {
		return ctx, errSuperadminTwoFactor()
	}
	if !r.org {
		return ctx, nil
	}
	var t *auth.Tenant
	if slug := h.Get("Izci-Org"); slug != "" {
		t, err = i.auth.ResolveTenant(ctx, p, slug)
	} else {
		t, err = i.auth.TokenOrg(ctx, p)
		if t == nil && err == nil {
			return ctx, apiError(connect.CodePermissionDenied, apiv1.ErrorReason_ERROR_REASON_ORGANIZATION_REQUIRED, "Izci-Org header required")
		}
	}
	if err != nil {
		return ctx, tenantError(ctx, i.log, err)
	}
	if t.Role < r.role {
		return ctx, errPermission("requires the " + strings.ToLower(strings.TrimPrefix(r.role.String(), "ROLE_")) + " role")
	}
	return auth.WithTenant(ctx, t), nil
}
