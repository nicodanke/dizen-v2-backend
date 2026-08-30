package interceptor

// Allowlist is the explicit set of fully qualified methods that skip authentication.
//
// 03 section 7 is categorical: nothing is public by default. A method becomes public only
// by being named here, which makes the public surface reviewable in a single diff.
type Allowlist struct {
	methods map[string]struct{}
}

// NewAllowlist builds an allowlist from fully qualified method names, such as
// "/dizen.identity.v1.AuthService/SignIn".
func NewAllowlist(methods ...string) *Allowlist {
	set := make(map[string]struct{}, len(methods))
	for _, m := range methods {
		set[m] = struct{}{}
	}

	return &Allowlist{methods: set}
}

// Contains reports whether the method is public.
func (a *Allowlist) Contains(method string) bool {
	if a == nil {
		return false
	}

	_, ok := a.methods[method]

	return ok
}

// Methods returns the allowlisted methods. Used by tests and by the startup log, which
// prints the public surface so it is visible in production logs.
func (a *Allowlist) Methods() []string {
	if a == nil {
		return nil
	}

	out := make([]string, 0, len(a.methods))
	for m := range a.methods {
		out = append(out, m)
	}

	return out
}

// HealthMethods are the methods every service exposes without authentication: the
// healthchecks the orchestrator and the probes call.
func HealthMethods() []string {
	return []string{
		"/grpc.health.v1.Health/Check",
		"/grpc.health.v1.Health/Watch",
	}
}
