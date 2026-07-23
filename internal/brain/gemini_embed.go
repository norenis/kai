package brain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Embedder is the interface implemented by neural embedding backends
// (Ollama, Gemini). LSI is handled separately via stored term vectors.
type Embedder interface {
	Embed(text string) ([]float32, error)
	EmbedBatch(texts []string) ([][]float32, error)
	Dims() int
	Available() bool
}

// Default Gemini embedding settings.
const (
	DefaultGeminiBaseURL    = "https://generativelanguage.googleapis.com/v1beta"
	DefaultGeminiEmbedModel = "text-embedding-004"
	GeminiTimeout           = 30 * time.Second
)

// GeminiEmbedder generates embeddings via Google's Gemini API
// (generativelanguage.googleapis.com). Free tier is generous for a personal brain.
type GeminiEmbedder struct {
	baseURL string
	model   string // normalized as "models/<name>"
	apiKey  string
	client  *http.Client
	dims    int // cached after first call
}

// GeminiEmbedConfig holds configuration for the Gemini embedder.
type GeminiEmbedConfig struct {
	BaseURL string // API base (default: https://generativelanguage.googleapis.com/v1beta)
	Model   string // Embedding model (default: text-embedding-004)
	APIKey  string // Google AI Studio API key
}

// NewGeminiEmbedder creates a Gemini-based embedder.
func NewGeminiEmbedder(cfg GeminiEmbedConfig) *GeminiEmbedder {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultGeminiBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = DefaultGeminiEmbedModel
	}
	model := cfg.Model
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	return &GeminiEmbedder{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   model,
		apiKey:  cfg.APIKey,
		client:  &http.Client{Timeout: GeminiTimeout},
	}
}

type geminiPart struct {
	Text string `json:"text"`
}
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}
type geminiEmbedRequestItem struct {
	Model   string        `json:"model"`
	Content geminiContent `json:"content"`
}
type geminiBatchRequest struct {
	Requests []geminiEmbedRequestItem `json:"requests"`
}
type geminiEmbedding struct {
	Values []float64 `json:"values"`
}
type geminiBatchResponse struct {
	Embeddings []geminiEmbedding `json:"embeddings"`
}

// Embed generates an embedding for a single text.
func (e *GeminiEmbedder) Embed(text string) ([]float32, error) {
	vecs, err := e.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// EmbedBatch generates embeddings for multiple texts via batchEmbedContents.
func (e *GeminiEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	reqItems := make([]geminiEmbedRequestItem, len(texts))
	for i, t := range texts {
		reqItems[i] = geminiEmbedRequestItem{
			Model:   e.model,
			Content: geminiContent{Parts: []geminiPart{{Text: t}}},
		}
	}
	data, err := json.Marshal(geminiBatchRequest{Requests: reqItems})
	if err != nil {
		return nil, fmt.Errorf("marshal gemini embed request: %w", err)
	}

	url := fmt.Sprintf("%s/%s:batchEmbedContents?key=%s", e.baseURL, e.model, e.apiKey)
	resp, err := e.client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gemini embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini embed: status %d: %s", resp.StatusCode, string(body))
	}

	var embedResp geminiBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("decode gemini embed response: %w", err)
	}
	if len(embedResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini returned %d embeddings for %d texts", len(embedResp.Embeddings), len(texts))
	}

	vecs := make([][]float32, len(embedResp.Embeddings))
	for i, emb := range embedResp.Embeddings {
		vec := make([]float32, len(emb.Values))
		for j, v := range emb.Values {
			vec[j] = float32(v)
		}
		vecs[i] = vec
		if e.dims == 0 {
			e.dims = len(vec)
		}
	}
	return vecs, nil
}

// Dims returns the embedding dimensionality (known after first Embed call).
func (e *GeminiEmbedder) Dims() int { return e.dims }

// Available checks that an API key is set and the endpoint responds.
func (e *GeminiEmbedder) Available() bool {
	if e.apiKey == "" {
		return false
	}
	_, err := e.Embed("test")
	return err == nil
}
