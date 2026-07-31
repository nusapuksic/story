package compiler

import (
	"strings"
	"testing"

	"github.com/nusapuksic/story/internal/config"
)

func TestEffectiveVerificationMode(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		cfg  config.CompileConfig
		want string
	}{
		{
			name: "explicit option wins",
			opts: Options{VerificationMode: "selective"},
			cfg:  config.CompileConfig{Verification: true, VerificationMode: VerificationModeAll},
			want: VerificationModeSelective,
		},
		{
			name: "config mode wins over legacy false",
			cfg:  config.CompileConfig{Verification: false, VerificationMode: VerificationModeRecovered},
			want: VerificationModeRecovered,
		},
		{
			name: "legacy true maps to all",
			cfg:  config.CompileConfig{Verification: true},
			want: VerificationModeAll,
		},
		{
			name: "legacy false maps to off",
			cfg:  config.CompileConfig{Verification: false},
			want: VerificationModeOff,
		},
		{
			name: "normalizes case and whitespace",
			opts: Options{VerificationMode: " Selective "},
			want: VerificationModeSelective,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EffectiveVerificationMode(tt.opts, tt.cfg)
			if err != nil {
				t.Fatalf("EffectiveVerificationMode: %v", err)
			}
			if got != tt.want {
				t.Fatalf("mode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEffectiveVerificationModeRejectsUnknown(t *testing.T) {
	_, err := EffectiveVerificationMode(Options{VerificationMode: "sometimes"}, config.CompileConfig{})
	if err == nil {
		t.Fatal("expected unknown verification mode to fail")
	}
	if !strings.Contains(err.Error(), "unknown verification mode") {
		t.Fatalf("error = %v, want unknown verification mode", err)
	}
}

func TestShouldVerifySceneCardForMode(t *testing.T) {
	recovered := SceneCardRecord{Recovery: &SceneCardRecovery{Action: "fallback"}}
	suspicious := SceneCardRecord{Summary: "Short", Evidence: []string{"p-1"}}
	ordinary := SceneCardRecord{Summary: "This scene card has enough detail to avoid selective verification.", Evidence: []string{"p-1"}}

	if !shouldVerifySceneCardForMode(recovered, VerificationModeRecovered) {
		t.Fatal("recovered card should be selected in recovered mode")
	}
	if shouldVerifySceneCardForMode(ordinary, VerificationModeRecovered) {
		t.Fatal("ordinary card should not be selected in recovered mode")
	}
	if !shouldVerifySceneCardForMode(suspicious, VerificationModeSelective) {
		t.Fatal("suspicious card should be selected in selective mode")
	}
	if shouldVerifySceneCardForMode(ordinary, VerificationModeOff) {
		t.Fatal("off mode should not select ordinary card")
	}
	if !shouldVerifySceneCardForMode(ordinary, VerificationModeAll) {
		t.Fatal("all mode should select ordinary card")
	}
}
