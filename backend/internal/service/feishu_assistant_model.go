package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const feishuAssistantMaxReplyRunes = 1800

type feishuAssistantToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type feishuAssistantChatResponse struct {
	Choices []struct {
		Message struct {
			Role      string                    `json:"role"`
			Content   string                    `json:"content"`
			ToolCalls []feishuAssistantToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func (s *FeishuNotificationService) renderFeishuAssistantReply(ctx context.Context, binding *FeishuUserIdentityBinding, input string) (string, error) {
	cfg, err := s.GetAssistantConfig(ctx)
	if err != nil || !cfg.Enabled {
		return feishuBotHelpText(), nil
	}
	if s.apiKeyService == nil || binding == nil || binding.UserID <= 0 {
		return "智能助手暂不可用，请发送 /帮助 使用确定性命令。", nil
	}
	key, err := s.apiKeyService.GetByID(ctx, cfg.APIKeyID)
	if err != nil || key == nil || !key.IsActive() {
		return "智能助手配置的 API Key 不可用，请发送 /帮助 使用确定性命令。", nil
	}
	baseURL, err := s.settingRepo.GetValue(ctx, SettingKeyAPIBaseURL)
	if err != nil || strings.TrimSpace(baseURL) == "" {
		return "智能助手暂不可用，请发送 /帮助 使用确定性命令。", nil
	}
	messages := []map[string]any{
		{"role": "system", "content": "你是 Sub2API 飞书账户助手。只能依据工具结果回答当前绑定用户的账户、订阅、额度、API Key 脱敏信息、今日用量和渠道状态。不得猜测，不得索取或输出完整 API Key、令牌、open_id、union_id。涉及申请 API Key 时提示用户发送 /申请key。回答简洁，使用中文纯文本。"},
		{"role": "user", "content": truncateFeishuAssistantText(input, 2000)},
	}
	tools := feishuAssistantTools()
	usedTrustedTool := false
	for round := 0; round < 2; round++ {
		response, callErr := callFeishuAssistantModel(ctx, strings.TrimSpace(baseURL), key.Key, cfg.Model, messages, tools)
		if callErr != nil {
			slog.Warn("feishu assistant model call failed", "user_id", binding.UserID, "error", callErr)
			return "智能助手暂时不可用。你仍可发送 /概览、/额度、/订阅、/key 或 /日报 查询。", nil
		}
		if len(response.Choices) == 0 {
			return "智能助手没有返回有效结果，请发送 /帮助 使用确定性命令。", nil
		}
		message := response.Choices[0].Message
		if len(message.ToolCalls) == 0 {
			if !usedTrustedTool {
				return "无法从可信账户数据中得出答案。请发送 /概览、/额度、/订阅、/key、/日报 或 /渠道。", nil
			}
			content := strings.TrimSpace(message.Content)
			if content == "" {
				return "没有可展示的结果，请换一种问法。", nil
			}
			return truncateFeishuAssistantText(content, feishuAssistantMaxReplyRunes), nil
		}
		usedTrustedTool = true
		assistantMessage := map[string]any{"role": "assistant", "content": message.Content, "tool_calls": message.ToolCalls}
		messages = append(messages, assistantMessage)
		for _, toolCall := range message.ToolCalls {
			result, toolErr := s.executeFeishuAssistantTool(ctx, binding.UserID, toolCall.Function.Name)
			if toolErr != nil {
				result = "查询失败，请稍后重试。"
			}
			messages = append(messages, map[string]any{
				"role": "tool", "tool_call_id": toolCall.ID,
				"content": truncateFeishuAssistantText(result, 4000),
			})
		}
	}
	return "查询步骤过多，请改用 /概览、/额度、/订阅、/key 或 /日报。", nil
}

func (s *FeishuNotificationService) executeFeishuAssistantTool(ctx context.Context, userID int64, name string) (string, error) {
	switch strings.TrimSpace(name) {
	case "get_account_overview":
		return s.renderFeishuBalance(ctx, userID)
	case "list_subscriptions":
		return s.renderFeishuSubscriptions(ctx, userID, false)
	case "get_subscription_quota":
		return s.renderFeishuSubscriptions(ctx, userID, true)
	case "list_api_keys_masked":
		return s.renderFeishuAPIKeys(ctx, userID)
	case "get_today_usage", "get_my_usage_rank":
		return s.renderFeishuDailyUsage(ctx, userID)
	case "get_channel_status":
		return s.renderFeishuChannels(ctx)
	case "get_notification_preferences":
		preferences, err := s.GetPreferences(ctx, userID)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(preferences)
		return string(encoded), err
	default:
		return "", fmt.Errorf("unsupported Feishu assistant tool %q", name)
	}
}

func feishuAssistantTools() []map[string]any {
	names := []struct {
		name        string
		description string
	}{
		{"get_account_overview", "查询当前绑定用户的余额和并发上限"},
		{"list_subscriptions", "查询当前绑定用户的有效订阅和到期时间"},
		{"get_subscription_quota", "查询当前绑定用户订阅的日、周、月额度和剩余额度"},
		{"list_api_keys_masked", "查询当前绑定用户的 API Key 列表，只返回名称、状态和尾号"},
		{"get_today_usage", "查询当前绑定用户今日消费、请求数、Token 和排名"},
		{"get_my_usage_rank", "查询当前绑定用户今日全站排名和消费占比"},
		{"get_channel_status", "查询全局渠道监控状态"},
		{"get_notification_preferences", "查询当前绑定用户的飞书通知偏好"},
	}
	tools := make([]map[string]any, 0, len(names))
	for _, item := range names {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": item.name, "description": item.description,
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
			},
		})
	}
	return tools
}

func callFeishuAssistantModel(ctx context.Context, baseURL, apiKey, model string, messages, tools any) (*feishuAssistantChatResponse, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	endpoint := baseURL + "/v1/chat/completions"
	payload, err := json.Marshal(map[string]any{
		"model": model, "messages": messages, "tools": tools,
		"tool_choice": "auto", "temperature": 0, "max_tokens": 800, "stream": false,
	})
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 22 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, fmt.Errorf("assistant model returned HTTP %d", resp.StatusCode)
	}
	var result feishuAssistantChatResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode assistant response: %w", err)
	}
	return &result, nil
}

func truncateFeishuAssistantText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes-1]) + "…"
}

func (s *FeishuNotificationService) TestAssistantModel(ctx context.Context) error {
	cfg, err := s.GetAssistantConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled || s.apiKeyService == nil {
		return fmt.Errorf("feishu assistant is not enabled")
	}
	key, err := s.apiKeyService.GetByID(ctx, cfg.APIKeyID)
	if err != nil || key == nil || !key.IsActive() {
		return fmt.Errorf("assistant API key is missing or inactive")
	}
	baseURL, err := s.settingRepo.GetValue(ctx, SettingKeyAPIBaseURL)
	if err != nil {
		return err
	}
	tools := []map[string]any{{
		"type": "function", "function": map[string]any{
			"name": "assistant_health_check", "description": "必须调用此工具完成健康检查",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
	}}
	messages := []map[string]any{{"role": "user", "content": "请调用 assistant_health_check 工具。"}}
	response, err := callFeishuAssistantModel(ctx, baseURL, key.Key, cfg.Model, messages, tools)
	if err != nil {
		return err
	}
	if len(response.Choices) == 0 || len(response.Choices[0].Message.ToolCalls) == 0 || response.Choices[0].Message.ToolCalls[0].Function.Name != "assistant_health_check" {
		return fmt.Errorf("model did not return the required Function Calling result")
	}
	return nil
}
