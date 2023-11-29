package absmartly

import (
	"context"

	"github.com/absmartly/go-sdk/internal/experiment"
	"github.com/absmartly/go-sdk/pkg/absmartly/field"
)

type SDK interface {
	Flush(ctx context.Context) error
	Close()
	QueueExposure(a Assignment)
	PushExposure(ctx context.Context, a Assignment) error
	Refresh(ctx context.Context) error
	UnitContext(u Units) *UnitContext
	getExperiment(name string) (experiment.Experiment, bool)
	CustomFieldValue(experiment string, key string) (field.Field, bool)
}

type NilSDK struct{}

func (n *NilSDK) Flush(_ context.Context) error { return nil }

func (n *NilSDK) Close() {}

func (n *NilSDK) QueueExposure(_ Assignment) {}

func (n *NilSDK) PushExposure(_ context.Context, _ Assignment) error { return nil }

func (n *NilSDK) Refresh(_ context.Context) error { return nil }

func (n *NilSDK) UnitContext(u Units) *UnitContext {
	return &UnitContext{
		u:  u,
		ab: n,
	}
}

func (n *NilSDK) getExperiment(_ string) (experiment.Experiment, bool) {
	return experiment.Experiment{}, false
}

func (n *NilSDK) CustomFieldValue(_ string, _ string) (field.Field, bool) {
	return field.Empty(), false
}

func Safe(sdk *ABSDK, err error) SDK {
	if err != nil {
		return &NilSDK{}
	}

	return sdk
}
