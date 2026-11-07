// Package sessionkey mints a fresh, budgeted, model-scoped ai-gateway virtual
// key for one session — the server-side half of "chat as a disposable key, not
// the human's JWT".
//
// Lifted from the shell's sessionkeymint/chat, decoupled from the CLI: it takes
// an *aigateway.Client and plain values (name, model, budget) and returns the
// minted secret + id. The shell supplies a clusters.Profile-derived name and
// prints to a terminal; leartech-console-api supplies a connection-derived name
// and streams nothing — both call this one Mint.
//
// WHY A FRESH KEY PER SESSION. A key cached per (cluster, model) is shared by
// every session on that model: one budget, one usage row, two conversations
// nobody can tell apart. Per-session mint gives each its own ceiling and its own
// attributable row.
package sessionkey

import (
	"context"
	"fmt"
	"time"

	"github.com/mikelear/leartech-go-common/pkg/aigateway"
)

// Gateway scope constants — the set a minted chat key carries: chat to
// mint-and-use, web_search/web_fetch so the gateway's hosted tools work under
// the metered key. Kept here (not imported from the shell's clusters package) so
// the library has no CLI dependency.
const (
	ScopeChat      = "leartechapi:gateway:chat"
	ScopeWebSearch = "leartechapi:gateway:web_search"
	ScopeWebFetch  = "leartechapi:gateway:web_fetch"
)

// Lifetime is how long a minted session key lives.
const Lifetime = 24 * time.Hour

// Minted is a freshly minted session key: the secret to spend with, and the
// gateway key id to report and look up later.
type Minted struct {
	Secret string
	KeyID  string
}

// Mint mints a FRESH budgeted key allowing exactly one model.
//
// A key the gateway created WITHOUT a secret exists, is billable and cannot be
// used — Mint treats that as an error (and the caller should revoke it) rather
// than returning an unusable key.
//
// proven-by: sessionkey_test.go
func Mint(ctx context.Context, c *aigateway.Client, name, model string, budgetMicros int64) (Minted, error) {
	got, err := c.CreateKey(ctx, aigateway.CreateKeyRequest{
		Name:           name,
		ModelAllowlist: []string{model},
		BudgetMicros:   &budgetMicros,
		ExpiresAt:      expiryIn(Lifetime),
		Scopes:         []string{ScopeChat, ScopeWebSearch, ScopeWebFetch},
	})
	if err != nil {
		return Minted{}, fmt.Errorf("minting a session key: %w", err)
	}
	if got.Secret == "" {
		return Minted{}, fmt.Errorf("the gateway minted %s with no secret — it exists, is billable "+
			"and cannot be used; revoke it", got.KeyID)
	}
	return Minted{Secret: got.Secret, KeyID: got.KeyID}, nil
}

func expiryIn(d time.Duration) *string {
	s := time.Now().UTC().Add(d).Format(time.RFC3339)
	return &s
}
