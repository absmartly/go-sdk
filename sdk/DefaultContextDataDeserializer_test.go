package sdk

import (
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"io/ioutil"
	"testing"
)

func TestDeserialize(t *testing.T) {
	var experiment = jsonmodels.Experiment{
		Id:            1,
		Name:          "exp_test_ab",
		Iteration:     1,
		UnitType:      "session_id",
		SeedHi:        3603515,
		SeedLo:        233373850,
		Split:         []float64{0.5, 0.5},
		TrafficSeedHi: 449867249,
		TrafficSeedLo: 455443629,
		TrafficSplit:  []float64{0.0, 1.0},
		FullOnVariant: 0,
		Applications:  []jsonmodels.ExperimentApplication{{Name: "website"}},
		Variants: []jsonmodels.ExperimentVariant{{
			Name:   "A",
			Config: "",
		},
			{
				Name:   "B",
				Config: "{\"banner.border\":1,\"banner.size\":\"large\"}",
			}},
		AudienceStrict: false,
		Audience:       "",
	}

	var experiment_2 = jsonmodels.Experiment{
		Id:            2,
		Name:          "exp_test_abc",
		Iteration:     1,
		UnitType:      "session_id",
		SeedHi:        55006150,
		SeedLo:        47189152,
		Split:         []float64{0.34, 0.33, 0.33},
		TrafficSeedHi: 705671872,
		TrafficSeedLo: 212903484,
		TrafficSplit:  []float64{0.0, 1.0},
		FullOnVariant: 0,
		Applications:  []jsonmodels.ExperimentApplication{{Name: "website"}},
		Variants: []jsonmodels.ExperimentVariant{{
			Name:   "A",
			Config: "",
		},
			{
				Name:   "B",
				Config: "{\"button.color\":\"blue\"}",
			},
			{
				Name:   "C",
				Config: "{\"button.color\":\"red\"}",
			}},
		AudienceStrict: false,
		Audience:       "",
	}

	var experiment_3 = jsonmodels.Experiment{
		Id:            3,
		Name:          "exp_test_not_eligible",
		Iteration:     1,
		UnitType:      "user_id",
		SeedHi:        503266407,
		SeedLo:        144942754,
		Split:         []float64{0.34, 0.33, 0.33},
		TrafficSeedHi: 87768905,
		TrafficSeedLo: 511357582,
		TrafficSplit:  []float64{0.99, 0.01},
		FullOnVariant: 0,
		Applications:  []jsonmodels.ExperimentApplication{{Name: "website"}},
		Variants: []jsonmodels.ExperimentVariant{{
			Name:   "A",
			Config: "",
		},
			{
				Name:   "B",
				Config: "{\"card.width\":\"80%\"}",
			},
			{
				Name:   "C",
				Config: "{\"card.width\":\"75%\"}",
			}},
		AudienceStrict: false,
		Audience:       "{}",
	}

	var experiment_4 = jsonmodels.Experiment{
		Id:            4,
		Name:          "exp_test_fullon",
		Iteration:     1,
		UnitType:      "session_id",
		SeedHi:        856061641,
		SeedLo:        990838475,
		Split:         []float64{0.25, 0.25, 0.25, 0.25},
		TrafficSeedHi: 360868579,
		TrafficSeedLo: 330937933,
		TrafficSplit:  []float64{0.0, 1.0},
		FullOnVariant: 2,
		Applications:  []jsonmodels.ExperimentApplication{{Name: "website"}},
		Variants: []jsonmodels.ExperimentVariant{{
			Name:   "A",
			Config: "",
		},
			{
				Name:   "B",
				Config: "{\"submit.color\":\"red\",\"submit.shape\":\"circle\"}",
			},
			{
				Name:   "C",
				Config: "{\"submit.color\":\"blue\",\"submit.shape\":\"rect\"}",
			},
			{
				Name:   "D",
				Config: "{\"submit.color\":\"green\",\"submit.shape\":\"square\"}",
			}},
		AudienceStrict: false,
		Audience:       "null",
	}

	var expected = jsonmodels.ContextData{
		Experiments: []jsonmodels.Experiment{
			experiment,
			experiment_2,
			experiment_3,
			experiment_4,
		},
	}

	content, err := ioutil.ReadFile("../context.json")
	assertAny(nil, err, t)
	var deser = DefaultContextDataDeserializer{}
	var result, er = deser.Deserialize(content)
	assertAny(nil, er, t)
	assertAny(expected, result, t)
}

func TestDeserializeBrokenJson(t *testing.T) {
	content, err := ioutil.ReadFile("../context-broken.json")
	assertAny(nil, err, t)
	var deser = DefaultContextDataDeserializer{}
	var result, er = deser.Deserialize(content)
	assertAny("invalid character '\\n' in string literal", er.Error(), t)
	assertAny(jsonmodels.ContextData{}, result, t)
}
