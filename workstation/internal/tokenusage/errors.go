package tokenusage

import "errors"

var (
	ErrProviderNotSupported = errors.New("tokenusage: 不支持的 Agent Runtime")
	ErrUsageNotAvailable    = errors.New("tokenusage: 真实 usage 不可用")
	ErrProviderTemporary    = errors.New("tokenusage: Provider 临时错误")
	ErrProviderParse        = errors.New("tokenusage: 解析 usage 失败")
	ErrExecutionNotFound    = errors.New("tokenusage: execution 不存在")
)
