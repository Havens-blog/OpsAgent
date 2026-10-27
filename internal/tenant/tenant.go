// Package tenant carries the authenticated tenant id through a request's
// context so storage layers can scope data per tenant without threading an
// extra parameter through every call site.
//
// This mirrors the legacy ops Agent's domain.WithTenantID / TenantIDFrom
// pattern. The tenant is written once by the EIAM auth middleware (taken from
// the verified session JWT) and read by stores to enforce isolation.
//
// Tenant 0 is the legitimate default bucket: when EIAM enforcement is
// disabled (static-token or no-auth mode) no tenant is injected and reads/
// writes fall into bucket 0, so behavior is unchanged (progressive
// enhancement). When EIAM is enabled the middleware rejects tenant_id == 0
// with 403 before any handler runs, so bucket 0 can never be reached by an
// authenticated-but-unselected user.
package tenant

import "context"

type ctxKey struct{}

// WithID returns a copy of ctx carrying the given tenant id.
func WithID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// IDFrom returns the tenant id carried by ctx, or 0 if none is set.
func IDFrom(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxKey{}).(int64)
	return id
}
