module github.com/youssefsiam38/agentpg

go 1.24

toolchain go1.25.4

require (
	github.com/anthropics/anthropic-sdk-go v1.19.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.6
	github.com/lib/pq v1.10.9
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/youssefsiam38/agentpg/driver/databasesql v0.2.2
	github.com/youssefsiam38/agentpg/driver/pgxv5 v0.2.2
	github.com/youssefsiam38/agentpg/mcp v0.0.0-00010101000000-000000000000
	github.com/yuin/goldmark v1.7.13
	golang.org/x/crypto v0.40.0
)

replace github.com/youssefsiam38/agentpg/mcp => ./mcp

require (
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/modelcontextprotocol/go-sdk v1.3.1 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.3 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/text v0.27.0 // indirect
)
