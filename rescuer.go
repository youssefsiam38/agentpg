package agentpg

import (
	"context"
	"time"
)

// rescuer monitors and rescues stuck runs.
// It runs only on the leader instance to prevent duplicate rescue attempts.
type rescuer[TTx any] struct {
	client *Client[TTx]
}

func newRescuer[TTx any](c *Client[TTx]) *rescuer[TTx] {
	return &rescuer[TTx]{
		client: c,
	}
}

func (r *rescuer[TTx]) run(ctx context.Context) {
	config := r.client.config.RunRescueConfig
	if config == nil {
		config = &RunRescueConfig{
			RescueInterval:    time.Minute,
			RescueTimeout:     5 * time.Minute,
			MaxRescueAttempts: 3,
		}
	}

	ticker := time.NewTicker(config.RescueInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.rescueStuckRuns(ctx, config)
		}
	}
}

func (r *rescuer[TTx]) rescueStuckRuns(ctx context.Context, config *RunRescueConfig) {
	store := r.client.driver.Store()
	log := r.client.log()

	// Only run on leader to prevent duplicate rescue attempts
	isLeader, err := store.IsLeader(ctx, r.client.instanceID)
	if err != nil {
		log.Error("failed to check leader status", "error", err)
		return
	}
	if !isLeader {
		return
	}

	// Get stuck runs eligible for rescue
	runs, err := store.GetStuckRuns(ctx, config.RescueTimeout, config.MaxRescueAttempts, 100)
	if err != nil {
		log.Error("failed to get stuck runs", "error", err)
		return
	}

	if len(runs) == 0 {
		return
	}

	log.Info("found stuck runs to rescue", "count", len(runs))

	for _, run := range runs {
		// Check if this run has exceeded max rescue attempts
		if run.RescueAttempts >= config.MaxRescueAttempts {
			// Mark as failed - too many rescue attempts
			log.Warn("run exceeded max rescue attempts, marking as failed",
				"run_id", run.ID,
				"agent_name", run.AgentName,
				"rescue_attempts", run.RescueAttempts,
				"max_rescue_attempts", config.MaxRescueAttempts,
			)
			if err := store.UpdateRunState(ctx, run.ID, "failed", map[string]any{
				"error_type":    "rescue_failed",
				"error_message": "run exceeded maximum rescue attempts",
				"finalized_at":  time.Now(),
			}); err != nil {
				log.Error("failed to mark run as failed", "error", err, "run_id", run.ID)
			}
			continue
		}

		// Rescue the run - reset to pending state
		log.Info("rescuing stuck run",
			"run_id", run.ID,
			"agent_name", run.AgentName,
			"state", run.State,
			"rescue_attempts", run.RescueAttempts+1,
			"claimed_by", run.ClaimedByInstanceID,
		)

		if err := store.RescueRun(ctx, run.ID); err != nil {
			log.Error("failed to rescue run", "error", err, "run_id", run.ID)
			continue
		}

		// Trigger run worker to pick up the rescued run
		if r.client.runWorker != nil {
			r.client.runWorker.trigger()
		}
	}
}
