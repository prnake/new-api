package service

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// cacheControlEphemeral 是 Anthropic 的 ephemeral 缓存控制配置
var cacheControlEphemeral = json.RawMessage(`{"type": "ephemeral"}`)

// hasUserCacheControl 检查用户是否已在请求中手动设置 cache_control
func hasUserCacheControl(request *dto.ClaudeRequest) bool {
	if request == nil {
		return false
	}

	// 检查 system 字段
	if request.System != nil {
		if hasCacheControlInSystem(request.System) {
			return true
		}
	}

	// 检查 messages 字段
	if hasCacheControlInMessages(request.Messages) {
		return true
	}

	return false
}

// hasCacheControlInSystem 检查 system 中是否有 cache_control
func hasCacheControlInSystem(system any) bool {
	if system == nil {
		return false
	}

	// 处理字符串类型的 system
	if _, ok := system.(string); ok {
		return false
	}

	// 处理 ClaudeMediaMessage 数组
	if mediaList, ok := system.([]dto.ClaudeMediaMessage); ok {
		for _, msg := range mediaList {
			if msg.CacheControl != nil {
				return true
			}
		}
	}

	// 处理 []any 类型
	if anyList, ok := system.([]any); ok {
		for _, item := range anyList {
			if contentMap, ok := item.(map[string]any); ok {
				if cc, ok := contentMap["cache_control"]; ok && cc != nil {
					return true
				}
			}
		}
	}

	return false
}

// hasCacheControlInMessages 检查 messages 中是否有 cache_control
func hasCacheControlInMessages(messages []dto.ClaudeMessage) bool {
	for _, msg := range messages {
		if msg.Content == nil {
			continue
		}

		// 处理数组类型的 content
		if contentList, ok := msg.Content.([]any); ok {
			for _, item := range contentList {
				if contentMap, ok := item.(map[string]any); ok {
					if cc, ok := contentMap["cache_control"]; ok && cc != nil {
						return true
					}
				}
			}
		}

		// 处理 ClaudeMediaMessage 数组
		if mediaList, ok := msg.Content.([]dto.ClaudeMediaMessage); ok {
			for _, media := range mediaList {
				if media.CacheControl != nil {
					return true
				}
			}
		}
	}

	return false
}

// ShouldApplyAutoCache 判断是否满足自动缓存条件
// 1. CLAUDE_AUTO_CACHE 环境变量为 true（默认）
// 2. 模型是 claude-*（上游可能是 aws/vertex/anthropic）
// 3. 存在缓存命中（前5轮算hash的 session affinity 逻辑）
// 4. 用户未手动设置任何 cache_control
func ShouldApplyAutoCache(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) bool {
	// 检查环境变量
	if !constant.ClaudeAutoCache {
		return false
	}

	// 检查模型是否为 claude-*
	if !strings.HasPrefix(info.UpstreamModelName, "claude-") {
		return false
	}

	// 检查是否存在 session affinity hit（前5轮对话已计算 hash）
	if !common.GetContextKeyBool(c, constant.ContextKeyAffinityHit) {
		return false
	}

	// 检查用户是否已手动设置 cache_control
	if hasUserCacheControl(request) {
		return false
	}

	return true
}

func ApplyClaudeAutoCache(request *dto.ClaudeRequest) {
	if request == nil || len(request.Messages) == 0 {
		return
	}

	lastIdx := len(request.Messages) - 1
	lastMsg := &request.Messages[lastIdx]
	applyCacheControlToLastBlock(lastMsg)
}

func applyCacheControlToLastBlock(msg *dto.ClaudeMessage) {
	if msg.Content == nil {
		return
	}

	// 处理字符串类型的 content
	if contentStr, ok := msg.Content.(string); ok {
		// 转换为 media content 数组并添加 cache_control
		msg.Content = []dto.ClaudeMediaMessage{
			{
				Type:         dto.ContentTypeText,
				Text:         &contentStr,
				CacheControl: cacheControlEphemeral,
			},
		}
		return
	}

	if contentList, ok := msg.Content.([]any); ok && len(contentList) > 0 {
		lastIdx := len(contentList) - 1
		lastItem := contentList[lastIdx]

		contentMap, ok := lastItem.(map[string]any)
		if ok {
			contentMap["cache_control"] = map[string]string{"type": "ephemeral"}
			contentList[lastIdx] = contentMap
			msg.Content = contentList
		}
		return
	}

	if mediaList, ok := msg.Content.([]dto.ClaudeMediaMessage); ok && len(mediaList) > 0 {
		lastIdx := len(mediaList) - 1
		mediaList[lastIdx].CacheControl = cacheControlEphemeral
		msg.Content = mediaList
	}
}
