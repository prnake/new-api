package service

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	affinityMinRounds      = 5    // 最少需要 5 轮对话才启用亲和性
	affinityMaxCharPerTurn = 1024 // 每轮取前 1024 字符
)

func getAffinityRedisKey(group, modelName, affinityHash string) string {
	modelHash := md5.Sum([]byte(modelName))
	return fmt.Sprintf("affinity:%s:%s:%s", group, hex.EncodeToString(modelHash[:8]), affinityHash)
}

func GetAffinityChannelId(group, modelName, affinityHash string) (int, int, bool) {
	if !common.RedisEnabled || affinityHash == "" {
		return 0, -1, false
	}

	key := getAffinityRedisKey(group, modelName, affinityHash)
	valueStr, err := common.RedisGet(key)
	if err != nil || valueStr == "" {
		return 0, -1, false
	}

	// Parse format: "channelId" or "channelId:keyIndex"
	parts := strings.Split(valueStr, ":")
	channelId, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, -1, false
	}

	keyIndex := -1
	if len(parts) >= 2 {
		if idx, err := strconv.Atoi(parts[1]); err == nil {
			keyIndex = idx
		}
	}

	return channelId, keyIndex, true
}

func SetAffinityChannelId(group, modelName, affinityHash string, channelId int, keyIndex int) {
	if !common.RedisEnabled || affinityHash == "" {
		return
	}

	ttl := constant.SessionAffinityTTL
	if ttl <= 0 {
		ttl = 300
	}

	key := getAffinityRedisKey(group, modelName, affinityHash)
	value := strconv.Itoa(channelId)
	if keyIndex >= 0 {
		value = fmt.Sprintf("%d:%d", channelId, keyIndex)
	}
	_ = common.RedisSet(key, value, time.Duration(ttl)*time.Second)
}

func ClearAffinityChannelId(group, modelName, affinityHash string) {
	if !common.RedisEnabled || affinityHash == "" {
		return
	}

	key := getAffinityRedisKey(group, modelName, affinityHash)
	_ = common.RedisDel(key)
}

func ValidateAffinityChannel(channelId int, group, modelName string) *model.Channel {
	channel, err := model.CacheGetChannel(channelId)
	if err != nil || channel == nil {
		return nil
	}

	if channel.Status != common.ChannelStatusEnabled {
		return nil
	}

	return channel
}

func ComputeClaudeMessagesHash(messages []dto.ClaudeMessage) string {
	if len(messages) < affinityMinRounds {
		return ""
	}

	var builder strings.Builder
	count := min(len(messages), affinityMinRounds)
	for i := 0; i < count; i++ {
		text := messages[i].GetStringContent()
		if len(text) > affinityMaxCharPerTurn {
			text = text[:affinityMaxCharPerTurn]
		}
		builder.WriteString(messages[i].Role)
		builder.WriteByte(':')
		builder.WriteString(text)
		builder.WriteByte('|')
	}

	hash := md5.Sum([]byte(builder.String()))
	return hex.EncodeToString(hash[:])
}

func ComputeOpenAIMessagesHash(messages []dto.Message) string {
	if len(messages) < affinityMinRounds {
		return ""
	}

	var builder strings.Builder
	count := min(len(messages), affinityMinRounds)
	for i := 0; i < count; i++ {
		text := messages[i].StringContent()
		if len(text) > affinityMaxCharPerTurn {
			text = text[:affinityMaxCharPerTurn]
		}
		builder.WriteString(messages[i].Role)
		builder.WriteByte(':')
		builder.WriteString(text)
		builder.WriteByte('|')
	}

	hash := md5.Sum([]byte(builder.String()))
	return hex.EncodeToString(hash[:])
}

func ComputeGeminiMessagesHash(contents []dto.GeminiChatContent) string {
	if len(contents) < affinityMinRounds {
		return ""
	}

	var builder strings.Builder
	count := min(len(contents), affinityMinRounds)
	for i := 0; i < count; i++ {
		var text string
		for _, part := range contents[i].Parts {
			if part.Text != "" {
				text = part.Text
				break
			}
		}
		if len(text) > affinityMaxCharPerTurn {
			text = text[:affinityMaxCharPerTurn]
		}
		builder.WriteString(contents[i].Role)
		builder.WriteByte(':')
		builder.WriteString(text)
		builder.WriteByte('|')
	}

	hash := md5.Sum([]byte(builder.String()))
	return hex.EncodeToString(hash[:])
}

// ClearAffinityOnFailure clears the session affinity cache entry when a request
// fails (e.g., 429 rate limit). This prevents subsequent new requests from being
// directed to the same channel/key via stale affinity cache.
func ClearAffinityOnFailure(c *gin.Context) {
	if c == nil {
		return
	}
	if !common.GetContextKeyBool(c, constant.ContextKeyAffinityHit) {
		return
	}

	affinityHash := common.GetContextKeyString(c, constant.ContextKeyAffinityHash)
	if affinityHash == "" {
		return
	}

	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	if group == "" || group == "auto" {
		return
	}

	modelName := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	if modelName == "" {
		return
	}

	ClearAffinityChannelId(group, modelName, affinityHash)
	// Mark as cleared so we don't try to clear again on subsequent retries
	common.SetContextKey(c, constant.ContextKeyAffinityHit, false)
}

// ShouldSkipRetryAfterChannelAffinityFailure is a stub for compatibility with main branch code.
// In deploy branch, we use the simpler affinity_hit logic and don't need skip retry.
func ShouldSkipRetryAfterChannelAffinityFailure(c *gin.Context) bool {
	return false
}

// GetPreferredChannelByAffinity is a stub for main branch compatibility.
// Deploy branch uses a different affinity mechanism via GetAffinityChannelId.
func GetPreferredChannelByAffinity(c *gin.Context, modelName string, usingGroup string) (int, bool) {
	return 0, false
}

// MarkChannelAffinityUsed is a stub for main branch compatibility.
func MarkChannelAffinityUsed(c *gin.Context, selectedGroup string, channelID int) {
}

// RecordChannelAffinity records the successful channel for session affinity.
// Called after request completes successfully, updates Redis with the actual successful channel.
func RecordChannelAffinity(c *gin.Context, initialChannelID int) {
	if c == nil {
		return
	}
	affinityHash := common.GetContextKeyString(c, constant.ContextKeyAffinityHash)
	if affinityHash == "" {
		return
	}

	successChannelID := c.GetInt("channel_id")
	if successChannelID <= 0 {
		successChannelID = initialChannelID
	}
	if successChannelID <= 0 {
		return
	}

	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	if group == "" || group == "auto" {
		return
	}

	modelName := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	if modelName == "" {
		return
	}

	keyIndex := -1
	if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		keyIndex = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}

	go SetAffinityChannelId(group, modelName, affinityHash, successChannelID, keyIndex)
}

// ObserveChannelAffinityUsageCacheFromContext is a stub for main branch compatibility.
func ObserveChannelAffinityUsageCacheFromContext(c *gin.Context, usage *dto.Usage) {
}

// ChannelAffinityCacheStats is a stub type for main branch compatibility.
type ChannelAffinityCacheStats struct {
	Enabled       bool           `json:"enabled"`
	Total         int            `json:"total"`
	Unknown       int            `json:"unknown"`
	ByRuleName    map[string]int `json:"by_rule_name"`
	CacheCapacity int            `json:"cache_capacity"`
	CacheAlgo     string         `json:"cache_algo"`
}

// GetChannelAffinityCacheStats is a stub for main branch compatibility.
func GetChannelAffinityCacheStats() ChannelAffinityCacheStats {
	return ChannelAffinityCacheStats{
		Enabled:    false,
		Total:      0,
		Unknown:    0,
		ByRuleName: map[string]int{},
	}
}

// ClearChannelAffinityCacheAll is a stub for main branch compatibility.
func ClearChannelAffinityCacheAll() int {
	return 0
}

// ClearChannelAffinityCacheByRuleName is a stub for main branch compatibility.
func ClearChannelAffinityCacheByRuleName(ruleName string) (int, error) {
	return 0, nil
}

// ChannelAffinityUsageCacheStats is a stub type for main branch compatibility.
type ChannelAffinityUsageCacheStats struct {
	RuleName             string `json:"rule_name"`
	UsingGroup           string `json:"using_group"`
	KeyFingerprint       string `json:"key_fp"`
	Hit                  int64  `json:"hit"`
	Total                int64  `json:"total"`
	WindowSeconds        int64  `json:"window_seconds"`
	PromptTokens         int64  `json:"prompt_tokens"`
	CompletionTokens     int64  `json:"completion_tokens"`
	TotalTokens          int64  `json:"total_tokens"`
	CachedTokens         int64  `json:"cached_tokens"`
	PromptCacheHitTokens int64  `json:"prompt_cache_hit_tokens"`
	LastSeenAt           int64  `json:"last_seen_at"`
}

// GetChannelAffinityUsageCacheStats is a stub for main branch compatibility.
func GetChannelAffinityUsageCacheStats(ruleName, usingGroup, keyFp string) ChannelAffinityUsageCacheStats {
	return ChannelAffinityUsageCacheStats{
		RuleName:       ruleName,
		UsingGroup:     usingGroup,
		KeyFingerprint: keyFp,
	}
}

func AppendChannelAffinityAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	affinityHit := common.GetContextKeyBool(c, constant.ContextKeyAffinityHit)
	if !affinityHit {
		return
	}
	affinityHash := common.GetContextKeyString(c, constant.ContextKeyAffinityHash)
	info := map[string]interface{}{
		"reason":    "session_affinity",
		"hit":       true,
		"hash_hint": truncateHash(affinityHash),
	}
	adminInfo["channel_affinity"] = info
}

func truncateHash(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}
