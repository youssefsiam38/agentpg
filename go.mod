module github.com/youssefsiam38/agentpg

go 1.24

toolchain go1.25.4

require (
	github.com/anthropics/anthropic-sdk-go v1.19.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.6
	github.com/lib/pq v1.10.9
	github.com/youssefsiam38/agentpg/driver/databasesql v0.0.0
	github.com/youssefsiam38/agentpg/driver/pgxv5 v0.0.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	golang.org/x/crypto v0.40.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/text v0.27.0 // indirect
)

replace (
	github.com/youssefsiam38/agentpg/driver/databasesql => ./driver/databasesql
	github.com/youssefsiam38/agentpg/driver/pgxv5 => ./driver/pgxv5
)
