package sandbox

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// Op is an operation class agents can request.
type Op string

// Operation classes.
const (
	OpRead  Op = "read"
	OpWrite Op = "write"
	OpExec  Op = "exec"
	OpNet   Op = "net"
)

// Known reports whether op is one of the defined operation classes.
func (op Op) Known() bool {
	switch op {
	case OpRead, OpWrite, OpExec, OpNet:
		return true
	}
	return false
}

// Effect is what a policy says about a matching request. Deny is the
// default when no rule matches (ADR-0006).
type Effect string

// Policy effects.
const (
	Allow Effect = "allow"
	Deny  Effect = "deny"
	Ask   Effect = "ask" // requires interactive approval at the UI seam
)

// Valid reports whether e is one of the defined effects.
func (e Effect) Valid() bool {
	switch e {
	case Allow, Deny, Ask:
		return true
	}
	return false
}

// Rule maps an operation class to an effect, optionally scoped by path
// glob. Path patterns are slash-separated and relative to the jail root
// being consulted:
//
//	""        matches every path for the op
//	"/**"     matches everything under the root (inclusive)
//	"src/**"  matches everything under src/
//	other     single-level glob via path.Match ("*.md", "docs/*")
type Rule struct {
	Op     Op     `json:"op"`
	Path   string `json:"path,omitempty"`
	Effect Effect `json:"effect"`
}

// Policy is an ordered rule set; the first matching rule wins and an
// unmatched request is denied.
type Policy struct {
	Rules []Rule `json:"rules"`
}

// ParsePolicy decodes and validates policy JSON.
func ParsePolicy(data []byte) (*Policy, error) {
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("sandbox: policy: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks rule well-formedness.
func (p *Policy) Validate() error {
	for i, r := range p.Rules {
		if !r.Op.Known() {
			return fmt.Errorf("sandbox: rule %d: unknown op %q", i, r.Op)
		}
		if !r.Effect.Valid() {
			return fmt.Errorf("sandbox: rule %d: unknown effect %q", i, r.Effect)
		}
		if strings.Contains(r.Path, `\`) || r.Path == "/*" {
			return fmt.Errorf("sandbox: rule %d: bad pattern %q", i, r.Path)
		}
	}
	return nil
}

// Decision is the outcome of consulting a policy.
type Decision struct {
	Effect Effect
	Reason string
}

// Evaluate returns the effect for op against relPath ("/"-separated,
// relative to the jail root under consideration). First match wins;
// no match denies.
func (p *Policy) Evaluate(op Op, relPath string) Decision {
	for _, r := range p.Rules {
		if r.Op != op || !matchPath(r.Path, relPath) {
			continue
		}
		return Decision{Effect: r.Effect, Reason: fmt.Sprintf("rule: %s %s → %s", r.Op, r.Path, r.Effect)}
	}
	return Decision{Effect: Deny, Reason: "default deny"}
}

func matchPath(pattern, rel string) bool {
	pattern = strings.TrimSuffix(pattern, "/")
	rel = strings.TrimSuffix(rel, "/")
	switch pattern {
	case "":
		return true
	case "/**", "**":
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		dir := strings.TrimSuffix(pattern, "/**")
		return rel == dir || strings.HasPrefix(rel, dir+"/")
	}
	ok, err := path.Match(pattern, rel)
	return err == nil && ok
}
