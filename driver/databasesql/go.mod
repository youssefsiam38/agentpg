module github.com/youssefsiam38/agentpg/driver/databasesql

go 1.24

toolchain go1.25.4

require (
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.10.9
	github.com/youssefsiam38/agentpg v0.0.0
)

replace github.com/youssefsiam38/agentpg => ../..
