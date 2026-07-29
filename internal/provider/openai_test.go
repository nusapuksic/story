package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nusapuksic/story/internal/provider"
)

// newFakeServer returns a test server that mimics a minimal OpenAI-compatible
// endpoint.  It always returns the same model and the same completion text.
func newFakeServer(t *testing.T, completionText string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{
					{"id": "test-model", "owned_by": "local"},
				},
			})
		case "/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message":       map[string]string{"content": completionText},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]int{
					"prompt_tokens":     10,
					"completion_tokens": 5,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestOpenAIHealth(t *testing.T) {
	srv := newFakeServer(t, "")
	defer srv.Close()

	p := provider.NewOpenAI(srv.URL, "", 10)
	if err := p.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
}

func TestOpenAIModels(t *testing.T) {
	srv := newFakeServer(t, "")
	defer srv.Close()

	p := provider.NewOpenAI(srv.URL, "", 10)
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "test-model" {
		t.Fatalf("Models() = %v, want [{test-model local}]", models)
	}
}

func TestOpenAIGenerate(t *testing.T) {
	want := `{"boundaries":[]}`
	srv := newFakeServer(t, want)
	defer srv.Close()

	p := provider.NewOpenAI(srv.URL, "", 10)
	resp, err := p.Generate(context.Background(), provider.GenerationRequest{
		Model:    "test-model",
		Messages: []provider.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.Content != want {
		t.Fatalf("Generate() content = %q, want %q", resp.Content, want)
	}
	if resp.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", resp.PromptTokens)
	}
}

func TestOpenAIGenerateOmitsMaxTokensWhenZero(t *testing.T) {
	var requestBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": `{"ok":true}`},
				"finish_reason": "stop",
			}},
		})
	}))
	defer srv.Close()

	p := provider.NewOpenAI(srv.URL, "", 10)
	_, err := p.Generate(context.Background(), provider.GenerationRequest{
		Model:    "test-model",
		Messages: []provider.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if _, ok := requestBody["max_tokens"]; ok {
		t.Fatalf("request body includes max_tokens when MaxTokens is zero: %v", requestBody)
	}
	messages, ok := requestBody["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("request body messages = %v, want one message", requestBody["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("request body first message = %v, want object", messages[0])
	}
	if got := message["role"]; got != "user" {
		t.Fatalf("request body message role = %v, want user", got)
	}
	if got := message["content"]; got != "hello" {
		t.Fatalf("request body message content = %v, want hello", got)
	}
}

func TestOpenAIGenerateJSONModeUsesJSONSchemaResponseFormat(t *testing.T) {
	var requestBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": `{"ok":true}`},
				"finish_reason": "stop",
			}},
		})
	}))
	defer srv.Close()

	p := provider.NewOpenAI(srv.URL, "", 10)
	_, err := p.Generate(context.Background(), provider.GenerationRequest{
		Model:    "test-model",
		Messages: []provider.Message{{Role: "user", Content: "hello"}},
		JSONMode: true,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	format, ok := requestBody["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format = %v, want object", requestBody["response_format"])
	}
	if got := format["type"]; got != "json_schema" {
		t.Fatalf("response_format.type = %v, want json_schema", got)
	}
	jsonSchema, ok := format["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("response_format.json_schema = %v, want object", format["json_schema"])
	}
	if got := jsonSchema["name"]; got != "story_json_response" {
		t.Fatalf("response_format.json_schema.name = %v, want story_json_response", got)
	}
	schema, ok := jsonSchema["schema"].(map[string]any)
	if !ok {
		t.Fatalf("response_format.json_schema.schema = %v, want object", jsonSchema["schema"])
	}
	if got := schema["type"]; got != "object" {
		t.Fatalf("response_format.json_schema.schema.type = %v, want object", got)
	}
	if got := schema["additionalProperties"]; got != true {
		t.Fatalf("response_format.json_schema.schema.additionalProperties = %v, want true", got)
	}
}

func TestOpenAIGenerateJSONModeFallsBackToJSONObjectResponseFormat(t *testing.T) {
	var responseFormats []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		format, ok := requestBody["response_format"].(map[string]any)
		if !ok {
			t.Fatalf("response_format = %v, want object", requestBody["response_format"])
		}
		responseFormats = append(responseFormats, format)
		w.Header().Set("Content-Type", "application/json")
		if len(responseFormats) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "unsupported response_format"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": `{"ok":true}`},
				"finish_reason": "stop",
			}},
		})
	}))
	defer srv.Close()

	p := provider.NewOpenAI(srv.URL, "", 10)
	resp, err := p.Generate(context.Background(), provider.GenerationRequest{
		Model:    "test-model",
		Messages: []provider.Message{{Role: "user", Content: "hello"}},
		JSONMode: true,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.Content != `{"ok":true}` {
		t.Fatalf("Generate() content = %q, want JSON response", resp.Content)
	}
	if len(responseFormats) != 2 {
		t.Fatalf("request count = %d, want 2", len(responseFormats))
	}
	if got := responseFormats[0]["type"]; got != "json_schema" {
		t.Fatalf("first response_format.type = %v, want json_schema", got)
	}
	if got := responseFormats[1]["type"]; got != "json_object" {
		t.Fatalf("second response_format.type = %v, want json_object", got)
	}
}

func TestOpenAIHealthUnreachable(t *testing.T) {
	p := provider.NewOpenAI("http://127.0.0.1:19999", "", 1)
	if err := p.Health(context.Background()); err == nil {
		t.Fatal("expected error for unreachable endpoint")
	}
}
