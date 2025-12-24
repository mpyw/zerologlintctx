package tracer

// Registry manages the interconnected tracer instances.
// This encapsulates the circular references between tracers.
type Registry struct {
	event   *EventTracer
	logger  *LoggerTracer
	context *ContextTracer
}

// NewRegistry creates and wires up all tracer instances.
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
