// package model —— GORM 模型（与 docs/system-design.md §8 DDL 严格对应）。
// 注意力集中在表/字段映射；权限与查询逻辑一律在 repository 层。
package model

import (
	"time"
)

// User 用户（账号体系 F1）。
type User struct {
	ID           int64     `gorm:"primaryKey;column:id"`
	Email        string    `gorm:"column:email;uniqueIndex"`
	PasswordHash *string   `gorm:"column:password_hash"`
	DisplayName  string    `gorm:"column:display_name"`
	Role         string    `gorm:"column:role;default:student"`      // student/teacher/super_admin
	Subscription string    `gorm:"column:subscription;default:free"` // free/pro
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (User) TableName() string { return "users" }

// Team 团队 / 知识库（F2）。
type Team struct {
	ID        int64     `gorm:"primaryKey;column:id"`
	Name      string    `gorm:"column:name"`
	Type      string    `gorm:"column:type"` // private/teacher/public
	JoinCode  *string   `gorm:"column:join_code;uniqueIndex"`
	OwnerID   *int64    `gorm:"column:owner_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (Team) TableName() string { return "teams" }

// TeamMember 成员关系（N:M，含审批态 F2.5）。
type TeamMember struct {
	TeamID   int64     `gorm:"primaryKey;column:team_id"`
	UserID   int64     `gorm:"primaryKey;column:user_id"`
	Role     string    `gorm:"column:role;default:member"`     // member/co_teacher
	Status   string    `gorm:"column:status;default:approved"` // pending/approved
	JoinedAt time.Time `gorm:"column:joined_at"`
}

func (TeamMember) TableName() string { return "team_members" }

// Material 学习资料（F2）。
type Material struct {
	ID                   int64       `gorm:"primaryKey;column:id"`
	TeamID               int64       `gorm:"column:team_id"`
	Title                string      `gorm:"column:title"`
	Subject              *string     `gorm:"column:subject"`
	Chapter              *string     `gorm:"column:chapter"`
	Tags                 StringArray `gorm:"column:tags"`
	Content              *string     `gorm:"column:content"`
	FileType             *string     `gorm:"column:file_type"`
	StorageKey           *string     `gorm:"column:storage_key"`
	Summary              *string     `gorm:"column:summary"`
	SemanticKeywords     StringArray `gorm:"column:semantic_keywords;type:text[];default:'{}'"`
	SuggestedQuestions   StringArray `gorm:"column:suggested_questions;type:text[];default:'{}'"`
	NormalizedStorageKey *string     `gorm:"column:normalized_storage_key"`
	ParserVersion        string      `gorm:"column:parser_version;default:legacy"`
	IndexVersion         string      `gorm:"column:index_version;default:legacy-v1"`
	CleaningStats        []byte      `gorm:"column:cleaning_stats;default:'{}'"`
	ParseStatus          string      `gorm:"column:parse_status;default:pending"` // pending/parsing/done/failed
	ParseError           *string     `gorm:"column:parse_error"`
	ParseGeneration      int64       `gorm:"column:parse_generation;default:1"` // 每次重新入队递增，隔离陈旧 worker
	Shared               bool        `gorm:"column:shared;default:false"`       // 仅 teacher team 生效
	OwnerID              int64       `gorm:"column:owner_id"`
	CreatedAt            time.Time   `gorm:"column:created_at"`
}

func (Material) TableName() string { return "materials" }

// MaterialChunk 向量片段（pgvector，检索层按 team 隔离）。
// embedding 通过 repository 的 Raw SQL 以 ::vector 写入/读取，避免 GORM 与 pgvector 类型摩擦。
type MaterialChunk struct {
	ID           int64   `gorm:"primaryKey;column:id"`
	TeamID       int64   `gorm:"column:team_id"`
	MaterialID   int64   `gorm:"column:material_id"`
	ChunkIdx     int     `gorm:"column:chunk_idx"`
	IndexVersion string  `gorm:"column:index_version;default:legacy-v1"`
	Kind         string  `gorm:"column:kind;default:body"`
	HeadingPath  *string `gorm:"column:heading_path"`
	PageNumber   *int    `gorm:"column:page_number"`
	TokenCount   int     `gorm:"column:token_count"`
	LexicalText  string  `gorm:"column:lexical_text"`
	AssetID      *int64  `gorm:"column:asset_id"`
	Content      string  `gorm:"column:content"`
	Embedding    string  `gorm:"column:embedding"` // 文本格式 "[0.1,0.2,...]"，repository 以 ::vector 处理
}

func (MaterialChunk) TableName() string { return "material_chunks" }

// MaterialAsset 是从文档提取并存入对象存储的图片资产。
type MaterialAsset struct {
	ID              int64     `gorm:"primaryKey;column:id"`
	MaterialID      int64     `gorm:"column:material_id"`
	ParseGeneration int64     `gorm:"column:parse_generation"`
	IndexVersion    string    `gorm:"column:index_version"`
	StorageKey      string    `gorm:"column:storage_key"`
	SHA256          string    `gorm:"column:sha256"`
	MimeType        string    `gorm:"column:mime_type"`
	PageNumber      *int      `gorm:"column:page_number"`
	ChunkIdx        *int      `gorm:"column:chunk_idx"`
	OCRText         *string   `gorm:"column:ocr_text"`
	Caption         *string   `gorm:"column:caption"`
	Width           *int      `gorm:"column:width"`
	Height          *int      `gorm:"column:height"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (MaterialAsset) TableName() string { return "material_assets" }

// RAGIndexVersion 记录可原子切换的检索索引版本。
type RAGIndexVersion struct {
	Version        string     `gorm:"primaryKey;column:version"`
	Status         string     `gorm:"column:status"`
	EmbeddingModel string     `gorm:"column:embedding_model"`
	EmbeddingDim   int        `gorm:"column:embedding_dim"`
	ParserVersion  string     `gorm:"column:parser_version"`
	ChunkConfig    []byte     `gorm:"column:chunk_config"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	ActivatedAt    *time.Time `gorm:"column:activated_at"`
}

func (RAGIndexVersion) TableName() string { return "rag_index_versions" }

// RAGProcessingRun 保存 Parser 可恢复阶段、进度和版本信息。
type RAGProcessingRun struct {
	ID                   int64      `gorm:"primaryKey;column:id"`
	MaterialID           int64      `gorm:"column:material_id"`
	ParseGeneration      int64      `gorm:"column:parse_generation"`
	IndexVersion         string     `gorm:"column:index_version"`
	Stage                string     `gorm:"column:stage"`
	Status               string     `gorm:"column:status"`
	ParserVersion        string     `gorm:"column:parser_version"`
	CleaningRulesVersion string     `gorm:"column:cleaning_rules_version"`
	Progress             []byte     `gorm:"column:progress"`
	Error                *string    `gorm:"column:error"`
	StartedAt            time.Time  `gorm:"column:started_at"`
	FinishedAt           *time.Time `gorm:"column:finished_at"`
}

func (RAGProcessingRun) TableName() string { return "rag_processing_runs" }

// LearningRecord 学习记录（F6）。
type LearningRecord struct {
	ID         int64     `gorm:"primaryKey;column:id"`
	UserID     int64     `gorm:"column:user_id"`
	MaterialID *int64    `gorm:"column:material_id"`
	DurationS  int       `gorm:"column:duration_s;default:0"`
	Progress   float64   `gorm:"column:progress;default:0"`
	Score      *float64  `gorm:"column:score"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (LearningRecord) TableName() string { return "learning_records" }

// AgentSession 会话（F5）。
type AgentSession struct {
	ID        string    `gorm:"primaryKey;column:id"`
	UserID    int64     `gorm:"column:user_id"`
	Title     *string   `gorm:"column:title"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (AgentSession) TableName() string { return "agent_sessions" }

// AgentMessage 消息（F4/F5，含引用来源）。
type AgentMessage struct {
	ID        int64     `gorm:"primaryKey;column:id"`
	SessionID string    `gorm:"column:session_id"`
	Role      string    `gorm:"column:role"` // user/assistant/system
	Content   string    `gorm:"column:content"`
	Citations []byte    `gorm:"column:citations"` // JSONB [{team_id,material_id,chapter,chunk_idx}]
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (AgentMessage) TableName() string { return "agent_messages" }

// MessageFeedback 是用户对单条助手回答的幂等评价。
type MessageFeedback struct {
	ID        int64     `gorm:"primaryKey;column:id"`
	MessageID int64     `gorm:"column:message_id"`
	UserID    int64     `gorm:"column:user_id"`
	Rating    string    `gorm:"column:rating"`
	Reason    *string   `gorm:"column:reason"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (MessageFeedback) TableName() string { return "message_feedback" }

// RAGRun 与 RAGRunHit 保存查询改写、阶段耗时和候选分数。
type RAGRun struct {
	ID             string      `gorm:"primaryKey;column:id"`
	UserID         int64       `gorm:"column:user_id"`
	SessionID      *string     `gorm:"column:session_id"`
	MessageID      *int64      `gorm:"column:message_id"`
	TraceID        string      `gorm:"column:trace_id"`
	OriginalQuery  string      `gorm:"column:original_query"`
	RewrittenQuery string      `gorm:"column:rewritten_query"`
	RewriteApplied bool        `gorm:"column:rewrite_applied"`
	IndexVersion   string      `gorm:"column:index_version"`
	StageDurations []byte      `gorm:"column:stage_durations"`
	DegradedStages StringArray `gorm:"column:degraded_stages"`
	CreatedAt      time.Time   `gorm:"column:created_at"`
}

func (RAGRun) TableName() string { return "rag_runs" }

type RAGRunHit struct {
	ID           int64    `gorm:"primaryKey;column:id"`
	RunID        string   `gorm:"column:run_id"`
	ChunkID      *int64   `gorm:"column:chunk_id"`
	MaterialID   int64    `gorm:"column:material_id"`
	Rank         int      `gorm:"column:rank"`
	VectorScore  *float64 `gorm:"column:vector_score"`
	LexicalScore *float64 `gorm:"column:lexical_score"`
	RRFScore     float64  `gorm:"column:rrf_score"`
	RerankScore  *float64 `gorm:"column:rerank_score"`
	Selected     bool     `gorm:"column:selected"`
}

func (RAGRunHit) TableName() string { return "rag_run_hits" }

// Exercise 测评题目（F8）。
type Exercise struct {
	ID         int64     `gorm:"primaryKey;column:id"`
	UserID     int64     `gorm:"column:user_id"`
	MaterialID *int64    `gorm:"column:material_id"`
	SessionID  *string   `gorm:"column:session_id"`
	Question   string    `gorm:"column:question"`
	Options    []byte    `gorm:"column:options"` // JSONB
	AnswerKey  *string   `gorm:"column:answer_key"`
	Difficulty *string   `gorm:"column:difficulty"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (Exercise) TableName() string { return "exercises" }

// QuizAttempt 作答（F8）。
type QuizAttempt struct {
	ID         int64     `gorm:"primaryKey;column:id"`
	UserID     int64     `gorm:"column:user_id"`
	ExerciseID int64     `gorm:"column:exercise_id"`
	Choice     *string   `gorm:"column:choice"`
	IsCorrect  *bool     `gorm:"column:is_correct"`
	Score      *float64  `gorm:"column:score"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (QuizAttempt) TableName() string { return "quiz_attempts" }

// StudyPlan 学习计划（F7）。
type StudyPlan struct {
	ID        int64      `gorm:"primaryKey;column:id"`
	UserID    int64      `gorm:"column:user_id"`
	Title     string     `gorm:"column:title"`
	Goal      *string    `gorm:"column:goal"`
	Deadline  *time.Time `gorm:"column:deadline"`
	Items     []byte     `gorm:"column:items"` // JSONB [{date,task,done}]
	CreatedAt time.Time  `gorm:"column:created_at"`
}

func (StudyPlan) TableName() string { return "study_plans" }

// UserProfile 长期画像（Memory Agent）。
type UserProfile struct {
	UserID      int64       `gorm:"primaryKey;column:user_id"`
	WeakPoints  StringArray `gorm:"column:weak_points"`
	Preferences []byte      `gorm:"column:preferences"` // JSONB
	UpdatedAt   time.Time   `gorm:"column:updated_at"`
}

func (UserProfile) TableName() string { return "user_profiles" }

// TokenUsage 成本归因（F8 限流/额度）。
type TokenUsage struct {
	ID               int64     `gorm:"primaryKey;column:id"`
	UserID           int64     `gorm:"column:user_id"`
	Service          string    `gorm:"column:service"` // chat/plan/quiz
	PromptTokens     int       `gorm:"column:prompt_tokens"`
	CompletionTokens int       `gorm:"column:completion_tokens"`
	TotalTokens      int       `gorm:"column:total_tokens"`
	CreatedAt        time.Time `gorm:"column:created_at"`
}

func (TokenUsage) TableName() string { return "token_usage" }

// MaterialNote 阅读笔记 / 标注（F3）。
type MaterialNote struct {
	ID         int64     `gorm:"primaryKey;column:id"`
	UserID     int64     `gorm:"column:user_id"`
	MaterialID int64     `gorm:"column:material_id"`
	Content    string    `gorm:"column:content"`
	Quote      *string   `gorm:"column:quote"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (MaterialNote) TableName() string { return "material_notes" }
