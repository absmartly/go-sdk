package jsonmodels

type GoalAchievement struct {
	Name       string                 `json:"name"`
	AchievedAt int64                  `json:"achievedAt"`
	Properties map[string]interface{} `json:"properties,omitempty,"`
}
