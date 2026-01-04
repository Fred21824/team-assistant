-- Team Assistant 数据库初始化脚本
-- 创建数据库
CREATE DATABASE IF NOT EXISTS team_assistant DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE team_assistant;

-- 1. 团队成员表（统一身份管理）
CREATE TABLE IF NOT EXISTS team_members (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL COMMENT '显示名',
    github_username VARCHAR(100) COMMENT 'GitHub 用户名',
    lark_user_id VARCHAR(100) COMMENT '飞书用户ID',
    lark_open_id VARCHAR(100) COMMENT '飞书 Open ID',
    email VARCHAR(200) COMMENT '邮箱',
    role VARCHAR(50) COMMENT '角色：backend/frontend/test/pm',
    department VARCHAR(100) COMMENT '部门',
    status TINYINT DEFAULT 1 COMMENT '状态：1启用 0禁用',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_github (github_username),
    UNIQUE KEY uk_lark_user (lark_user_id),
    KEY idx_name (name),
    KEY idx_role (role)
) ENGINE=InnoDB COMMENT='团队成员';

-- 2. Git 提交记录表
CREATE TABLE IF NOT EXISTS git_commits (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    member_id BIGINT UNSIGNED COMMENT '关联成员ID',
    author_name VARCHAR(100) NOT NULL COMMENT '提交者名称',
    author_email VARCHAR(200) COMMENT '提交者邮箱',
    repo_name VARCHAR(100) NOT NULL COMMENT '仓库名',
    repo_full_name VARCHAR(200) COMMENT '完整仓库名 org/repo',
    branch VARCHAR(100) COMMENT '分支',
    commit_sha VARCHAR(40) NOT NULL COMMENT 'Commit SHA',
    commit_message TEXT COMMENT '提交信息',
    files_changed INT DEFAULT 0 COMMENT '变更文件数',
    additions INT DEFAULT 0 COMMENT '新增行数',
    deletions INT DEFAULT 0 COMMENT '删除行数',
    committed_at TIMESTAMP NOT NULL COMMENT '提交时间',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_commit (repo_full_name, commit_sha),
    KEY idx_member (member_id),
    KEY idx_author (author_name),
    KEY idx_repo (repo_name),
    KEY idx_time (committed_at),
    KEY idx_member_time (member_id, committed_at)
) ENGINE=InnoDB COMMENT='Git 提交记录';

-- 3. 群聊信息表
CREATE TABLE IF NOT EXISTS chat_groups (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    chat_id VARCHAR(100) NOT NULL COMMENT '飞书群ID',
    chat_name VARCHAR(200) COMMENT '群名称',
    chat_type VARCHAR(50) COMMENT '群类型：需求群/开发群/反馈群',
    owner_id VARCHAR(100) COMMENT '群主ID',
    member_count INT DEFAULT 0 COMMENT '成员数',
    status TINYINT DEFAULT 1 COMMENT '状态：1启用 0禁用',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_chat (chat_id),
    KEY idx_type (chat_type)
) ENGINE=InnoDB COMMENT='群聊信息';

-- 4. 聊天消息表
CREATE TABLE IF NOT EXISTS chat_messages (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    message_id VARCHAR(100) NOT NULL COMMENT '飞书消息ID',
    chat_id VARCHAR(100) NOT NULL COMMENT '群ID',
    sender_id VARCHAR(100) COMMENT '发送者飞书ID',
    sender_name VARCHAR(100) COMMENT '发送者名称',
    member_id BIGINT UNSIGNED COMMENT '关联成员ID',
    msg_type VARCHAR(20) COMMENT '消息类型：text/post/image/file',
    content TEXT COMMENT '消息内容（纯文本）',
    raw_content TEXT COMMENT '原始内容（JSON）',
    mentions JSON COMMENT '@的人员列表',
    reply_to_id VARCHAR(100) COMMENT '回复的消息ID',
    is_at_bot TINYINT DEFAULT 0 COMMENT '是否@了机器人',
    created_at TIMESTAMP NOT NULL COMMENT '消息时间',
    indexed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_message (message_id),
    KEY idx_chat (chat_id),
    KEY idx_sender (sender_id),
    KEY idx_member (member_id),
    KEY idx_time (created_at),
    KEY idx_chat_time (chat_id, created_at),
    KEY idx_at_bot (is_at_bot, created_at),
    FULLTEXT KEY ft_content (content) WITH PARSER ngram
) ENGINE=InnoDB COMMENT='聊天消息';

-- 5. 需求/任务表
CREATE TABLE IF NOT EXISTS requirements (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    source VARCHAR(50) NOT NULL COMMENT '来源：lark/jira/zentao',
    source_id VARCHAR(100) COMMENT '原始ID',
    chat_id VARCHAR(100) COMMENT '相关群ID',
    title VARCHAR(500) NOT NULL COMMENT '需求标题',
    description TEXT COMMENT '需求描述',
    status VARCHAR(50) DEFAULT 'pending' COMMENT '状态：pending/in_progress/completed/cancelled',
    priority VARCHAR(20) DEFAULT 'normal' COMMENT '优先级：low/normal/high/urgent',
    assignee_id BIGINT UNSIGNED COMMENT '负责人ID',
    assignee_name VARCHAR(100) COMMENT '负责人名称',
    reporter_id BIGINT UNSIGNED COMMENT '报告人ID',
    reporter_name VARCHAR(100) COMMENT '报告人名称',
    deadline TIMESTAMP NULL COMMENT '截止时间',
    completed_at TIMESTAMP NULL COMMENT '完成时间',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    KEY idx_source (source, source_id),
    KEY idx_chat (chat_id),
    KEY idx_status (status),
    KEY idx_assignee (assignee_id),
    KEY idx_priority (priority),
    KEY idx_deadline (deadline)
) ENGINE=InnoDB COMMENT='需求/任务';

-- 6. AI 对话记录表
CREATE TABLE IF NOT EXISTS ai_conversations (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    chat_id VARCHAR(100) NOT NULL COMMENT '群ID',
    user_id VARCHAR(100) COMMENT '提问用户ID',
    user_name VARCHAR(100) COMMENT '提问用户名',
    query TEXT NOT NULL COMMENT '用户问题',
    intent VARCHAR(50) COMMENT '识别的意图',
    params JSON COMMENT '提取的参数',
    response TEXT COMMENT 'AI 回复',
    tokens_used INT DEFAULT 0 COMMENT '消耗的 tokens',
    latency_ms INT DEFAULT 0 COMMENT '响应延迟(毫秒)',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    KEY idx_chat (chat_id),
    KEY idx_user (user_id),
    KEY idx_intent (intent),
    KEY idx_time (created_at)
) ENGINE=InnoDB COMMENT='AI 对话记录';

-- 7. 定时任务执行记录
CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    task_name VARCHAR(100) NOT NULL COMMENT '任务名称',
    task_type VARCHAR(50) NOT NULL COMMENT '任务类型',
    status VARCHAR(20) DEFAULT 'pending' COMMENT '状态：pending/running/success/failed',
    params JSON COMMENT '任务参数',
    result TEXT COMMENT '执行结果',
    error_msg TEXT COMMENT '错误信息',
    started_at TIMESTAMP NULL,
    finished_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    KEY idx_name (task_name),
    KEY idx_type (task_type),
    KEY idx_status (status),
    KEY idx_time (created_at)
) ENGINE=InnoDB COMMENT='定时任务执行记录';

-- 8. 消息同步任务表
CREATE TABLE IF NOT EXISTS message_sync_tasks (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    chat_id VARCHAR(100) NOT NULL COMMENT '群ID',
    chat_name VARCHAR(200) COMMENT '群名称',
    status VARCHAR(20) DEFAULT 'pending' COMMENT '状态：pending/running/completed/failed/cancelled',
    total_messages INT DEFAULT 0 COMMENT '总消息数',
    synced_messages INT DEFAULT 0 COMMENT '已同步消息数',
    page_token VARCHAR(500) COMMENT '分页Token（用于续传）',
    start_time VARCHAR(20) COMMENT '开始时间戳（毫秒）',
    end_time VARCHAR(20) COMMENT '结束时间戳（毫秒）',
    error_msg TEXT COMMENT '错误信息',
    requested_by VARCHAR(100) COMMENT '请求者ID',
    started_at TIMESTAMP NULL COMMENT '开始同步时间',
    finished_at TIMESTAMP NULL COMMENT '完成时间',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    KEY idx_chat (chat_id),
    KEY idx_status (status),
    KEY idx_time (created_at)
) ENGINE=InnoDB COMMENT='消息同步任务';

-- 初始化一些测试数据
INSERT INTO team_members (name, github_username, role) VALUES
    ('测试用户', 'test-user', 'backend')
ON DUPLICATE KEY UPDATE name = VALUES(name);
