// package service —— 业务逻辑、RBAC 编排、可见 team 计算。
// 不直接拼「资料可见性」SQL（那是 repository 的专属职责）。
package service

import (
	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/repository"
)

// Services 聚合所有 service。
type Services struct {
	Repos        *repository.Repositories
	Auth         *AuthService
	Teams        *TeamService
	Materials    *MaterialService
	Learning     *LearningService
	Conversation *ConversationService
	Agent        *AgentService
	Cfg          *config.Config
}

func New(repos *repository.Repositories, cfg *config.Config) *Services {
	agent := NewAgentService(cfg, repos)
	return &Services{
		Repos:        repos,
		Auth:         NewAuthService(repos, cfg),
		Teams:        NewTeamService(repos),
		Materials:    NewMaterialService(repos, agent, cfg.ParseAlertWebhookURL),
		Learning:     NewLearningService(repos),
		Conversation: NewConversationService(repos),
		Agent:        agent,
		Cfg:          cfg,
	}
}
