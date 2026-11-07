package mockgateway_test

import (
	"context"
	"testing"

	"github.com/mikelear/leartech-go-common/pkg/aigateway"

	"github.com/mikelear/leartech-agent-core/mockgateway"
	"github.com/mikelear/leartech-agent-core/sessionkey"
)

func TestMockGateway_ModelsToolsAndKey(t *testing.T) {
	srv := mockgateway.New(t, mockgateway.Options{
		Models:    []mockgateway.Model{{ID: "claude", Provider: "anthropic"}},
		Tools:     []mockgateway.GatewayTool{{Name: "search", Available: true}},
		KeySecret: "sk-lt-x",
	})
	gw := aigateway.New(srv.URL, "jwt", nil)

	models, err := gw.Models(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "claude" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	tools, err := gw.Tools(context.Background())
	if err != nil || len(tools) != 1 || !tools[0].Available {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	// sessionkey.Mint drives /admin/v1/keys against this stub.
	minted, err := sessionkey.Mint(context.Background(), gw, "n", "claude", 500_000)
	if err != nil || minted.Secret != "sk-lt-x" {
		t.Fatalf("mint=%+v err=%v", minted, err)
	}
}

func TestMockGateway_SecretlessKeyDrivesTheErrorBranch(t *testing.T) {
	srv := mockgateway.New(t, mockgateway.Options{
		Models: []mockgateway.Model{{ID: "claude"}},
		// KeySecret empty -> the stub returns a key with no secret.
	})
	gw := aigateway.New(srv.URL, "jwt", nil)
	if _, err := sessionkey.Mint(context.Background(), gw, "n", "claude", 1); err == nil {
		t.Fatal("a secretless minted key must be an error")
	}
}

func TestMockGateway_ChatStreamsPlainAndToolCall(t *testing.T) {
	srv := mockgateway.New(t, mockgateway.Options{
		Models:       []mockgateway.Model{{ID: "claude"}},
		ChatReply:    "hello there",
		ToolCallOn:   "use the tool",
		ToolCallName: "gateway__search",
	})
	gw := aigateway.New(srv.URL, "jwt", nil)

	// plain
	plain := drain(t, gw, "say hi")
	if plain.text != "hello there" || plain.toolCalls != 0 {
		t.Fatalf("plain: text=%q calls=%d", plain.text, plain.toolCalls)
	}
	// tool call
	tc := drain(t, gw, "use the tool now")
	if tc.toolCalls != 1 {
		t.Fatalf("expected one tool call, got %d", tc.toolCalls)
	}
}

type streamed struct {
	text      string
	toolCalls int
}

func drain(t *testing.T, gw *aigateway.Client, msg string) streamed {
	t.Helper()
	chunks, err := gw.ChatStream(context.Background(), aigateway.ChatRequest{
		Model:    "claude",
		Messages: []aigateway.ChatRequestMessage{{Role: "user", Content: msg}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var out streamed
	for ch := range chunks {
		if ch.Text != "" {
			out.text += ch.Text
		}
		if ch.ToolCall != nil {
			out.toolCalls++
		}
	}
	return out
}
