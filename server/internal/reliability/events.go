package reliability

import (
	"sync"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
)

// EventRecord 已接受事件。
type EventRecord struct {
	Event     *aiev1.Event
	AcceptedAt time.Time
}

// EventStore Workstation→Server 事件幂等去重。
// 乱序策略：拒绝 sequence != expected（expected = last+1）；不缓冲。
type EventStore struct {
	mu      sync.Mutex
	byWS    map[string]*wsEvents
	skewMs  int64
	nowFunc func() time.Time
}

type wsEvents struct {
	lastSeq   uint64
	byEventID map[string]struct{}
	byMsgID   map[string]struct{}
}

// NewEventStore 创建事件库。
func NewEventStore(skewMs int64) *EventStore {
	if skewMs <= 0 {
		skewMs = 5 * 60 * 1000
	}
	return &EventStore{
		byWS:    make(map[string]*wsEvents),
		skewMs:  skewMs,
		nowFunc: time.Now,
	}
}

func (s *EventStore) ensure(wsID string) *wsEvents {
	w, ok := s.byWS[wsID]
	if !ok {
		w = &wsEvents{
			byEventID: make(map[string]struct{}),
			byMsgID:   make(map[string]struct{}),
		}
		s.byWS[wsID] = w
	}
	return w
}

// Accept 结果。
type AcceptResult struct {
	Accepted bool
	Duplicate bool
	Error     string
}

// Accept 校验并接受事件；重复 event_id/message_id 幂等成功。
func (s *EventStore) Accept(ev *aiev1.Event) AcceptResult {
	if ev == nil || ev.GetWorkstationId() == "" || ev.GetEventId() == "" {
		return AcceptResult{Error: "事件不完整"}
	}
	meta := ev.GetMeta()
	if meta == nil {
		return AcceptResult{Error: "缺少 EnvelopeMeta"}
	}
	now := s.nowFunc().UnixMilli()
	delta := now - meta.TimestampUnixMs
	if delta < 0 {
		delta = -delta
	}
	if delta > s.skewMs {
		return AcceptResult{Error: "timestamp 超窗"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.ensure(ev.WorkstationId)

	if _, ok := w.byEventID[ev.EventId]; ok {
		return AcceptResult{Accepted: true, Duplicate: true}
	}
	if meta.MessageId != "" {
		if _, ok := w.byMsgID[meta.MessageId]; ok {
			return AcceptResult{Accepted: true, Duplicate: true}
		}
	}

	// 严格递增：允许首次任意正序号，之后必须 last+1
	seq := meta.Sequence
	if seq == 0 {
		return AcceptResult{Error: "sequence 无效"}
	}
	if w.lastSeq == 0 {
		// 首条：接受任意 >0
	} else if seq != w.lastSeq+1 {
		return AcceptResult{Error: "乱序：拒绝（策略=strict，不缓冲）"}
	}
	if seq <= w.lastSeq {
		return AcceptResult{Error: "sequence <= last_sequence"}
	}

	w.byEventID[ev.EventId] = struct{}{}
	if meta.MessageId != "" {
		w.byMsgID[meta.MessageId] = struct{}{}
	}
	w.lastSeq = seq
	return AcceptResult{Accepted: true}
}

// ResetWorkstation 在 Workstation 重新 Connect/Hello 时重置事件序号游标。
// daemon 重启后会从 sequence=1 重计；若不重置，旧的超大 lastSeq 会导致全部 JOB_* 被拒。
func (s *EventStore) ResetWorkstation(wsID string) {
	if wsID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byWS[wsID] = &wsEvents{
		byEventID: make(map[string]struct{}),
		byMsgID:   make(map[string]struct{}),
	}
}

// LastSequence 返回已接受最大序号。
func (s *EventStore) LastSequence(wsID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensure(wsID).lastSeq
}
