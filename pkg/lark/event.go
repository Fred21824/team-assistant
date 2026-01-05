package lark

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// EventCallback 飞书事件回调的通用结构
type EventCallback struct {
	Schema string          `json:"schema"`
	Header *EventHeader    `json:"header"`
	Event  json.RawMessage `json:"event"`

	// 旧版事件格式
	UUID      string `json:"uuid"`
	Token     string `json:"token"`
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
}

type EventHeader struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	CreateTime string `json:"create_time"`
	Token      string `json:"token"`
	AppID      string `json:"app_id"`
	TenantKey  string `json:"tenant_key"`
}

// MessageReceiveEvent 消息接收事件
type MessageReceiveEvent struct {
	Sender struct {
		SenderID struct {
			OpenID  string `json:"open_id"`
			UserID  string `json:"user_id"`
			UnionID string `json:"union_id"`
		} `json:"sender_id"`
		SenderType string `json:"sender_type"`
	} `json:"sender"`
	Message struct {
		MessageID   string `json:"message_id"`
		RootID      string `json:"root_id"`
		ParentID    string `json:"parent_id"`
		CreateTime  string `json:"create_time"`
		ChatID      string `json:"chat_id"`
		ChatType    string `json:"chat_type"`
		MessageType string `json:"message_type"`
		Content     string `json:"content"`
		Mentions    []struct {
			Key  string `json:"key"`
			ID   struct {
				OpenID  string `json:"open_id"`
				UserID  string `json:"user_id"`
				UnionID string `json:"union_id"`
			} `json:"id"`
			Name string `json:"name"`
		} `json:"mentions"`
	} `json:"message"`
}

// ParseMessageContent 解析消息内容
func ParseMessageContent(msgType, content string) string {
	switch msgType {
	case "text":
		var textContent struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &textContent); err == nil {
			return textContent.Text
		}
	case "post":
		var postContent struct {
			Title   string `json:"title"`
			Content [][]struct {
				Tag  string `json:"tag"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal([]byte(content), &postContent); err == nil {
			var texts []string
			if postContent.Title != "" {
				texts = append(texts, postContent.Title)
			}
			for _, row := range postContent.Content {
				for _, item := range row {
					if item.Tag == "text" && item.Text != "" {
						texts = append(texts, item.Text)
					}
				}
			}
			return strings.Join(texts, " ")
		}
	case "interactive":
		// 解析卡片消息（如告警通知、支付失败告警等）
		return parseInteractiveContent(content)
	}
	return ""
}

// parseInteractiveContent 解析 interactive 卡片消息内容
func parseInteractiveContent(content string) string {
	var cardContent struct {
		Title    string          `json:"title"`
		Elements json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal([]byte(content), &cardContent); err != nil {
		return ""
	}

	var texts []string
	if cardContent.Title != "" {
		texts = append(texts, cardContent.Title)
	}

	// 尝试解析 elements 数组
	var elements []json.RawMessage
	if err := json.Unmarshal(cardContent.Elements, &elements); err == nil {
		for _, elem := range elements {
			extractTextsFromElement(elem, &texts)
		}
	}

	return strings.Join(texts, "\n")
}

// extractTextsFromElement 递归提取卡片元素中的文本
func extractTextsFromElement(elem json.RawMessage, texts *[]string) {
	// 尝试解析为数组（嵌套的元素行）
	var elemArray []json.RawMessage
	if err := json.Unmarshal(elem, &elemArray); err == nil {
		for _, subElem := range elemArray {
			extractTextsFromElement(subElem, texts)
		}
		return
	}

	// 尝试解析为对象
	var elemObj struct {
		Tag      string          `json:"tag"`
		Text     string          `json:"text"`
		Content  string          `json:"content"`
		Elements json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal(elem, &elemObj); err != nil {
		return
	}

	// 提取文本（支持多种标签类型）
	// text/plain_text/lark_md: 普通文本
	// a: 链接标签，text 字段包含链接文字（如站点名称、订单号等）
	switch elemObj.Tag {
	case "text", "plain_text", "lark_md", "a":
		if elemObj.Text != "" {
			*texts = append(*texts, elemObj.Text)
		}
		if elemObj.Content != "" {
			*texts = append(*texts, elemObj.Content)
		}
	}

	// 递归处理嵌套元素
	if len(elemObj.Elements) > 0 {
		var subElements []json.RawMessage
		if err := json.Unmarshal(elemObj.Elements, &subElements); err == nil {
			for _, subElem := range subElements {
				extractTextsFromElement(subElem, texts)
			}
		}
	}
}

// ExtractTextFromMentions 从内容中移除@信息，只保留文本
func ExtractTextFromMentions(text string) string {
	// @xxx 通常以 @_user_xxx 格式出现在原始内容中
	// 这里简单处理，保留原文
	return strings.TrimSpace(text)
}

// IsAtBot 检查消息是否@了机器人
func IsAtBot(event *MessageReceiveEvent, botOpenID string) bool {
	// 如果没有 mentions，说明没有 @ 任何人
	if len(event.Message.Mentions) == 0 {
		return false
	}

	// 如果配置了 BotOpenID，精确匹配
	if botOpenID != "" {
		for _, mention := range event.Message.Mentions {
			if mention.ID.OpenID == botOpenID {
				return true
			}
		}
		return false
	}

	// 如果没有配置 BotOpenID，通过名字判断是否@了机器人
	// 检查 mentions 中是否有名字包含 "助手"、"bot"、"机器人" 的
	for _, mention := range event.Message.Mentions {
		nameLower := strings.ToLower(mention.Name)
		if strings.Contains(nameLower, "助手") ||
			strings.Contains(nameLower, "bot") ||
			strings.Contains(nameLower, "机器人") ||
			strings.Contains(nameLower, "assistant") {
			return true
		}
	}
	return false
}

// DecryptEvent 解密事件内容（如果启用了加密）
func DecryptEvent(encryptedData, encryptKey string) ([]byte, error) {
	if encryptKey == "" {
		return nil, fmt.Errorf("encrypt key is empty")
	}

	// Base64 解码
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedData)
	if err != nil {
		return nil, err
	}

	// SHA256 生成密钥
	key := sha256.Sum256([]byte(encryptKey))

	// AES-256-CBC 解密
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)

	// 去除 PKCS7 填充
	padding := int(ciphertext[len(ciphertext)-1])
	if padding > aes.BlockSize || padding == 0 {
		return nil, fmt.Errorf("invalid padding")
	}
	ciphertext = ciphertext[:len(ciphertext)-padding]

	return ciphertext, nil
}

// VerifyToken 验证事件的 token
func VerifyToken(eventToken, verificationToken string) bool {
	return eventToken == verificationToken
}
