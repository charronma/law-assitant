package agent

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// Agent defines the interface for all legal assistant agents
type Agent interface {
	// Handle processes a chat request and returns a streaming response
	Handle(ctx context.Context, messages []*schema.Message) (*schema.StreamReader[*schema.Message], error)
}

// ModuleType represents the type of agent module
type ModuleType string

const (
	ModuleConsult       ModuleType = "consult"
	ModulePleading      ModuleType = "pleading"
	ModuleContract      ModuleType = "contract"
	ModuleEvidenceOrg   ModuleType = "evidence_org"
	ModuleEvidence      ModuleType = "evidence"
	ModuleCommunication ModuleType = "communication"
)

// ModuleInfo contains display information for a module
type ModuleInfo struct {
	Type        ModuleType `json:"type"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Icon        string     `json:"icon"`
}

// GetAllModules returns all available modules
func GetAllModules() []ModuleInfo {
	return []ModuleInfo{
		{Type: ModuleConsult, Name: "法律咨询", Description: "多轮对话式法律问答", Icon: "MessageSquare"},
		{Type: ModulePleading, Name: "诉状撰写", Description: "自动生成法律文书", Icon: "FileText"},
		{Type: ModuleContract, Name: "合同优化", Description: "审查合同风险条款", Icon: "FileCheck"},
		{Type: ModuleEvidenceOrg, Name: "证据整理", Description: "分类整理证据材料", Icon: "FolderOpen"},
		{Type: ModuleEvidence, Name: "取证指导", Description: "指导证据收集方法", Icon: "Search"},
		{Type: ModuleCommunication, Name: "沟通话术", Description: "生成沟通策略话术", Icon: "Users"},
	}
}
