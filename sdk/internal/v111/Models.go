// Package v111 holds the v1.1.1 context-data model, so the default
// deserializer can decode a payload exactly as v1.1.1 did: same partial data
// and same errors, including their offsets. The type names match the public
// jsonmodels ones because encoding/json puts them in its error texts.
package jsonmodels

import publicmodels "github.com/absmartly/go-sdk/sdk/jsonmodels"

type ContextData struct {
	Experiments []Experiment `json:"experiments"`
}

type Experiment struct {
	Id                int                                  `json:"id"`
	Name              string                               `json:"name"`
	UnitType          string                               `json:"unitType"`
	Iteration         int                                  `json:"iteration"`
	SeedHi            int                                  `json:"seedHi"`
	SeedLo            int                                  `json:"seedLo"`
	Split             []float64                            `json:"split"`
	TrafficSeedHi     int                                  `json:"trafficSeedHi"`
	TrafficSeedLo     int                                  `json:"trafficSeedLo"`
	TrafficSplit      []float64                            `json:"trafficSplit"`
	FullOnVariant     int                                  `json:"fullOnVariant"`
	Applications      []publicmodels.ExperimentApplication `json:"applications"`
	Variants          []publicmodels.ExperimentVariant     `json:"variants"`
	CustomFieldValues []publicmodels.CustomFieldValue      `json:"customFieldValues"`
	AudienceStrict    bool                                 `json:"audienceStrict"`
	Audience          string                               `json:"audience"`
}

// Public converts the decoded data to the public model, keeping a nil
// experiments slice nil.
func (d ContextData) Public() publicmodels.ContextData {
	var data publicmodels.ContextData
	if d.Experiments != nil {
		data.Experiments = make([]publicmodels.Experiment, len(d.Experiments))
		for i, e := range d.Experiments {
			data.Experiments[i] = publicmodels.Experiment{Id: e.Id, Name: e.Name, UnitType: e.UnitType,
				Iteration: e.Iteration, SeedHi: e.SeedHi, SeedLo: e.SeedLo, Split: e.Split,
				TrafficSeedHi: e.TrafficSeedHi, TrafficSeedLo: e.TrafficSeedLo, TrafficSplit: e.TrafficSplit,
				FullOnVariant: e.FullOnVariant, Applications: e.Applications, Variants: e.Variants,
				CustomFieldValues: e.CustomFieldValues, AudienceStrict: e.AudienceStrict, Audience: e.Audience}
		}
	}
	return data
}
