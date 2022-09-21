package sdk

type ContextConfig struct {
	Units_           map[string]string
	Attributes_      map[string]interface{}
	Overrides_       map[string]int
	Cassigmnents_    map[string]int
	EventLogger_     ContextEventLogger
	PublishDelay_    int64
	RefreshInterval_ int64
}

func CreateDefaultContextConfig() ContextConfig {
	var cntx = ContextConfig{}
	cntx.PublishDelay_ = 100
	cntx.RefreshInterval_ = 0
	cntx.Units_ = map[string]string{}
	cntx.Attributes_ = map[string]interface{}{}
	cntx.Cassigmnents_ = map[string]int{}
	cntx.Overrides_ = map[string]int{}
	return cntx
}
