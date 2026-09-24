// Package authz 实现权限 Scope 解析与请求上下文注入。
// 设计依据：MultiUser_RBAC_Design §7、§18、§45–§48。
package authz

import (
	"context"
	"strings"
)

// Scope 数据范围。
type Scope string

const (
	ScopeALL      Scope = "ALL"
	ScopeOWN      Scope = "OWN"
	ScopeASSIGNED Scope = "ASSIGNED"
	ScopeNONE     Scope = "NONE"
)

// Grant 权限码 + Scope。
type Grant struct {
	Name  string `json:"name"`
	Scope Scope  `json:"scope"`
}

type ctxKey int

const (
	keyGrants ctxKey = iota + 1
	keyScope
	keyPerm
)

// WithAuthz 将本次请求的权限裁决写入 context。
func WithAuthz(ctx context.Context, perm string, scope Scope, grants []Grant) context.Context {
	ctx = context.WithValue(ctx, keyPerm, perm)
	ctx = context.WithValue(ctx, keyScope, scope)
	ctx = context.WithValue(ctx, keyGrants, grants)
	return ctx
}

// ScopeFrom 读取本次请求 Scope，缺省 NONE。
func ScopeFrom(ctx context.Context) Scope {
	if v, ok := ctx.Value(keyScope).(Scope); ok && v != "" {
		return v
	}
	return ScopeNONE
}

// PermFrom 读取本次请求声明的权限码。
func PermFrom(ctx context.Context) string {
	if v, ok := ctx.Value(keyPerm).(string); ok {
		return v
	}
	return ""
}

// GrantsFrom 读取全部授权。
func GrantsFrom(ctx context.Context) []Grant {
	if v, ok := ctx.Value(keyGrants).([]Grant); ok {
		return v
	}
	return nil
}

// Resolve 从授权列表解析某权限码的 Scope。
// SUPER_ADMIN 的 "*" → ALL；同码多条取最宽（ALL > ASSIGNED > OWN > NONE）。
func Resolve(grants []Grant, need string) Scope {
	best := ScopeNONE
	for _, g := range grants {
		if g.Name == "*" {
			return ScopeALL
		}
		if g.Name != need {
			continue
		}
		best = wider(best, g.Scope)
	}
	return best
}

func wider(a, b Scope) Scope {
	rank := func(s Scope) int {
		switch strings.ToUpper(string(s)) {
		case "ALL":
			return 4
		case "ASSIGNED":
			return 3
		case "OWN":
			return 2
		default:
			return 1
		}
	}
	if rank(b) > rank(a) {
		return Scope(strings.ToUpper(string(b)))
	}
	return Scope(strings.ToUpper(string(a)))
}

// Allowed 是否具备权限码（Scope≠NONE 或存在该码）。
func Allowed(grants []Grant, need string) bool {
	s := Resolve(grants, need)
	return s != ScopeNONE || hasCode(grants, need) || hasCode(grants, "*")
}

func hasCode(grants []Grant, need string) bool {
	for _, g := range grants {
		if g.Name == need || g.Name == "*" {
			return true
		}
	}
	return false
}
