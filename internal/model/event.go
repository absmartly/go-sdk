package model

type Event struct {
	Hashed      bool              `json:"hashed"`
	Units       []Unit            `json:"units"`
	PublishedAt int64             `json:"publishedAt"`
	Exposures   []Exposure        `json:"exposures"`
	Goals       []GoalAchievement `json:"goals"`
	Attributes  []Attribute       `json:"attributes"`
}

type Unit struct {
	Type string `json:"type"`
	Uid  string `json:"uid"`
}

type Exposure struct {
	Id               int    `json:"id"`
	Name             string `json:"name"`
	Unit             string `json:"unit"`
	Variant          int    `json:"variant"`
	ExposedAt        int64  `json:"exposedAt"`
	Assigned         bool   `json:"assigned"`
	Eligible         bool   `json:"eligible"`
	Overridden       bool   `json:"overridden"`
	FullOn           bool   `json:"fullOn"`
	Custom           bool   `json:"custom"`
	AudienceMismatch bool   `json:"audienceMismatch"`
}

type GoalAchievement struct {
	Name       string `json:"name"`
	AchievedAt int64  `json:"achievedAt"`
	// todo interface{} type check
	Properties map[string]interface{} `json:"properties,omitempty,"`
}

type Attribute struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value,omitempty,"`
	SetAt int64       `json:"setAt"`
}
