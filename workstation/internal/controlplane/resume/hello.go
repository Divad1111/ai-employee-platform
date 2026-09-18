package resume

import (
	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
)

// BuildHello 构造 Connect 首帧 Resume 信息。
func BuildHello(workstationID, agentVersion string, lastAckedCommandSeq uint64) *aiev1.WorkerHello {
	return &aiev1.WorkerHello{
		WorkstationId:             workstationID,
		LastAckedCommandSequence:  lastAckedCommandSeq,
		AgentVersion:              agentVersion,
	}
}

// HelloFrame 包装为上行帧。
func HelloFrame(h *aiev1.WorkerHello) *aiev1.WorkerToServer {
	return &aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Hello{Hello: h}}
}
