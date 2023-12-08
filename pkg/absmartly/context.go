package absmartly

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/absmartly/go-sdk/internal/model"
	"github.com/absmartly/go-sdk/pkg/absmartly/types"
)

var ErrExpNotFound = errors.New("experiment not found")
var ErrVarNotFound = errors.New("variable not found")

type UnitContext struct {
	u  Units
	ab SDK

	attributes   []model.Attribute
	attributesMu sync.RWMutex
}

func (uc *UnitContext) SetAttribute(key string, value interface{}) {
	a := model.Attribute{
		Name:  key,
		Value: value,
		SetAt: time.Now().UnixMilli(),
	}
	uc.attributesMu.Lock()
	uc.attributes = append(uc.attributes, a)
	uc.attributesMu.Unlock()
}

func (uc *UnitContext) SetAttributes(kv map[string]interface{}) {
	ts := time.Now().UnixMilli()
	a := make([]model.Attribute, 0, len(kv))
	for key, value := range kv {
		a = append(a, model.Attribute{
			Name:  key,
			Value: value,
			SetAt: ts,
		})
	}
	uc.attributesMu.Lock()
	uc.attributes = append(uc.attributes, a...)
	uc.attributesMu.Unlock()
}

func (uc *UnitContext) GetTreatment(experiment string) (int, error) {
	a, err := uc.GetAssignment(experiment)
	if err != nil {
		return 0, err
	}
	uc.QueueExposure(a)

	return a.Variant(), nil
}

func (uc *UnitContext) PeekTreatment(experiment string) (int, *assignment, error) {
	a, err := uc.GetAssignment(experiment)
	if err != nil {
		return 0, nil, err
	}

	return a.Variant(), a, nil
}

func (uc *UnitContext) GetVariable(key string, defaultValue interface{}) (types.Variable, error) {
	v, a, err := uc.PeekVariableValue(key, defaultValue)
	if err == nil {
		uc.QueueExposure(a)
	}

	return v, err
}

func (uc *UnitContext) PeekVariableValue(key string, defaultValue interface{}) (types.Variable, *assignment, error) {
	dv := types.NewVariable(defaultValue)
	experiment, found := uc.ab.experimentNameByVariable(key)
	if !found {
		return dv, nil, ErrVarNotFound
	}
	variant, a, err := uc.PeekTreatment(experiment)
	if err != nil {
		return dv, nil, err
	}
	exp, found := uc.ab.experiment(experiment)
	if !found {
		return dv, nil, ErrExpNotFound
	}
	if v, ok := exp.Variables[variant][key]; ok {
		return v, a, nil
	}

	return dv, a, nil
}

func (uc *UnitContext) GetAssignment(experiment string) (*assignment, error) {
	exp, found := uc.ab.experiment(experiment)
	if !found {
		return nil, ErrExpNotFound
	}
	_ = exp.UnitType
	a := &assignment{
		id:   exp.Id,
		name: experiment,
		ts:   time.Now(),
	}
	uc.attributesMu.RLock()
	copy(a.attr, uc.attributes)
	uc.attributesMu.RUnlock()

	unitType := exp.UnitType
	unitValue, unitFound := uc.u[unitType]

	switch {
	case exp.Data.FullOnVariant > 0:
		a.variant = exp.Data.FullOnVariant
		a.by = byFullOn
	case unitFound:
		a.variant, a.unitHash = exp.Assigner.Assign(unitValue)
		a.unitType = unitType
		a.by = byNormal
	}

	return a, nil
}

func (uc *UnitContext) QueueExposure(a Assignment) {
	uc.ab.QueueExposure(a)
}

func (uc *UnitContext) PushExposure(ctx context.Context, a *assignment) error {
	return uc.ab.PushExposure(ctx, a)
}

func (uc *UnitContext) Flush(ctx context.Context) error {
	return uc.ab.Flush(ctx)
}
