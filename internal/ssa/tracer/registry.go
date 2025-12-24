package tracer

// Registry manages the interconnected tracer instances.
// This encapsulates the circular references between tracers.
//
// The tracers form a cyclic graph because zerolog types can transform
// into each other:
//
//	Logger.With()     → Context
//	Context.Logger()  → Logger
//	Logger.Info()     → Event
//
// Wiring diagram:
//
//	┌──────────────────────────────────────────────────────┐
//	│                    Registry                          │
//	│                                                      │
//	│  event ──────┬──────▶ logger ──────▶ context        │
//	│              │           ▲              │            │
//	│              │           └──────────────┘            │
//	│              │                                       │
//	│              └──────────────────────▶ context        │
//	└──────────────────────────────────────────────────────┘
type Registry struct {
	event   *EventTracer
	logger  *LoggerTracer
	context *ContextTracer
}

// NewRegistry creates and wires up all tracer instances.
//
// The wiring is done in two phases to handle circular dependencies:
//  1. Create all instances (with nil references)
//  2. Wire up the cross-references
func NewRegistry() *Registry {
	r := &Registry{
		context: &ContextTracer{},
		logger:  &LoggerTracer{},
		event:   &EventTracer{},
	}

	// Wire up the circular references
	r.context.logger = r.logger
	r.logger.context = r.context
	r.event.logger = r.logger
	r.event.context = r.context

	return r
}

// EventTracer returns the tracer for zerolog.Event values.
func (r *Registry) EventTracer() Tracer {
	return r.event
}
