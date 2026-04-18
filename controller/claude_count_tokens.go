package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// claudeCountTokensMeta is a minimal shape used only to pull the model name
// out of the request body for channel selection. The body is forwarded to the
// upstream verbatim.
type claudeCountTokensMeta struct {
	Model string `json:"model"`
}

// RelayClaudeCountTokens proxies POST /v1/messages/count_tokens to a random
// Claude-capable upstream channel (Anthropic native or Vertex AI), reusing the
// normal retry priority logic. The endpoint is treated as a free utility — no
// quota is pre-consumed or settled. The request body is forwarded verbatim.
func RelayClaudeCountTokens(c *gin.Context) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		writeClaudeCountError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	rawBody, err := storage.Bytes()
	if err != nil {
		writeClaudeCountError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	var meta claudeCountTokensMeta
	if err := common.Unmarshal(rawBody, &meta); err != nil {
		writeClaudeCountError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if meta.Model == "" {
		writeClaudeCountError(c, http.StatusBadRequest, "invalid_request_error", "field model is required")
		return
	}

	modelName := meta.Model
	// Mirror the same suffix handling that /v1/messages applies so channel
	// lookup uses the base model name.
	if strings.HasSuffix(modelName, "-cc") {
		modelName = strings.TrimSuffix(modelName, "-cc")
		common.SetContextKey(c, constant.ContextKeyCCMode, true)
	}
	common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)
	c.Set("original_model", modelName)

	tokenGroup := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	if tokenGroup == "" {
		tokenGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	}

	anthropicBeta := c.Request.Header.Get("anthropic-beta")
	if anthropicBeta != "" {
		common.SetContextKey(c, constant.ContextKeyAnthropicBeta, anthropicBeta)
		common.SetContextKey(c, constant.ContextKeyOriginalAnthropicBeta, anthropicBeta)
	}
	var requestBetas []string
	if !common.GetContextKeyBool(c, constant.ContextKeyCCMode) {
		requestBetas = model.ParseAnthropicBeta(anthropicBeta)
	}

	var (
		lastStatus int
		lastBody   []byte
	)

	retryParam := &service.RetryParam{
		Ctx:          c,
		TokenGroup:   tokenGroup,
		ModelName:    modelName,
		Retry:        common.GetPointer(0),
		RequestBetas: requestBetas,
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		channel, _, selErr := service.CacheGetRandomSatisfiedChannel(retryParam)
		if selErr != nil {
			logger.LogError(c, fmt.Sprintf("count_tokens select channel failed: %s", selErr.Error()))
			break
		}
		if channel == nil {
			break
		}

		// count_tokens has two supported upstream forms: the native Anthropic
		// API and Vertex AI. Skip any other channel type.
		if channel.Type != constant.ChannelTypeAnthropic && channel.Type != constant.ChannelTypeVertexAi {
			continue
		}

		if setupErr := middleware.SetupContextForSelectedChannel(c, channel, modelName); setupErr != nil {
			logger.LogError(c, fmt.Sprintf("count_tokens setup channel #%d failed: %s", channel.Id, setupErr.Error()))
			continue
		}
		addUsedChannel(c, channel.Id)

		status, respBody, reqErr := forwardClaudeCountTokens(c, channel, rawBody, modelName)
		if reqErr != nil {
			logger.LogError(c, fmt.Sprintf("count_tokens forward failed (channel #%d): %s", channel.Id, reqErr.Error()))
			continue
		}

		if status == http.StatusOK {
			c.Data(status, "application/json", respBody)
			return
		}

		lastStatus = status
		lastBody = respBody
		if !shouldRetryCountTokensStatus(status) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		logger.LogInfo(c, fmt.Sprintf("重试：%s",
			strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]")))
	}

	if lastStatus == 0 {
		writeClaudeCountError(c, http.StatusServiceUnavailable, "api_error",
			fmt.Sprintf("no available anthropic/vertex channel for model %s", modelName))
		return
	}
	c.Data(lastStatus, "application/json", lastBody)
}

func shouldRetryCountTokensStatus(status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	return status >= 500 && status <= 599
}

func writeClaudeCountError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// forwardClaudeCountTokens dispatches to the per-upstream forwarder based on
// the channel type.
func forwardClaudeCountTokens(c *gin.Context, channel *model.Channel, body []byte, modelName string) (int, []byte, error) {
	switch channel.Type {
	case constant.ChannelTypeVertexAi:
		return forwardVertexCountTokens(c, channel, body, modelName)
	default:
		return forwardAnthropicCountTokens(c, channel, body)
	}
}

func forwardAnthropicCountTokens(c *gin.Context, channel *model.Channel, body []byte) (int, []byte, error) {
	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost,
		baseURL+"/v1/messages/count_tokens", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	apiKey := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	req.Header.Set("x-api-key", apiKey)

	version := c.Request.Header.Get("anthropic-version")
	if version == "" {
		version = "2023-06-01"
	}
	req.Header.Set("anthropic-version", version)

	if beta := common.GetContextKeyString(c, constant.ContextKeyAnthropicBeta); beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}

	return doCountTokensHTTP(c, channel, apiKey, req)
}

func forwardVertexCountTokens(c *gin.Context, channel *model.Channel, body []byte, modelName string) (int, []byte, error) {
	apiKey := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	otherSettings := channel.GetOtherSettings()
	if otherSettings.VertexKeyType == dto.VertexKeyTypeAPIKey {
		return 0, nil, fmt.Errorf("vertex count_tokens does not support api_key credentials, use a service-account JSON key")
	}

	creds := vertex.Credentials{}
	if err := common.Unmarshal([]byte(apiKey), &creds); err != nil {
		return 0, nil, fmt.Errorf("failed to decode vertex credentials: %w", err)
	}
	if creds.ProjectID == "" {
		return 0, nil, fmt.Errorf("vertex credentials missing project_id")
	}

	region := vertex.GetModelRegion(channel.Other, modelName)
	if region == "" {
		region = "global"
	}

	var url string
	if region == "global" {
		url = fmt.Sprintf(
			"https://aiplatform.googleapis.com/v1/projects/%s/locations/global/publishers/anthropic/models/count-tokens:rawPredict",
			creds.ProjectID,
		)
	} else {
		url = fmt.Sprintf(
			"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/count-tokens:rawPredict",
			region, creds.ProjectID, region,
		)
	}

	proxy := channel.GetSetting().Proxy
	cacheKey := fmt.Sprintf("access-token-%d", channel.Id)
	var accessToken string
	if val, cacheErr := vertex.Cache.Get(cacheKey); cacheErr == nil {
		if s, ok := val.(string); ok {
			accessToken = s
		}
	}
	if accessToken == "" {
		tok, tokErr := vertex.AcquireAccessToken(creds, proxy)
		if tokErr != nil {
			return 0, nil, fmt.Errorf("vertex access token failed: %w", tokErr)
		}
		accessToken = tok
		_ = vertex.Cache.SetDefault(cacheKey, accessToken)
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("x-goog-user-project", creds.ProjectID)

	if beta := common.GetContextKeyString(c, constant.ContextKeyAnthropicBeta); beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}

	return doCountTokensHTTP(c, channel, apiKey, req)
}

func doCountTokensHTTP(c *gin.Context, channel *model.Channel, apiKey string, req *http.Request) (int, []byte, error) {
	proxyURL := service.ResolveChannelProxy(channel.GetSetting().Proxy, channel.Id, apiKey)
	var (
		client *http.Client
		err    error
	)
	if proxyURL != "" {
		client, err = service.NewProxyHttpClient(proxyURL)
		if err != nil {
			return 0, nil, err
		}
	} else {
		client = service.GetHttpClient()
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}
