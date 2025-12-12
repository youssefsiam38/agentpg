// Package databasesql provides a database/sql driver implementation for AgentPG.
//
// This driver enables AgentPG to work with any database/sql compatible driver
// (lib/pq, pgx/stdlib, etc.). It supports nested transactions via savepoints.
//
// Usage:
//
//	import (
//	    "database/sql"
//	    _ "github.com/lib/pq"
//	    "github.com/youssefsiam38/agentpg/driver/databasesql"
//	)
//
//	db, _ := sql.Open("postgres", databaseURL)
//	drv := databasesql.New(db)
//	agent, _ := agentpg.New(drv, agentpg.Config{...})
package databasesql
