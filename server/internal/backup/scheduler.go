package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-employee-platform/server/internal/backup/destination"
	"github.com/robfig/cron/v3"
)

// BackupScheduler 备份定时调度器
type BackupScheduler struct {
	store    Store
	executor *BackupExecutor
	getDest  func(destID string) (destination.BackupDestination, error)
	parser   cron.Parser
}

// NewBackupScheduler 初始化调度器
func NewBackupScheduler(
	store Store,
	executor *BackupExecutor,
	getDest func(destID string) (destination.BackupDestination, error),
) *BackupScheduler {
	return &BackupScheduler{
		store:    store,
		executor: executor,
		getDest:  getDest,
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// CalculateNextRun 计算策略的下一次执行时间
func (s *BackupScheduler) CalculateNextRun(expr, tz string, from time.Time) (*time.Time, error) {
	if expr == "" {
		return nil, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.Local
	}
	sched, err := s.parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("解析 Cron 表达式失败: %w", err)
	}

	fromInLoc := from.In(loc)
	next := sched.Next(fromInLoc).UTC()
	return &next, nil
}

// Tick 调度心跳探测（供服务主循环定期调用）
func (s *BackupScheduler) Tick(ctx context.Context, now time.Time) {
	policies, err := s.store.ListPolicies(ctx)
	if err != nil {
		return
	}

	for _, p := range policies {
		if !p.Enabled || p.ScheduleType != ScheduleTypeCron || p.CronExpression == "" {
			continue
		}

		// 若未计算过下次执行时间，先初始化
		if p.NextRunAt == nil {
			next, err := s.CalculateNextRun(p.CronExpression, p.Timezone, now)
			if err == nil && next != nil {
				_ = s.store.UpdatePolicyRunStatus(ctx, p.ID, time.Time{}, p.LastRunStatus, next)
			}
			continue
		}

		// 判断是否到达触发时间
		if now.After(*p.NextRunAt) || now.Equal(*p.NextRunAt) {
			// 计算未来的下一次执行时间并立即更新，防止重复拉起
			next, err := s.CalculateNextRun(p.CronExpression, p.Timezone, now)
			if err == nil {
				_ = s.store.UpdatePolicyRunStatus(ctx, p.ID, p.LastRunAt.UTC(), p.LastRunStatus, next)
			}

			// 异步触发备份执行
			go func(pol Policy) {
				_, _ = s.executor.Execute(context.Background(), RunOptions{
					PolicyID:       pol.ID,
					TriggerType:    "SCHEDULED",
					CreatedBy:      "system:scheduler",
					DestinationIDs: pol.DestinationIDs,
				}, s.getDest)
			}(p)
		}
	}
}
