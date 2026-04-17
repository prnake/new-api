package service

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

// proxySessionPlaceholder is the literal substring inside a proxy URL that
// gets replaced with a per-(channel,key) session identifier. When the proxy
// URL does not contain this placeholder, the URL is used verbatim and the
// session-stickiness logic is effectively inert.
const proxySessionPlaceholder = "%s"

// sessionIDHexLen controls the length of the hex-encoded session identifier
// substituted into the proxy URL. 16 hex chars (64 bits) is more than enough
// to keep collisions negligible for realistic channel counts while keeping the
// resulting proxy URL short.
const sessionIDHexLen = 16

// ResolveChannelProxy returns the effective proxy URL to use for an outbound
// request issued on behalf of (channelId, apiKey).
//
// The channel's own proxy takes precedence when configured; when empty, the
// globally-configured proxy (if any) is used as a fallback. If the resulting
// URL contains the "%s" placeholder, it is replaced with a deterministic
// session identifier derived from (channelId, apiKey) that rotates on the
// configured TTL. This pins the outbound IP of sticky-session-style proxies
// to a single address per (channel, key) for the lifetime of the window,
// maximising the useful life of the upstream key.
//
// When neither proxy is configured, or when the resolved URL is empty, the
// empty string is returned; callers should fall back to the default HTTP
// client in that case.
func ResolveChannelProxy(channelProxy string, channelId int, apiKey string) string {
	channelProxy = strings.TrimSpace(channelProxy)
	if channelProxy != "" {
		return applyProxySession(channelProxy, channelId, apiKey)
	}
	global := strings.TrimSpace(system_setting.GetGlobalProxy())
	if global == "" {
		return ""
	}
	return applyProxySession(global, channelId, apiKey)
}

// NewProxyHttpClientForChannel resolves the proxy URL for a channel request
// and returns the matching pooled *http.Client. It combines ResolveChannelProxy
// with NewProxyHttpClient so callers with channel context can swap in a single
// call.
func NewProxyHttpClientForChannel(channelProxy string, channelId int, apiKey string) (*http.Client, error) {
	resolved := ResolveChannelProxy(channelProxy, channelId, apiKey)
	if resolved == "" {
		if c := GetHttpClient(); c != nil {
			return c, nil
		}
		return http.DefaultClient, nil
	}
	return NewProxyHttpClient(resolved)
}

func applyProxySession(proxyURL string, channelId int, apiKey string) string {
	if !strings.Contains(proxyURL, proxySessionPlaceholder) {
		return proxyURL
	}
	ttl := system_setting.GetGlobalProxySessionTTL()
	if ttl <= 0 {
		ttl = system_setting.DefaultGlobalProxySessionTTL
	}
	bucket := time.Now().Unix() / int64(ttl)

	h := sha256.New()
	h.Write([]byte(strconv.Itoa(channelId)))
	h.Write([]byte{0})
	h.Write([]byte(apiKey))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(bucket, 10)))
	sessionID := hex.EncodeToString(h.Sum(nil))
	if len(sessionID) > sessionIDHexLen {
		sessionID = sessionID[:sessionIDHexLen]
	}
	return strings.ReplaceAll(proxyURL, proxySessionPlaceholder, sessionID)
}
