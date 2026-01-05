package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaClient Ollama embedding 客户端
type OllamaClient struct {
	endpoint  string
	model     string
	dimension int
	client    *http.Client
}

// NewOllamaClient 创建 Ollama 客户端
func NewOllamaClient(endpoint, model string) *OllamaClient {
	return NewOllamaClientWithDimension(endpoint, model, 768)
}

// NewOllamaClientWithDimension 创建指定维度的 Ollama 客户端
func NewOllamaClientWithDimension(endpoint, model string, dimension int) *OllamaClient {
	if model == "" {
		model = "nomic-embed-text"
	}
	if dimension <= 0 {
		dimension = 768 // 默认维度
	}
	return &OllamaClient{
		endpoint:  endpoint,
		model:     model,
		dimension: dimension,
		client: &http.Client{
			Timeout: 120 * time.Second, // 增加超时时间，大文本可能需要更长
		},
	}
}

// EmbeddingRequest Ollama embedding 请求
type EmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// EmbeddingResponse Ollama embedding 响应
type EmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

// GetEmbedding 获取文本的 embedding
func (c *OllamaClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	req := EmbeddingRequest{
		Model:  c.model,
		Prompt: text,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama error: HTTP %d - %s", resp.StatusCode, string(respBody))
	}

	var embResp EmbeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return embResp.Embedding, nil
}

// GetEmbeddings 批量获取 embedding
func (c *OllamaClient) GetEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := c.GetEmbedding(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("get embedding for text %d: %w", i, err)
		}
		embeddings[i] = emb
	}
	return embeddings, nil
}

// GetDimension 获取 embedding 维度
func (c *OllamaClient) GetDimension() int {
	return c.dimension
}
