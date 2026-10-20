package feishu

import (
	"context"
	"errors"
	"sort"

	"github.com/ai-employee-platform/server/internal/employee"
)

// CommandEmployee reads live configuration; cached command results would become
// misleading immediately after /change model or a workstation disconnect.
func (b *Bridge) CommandEmployee(ctx context.Context, id string) (CommandEmployee, error) {
	if b.Employees == nil {
		return CommandEmployee{}, errors.New("员工服务未配置")
	}
	e, err := b.Employees.Get(ctx, id)
	if err != nil {
		return CommandEmployee{}, err
	}
	result := CommandEmployee{
		ID: e.ID, Name: e.Name, Status: e.Status, Provider: e.DefaultProvider, Model: e.DefaultModel,
		WorkstationStatus: "未绑定", Assignable: false,
	}
	if e.WorkstationID != "" {
		result.WorkstationStatus = "未知"
		if b.Workstations != nil {
			view := b.Workstations.Get(ctx, e.WorkstationID)
			result.WorkstationStatus = view.Status
			result.Assignable = e.Status != employee.StatusDisabled && view.Status == "ONLINE"
		}
	}
	return result, nil
}

func (b *Bridge) CommandModels(ctx context.Context, id string) ([]CommandModel, error) {
	if b.Employees == nil || b.Workstations == nil {
		return nil, errors.New("模型查询服务未配置")
	}
	e, err := b.Employees.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if e.WorkstationID == "" {
		return nil, ErrDisabled
	}
	view := b.Workstations.Get(ctx, e.WorkstationID)
	var models []CommandModel
	for provider, options := range view.Models {
		for _, option := range options {
			models = append(models, CommandModel{Provider: provider, ID: option.ID, Label: option.Label})
		}
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].ID < models[j].ID
	})
	return models, nil
}
