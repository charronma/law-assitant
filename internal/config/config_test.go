package config

import (
	"reflect"
	"strings"
	"testing"

	"law-assistant/internal/model"
)

// baseEnv sets the variables Load needs and clears the ones under test.
func baseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DASHSCOPE_API_KEY", "sk-test")
	t.Setenv("QWEN_API_KEY", "")
	t.Setenv("UPLOAD_DIR", t.TempDir())
	t.Setenv("QWEN_MODEL", "")
	t.Setenv("QWEN_MODELS", "")
}

func TestLoad_ModelDefaults(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QwenModel != "qwen3.8-max-0902" {
		t.Errorf("default model = %q, want qwen3.8-max-0902", cfg.QwenModel)
	}
	if !reflect.DeepEqual(cfg.QwenModels, model.DefaultIDs()) || len(cfg.QwenModels) != 12 {
		t.Errorf("default allow-list = %v", cfg.QwenModels)
	}
	if cfg.QwenModels[0] != "qwen3.8-max-0902" {
		t.Errorf("the allow-list must keep the recommended order, starting with the default: %v", cfg.QwenModels)
	}
}

func TestLoad_CustomAllowList(t *testing.T) {
	baseEnv(t)
	t.Setenv("QWEN_MODELS", " kimi-k3 , glm-5.3,,kimi-k3 , brand-new-model ")
	t.Setenv("QWEN_MODEL", "glm-5.3")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kimi-k3", "glm-5.3", "brand-new-model"}
	if !reflect.DeepEqual(cfg.QwenModels, want) {
		t.Errorf("allow-list = %v, want %v (trimmed, deduplicated, original order)", cfg.QwenModels, want)
	}
	if cfg.QwenModel != "glm-5.3" {
		t.Errorf("default = %q", cfg.QwenModel)
	}
}

func TestLoad_BlankAllowListFallsBackToBuiltins(t *testing.T) {
	for _, v := range []string{"", "   ", ",,, ,"} {
		baseEnv(t)
		t.Setenv("QWEN_MODELS", v)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("QWEN_MODELS=%q: %v", v, err)
		}
		if len(cfg.QwenModels) != 12 {
			t.Errorf("QWEN_MODELS=%q: got %d models, want the 12 built-ins", v, len(cfg.QwenModels))
		}
	}
}

func TestLoad_DefaultModelMustBeInAllowList(t *testing.T) {
	// QWEN_MODEL outside the list.
	baseEnv(t)
	t.Setenv("QWEN_MODELS", "kimi-k3,glm-5.3")
	t.Setenv("QWEN_MODEL", "qwen-max")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "qwen-max") {
		t.Errorf("want an error naming qwen-max, got %v", err)
	}

	// A custom list that omits the built-in default, with QWEN_MODEL unset,
	// must fail with a message that tells the operator what to do.
	baseEnv(t)
	t.Setenv("QWEN_MODELS", "kimi-k3,glm-5.3")
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error: the built-in default is not in QWEN_MODELS")
	}
	for _, want := range []string{"qwen3.8-max-0902", "QWEN_MODELS", "QWEN_MODEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
}

func TestLoad_AnyAllowedModelCanBeTheDefault(t *testing.T) {
	baseEnv(t)
	t.Setenv("QWEN_MODEL", "qwen3.7-flash")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QwenModel != "qwen3.7-flash" {
		t.Errorf("default = %q", cfg.QwenModel)
	}
}

func TestParseModelList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ", []string{"a", "b"}},
		{"a,,b,", []string{"a", "b"}},
		{"b,a,b,a", []string{"b", "a"}},
	}
	for _, tt := range tests {
		if got := ParseModelList(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseModelList(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
