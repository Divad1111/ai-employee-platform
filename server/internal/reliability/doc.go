// Package reliability 实现 Control Plane 可靠通信：
// 命令序号、ACK、事件幂等、心跳 Offline、Resume 续传。
// 设计依据：设计文档 §21–§24、§67、§91。
//
// 乱序策略（已决）：严格递增；sequence != last+1 则拒绝（不缓冲）。
// 重复 message_id / command_id / event_id：幂等成功，不重复执行。
package reliability
