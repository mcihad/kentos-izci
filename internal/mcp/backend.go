// Package mcp serves izci to AI assistants over the Model Context Protocol:
// tools to search and triage issues, read events, check customers and run AI
// analyses, resources for issues and events, and prompts for common
// investigations.
//
// Every tool calls izci.api.v1 with the caller's personal access token —
// in-process for the /mcp endpoint and `izci mcp`, or over the network for
// `izci mcp --url` — so authorization, tenant isolation, token scopes and
// validation are exactly those of the API.
package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
)

// Credentials authenticate the API calls made for one MCP client.
type Credentials struct {
	// Token is a personal access token ("izp_…").
	Token string
	// Org is the organization slug; empty uses the organization the token is
	// bound to, or the user's only organization.
	Org string
}

type credentialsKey struct{}

func withCredentials(ctx context.Context, c Credentials) context.Context {
	return context.WithValue(ctx, credentialsKey{}, c)
}

// API is the part of izci.api.v1 the MCP server uses.
type API struct {
	auth      apiv1connect.AuthServiceClient
	projects  apiv1connect.ProjectServiceClient
	issues    apiv1connect.IssueServiceClient
	events    apiv1connect.EventServiceClient
	customers apiv1connect.CustomerServiceClient
	ai        apiv1connect.AiServiceClient
	orgs      apiv1connect.OrganizationServiceClient
	monitors  apiv1connect.MonitorServiceClient
}

// NewAPI builds API clients on hc against baseURL. Credentials travel in
// the request context and become the Authorization and Izci-Org headers.
func NewAPI(hc connect.HTTPClient, baseURL string) *API {
	opt := connect.WithInterceptors(credentialsInterceptor{})
	base := strings.TrimRight(baseURL, "/")
	return &API{
		auth:      apiv1connect.NewAuthServiceClient(hc, base, opt),
		projects:  apiv1connect.NewProjectServiceClient(hc, base, opt),
		issues:    apiv1connect.NewIssueServiceClient(hc, base, opt),
		events:    apiv1connect.NewEventServiceClient(hc, base, opt),
		customers: apiv1connect.NewCustomerServiceClient(hc, base, opt),
		ai:        apiv1connect.NewAiServiceClient(hc, base, opt),
		orgs:      apiv1connect.NewOrganizationServiceClient(hc, base, opt),
		monitors:  apiv1connect.NewMonitorServiceClient(hc, base, opt),
	}
}

type credentialsInterceptor struct{}

func (credentialsInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		c, _ := ctx.Value(credentialsKey{}).(Credentials)
		if c.Token != "" {
			req.Header().Set("Authorization", "Bearer "+c.Token)
		}
		if c.Org != "" {
			req.Header().Set("Izci-Org", c.Org)
		}
		return next(ctx, req)
	}
}

func (credentialsInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (credentialsInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// InProcess returns an HTTP client that serves requests with h directly,
// without a network round trip. Only the headers the API clients set are
// passed on: no cookies, so an MCP client can only act with its token.
func InProcess(h http.Handler) connect.HTTPClient { return handlerClient{h} }

type handlerClient struct{ h http.Handler }

func (c handlerClient) Do(r *http.Request) (*http.Response, error) {
	if r.Body == nil {
		r.Body = http.NoBody
	}
	r.RequestURI = r.URL.RequestURI()
	w := &bufferedResponse{header: http.Header{}}
	c.h.ServeHTTP(w, r)
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return &http.Response{
		Status: http.StatusText(w.status), StatusCode: w.status, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: w.header, Body: io.NopCloser(bytes.NewReader(w.body.Bytes())), ContentLength: int64(w.body.Len()), Request: r,
	}, nil
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *bufferedResponse) Header() http.Header { return w.header }

func (w *bufferedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(b)
}

// callerCredentials returns the credentials of a tool call: the HTTP
// headers of the MCP request, or the fixed credentials of a stdio server.
func (s *server) callerCredentials(extra *mcpsdk.RequestExtra, org string) (Credentials, error) {
	c := s.static
	if extra != nil && extra.Header != nil {
		c = Credentials{Token: bearer(extra.Header), Org: extra.Header.Get("Izci-Org")}
	}
	if org = strings.TrimSpace(org); org != "" {
		c.Org = strings.ToLower(org)
	}
	if c.Token == "" {
		return c, errors.New("no access token: send Authorization: Bearer izp_… (create one under Hesabım › Erişim anahtarları)")
	}
	return c, nil
}

func bearer(h http.Header) string {
	v := h.Get("Authorization")
	if len(v) > 7 && strings.EqualFold(v[:7], "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return ""
}

// apiError turns an API error into a message for the assistant.
func apiError(err error) error {
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		return err
	}
	reason := ""
	for _, d := range cerr.Details() {
		if v, derr := d.Value(); derr == nil {
			if ed, ok := v.(*apiv1.ErrorDetail); ok && ed.GetReason() != apiv1.ErrorReason_ERROR_REASON_UNSPECIFIED {
				reason = strings.ToLower(strings.TrimPrefix(ed.GetReason().String(), "ERROR_REASON_"))
				if ed.GetField() != "" {
					reason += ", field " + ed.GetField()
				}
			}
		}
	}
	msg := cerr.Message()
	switch {
	case reason == "organization_required":
		msg = "choose an organization: pass `organization` (see list_organizations) or send the Izci-Org header"
	case cerr.Code() == connect.CodePermissionDenied && strings.Contains(msg, "scope"):
		msg += "; create a token with the needed scope under Hesabım › Erişim anahtarları"
	}
	if reason != "" {
		return fmt.Errorf("%s (%s)", msg, reason)
	}
	return errors.New(msg)
}
