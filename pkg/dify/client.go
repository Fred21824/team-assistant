package dify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client Dify API 客户端
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient 创建 Dify 客户端
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// ChatRequest 对话请求
type ChatRequest struct {
	Query          string                 `json:"query"`
	User           string                 `json:"user"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	Inputs         map[string]interface{} `json:"inputs,omitempty"`
	ResponseMode   string                 `json:"response_mode"` // blocking 或 streaming
}

// ChatResponse 对话响应
type ChatResponse struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Answer         string `json:"answer"`
	CreatedAt      int64  `json:"created_at"`
	Metadata       struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	} `json:"metadata"`
}

// Chat 发送对话请求
func (c *Client) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if req.ResponseMode == "" {
		req.ResponseMode = "blocking"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat-messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dify api error: %s, body: %s", resp.Status, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &chatResp, nil
}

// WorkflowRequest 工作流执行请求
type WorkflowRequest struct {
	Inputs       map[string]interface{} `json:"inputs"`
	User         string                 `json:"user"`
	ResponseMode string                 `json:"response_mode"` // blocking 或 streaming
}

// WorkflowResponse 工作流响应
type WorkflowResponse struct {
	WorkflowRunID string                 `json:"workflow_run_id"`
	TaskID        string                 `json:"task_id"`
	Data          map[string]interface{} `json:"data"`
}

// RunWorkflow 执行工作流
func (c *Client) RunWorkflow(ctx context.Context, req *WorkflowRequest) (*WorkflowResponse, error) {
	if req.ResponseMode == "" {
		req.ResponseMode = "blocking"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/workflows/run", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dify api error: %s, body: %s", resp.Status, string(respBody))
	}

	var workflowResp WorkflowResponse
	if err := json.Unmarshal(respBody, &workflowResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &workflowResp, nil
}

// KnowledgeSearchRequest 知识库搜索请求
type KnowledgeSearchRequest struct {
	Query          string `json:"query"`
	TopK           int    `json:"top_k,omitempty"`
	ScoreThreshold float64 `json:"score_threshold,omitempty"`
}

// KnowledgeSearchResult 知识库搜索结果
type KnowledgeSearchResult struct {
	Records []struct {
		Segment struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			Score   float64 `json:"score"`
		} `json:"segment"`
		Document struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"document"`
	} `json:"records"`
}

// SearchKnowledge 搜索知识库
func (c *Client) SearchKnowledge(ctx context.Context, datasetID string, req *KnowledgeSearchRequest) (*KnowledgeSearchResult, error) {
	if req.TopK == 0 {
		req.TopK = 5
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/datasets/%s/retrieve", c.baseURL, datasetID)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dify api error: %s, body: %s", resp.Status, string(respBody))
	}

	var searchResult KnowledgeSearchResult
	if err := json.Unmarshal(respBody, &searchResult); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &searchResult, nil
}

// DocumentCreateRequest 创建文档请求
type DocumentCreateRequest struct {
	Name              string `json:"name"`
	Text              string `json:"text"`
	IndexingTechnique string `json:"indexing_technique"` // high_quality 或 economy
	ProcessRule       struct {
		Mode  string `json:"mode"` // automatic 或 custom
		Rules struct {
			PreProcessingRules []struct {
				ID      string `json:"id"`
				Enabled bool   `json:"enabled"`
			} `json:"pre_processing_rules,omitempty"`
			Segmentation struct {
				Separator  string `json:"separator,omitempty"`
				MaxTokens  int    `json:"max_tokens,omitempty"`
			} `json:"segmentation,omitempty"`
		} `json:"rules,omitempty"`
	} `json:"process_rule"`
}

// DocumentCreateResponse 创建文档响应
type DocumentCreateResponse struct {
	Document struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		IndexingStatus string `json:"indexing_status"`
	} `json:"document"`
	Batch string `json:"batch"`
}

// CreateDocumentByText 通过文本创建文档
func (c *Client) CreateDocumentByText(ctx context.Context, datasetID, name, text string) (*DocumentCreateResponse, error) {
	req := map[string]interface{}{
		"name":               name,
		"text":               text,
		"indexing_technique": "high_quality",
		"process_rule": map[string]interface{}{
			"mode": "automatic",
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/datasets/%s/document/create_by_text", c.baseURL, datasetID)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("dify api error: %s, body: %s", resp.Status, string(respBody))
	}

	var docResp DocumentCreateResponse
	if err := json.Unmarshal(respBody, &docResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &docResp, nil
}

// UpdateDocumentByText 更新文档
func (c *Client) UpdateDocumentByText(ctx context.Context, datasetID, documentID, name, text string) error {
	req := map[string]interface{}{
		"name": name,
		"text": text,
		"process_rule": map[string]interface{}{
			"mode": "automatic",
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/datasets/%s/documents/%s/update_by_text", c.baseURL, datasetID, documentID)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dify api error: %s, body: %s", resp.Status, string(respBody))
	}

	return nil
}

// DeleteDocument 删除文档
func (c *Client) DeleteDocument(ctx context.Context, datasetID, documentID string) error {
	url := fmt.Sprintf("%s/v1/datasets/%s/documents/%s", c.baseURL, datasetID, documentID)
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dify api error: %s, body: %s", resp.Status, string(respBody))
	}

	return nil
}

// ListDocuments 列出知识库中的文档
func (c *Client) ListDocuments(ctx context.Context, datasetID string, page, limit int) ([]map[string]interface{}, error) {
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = 20
	}

	url := fmt.Sprintf("%s/v1/datasets/%s/documents?page=%d&limit=%d", c.baseURL, datasetID, page, limit)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dify api error: %s, body: %s", resp.Status, string(respBody))
	}

	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return result.Data, nil
}
