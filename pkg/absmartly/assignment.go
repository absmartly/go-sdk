package absmartly

import (
	"encoding/json"
	"time"

	"github.com/absmartly/go-sdk/internal/model"
)

type assigned int

const (
	byNormal assigned = iota
	byFullOn assigned = iota
)

type assignment struct {
	id       int
	variant  int
	name     string
	unitType string
	unitHash string
	ts       time.Time
	by       assigned
}

func (a *assignment) Variant() int {
	return a.variant
}

func (a *assignment) encode() (json.RawMessage, error) {
	event := &model.Event{
		Hashed: true,
		Units: []model.Unit{{
			Type: a.unitType,
			Uid:  a.unitHash,
		}},
		PublishedAt: time.Now().UnixMilli(),
		Exposures: []model.Exposure{
			{
				Id:        a.id,
				Name:      a.name,
				Variant:   a.variant,
				ExposedAt: a.ts.UnixMilli(),
			},
		},
	}
	var msg json.RawMessage
	msg, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	return msg, nil
}
