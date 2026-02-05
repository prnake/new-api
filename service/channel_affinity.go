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
)

const (
	affinityMinRounds      = 5    // 最少需要 5 轮对话才启用亲和性
	affinityMaxCharPerTurn = 1024 // 每轮取前 1024 字符
)

func getAffinityRedisKey(group, modelName, affinityHash string) string {
	modelHash := md5.Sum([]byte(modelName))
	return fmt.Sprintf("affinity:%s:%s:%s", group, hex.EncodeToString(modelHash[:8]), affinityHash)
}

func GetAffinityChannelId(group, modelName, affinityHash string) (int, bool) {
	if !common.RedisEnabled || affinityHash == "" {
		return 0, false
	}

	key := getAffinityRedisKey(group, modelName, affinityHash)
	channelIdStr, err := common.RedisGet(key)
	if err != nil || channelIdStr == "" {
		return 0, false
	}

	channelId, err := strconv.Atoi(channelIdStr)
	if err != nil {
		return 0, false
	}

	return channelId, true
}

func SetAffinityChannelId(group, modelName, affinityHash string, channelId int) {
	if !common.RedisEnabled || affinityHash == "" {
		return
	}

	ttl := constant.SessionAffinityTTL
	if ttl <= 0 {
		ttl = 300
	}

	key := getAffinityRedisKey(group, modelName, affinityHash)
	_ = common.RedisSet(key, strconv.Itoa(channelId), time.Duration(ttl)*time.Second)
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
