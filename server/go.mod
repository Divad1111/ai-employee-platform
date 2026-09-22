module github.com/ai-employee-platform/server

go 1.25.0

require (
	github.com/ai-employee-platform/gen v0.0.0
	github.com/jackc/pgx/v5 v5.7.1
	github.com/larksuite/oapi-sdk-go/v3 v3.12.0
	github.com/pressly/goose/v3 v3.24.1
	golang.org/x/crypto v0.31.0
	google.golang.org/grpc v1.68.0
)

require (
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.33.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/protobuf v1.35.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/sqlite v1.35.0 // indirect
)

replace github.com/ai-employee-platform/gen => ../gen

// goose 测试依赖 modernc，避免 tidy 拉取失败影响构建
exclude modernc.org/sqlite v1.34.1
