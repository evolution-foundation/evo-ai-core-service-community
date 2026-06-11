package model

import (
	"encoding/json"
	"evo-ai-core-service/internal/utils/stringutils"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Agent struct {
	ID               uuid.UUID  `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	TenantID         uuid.UUID  `json:"-" gorm:"column:tenant_id;type:uuid;not null;default:'00000000-0000-0000-0000-000000000000'"`
	Name             string     `json:"name" gorm:"not null; type:varchar(255)"`
	Description      string     `json:"description" gorm:"type:text"`
	Type             string     `json:"type" gorm:"not null; type:varchar(10)"`
	Model            string     `json:"model" gorm:"type:varchar(255)"`
	ApiKeyID         *uuid.UUID `json:"api_key_id" gorm:"type:uuid;references:evo_core_api_keys(id)"`
	Instruction      string     `json:"instruction" gorm:"type:text"`
	CardURL          string     `json:"card_url" gorm:"type:varchar(1024)"`
	FolderID         *uuid.UUID `json:"folder_id" gorm:"type:uuid;references:evo_core_folders(id)"`
	Config           string     `json:"config" gorm:"type:json;default:'{}'"`
	Role             string     `json:"role" gorm:"type:text"`                                 // Papel do agente
	Goal             string     `json:"goal" gorm:"type:text"`                                 // Objetivo do agente
	EvolutionBotID   *uuid.UUID `json:"evolution_bot_id" gorm:"type:uuid;"`                    // Internal field - managed by evolutionService only, not from frontend requests
	EvolutionBotSync bool       `json:"evolution_bot_sync" gorm:"type:boolean;default:false;"` // Internal field - managed by evolutionService only, not from frontend requests
	CreatedAt        time.Time  `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt        time.Time  `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (Agent) TableName() string {
	return "evo_core_agents"
}

// NullableUUID é um tipo customizado que permite strings vazias serem nil
type NullableUUID struct {
	*uuid.UUID
}

func (n *NullableUUID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Se string vazia, deixar como nil
	if s == "" {
		n.UUID = nil
		return nil
	}

	// Tentar fazer parse do UUID
	parsed, err := uuid.Parse(s)
	if err != nil {
		return err
	}

	n.UUID = &parsed
	return nil
}

func (n NullableUUID) MarshalJSON() ([]byte, error) {
	if n.UUID == nil {
		return json.Marshal("")
	}
	return json.Marshal(n.UUID.String())
}

type AgentBase struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	Type        string                 `json:"type" binding:"required"`
	Model       string                 `json:"model"`
	ApiKeyID    NullableUUID           `json:"api_key_id"`
	Instruction string                 `json:"instruction"`
	CardURL     string                 `json:"card_url"`
	FolderID    NullableUUID           `json:"folder_id"`
	Config      map[string]interface{} `json:"config"`
	Role        string                 `json:"role"` // Campo do frontend
	Goal        string                 `json:"goal"` // Campo do frontend
}

type AgentRequest struct {
	AgentBase
}

type AgentUpdateRequest struct {
	AgentBase
}

type AgentImportRequest struct {
	FolderID  *uuid.UUID               `json:"folder_id"`
	AgentData []map[string]interface{} `json:"-"`
}

type AgentResponse struct {
	ID               uuid.UUID              `json:"id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Type             string                 `json:"type"`
	Model            string                 `json:"model"`
	ApiKeyID         *uuid.UUID             `json:"api_key_id"`
	Instruction      string                 `json:"instruction"`
	CardURL          string                 `json:"card_url"`
	FolderID         *uuid.UUID             `json:"folder_id"`
	Config           map[string]interface{} `json:"config"`
	Role             string                 `json:"role"`
	Goal             string                 `json:"goal"`
	EvolutionBotID   *uuid.UUID             `json:"evolution_bot_id"`
	EvolutionBotSync bool                   `json:"evolution_bot_sync"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

type AgentFolderRequest struct {
	FolderID *uuid.UUID `json:"folder_id"`
}

type AgentReadRequest struct {
	FolderID string `json:"folder_id"`
	Page     int    `json:"page" binding:"required"`
	PageSize int    `json:"page_size" binding:"required"`
}

type AgentListResponse struct {
	Items      []AgentResponse `json:"-"`
	Page       int             `json:"-"`
	PageSize   int             `json:"-"`
	Skip       int             `json:"-"`
	Limit      int             `json:"-"`
	TotalItems int64           `json:"-"`
	TotalPages int             `json:"-"`
}

func (a *Agent) forceReturnCardUrl(cardUrl string, id uuid.UUID, aiProcessorURL string) string {
	if cardUrl == "" || cardUrl == "null" {
		return fmt.Sprintf("%s/api/v1/a2a/%s/.well-known/agent.json", aiProcessorURL, id)
	}

	return cardUrl
}

func (a *Agent) ToResponse(aiProcessorURL string) *AgentResponse {
	return &AgentResponse{
		ID:               a.ID,
		Name:             a.Name,
		Description:      a.Description,
		Type:             a.Type,
		Model:            a.Model,
		ApiKeyID:         a.ApiKeyID,
		Instruction:      a.Instruction,
		CardURL:          a.forceReturnCardUrl(a.CardURL, a.ID, aiProcessorURL),
		FolderID:         a.FolderID,
		Config:           stringutils.JSONToInterfaceMap(a.Config),
		Role:             a.Role,
		Goal:             a.Goal,
		EvolutionBotID:   a.EvolutionBotID,
		EvolutionBotSync: a.EvolutionBotSync,
		CreatedAt:        a.CreatedAt,
		UpdatedAt:        a.UpdatedAt,
	}
}
