package jsonmodels

type PublishEvent struct {
	Hashed      bool              `json:"hashed"`
	Units       []Unit            `json:"units"`
	PublishedAt int64             `json:"publishedAt"`
	Exposures   []Exposure        `json:"exposures"`
	Goals       []GoalAchievement `json:"goals"`
	Attributes  []Attribute       `json:"attributes"`
}
