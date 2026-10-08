package tokenusage

// Aggregate 汇总多条 Run Usage。权威明细按条累加；Cached 不重复加进 Total。
func Aggregate(runs []*TokenUsage) *TokenUsageSummary {
	sum := &TokenUsageSummary{UsageStatus: StatusUnavailable}
	if len(runs) == 0 {
		return sum
	}
	finals, partials, pendings, unavail := 0, 0, 0, 0
	for _, u := range runs {
		if u == nil {
			continue
		}
		u.Normalize()
		sum.RunCount++
		sum.InputTokens += u.InputTokens
		sum.CachedInputTokens += u.CachedInputTokens
		sum.CacheWriteInputTokens += u.CacheWriteInputTokens
		sum.CacheReadInputTokens += u.CacheReadInputTokens
		sum.OutputTokens += u.OutputTokens
		sum.ReasoningOutputTokens += u.ReasoningOutputTokens
		sum.TotalTokens += u.TotalTokens
		switch u.UsageStatus {
		case StatusFinal:
			finals++
		case StatusPartial:
			partials++
		case StatusPending:
			pendings++
		default:
			unavail++
		}
	}
	switch {
	case sum.RunCount == 0:
		sum.UsageStatus = StatusUnavailable
	case pendings > 0 && finals+partials == 0:
		sum.UsageStatus = StatusPending
	case pendings > 0 || partials > 0 || (finals > 0 && unavail > 0):
		sum.UsageStatus = StatusPartial
	case finals == sum.RunCount:
		sum.UsageStatus = StatusFinal
	case unavail == sum.RunCount:
		sum.UsageStatus = StatusUnavailable
	default:
		sum.UsageStatus = StatusPartial
	}
	return sum
}
