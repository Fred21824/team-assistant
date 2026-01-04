# Team Assistant

智能团队助手 - 基于飞书的 AI 驱动团队协作工具

## 功能特性

- **📊 工作量统计**: 自动采集 GitHub 提交记录，按日/周/月统计团队成员工作量
- **🔍 消息搜索**: 搜索飞书群聊中的历史消息，支持按关键词、发送者筛选
- **📋 AI 总结**: 使用 AI 自动总结群聊讨论内容，提取关键信息和待办事项
- **💬 自然语言交互**: 在群里 @机器人，用自然语言查询各类信息
- **🤖 双 AI 模式**: 支持 Dify 平台（RAG + Agent）或原生 LLM API

## 架构

```
┌─────────────────────────────────────────────────────────┐
│                      飞书群组                            │
│                         │                               │
│                         ▼                               │
│              team-assistant (Go)                        │
│         ┌───────────────┴───────────────┐              │
│         │                               │              │
│         ▼                               ▼              │
│   GitHub 数据采集              AI 处理器                │
│   消息存储/检索           (Dify / 原生 LLM)            │
│         │                               │              │
│         └───────────┬───────────────────┘              │
│                     │                                   │
│                     ▼                                   │
│              MySQL + Redis                              │
└─────────────────────────────────────────────────────────┘
```

## 快速开始

### 1. 环境要求

- Go 1.23+
- MySQL 8.0+
- Redis 6.0+

### 2. 初始化数据库

```bash
mysql -u root -p < deploy/sql/init.sql
```

### 3. 配置

编辑 `etc/config.yaml`：

```yaml
# 飞书配置
Lark:
  Domain: "https://open.larksuite.com"  # 国内版用 feishu.cn
  AppID: "your_app_id"
  AppSecret: "your_app_secret"
  VerificationToken: "your_token"
  BotOpenID: "your_bot_open_id"

# GitHub 配置
GitHub:
  Token: "your_github_token"
  Organizations:
    - "your_org"

# LLM 配置
LLM:
  Provider: "groq"
  APIKey: "your_groq_api_key"
  Model: "llama-3.3-70b-versatile"
```

### 4. 运行

```bash
# 安装依赖
make deps

# 编译运行
make run

# 或者编译后运行
make build
./build/team-assistant -f etc/config.yaml
```

## 飞书配置指南

### 创建应用

1. 访问 [飞书开放平台](https://open.larksuite.com) 或 [飞书开放平台(国内)](https://open.feishu.cn)
2. 创建企业自建应用
3. 获取 App ID 和 App Secret

### 配置权限

在「权限管理」中添加以下权限：

- `im:message` - 获取与发送单聊、群组消息
- `im:message:send_as_bot` - 以应用的身份发消息
- `im:chat` - 获取群组信息
- `im:chat:readonly` - 获取用户或机器人所在的群列表

### 配置事件订阅

1. 在「事件订阅」中配置请求地址：`https://your-domain.com/webhook/lark`
2. 添加事件：`im.message.receive_v1`（接收消息）

### 获取 Bot Open ID

机器人的 open_id 可以通过以下方式获取：
1. 调用 `/open-apis/bot/v3/info` 接口
2. 或在群里 @机器人后查看事件日志

## GitHub 配置指南

### 创建 Token

1. 访问 GitHub Settings -> Developer settings -> Personal access tokens
2. 创建 token，勾选 `repo` 权限

### 配置 Webhook（可选）

1. 在仓库 Settings -> Webhooks 中添加
2. Payload URL: `https://your-domain.com/webhook/github`
3. Content type: `application/json`
4. Events: 选择 `Push events`

## 使用示例

在群里 @机器人：

```
@团队助手 小明这周干了多少活？
@团队助手 今天谁提交了代码？
@团队助手 搜索关于登录的讨论
@团队助手 总结一下今天的群消息
```

## API 接口

### 健康检查
```
GET /health
```

### 获取统计数据
```
GET /api/stats?start=2024-01-01&end=2024-01-31
```

### 成员管理
```
GET /api/members
POST /api/members
```

## 项目结构

```
team-assistant/
├── cmd/
│   └── main.go              # 程序入口
├── internal/
│   ├── config/              # 配置定义
│   ├── handler/             # HTTP 处理器
│   ├── logic/               # 业务逻辑
│   │   └── ai/              # AI 处理器
│   ├── model/               # 数据模型
│   ├── collector/           # 数据采集器
│   └── svc/                 # 服务上下文
├── pkg/
│   ├── github/              # GitHub 客户端
│   ├── lark/                # 飞书客户端
│   ├── llm/                 # LLM 客户端
│   └── dify/                # Dify 客户端
├── deploy/
│   ├── sql/                 # 数据库脚本
│   └── dify/                # Dify 部署配置
├── etc/
│   └── config.yaml          # 配置文件
└── scripts/
    ├── build.sh             # 编译脚本
    ├── start.sh             # 启动脚本
    └── deploy-dify.sh       # Dify 部署脚本
```

## 使用 Dify（推荐）

Dify 是一个开源 LLMOps 平台，提供更强大的 AI 能力：
- **RAG 知识库**: 上传技术文档、需求文档，AI 可以基于文档回答问题
- **可视化工作流**: 拖拽式构建 AI 处理流程
- **Agent 能力**: 自动调用工具完成复杂任务

### 部署 Dify

```bash
# 启动 Dify
./scripts/deploy-dify.sh start

# 访问 http://localhost:3000 配置 Dify
# 1. 创建管理员账号
# 2. 创建「Chat」类型应用
# 3. 获取 API Key
# 4. 更新 etc/config.yaml 中的 Dify 配置
```

### Dify 配置

```yaml
Dify:
  Enabled: true
  BaseURL: "http://localhost/v1"
  APIKey: "your_dify_api_key"
  DatasetID: ""  # 可选，知识库 ID
```

## 开发

```bash
# 格式化代码
make fmt

# 运行测试
make test

# 清理构建
make clean
```

## License

MIT
