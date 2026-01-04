# Dify 集成配置指南

本文档介绍如何部署和配置 Dify，以增强 Team Assistant 的自然语言查询能力。

## 为什么使用 Dify？

| 功能 | 原生 LLM | Dify |
|------|---------|------|
| 意图识别准确率 | ~70-80% | ~85-95% |
| 多轮对话 | ❌ 无状态 | ✅ 支持 |
| 知识库检索 | 手写 SQL | ✅ 向量数据库 RAG |
| 调试优化 | 改代码重部署 | ✅ 网页界面调参 |

## 1. 部署 Dify

### 方式一：Docker Compose（推荐）

```bash
# 克隆 Dify 仓库
git clone https://github.com/langgenius/dify.git
cd dify/docker

# 启动服务
docker compose up -d

# 等待所有服务启动（约 2-3 分钟）
docker compose ps
```

Dify 默认端口：
- Web UI: http://localhost:80
- API: http://localhost:80/v1

### 方式二：云服务

使用 Dify 官方云服务：https://cloud.dify.ai

## 2. 创建 Dify 应用

1. 访问 Dify Web UI（默认 http://localhost）
2. 注册管理员账号
3. 创建应用 → 选择「对话型应用」
4. 配置应用：
   - 名称：Team Assistant Bot
   - 描述：团队助手 AI 对话机器人

## 3. 配置知识库

### 创建知识库

1. 进入「知识库」页面
2. 点击「创建知识库」
3. 命名：Team Messages（团队消息库）
4. 选择索引模式：高质量（High Quality）

### 获取知识库 ID

1. 进入知识库详情页
2. 从 URL 中获取 Dataset ID
   - URL 格式：`/datasets/{dataset_id}/documents`
   - 复制 `dataset_id` 部分

## 4. 获取 API Key

1. 进入「设置」→「API 密钥」
2. 创建新的 API 密钥
3. 复制密钥（格式：`app-xxxxxxxx`）

## 5. 配置 Team Assistant

编辑 `etc/config.yaml`：

```yaml
# Dify 配置
Dify:
  Enabled: true
  BaseURL: "http://localhost"      # Dify API 地址
  APIKey: "app-xxxxxxxx"           # 从 Dify 获取的 API Key
  DatasetID: "xxxxxxxx"            # 知识库 ID
```

## 6. 配置 Dify 应用提示词

在 Dify 应用编排中配置 System Prompt：

```
你是一个智能团队助手，帮助团队成员查询工作相关信息。

你可以访问以下数据：
1. 团队成员的 Git 提交记录和工作量统计
2. 群聊历史消息
3. 知识库中的相关文档

用户可能会问：
- 工作量查询：「小明这周干了多少活？」
- 消息搜索：「张三说过什么关于登录的？」
- 内容总结：「总结一下今天的讨论」

回答要求：
- 使用简洁、友好的中文回复
- 基于实际数据回答，不要编造
- 如果没有相关数据，诚实告知用户

当前时间：{{current_time}}
Git 统计数据：{{git_stats}}
相关消息：{{recent_messages}}
知识库检索结果：{{knowledge_context}}
```

## 7. 变量配置

在 Dify 应用中添加以下输入变量：

| 变量名 | 类型 | 描述 |
|--------|------|------|
| git_stats | String | Git 统计数据 JSON |
| recent_messages | String | 最近的聊天消息 |
| knowledge_context | String | 知识库检索结果 |
| current_time | String | 当前时间 |

## 8. 验证集成

### 检查日志

```bash
# 查看 Team Assistant 日志
tail -f logs/team-assistant.log

# 应该看到：
# Using Dify for AI processing
# Dify syncer started
```

### 测试查询

在飞书私聊中发送：
```
小明这周干了多少活？
```

如果 Dify 正常工作，会看到日志：
```
Found 5 relevant knowledge segments
```

## 9. 消息同步机制

Team Assistant 会自动将消息同步到 Dify 知识库：

- **同步频率**：每 5 分钟自动同步
- **同步范围**：最近 7 天的消息
- **文档格式**：按日期分组，每天一个文档

### 手动触发同步

可以通过代码调用：
```go
difySyncer.SyncNow()
```

## 10. 故障排除

### Dify 连接失败

检查：
1. Dify 服务是否运行：`docker compose ps`
2. 网络是否可达：`curl http://localhost/v1/health`
3. API Key 是否正确

### 知识库搜索无结果

检查：
1. Dataset ID 是否正确
2. 知识库中是否有文档
3. 索引是否完成（查看 Dify 文档状态）

### 回退到原生 LLM

如果 Dify 出错，系统会自动回退到原生 LLM：
```
Dify chat error: xxx, falling back to native LLM
```

## 11. 生产环境建议

1. **使用 HTTPS**：配置反向代理添加 SSL
2. **资源分配**：Dify 需要至少 4GB 内存
3. **备份**：定期备份 Dify 数据卷
4. **监控**：监控 API 响应时间和错误率

## 参考链接

- [Dify 官方文档](https://docs.dify.ai/)
- [Dify GitHub](https://github.com/langgenius/dify)
- [Dify API 参考](https://docs.dify.ai/api-reference)
