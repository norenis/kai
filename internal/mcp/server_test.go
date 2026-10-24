package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/config"
	"github.com/norenis/kai/internal/prompt"
	"github.com/norenis/kai/internal/provider"
	"github.com/norenis/kai/internal/retriever"
	"github.com/norenis/kai/internal/router"
	"github.com/norenis/kai/internal/session"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

type mockProvider struct{}

func (m *mockProvider) Chat(ctx context.Context, systemPrompt string, messages []provider.Message, opts ...provider.Option) (*provider.Response, error) {
	lastMsg := messages[len(messages)-1]
	if len(lastMsg.Content) > 50 && strings.HasPrefix(lastMsg.Content, "Given") {
		learnings := []brain.Learning{
			{
				Category:   "opinions",
				Topic:      "testing",
				Tags:       []string{"testing", "tdd"},
				Content:    "Kyle loves TDD.",
				Confidence: "high",
				Action:     "create",
			},
		}
		data, _ := json.Marshal(learnings)
		return &provider.Response{Content: string(data)}, nil
	}
	return &provider.Response{Content: "mock response"}, nil
}

func (m *mockProvider) Stream(ctx context.Context, systemPrompt string, messages []provider.Message, opts ...provider.Option) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Content: "mock", Done: true}
	close(ch)
	return ch, nil
}

type emptyLearningProvider struct{}

func (m *emptyLearningProvider) Chat(ctx context.Context, systemPrompt string, messages []provider.Message, opts ...provider.Option) (*provider.Response, error) {
	return &provider.Response{Content: "[]"}, nil
}

func (m *emptyLearningProvider) Stream(ctx context.Context, systemPrompt string, messages []provider.Message, opts ...provider.Option) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Content: "mock", Done: true}
	close(ch)
	return ch, nil
}

func setupTestServer(t *testing.T) (*Server, *brain.FileBrain) {
	t.Helper()

	brainDir := t.TempDir()
	sessDir := t.TempDir()

	b := brain.New(brainDir)

	os.MkdirAll(filepath.Join(brainDir, "identity"), 0755)
	b.Save(&brain.BrainFile{
		Path:       "identity/profile.md",
		Tags:       []string{"identity", "background"},
		Confidence: "high",
		Source:     "direct",
		Updated:    brain.DateOnly{Time: time.Now()},
		Content:    "\nKyle is a software engineer who loves Go.\n",
	})
	b.RebuildIndex()

	prov := &mockProvider{}
	sessMgr := session.NewManager(sessDir)
	ret := retriever.New(b, 10)
	r := router.New(b, ret, prompt.NewBuilder("kai", "TestUser"), prov, sessMgr)

	cfg := &config.Config{
		Provider: "mock",
		Brain:    config.BrainConfig{Path: brainDir, MaxContextFiles: 10},
	}

	srv := NewServer(r, b, sessMgr, prov, cfg)
	return srv, b
}

func TestBrainListHandler(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := mcplib.CallToolRequest{}
	req.Params.Name = "brain_list"

	result, err := srv.handleBrainList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleBrainList error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text

	var files []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &files); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
	if files[0]["path"] != "identity/profile.md" {
		t.Errorf("expected identity/profile.md, got %v", files[0]["path"])
	}
}

func TestBrainReadHandler(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := mcplib.CallToolRequest{}
	req.Params.Name = "brain_read"
	req.Params.Arguments = map[string]interface{}{
		"path": "identity/profile.md",
	}

	result, err := srv.handleBrainRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleBrainRead error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text

	var file map[string]interface{}
	if err := json.Unmarshal([]byte(text), &file); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if file["path"] != "identity/profile.md" {
		t.Errorf("unexpected path: %v", file["path"])
	}
	content, ok := file["content"].(string)
	if !ok || content == "" {
		t.Error("expected non-empty content")
	}
}

func TestBrainReadHandler_NotFound(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := mcplib.CallToolRequest{}
	req.Params.Name = "brain_read"
	req.Params.Arguments = map[string]interface{}{
		"path": "nonexistent/file.md",
	}

	result, err := srv.handleBrainRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleBrainRead error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for missing file")
	}
}

func TestBrainSearchHandler(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := mcplib.CallToolRequest{}
	req.Params.Name = "brain_search"
	req.Params.Arguments = map[string]interface{}{
		"query": "identity background",
	}

	result, err := srv.handleBrainSearch(context.Background(), req)
	if err != nil {
		t.Fatalf("handleBrainSearch error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text

	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected at least one result")
	}
	if results[0]["path"] != "identity/profile.md" {
		t.Errorf("expected identity/profile.md, got %v", results[0]["path"])
	}
	preview, ok := results[0]["preview"].(string)
	if !ok || preview == "" {
		t.Error("expected non-empty preview")
	}
}

func TestBrainSearchHandler_NoResults(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := mcplib.CallToolRequest{}
	req.Params.Name = "brain_search"
	req.Params.Arguments = map[string]interface{}{
		"query": "nonexistent topic xyz",
	}

	result, err := srv.handleBrainSearch(context.Background(), req)
	if err != nil {
		t.Fatalf("handleBrainSearch error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text

	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestAskHandler(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := mcplib.CallToolRequest{}
	req.Params.Name = "ask"
	req.Params.Arguments = map[string]interface{}{
		"question": "What do I think about Go?",
	}

	result, err := srv.handleAsk(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAsk error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["response"] == nil || resp["response"] == "" {
		t.Error("expected non-empty response")
	}
	if resp["confidence"] == nil {
		t.Error("expected confidence field")
	}
	if resp["brain_files_used"] == nil {
		t.Error("expected brain_files_used field")
	}
}

func TestTeachHandler(t *testing.T) {
	srv, b := setupTestServer(t)

	req := mcplib.CallToolRequest{}
	req.Params.Name = "teach"
	req.Params.Arguments = map[string]interface{}{
		"input": "I really love test-driven development for all business logic",
	}

	result, err := srv.handleTeach(context.Background(), req)
	if err != nil {
		t.Fatalf("handleTeach error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "opinions/testing.md") {
		t.Errorf("expected output to mention saved file, got: %s", text)
	}

	bf, err := b.Load("opinions/testing.md")
	if err != nil {
		t.Fatalf("expected opinions/testing.md to exist: %v", err)
	}
	if bf.Confidence != "high" {
		t.Errorf("expected high confidence, got %s", bf.Confidence)
	}
}

func TestTeachHandler_NoLearnings(t *testing.T) {
	srv, _ := setupTestServer(t)

	srv.provider = &emptyLearningProvider{}

	req := mcplib.CallToolRequest{}
	req.Params.Name = "teach"
	req.Params.Arguments = map[string]interface{}{
		"input": "hello",
	}

	result, err := srv.handleTeach(context.Background(), req)
	if err != nil {
		t.Fatalf("handleTeach error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "couldn't extract") {
		t.Errorf("expected extraction failure message, got: %s", text)
	}
}

func TestVaultPathTricksNeedPin(t *testing.T) {
	srv, b := setupTestServer(t)
	srv.cfg.Brain.VaultPin = "4321"
	b.Save(&brain.BrainFile{Path: "vault/bank.md", Tags: []string{"bank"}, Confidence: "high", Source: "direct",
		Updated: brain.DateOnly{Time: time.Now()}, Content: "\nAccount 0000.\n"})

	for _, p := range []string{"vault/bank.md", "knowledge/../vault/bank.md", "./vault/bank.md"} {
		req := mcplib.CallToolRequest{}
		req.Params.Name = "brain_read"
		req.Params.Arguments = map[string]interface{}{"path": p}
		result, err := srv.handleBrainRead(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if text := result.Content[0].(mcplib.TextContent).Text; strings.Contains(text, "0000") {
			t.Errorf("brain_read %q without pin returned vault content", p)
		}

		w := mcplib.CallToolRequest{}
		w.Params.Name = "brain_write"
		w.Params.Arguments = map[string]interface{}{"path": "knowledge/../vault/new.md", "content": "x", "tags": "x"}
		result, err = srv.handleBrainWrite(context.Background(), w)
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Error("brain_write into the vault through ../ succeeded without a pin")
		}
	}
}
