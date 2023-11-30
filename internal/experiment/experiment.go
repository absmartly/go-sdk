package experiment

import (
	"encoding/json"

	"github.com/absmartly/go-sdk/internal/assigner"
	"github.com/absmartly/go-sdk/internal/model"
	"github.com/absmartly/go-sdk/pkg/absmartly/types"
)

type Experiment struct {
	Data model.Experiment // todo  temporary field, transform all required data to own fields

	Id       int
	UnitType string

	Variables    []map[string]types.Variable
	CustomFields map[string]types.Field
	Assigner     *assigner.Assigner
}

func New(data model.Experiment) Experiment {
	exp := Experiment{
		Data:         data,
		Id:           data.Id,
		UnitType:     data.UnitType,
		Variables:    make([]map[string]types.Variable, len(data.Variants)),
		CustomFields: make(map[string]types.Field, len(data.CustomFields)),
	}
	for i, variant := range data.Variants {
		err := json.Unmarshal([]byte(variant.Config), &exp.Variables[i])
		if err != nil {
			// todo log
		}
	}
	for _, cf := range data.CustomFields {
		f, err := types.NewField(cf.Value, cf.Type)
		if err != nil {
			// todo log
		}
		if _, ok := exp.CustomFields[cf.Name]; ok {
			// todo log overwrite
		}
		exp.CustomFields[cf.Name] = f
	}
	exp.Assigner = assigner.New(uint32(data.SeedHi), uint32(data.SeedLo), data.Split)

	return exp
}
