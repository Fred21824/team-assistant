package service

import "team-assistant/pkg/lark"

// FromWebhookEvent 从 Webhook 事件创建 RawMessage
func FromWebhookEvent(event *lark.MessageReceiveEvent) RawMessage {
	mentions := make([]MentionInfo, len(event.Message.Mentions))
	for i, m := range event.Message.Mentions {
		mentions[i] = MentionInfo{
			Key:    m.Key,
			Name:   m.Name,
			OpenID: m.ID.OpenID,
		}
	}

	return RawMessage{
		MessageID:  event.Message.MessageID,
		ChatID:     event.Message.ChatID,
		MsgType:    event.Message.MessageType,
		RawContent: event.Message.Content,
		CreateTime: event.Message.CreateTime,
		ParentID:   event.Message.ParentID,
		RootID:     event.Message.RootID,
		ThreadID:   "", // Webhook 事件无此字段
		SenderID:   event.Sender.SenderID.OpenID,
		Mentions:   mentions,
	}
}

// FromMessageItem 从 API 拉取的消息创建 RawMessage
func FromMessageItem(item *lark.MessageItem) RawMessage {
	mentions := make([]MentionInfo, len(item.Mentions))
	for i, m := range item.Mentions {
		mentions[i] = MentionInfo{
			Key:    m.Key,
			Name:   m.Name,
			OpenID: m.OpenID,
		}
	}

	return RawMessage{
		MessageID:  item.MessageID,
		ChatID:     item.ChatID,
		MsgType:    item.MsgType,
		RawContent: item.Body.Content,
		CreateTime: item.CreateTime,
		ParentID:   item.ParentID,
		RootID:     item.RootID,
		ThreadID:   item.ThreadID,
		SenderID:   item.Sender.ID,
		Mentions:   mentions,
	}
}
