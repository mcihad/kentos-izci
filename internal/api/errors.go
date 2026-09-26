// Package api implements the izci.api.v1 Connect services used by the web
// UI, the CLI and MCP clients. Handlers are thin: they authorize (through
// the interceptor), open the tenant-scoped transaction, call the store and
// convert results to API messages.
package api

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/store"
)

// apiError builds a Connect error carrying an ErrorDetail. The message is
// English; clients localize by reason.
func apiError(code connect.Code, reason apiv1.ErrorReason, msg string, meta ...string) *connect.Error {
	err := connect.NewError(code, errors.New(msg))
	d := &apiv1.ErrorDetail{Reason: reason}
	if len(meta) > 0 {
		d.Metadata = map[string]string{}
		for i := 0; i+1 < len(meta); i += 2 {
			d.Metadata[meta[i]] = meta[i+1]
		}
	}
	if detail, derr := connect.NewErrorDetail(d); derr == nil {
		err.AddDetail(detail)
	}
	return err
}

// fieldError reports an invalid request field.
func fieldError(field, msg string) *connect.Error {
	err := connect.NewError(connect.CodeInvalidArgument, errors.New(msg))
	if detail, derr := connect.NewErrorDetail(&apiv1.ErrorDetail{Reason: apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT, Field: field}); derr == nil {
		err.AddDetail(detail)
	}
	return err
}

var (
	errUnauthenticated = func() error {
		return apiError(connect.CodeUnauthenticated, apiv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED, "sign in required")
	}
	errPermission = func(msg string) error {
		return apiError(connect.CodePermissionDenied, apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED, msg)
	}
	errNotFound = func(what string) error {
		return apiError(connect.CodeNotFound, apiv1.ErrorReason_ERROR_REASON_NOT_FOUND, what+" not found")
	}
)

// internal logs an unexpected error and hides its details from the client.
func internal(ctx context.Context, log *slog.Logger, op string, err error) error {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr
	}
	if errors.Is(err, store.ErrNotFound) {
		return errNotFound("resource")
	}
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, err)
	}
	log.ErrorContext(ctx, "api error", slog.String("op", op), slog.Any("err", err))
	return apiError(connect.CodeInternal, apiv1.ErrorReason_ERROR_REASON_INTERNAL, "internal error")
}
