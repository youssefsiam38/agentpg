// Package pgxv5 provides a pgx/v5 driver implementation for AgentPG.
//
// This is the primary/recommended driver for AgentPG, offering the best
// performance and feature support including native batch operations and
// nested transactions via savepoints.
//
// Usage:
//
//	pool, _ := pgxpool.New(ctx, databaseURL)
//	drv := pgxv5.New(pool)
//	agent, _ := agentpg.New(drv, agentpg.Config{...})
package pgxv5
