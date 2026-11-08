module github.com/ai-employee-platform/workstation

go 1.27.3

require (
	github.com/ai-employee-platform/gen v0.0.0
	github.com/google/uuid v1.6.0
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
	google.golang.org/grpc v1.83.2
	gopkg.in/yaml.v3 v3.0.1
)

require (
	golang.org/x/net v0.62.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/ai-employee-platform/gen => ../gen
