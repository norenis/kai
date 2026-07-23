package brain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	DefaultGeminiEmbedModel = "gemini-embedding-001"
	DefaultGeminiDims       = 1536 // MRL-truncated output; kai normalizes after.
	GeminiTimeout           = 30 * time.Second
	geminiMaxRetries        = 8               // free-tier RPM is low → back off & retry
	geminiMaxBackoff        = 90 * time.Second
)

// GeminiEmbedder generates embeddings via Google's Gemini API
// (generativelanguage.googleapis.com). Free tier is generous for a personal brain.
// Note: gemini-embedding-* models expose embedContent (single) — not the old
// sync batchEmbedContents — so EmbedBatch loops single calls.
type GeminiEmbedder struct {
	baseURL string
	model   string // normalized as "models/<name>"
	apiKey  string
	dimsReq int // requested outputDimensionality (0 = model default)
	client  *http.Client
	dims    int // actual dims, cached after first call
}

// GeminiEmbedConfig holds configuration for the Gemini embedder.
type GeminiEmbedConfig struct {
	BaseURL string // API base (default: https://generativelanguage.googleapis.com/v1beta)
	Model   string // Embedding model (default: gemini-embedding-001)
	APIKey  string // Google AI Studio API key
	Dims    int    // outputDimensionality (default: 1536; 0 uses model default)
}

// NewGeminiEmbedder creates a Gemini-based embedder.
func NewGeminiEmbedder(cfg GeminiEmbedConfig) *GeminiEmbedder {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultGeminiBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = DefaultGeminiEmbedModel
	}
	if cfg.Dims == 0 {
		cfg.Dims = DefaultGeminiDims
	}
	model := cfg.Model
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	return &GeminiEmbedder{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   model,
		apiKey:  cfg.APIKey,
		dimsReq: cfg.Dims,
		client:  &http.Client{Timeout: GeminiTimeout},
	}
}

type geminiPart struct {
	Text string `json:"text"`
}
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}
type geminiEmbedRequest struct {
	Model                string        `json:"model"`
	Content              geminiContent `json:"content"`
	OutputDimensionality int           `json:"outputDimensionality,omitempty"`
}
type geminiEmbedding struct {
	Values []float64 `json:"values"`
}
type geminiEmbedResponse struct {
	Embedding geminiEmbedding `json:"embedding"`
}

// geminiErrorBody carries the RetryInfo for 429 backoff.
type geminiErrorBody struct {
	Error struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			Type       string `json:"@type"`
			RetryDelay string `json:"retryDelay"` // e.g. "37s"
		} `json:"details"`
	} `json:"error"`
}

// retryDelayFrom extracts the server-suggested retry delay from a 429 body.
func retryDelayFrom(body []byte) time.Duration {
	var eb geminiErrorBody
	if json.Unmarshal(body, &eb) != nil {
		return 0
	}
	for _, d := range eb.Error.Details {
		if strings.Contains(d.Type, "RetryInfo") && d.RetryDelay != "" {
			if dur, err := time.ParseDuration(d.RetryDelay); err == nil {
				return dur
			}
		}
	}
	return 0
}

// Embed generates an embedding for a single text.
func (e *GeminiEmbedder) Embed(text string) ([]float32, error) {
	return e.embedOne(text)
}

// EmbedBatch embeds multiple texts (one embedContent call each).
func (e *GeminiEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := e.embedOne(t)
		if err != nil {
			return nil, err
		}
		vecs[i] = v
	}
	return vecs, nil
}

func (e *GeminiEmbedder) embedOne(text string) ([]float32, error) {
	reqBody := geminiEmbedRequest{
		Model:                e.model,
		Content:              geminiContent{Parts: []geminiPart{{Text: text}}},
		OutputDimensionality: e.dimsReq,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini embed request: %w", err)
	}
	url := fmt.Sprintf("%s/%s:embedContent?key=%s", e.baseURL, e.model, e.apiKey)

	backoff := 2 * time.Second
	var lastErr error
	for attempt := 0; attempt < geminiMaxRetries; attempt++ {
		resp, err := e.client.Post(url, "application/json", bytes.NewReader(data))
		if err != nil {
			lastErr = fmt.Errorf("gemini embed request: %w", err)
			time.Sleep(backoff)
			backoff = minDur(backoff*2, geminiMaxBackoff)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Rate-limited or transient server error → back off and retry.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			wait := retryDelayFrom(body)
			if wait <= 0 {
				wait = backoff
			}
			wait = minDur(wait+time.Second, geminiMaxBackoff) // pad server hint slightly
			slog.Debug("gemini rate-limited, backing off", "status", resp.StatusCode, "wait", wait, "attempt", attempt+1)
			lastErr = fmt.Errorf("gemini embed: status %d: %s", resp.StatusCode, string(body))
			time.Sleep(wait)
			backoff = minDur(backoff*2, geminiMaxBackoff)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("gemini embed: status %d: %s", resp.StatusCode, string(body))
		}

		var er geminiEmbedResponse
		if err := json.Unmarshal(body, &er); err != nil {
			return nil, fmt.Errorf("decode gemini embed response: %w", err)
		}
		if len(er.Embedding.Values) == 0 {
			return nil, fmt.Errorf("gemini returned empty embedding")
		}
		vec := make([]float32, len(er.Embedding.Values))
		for j, v := range er.Embedding.Values {
			vec[j] = float32(v)
		}
		if e.dims == 0 {
			e.dims = len(vec)
		}
		return vec, nil
	}
	return nil, fmt.Errorf("gemini embed: exhausted retries: %w", lastErr)
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
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
