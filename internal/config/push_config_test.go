package config

import "testing"

func TestPushConfigurationValidateRequiresCredentialsWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := PushConfiguration{
		FCM: PushFCMConfiguration{
			Enabled: true,
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error when credentials are missing")
	}
}

func TestPushConfigurationValidateAllowsDisabledFCM(t *testing.T) {
	t.Parallel()

	cfg := PushConfiguration{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
