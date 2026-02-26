package sdk

type ContextConfig struct {
	Units            map[string]string
	Attributes       map[string]interface{}
	Overrides        map[string]int
	CustomAssignments map[string]int
	EventLogger      ContextEventLogger
	PublishDelay     int64
	RefreshInterval  int64

	// Deprecated: Use Units instead.
	Units_ map[string]string
	// Deprecated: Use Attributes instead.
	Attributes_ map[string]interface{}
	// Deprecated: Use Overrides instead.
	Overrides_ map[string]int
	// Deprecated: Use CustomAssignments instead.
	Cassigmnents_ map[string]int
	// Deprecated: Use EventLogger instead.
	EventLogger_ ContextEventLogger
	// Deprecated: Use PublishDelay instead.
	PublishDelay_ int64
	// Deprecated: Use RefreshInterval instead.
	RefreshInterval_ int64
}

func (c ContextConfig) units() map[string]string {
	if len(c.Units) > 0 {
		return c.Units
	}
	if len(c.Units_) > 0 {
		return c.Units_
	}
	if c.Units != nil {
		return c.Units
	}
	return c.Units_
}

func (c ContextConfig) attributes() map[string]interface{} {
	if len(c.Attributes) > 0 {
		return c.Attributes
	}
	if len(c.Attributes_) > 0 {
		return c.Attributes_
	}
	if c.Attributes != nil {
		return c.Attributes
	}
	return c.Attributes_
}

func (c ContextConfig) overrides() map[string]int {
	if len(c.Overrides) > 0 {
		return c.Overrides
	}
	if len(c.Overrides_) > 0 {
		return c.Overrides_
	}
	if c.Overrides != nil {
		return c.Overrides
	}
	return c.Overrides_
}

func (c ContextConfig) customAssignments() map[string]int {
	if len(c.CustomAssignments) > 0 {
		return c.CustomAssignments
	}
	if len(c.Cassigmnents_) > 0 {
		return c.Cassigmnents_
	}
	if c.CustomAssignments != nil {
		return c.CustomAssignments
	}
	return c.Cassigmnents_
}

func (c ContextConfig) eventLogger() ContextEventLogger {
	if c.EventLogger != nil {
		return c.EventLogger
	}
	return c.EventLogger_
}

func (c ContextConfig) publishDelay() int64 {
	if c.PublishDelay != 0 {
		return c.PublishDelay
	}
	return c.PublishDelay_
}

func (c ContextConfig) refreshInterval() int64 {
	if c.RefreshInterval != 0 {
		return c.RefreshInterval
	}
	return c.RefreshInterval_
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
