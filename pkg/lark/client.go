package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Client struct {
	domain    string
	appID     string
	appSecret string

	token     string
	tokenLock sync.RWMutex
	expireAt  time.Time
}

func NewClient(domain, appID, appSecret string) *Client {
	return &Client{
		domain:    domain,
		appID:     appID,
		appSecret: appSecret,
	}
}

// GetTenantAccessToken 获取 tenant_access_token
func (c *Client) GetTenantAccessToken(ctx context.Context) (string, error) {
	c.tokenLock.RLock()
	if c.token != "" && time.Now().Before(c.expireAt) {
		token := c.token
		c.tokenLock.RUnlock()
		return token, nil
	}
	c.tokenLock.RUnlock()

	c.tokenLock.Lock()
	defer c.tokenLock.Unlock()

	// Double check
	if c.token != "" && time.Now().Before(c.expireAt) {
		return c.token, nil
	}

	url := fmt.Sprintf("%s/open-apis/auth/v3/tenant_access_token/internal", c.domain)
	body := map[string]string{
		"app_id":     c.appID,
		"app_secret": c.appSecret,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}

	if result.Code != 0 {
		return "", fmt.Errorf("get token failed: %s", result.Msg)
	}

	c.token = result.TenantAccessToken
	c.expireAt = time.Now().Add(time.Duration(result.Expire-300) * time.Second) // 提前5分钟过期

	return c.token, nil
}

// SendMessage 发送消息到群
func (c *Client) SendMessage(ctx context.Context, chatID, msgType, content string) error {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/open-apis/im/v1/messages?receive_id_type=chat_id", c.domain)

	contentJSON, _ := json.Marshal(map[string]string{"text": content})
	body := map[string]string{
		"receive_id": chatID,
		"msg_type":   msgType,
		"content":    string(contentJSON),
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return err
	}

	if result.Code != 0 {
		return fmt.Errorf("send message failed: %s", result.Msg)
	}

	return nil
}

// ReplyMessage 回复消息
func (c *Client) ReplyMessage(ctx context.Context, messageID, msgType, content string) error {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/open-apis/im/v1/messages/%s/reply", c.domain, messageID)

	contentJSON, _ := json.Marshal(map[string]string{"text": content})
	body := map[string]string{
		"msg_type": msgType,
		"content":  string(contentJSON),
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return err
	}

	if result.Code != 0 {
		return fmt.Errorf("reply message failed: %s", result.Msg)
	}

	return nil
}

// GetChatMessages 获取群消息
func (c *Client) GetChatMessages(ctx context.Context, chatID string, pageSize int, pageToken string) (*GetMessagesResponse, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/im/v1/messages?container_id_type=chat&container_id=%s&page_size=%d",
		c.domain, chatID, pageSize)
	if pageToken != "" {
		url += "&page_token=" + pageToken
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result GetMessagesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("get messages failed: %s", result.Msg)
	}

	return &result, nil
}

// GetChats 获取机器人加入的群列表
func (c *Client) GetChats(ctx context.Context) ([]*ChatInfo, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/im/v1/chats?page_size=100", c.domain)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []*ChatInfo `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("get chats failed: %s", result.Msg)
	}

	return result.Data.Items, nil
}

type GetMessagesResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Items     []*MessageItem `json:"items"`
		HasMore   bool           `json:"has_more"`
		PageToken string         `json:"page_token"`
	} `json:"data"`
}

type MessageItem struct {
	MessageID   string `json:"message_id"`
	ThreadID    string `json:"thread_id"`
	RootID      string `json:"root_id"`
	ParentID    string `json:"parent_id"`
	MsgType     string `json:"msg_type"`
	CreateTime  string `json:"create_time"`
	UpdateTime  string `json:"update_time"`
	Deleted     bool   `json:"deleted"`
	ChatID      string `json:"chat_id"`
	Sender      struct {
		ID         string `json:"id"`
		IDType     string `json:"id_type"`
		SenderType string `json:"sender_type"`
		TenantKey  string `json:"tenant_key"`
	} `json:"sender"`
	Body struct {
		Content string `json:"content"`
	} `json:"body"`
	Mentions []struct {
		ID      string `json:"id"`
		IDType  string `json:"id_type"`
		Key     string `json:"key"`
		Name    string `json:"name"`
		OpenID  string `json:"open_id"`
		UserID  string `json:"user_id"`
		UnionID string `json:"union_id"`
	} `json:"mentions"`
}

type ChatInfo struct {
	ChatID      string `json:"chat_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerID     string `json:"owner_id"`
	MemberCount int    `json:"user_count"`
}

// UserInfo 用户信息
type UserInfo struct {
	OpenID    string `json:"open_id"`
	UserID    string `json:"user_id"`
	UnionID   string `json:"union_id"`
	Name      string `json:"name"`
	EnName    string `json:"en_name"`
	Nickname  string `json:"nickname"`
	Email     string `json:"email"`
	Mobile    string `json:"mobile"`
	AvatarURL string `json:"avatar_url"`
}

// GetUserInfo 获取用户信息
func (c *Client) GetUserInfo(ctx context.Context, openID string) (*UserInfo, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/contact/v3/users/%s?user_id_type=open_id", c.domain, openID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			User *UserInfo `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("get user info failed: %s", result.Msg)
	}

	return result.Data.User, nil
}

// SendMessageToUser 发送消息给用户（私聊）
func (c *Client) SendMessageToUser(ctx context.Context, openID, msgType, content string) error {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/open-apis/im/v1/messages?receive_id_type=open_id", c.domain)

	contentJSON, _ := json.Marshal(map[string]string{"text": content})
	body := map[string]string{
		"receive_id": openID,
		"msg_type":   msgType,
		"content":    string(contentJSON),
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return err
	}

	if result.Code != 0 {
		return fmt.Errorf("send message to user failed: %s", result.Msg)
	}

	return nil
}

// DownloadMessageResource 下载消息中的资源文件（图片、文件等）
// 使用 /im/v1/messages/{message_id}/resources/{file_key} 接口
func (c *Client) DownloadMessageResource(ctx context.Context, messageID, fileKey, resourceType string) ([]byte, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	// 使用正确的 API: 获取消息中的资源文件
	// resourceType: image 或 file
	url := fmt.Sprintf("%s/open-apis/im/v1/messages/%s/resources/%s?type=%s",
		c.domain, messageID, fileKey, resourceType)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download resource failed: status=%d, body=%s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read resource data failed: %w", err)
	}

	return data, nil
}

// DownloadImage 下载图片（保留旧接口兼容，但推荐使用 DownloadMessageResource）
func (c *Client) DownloadImage(ctx context.Context, messageID, imageKey string) ([]byte, error) {
	return c.DownloadMessageResource(ctx, messageID, imageKey, "image")
}

// ChatMember 群成员信息
type ChatMember struct {
	MemberID     string `json:"member_id"`
	MemberIDType string `json:"member_id_type"`
	Name         string `json:"name"`
	TenantKey    string `json:"tenant_key"`
}

// GetChatMembers 获取群成员列表（用于获取用户名称）
func (c *Client) GetChatMembers(ctx context.Context, chatID string) (map[string]string, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	members := make(map[string]string) // open_id -> name
	pageToken := ""

	for {
		url := fmt.Sprintf("%s/open-apis/im/v1/chats/%s/members?member_id_type=open_id&page_size=100",
			c.domain, chatID)
		if pageToken != "" {
			url += "&page_token=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				Items     []ChatMember `json:"items"`
				HasMore   bool         `json:"has_more"`
				PageToken string       `json:"page_token"`
			} `json:"data"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, err
		}

		if result.Code != 0 {
			return nil, fmt.Errorf("get chat members failed: %s", result.Msg)
		}

		for _, m := range result.Data.Items {
			members[m.MemberID] = m.Name
		}

		if !result.Data.HasMore {
			break
		}
		pageToken = result.Data.PageToken
	}

	return members, nil
}

// GetChatInfo 获取单个群的信息（包括成员数）
func (c *Client) GetChatInfo(ctx context.Context, chatID string) (*ChatInfo, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/im/v1/chats/%s", c.domain, chatID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data *ChatInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("get chat info failed: %s", result.Msg)
	}

	return result.Data, nil
}

// ======================== Bitable (多维表格) API ========================

// BitableRecord 多维表格记录
type BitableRecord struct {
	RecordID string                 `json:"record_id"`
	Fields   map[string]interface{} `json:"fields"`
}

// BitableSearchResult 多维表格搜索结果
type BitableSearchResult struct {
	Total    int              `json:"total"`
	HasMore  bool             `json:"has_more"`
	Records  []*BitableRecord `json:"records"`
	PageToken string          `json:"page_token"`
}

// GetBitableRecords 获取多维表格记录
func (c *Client) GetBitableRecords(ctx context.Context, appToken, tableID string, pageSize int, pageToken string) (*BitableSearchResult, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables/%s/records?page_size=%d",
		c.domain, appToken, tableID, pageSize)
	if pageToken != "" {
		url += "&page_token=" + pageToken
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Total     int              `json:"total"`
			HasMore   bool             `json:"has_more"`
			Items     []*BitableRecord `json:"items"`
			PageToken string           `json:"page_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("get bitable records failed: %s", result.Msg)
	}

	return &BitableSearchResult{
		Total:     result.Data.Total,
		HasMore:   result.Data.HasMore,
		Records:   result.Data.Items,
		PageToken: result.Data.PageToken,
	}, nil
}

// SearchBitableRecords 搜索多维表格记录（支持筛选条件）
func (c *Client) SearchBitableRecords(ctx context.Context, appToken, tableID string, filter string, pageSize int) (*BitableSearchResult, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables/%s/records/search",
		c.domain, appToken, tableID)

	body := map[string]interface{}{
		"page_size": pageSize,
	}
	if filter != "" {
		body["filter"] = map[string]interface{}{
			"conjunction": "and",
			"conditions": []map[string]interface{}{
				{
					"field_name": "站点前缀",
					"operator":   "contains",
					"value":      []string{filter},
				},
			},
		}
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Total     int              `json:"total"`
			HasMore   bool             `json:"has_more"`
			Items     []*BitableRecord `json:"items"`
			PageToken string           `json:"page_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("search bitable records failed: %s", result.Msg)
	}

	return &BitableSearchResult{
		Total:    result.Data.Total,
		HasMore:  result.Data.HasMore,
		Records:  result.Data.Items,
	}, nil
}

// GetSiteInfoByPrefix 根据站点前缀查询站点信息
func (c *Client) GetSiteInfoByPrefix(ctx context.Context, appToken, tableID, prefix string) (*BitableRecord, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables/%s/records/search",
		c.domain, appToken, tableID)

	body := map[string]interface{}{
		"page_size": 1,
		"filter": map[string]interface{}{
			"conjunction": "and",
			"conditions": []map[string]interface{}{
				{
					"field_name": "站点前缀",
					"operator":   "is",
					"value":      []string{prefix},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []*BitableRecord `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("search site info failed: %s", result.Msg)
	}

	if len(result.Data.Items) == 0 {
		return nil, nil
	}

	return result.Data.Items[0], nil
}

// BitableField 多维表格字段信息
type BitableField struct {
	FieldID   string `json:"field_id"`
	FieldName string `json:"field_name"`
	Type      int    `json:"type"`
}

// GetBitableFields 获取多维表格字段列表
func (c *Client) GetBitableFields(ctx context.Context, appToken, tableID string) ([]*BitableField, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables/%s/fields",
		c.domain, appToken, tableID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []*BitableField `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("get bitable fields failed: %s", result.Msg)
	}

	return result.Data.Items, nil
}

// GetSiteInfoBySiteID 根据站点ID查询站点信息
func (c *Client) GetSiteInfoBySiteID(ctx context.Context, appToken, tableID, siteID string) (*BitableRecord, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables/%s/records/search",
		c.domain, appToken, tableID)

	body := map[string]interface{}{
		"page_size": 1,
		"filter": map[string]interface{}{
			"conjunction": "and",
			"conditions": []map[string]interface{}{
				{
					"field_name": "站点ID",
					"operator":   "is",
					"value":      []string{siteID},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []*BitableRecord `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("search site info by ID failed: %s", result.Msg)
	}

	if len(result.Data.Items) == 0 {
		return nil, nil
	}

	return result.Data.Items[0], nil
}

// GetChatHistory 获取群聊历史消息（支持时间范围）
func (c *Client) GetChatHistory(ctx context.Context, chatID string, startTime, endTime string, pageSize int, pageToken string) (*GetMessagesResponse, error) {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/open-apis/im/v1/messages?container_id_type=chat&container_id=%s&page_size=%d",
		c.domain, chatID, pageSize)
	if startTime != "" {
		url += "&start_time=" + startTime
	}
	if endTime != "" {
		url += "&end_time=" + endTime
	}
	if pageToken != "" {
		url += "&page_token=" + pageToken
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result GetMessagesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("get chat history failed: %s", result.Msg)
	}

	return &result, nil
}
