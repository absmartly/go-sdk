package experiment

import (
	"github.com/absmartly/go-sdk/internal/assigner"
	"github.com/absmartly/go-sdk/internal/model"
	"github.com/absmartly/go-sdk/pkg/absmartly/field"
)

type Experiment struct {
	Data model.Experiment // todo transform all required data to own fields

	Id       int
	UnitType string

	CustomFields map[string]field.Field
	Assigner     *assigner.Assigner
}

func New(data model.Experiment) Experiment {
	exp := Experiment{
		Data:         data,
		Id:           data.Id,
		UnitType:     data.UnitType,
		CustomFields: make(map[string]field.Field, len(data.CustomFields)),
	}
	for _, cf := range data.CustomFields {
		f, err := field.New(cf.Value, cf.Type)
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
