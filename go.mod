module github.com/youssefsiam38/agentpg

go 1.24.0

toolchain go1.25.4

require (
	github.com/anthropics/anthropic-sdk-go v1.19.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.8.0
	github.com/lib/pq v1.10.9
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/youssefsiam38/agentpg/driver/databasesql v0.0.0
	github.com/youssefsiam38/agentpg/driver/pgxv5 v0.0.0
	github.com/yuin/goldmark v1.7.16
	golang.org/x/crypto v0.47.0
)

require (
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/text v0.33.0 // indirect
)

replace (
	github.com/youssefsiam38/agentpg/driver/databasesql => ./driver/databasesql
	github.com/youssefsiam38/agentpg/driver/pgxv5 => ./driver/pgxv5
)
