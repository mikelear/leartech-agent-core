package sessionkey_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/mikelear/leartech-go-common/pkg/aigateway"

	"github.com/mikelear/leartech-agent-core/sessionkey"
)

// keyGatewayStub serves POST /admin/v1/keys, recording the request and
// returning a minted key (with or without a secret, per want).
func keyGatewayStub(t *testing.T, withSecret bool, captured *aigateway.CreateKeyRequest) (*aigateway.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req aigateway.CreateKeyRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if captured != nil {
			*captured = req
		}
		resp := aigateway.CreateKeyResponse{KeyID: "key-123"}
		if withSecret {
			resp.Secret = "sk-lt-abc"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return aigateway.New(srv.URL, "jwt", nil), srv
}

func TestMint_ReturnsSecretAndKeyID(t *testing.T) {
	c, _ := keyGatewayStub(t, true, nil)
	got, err := sessionkey.Mint(context.Background(), c, "console-user-1-claude", "claude", 500_000)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if got.Secret != "sk-lt-abc" || got.KeyID != "key-123" {
		t.Fatalf("got %+v", got)
	}
}

func TestMint_AllowsOnlyTheChosenModelAndCarriesTheScopes(t *testing.T) {
	var req aigateway.CreateKeyRequest
	c, _ := keyGatewayStub(t, true, &req)
	if _, err := sessionkey.Mint(context.Background(), c, "n", "glm-4", 500_000); err != nil {
		t.Fatalf("mint: %v", err)
	}
	if len(req.ModelAllowlist) != 1 || req.ModelAllowlist[0] != "glm-4" {
		t.Fatalf("model allowlist = %v, want [glm-4]", req.ModelAllowlist)
	}
	for _, want := range []string{sessionkey.ScopeChat, sessionkey.ScopeWebSearch, sessionkey.ScopeWebFetch} {
		if !slices.Contains(req.Scopes, want) {
			t.Fatalf("scopes %v missing %s", req.Scopes, want)
		}
	}
	if req.BudgetMicros == nil || *req.BudgetMicros != 500_000 {
		t.Fatalf("budget = %v, want 500000", req.BudgetMicros)
	}
	if req.ExpiresAt == nil || *req.ExpiresAt == "" {
		t.Fatal("expiry must be set")
	}
}

func TestMint_ASecretlessKeyIsAnErrorNotAnUnusableKey(t *testing.T) {
	c, _ := keyGatewayStub(t, false, nil)
	_, err := sessionkey.Mint(context.Background(), c, "n", "claude", 500_000)
	if err == nil {
		t.Fatal("a minted key with no secret must be an error")
	}
}
