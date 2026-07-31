package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateOpenAIQuotaResetResultRequiresExplicitSuccess(t *testing.T) {
	tests := []struct {
		name    string
		payload *OpenAIQuotaResetResult
		wantErr bool
	}{
		{name: "confirmed", payload: &OpenAIQuotaResetResult{Code: "ok", WindowsReset: 2}},
		{name: "empty", payload: &OpenAIQuotaResetResult{}, wantErr: true},
		{name: "business error", payload: &OpenAIQuotaResetResult{Code: "failed", WindowsReset: 2}, wantErr: true},
		{name: "no windows", payload: &OpenAIQuotaResetResult{Code: "ok"}, wantErr: true},
		{name: "nil", payload: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOpenAIQuotaResetResult(tt.payload)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
