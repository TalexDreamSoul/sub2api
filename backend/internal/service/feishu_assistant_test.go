//go:build unit

package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeFeishuNaturalCommandRoutesAccountIntentsDeterministically(t *testing.T) {
	tests := map[string]string{
		"帮我申请一个 API Key": "/申请key",
		"我今天用了多少钱":       "/日报",
		"还剩多少周额度":        "/额度",
		"订阅什么时候到期":       "/订阅",
		"账户余额":           "/余额",
	}
	for input, want := range tests {
		require.Equal(t, want, normalizeFeishuNaturalCommand(input), input)
	}
	require.Equal(t, "解释一下计费规则", normalizeFeishuNaturalCommand("解释一下计费规则"))
}

func TestFeishuAssistantToolsCannotSelectAnotherUser(t *testing.T) {
	encoded, err := json.Marshal(feishuAssistantTools())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "user_id")
	require.NotContains(t, string(encoded), "http")
	require.NotContains(t, string(encoded), "sql")
}

func TestFeishuAPIKeyIssuedCardNeverContainsFullCredential(t *testing.T) {
	svc, _ := newFeishuEventTestService()
	key := &APIKey{ID: 7, Name: "Feishu key", Key: "sk-super-secret-1234"}
	encoded, err := json.Marshal(svc.feishuAPIKeyIssuedCard(t.Context(), key))
	require.NoError(t, err)
	body := string(encoded)
	require.NotContains(t, body, key.Key)
	require.Contains(t, body, "1234")
	require.Contains(t, body, "不会发送到飞书")
}

func TestValidateFeishuAssistantConfigRejectsUnsafeOrIncompleteConfiguration(t *testing.T) {
	valid := defaultFeishuAssistantConfig()
	valid.Enabled = true
	valid.APIKeyID = 8
	valid.Model = "gpt-5.5"
	require.NoError(t, validateFeishuAssistantConfig(valid))

	missingKey := valid
	missingKey.APIKeyID = 0
	require.ErrorContains(t, validateFeishuAssistantConfig(missingKey), "API key")

	badTime := valid
	badTime.DailyDigestTime = "25:61"
	require.ErrorContains(t, validateFeishuAssistantConfig(badTime), "HH:MM")

	badMode := valid
	badMode.APIKeyRequestMode = "unrestricted"
	require.ErrorContains(t, validateFeishuAssistantConfig(badMode), "request mode")
}

func TestTruncateFeishuAssistantTextPreservesRuneBoundary(t *testing.T) {
	got := truncateFeishuAssistantText(strings.Repeat("额", 10), 5)
	require.Equal(t, "额额额额…", got)
}

func TestCallFeishuAssistantModelUsesOpenAICompatibleEndpoint(t *testing.T) {
	var receivedPath string
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"get_account_overview","arguments":"{}"}}]}}]}`))
	}))
	defer server.Close()

	response, err := callFeishuAssistantModel(t.Context(), server.URL+"/v1", "secret-token", "test-model", []map[string]any{{"role": "user", "content": "余额"}}, feishuAssistantTools())
	require.NoError(t, err)
	require.Equal(t, "/v1/chat/completions", receivedPath)
	require.Equal(t, "Bearer secret-token", receivedAuth)
	require.Equal(t, "get_account_overview", response.Choices[0].Message.ToolCalls[0].Function.Name)
}
