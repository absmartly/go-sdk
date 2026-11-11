package sdk

type ContextConfig struct {
	Units_           map[string]string
	Attributes_      map[string]interface{}
	Overrides_       map[string]int
	Cassigmnents_    map[string]int
	EventLogger_     ContextEventLogger
	PublishDelay_    int64
	RefreshInterval_ int64

	// application_ and environment_ are the client's application and
	// environment names. ABsmartly.CreateContext(With) fills them from the
	// client before CreateContext runs, so the context has them set before the
	// data future delivers its Ready callback (which may evaluate an audience).
	// They are unexported, so callers cannot set them directly.
	application_ string
	environment_ string
}

func CreateDefaultContextConfig() ContextConfig {
	var cntx = ContextConfig{}
	cntx.PublishDelay_ = 1000
	cntx.RefreshInterval_ = 1000
	cntx.Units_ = map[string]string{}
	cntx.Attributes_ = map[string]interface{}{}
	cntx.Cassigmnents_ = map[string]int{}
	cntx.Overrides_ = map[string]int{}
	return cntx
}
