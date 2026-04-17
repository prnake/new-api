package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

func TestResolveChannelProxy_ChannelProxyTakesPrecedence(t *testing.T) {
	system_setting.SetGlobalProxy("http://global:8080")
	t.Cleanup(func() { system_setting.SetGlobalProxy("") })

	got := ResolveChannelProxy("http://channel:1080", 42, "sk-abc")
	if got != "http://channel:1080" {
		t.Fatalf("expected channel proxy to win, got %q", got)
	}
}

func TestResolveChannelProxy_FallsBackToGlobal(t *testing.T) {
	system_setting.SetGlobalProxy("http://global:8080")
	t.Cleanup(func() { system_setting.SetGlobalProxy("") })

	got := ResolveChannelProxy("", 42, "sk-abc")
	if got != "http://global:8080" {
		t.Fatalf("expected global proxy, got %q", got)
	}
}

func TestResolveChannelProxy_NoProxyConfigured(t *testing.T) {
	system_setting.SetGlobalProxy("")
	if got := ResolveChannelProxy("", 1, "k"); got != "" {
		t.Fatalf("expected empty URL when nothing configured, got %q", got)
	}
}

func TestResolveChannelProxy_PlaceholderReplacementIsDeterministic(t *testing.T) {
	system_setting.SetGlobalProxy("http://user-%s:pass@proxy.example.com:8080")
	system_setting.SetGlobalProxySessionTTL(system_setting.DefaultGlobalProxySessionTTL)
	t.Cleanup(func() { system_setting.SetGlobalProxy("") })

	a := ResolveChannelProxy("", 7, "sk-abc")
	b := ResolveChannelProxy("", 7, "sk-abc")
	if a != b {
		t.Fatalf("same (channel,key,window) must produce identical URLs: %q vs %q", a, b)
	}
	if strings.Contains(a, "%s") {
		t.Fatalf("placeholder should have been substituted, got %q", a)
	}
	if !strings.HasPrefix(a, "http://user-") {
		t.Fatalf("unexpected URL shape: %q", a)
	}
}

func TestResolveChannelProxy_DifferentKeysProduceDifferentSessions(t *testing.T) {
	system_setting.SetGlobalProxy("http://user-%s@proxy.example.com:8080")
	t.Cleanup(func() { system_setting.SetGlobalProxy("") })

	a := ResolveChannelProxy("", 1, "key-a")
	b := ResolveChannelProxy("", 1, "key-b")
	if a == b {
		t.Fatalf("different keys must map to different sessions: both=%q", a)
	}
}

func TestResolveChannelProxy_NoPlaceholderIsPassthrough(t *testing.T) {
	system_setting.SetGlobalProxy("http://plain-proxy:9999")
	t.Cleanup(func() { system_setting.SetGlobalProxy("") })

	got := ResolveChannelProxy("", 1, "whatever")
	if got != "http://plain-proxy:9999" {
		t.Fatalf("without %%s the URL must be used verbatim, got %q", got)
	}
}
