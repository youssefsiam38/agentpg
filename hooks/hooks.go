package hooks

import (
	"context"
	"encoding/json"

	"github.com/youssefsiam38/agentpg/compaction"
	"github.com/youssefsiam38/agentpg/types"
)

// BeforeMessageHook is called before sending messages to the API
type BeforeMessageHook func(ctx context.Context, messages []*types.Message) error

// AfterMessageHook is called after receiving a response from the API
type AfterMessageHook func(ctx context.Context, response *types.Response) error

// ToolCallHook is called when a tool is executed
// Parameters: ctx, toolName, input, output, error
type ToolCallHook func(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error

// BeforeCompactionHook is called before context compaction
type BeforeCompactionHook func(ctx context.Context, sessionID string) error

// AfterCompactionHook is called after context compaction
type AfterCompactionHook func(ctx context.Context, result *compaction.CompactionResult) error

// Registry holds all registered hooks
type Registry struct {
	beforeMessage    []BeforeMessageHook
	afterMessage     []AfterMessageHook
	toolCall         []ToolCallHook
	beforeCompaction []BeforeCompactionHook
	afterCompaction  []AfterCompactionHook
}

// NewRegistry creates a new hook registry
func NewRegistry() *Registry {
	return &Registry{
		beforeMessage:    []BeforeMessageHook{},
		afterMessage:     []AfterMessageHook{},
		toolCall:         []ToolCallHook{},
		beforeCompaction: []BeforeCompactionHook{},
		afterCompaction:  []AfterCompactionHook{},
	}
}

// OnBeforeMessage registers a hook to be called before sending messages
func (r *Registry) OnBeforeMessage(hook BeforeMessageHook) {
	r.beforeMessage = append(r.beforeMessage, hook)
}

// OnAfterMessage registers a hook to be called after receiving a response
func (r *Registry) OnAfterMessage(hook AfterMessageHook) {
	r.afterMessage = append(r.afterMessage, hook)
}

// OnToolCall registers a hook to be called when a tool is executed
func (r *Registry) OnToolCall(hook ToolCallHook) {
	r.toolCall = append(r.toolCall, hook)
}

// OnBeforeCompaction registers a hook to be called before compaction
func (r *Registry) OnBeforeCompaction(hook BeforeCompactionHook) {
	r.beforeCompaction = append(r.beforeCompaction, hook)
}

// OnAfterCompaction registers a hook to be called after compaction
func (r *Registry) OnAfterCompaction(hook AfterCompactionHook) {
	r.afterCompaction = append(r.afterCompaction, hook)
}

// TriggerBeforeMessage calls all registered before-message hooks
func (r *Registry) TriggerBeforeMessage(ctx context.Context, messages []*types.Message) error {
	for _, hook := range r.beforeMessage {
		if err := hook(ctx, messages); err != nil {
			return err
		}
	}
	return nil
}

// TriggerAfterMessage calls all registered after-message hooks
func (r *Registry) TriggerAfterMessage(ctx context.Context, response *types.Response) error {
	for _, hook := range r.afterMessage {
		if err := hook(ctx, response); err != nil {
			return err
		}
	}
	return nil
}

// TriggerToolCall calls all registered tool-call hooks
func (r *Registry) TriggerToolCall(ctx context.Context, toolName string, input json.RawMessage, output string, err error) error {
	for _, hook := range r.toolCall {
		if hookErr := hook(ctx, toolName, input, output, err); hookErr != nil {
			return hookErr
		}
	}
	return nil
}

// TriggerBeforeCompaction calls all registered before-compaction hooks
func (r *Registry) TriggerBeforeCompaction(ctx context.Context, sessionID string) error {
	for _, hook := range r.beforeCompaction {
		if err := hook(ctx, sessionID); err != nil {
			return err
		}
	}
	return nil
}

// TriggerAfterCompaction calls all registered after-compaction hooks
func (r *Registry) TriggerAfterCompaction(ctx context.Context, result *compaction.CompactionResult) error {
	for _, hook := range r.afterCompaction {
		if err := hook(ctx, result); err != nil {
			return err
		}
	}
	return nil
}
