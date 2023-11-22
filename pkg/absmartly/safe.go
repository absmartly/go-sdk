package absmartly

import (
	"context"

	"github.com/absmartly/go-sdk/internal/model"
)

type SDK interface {
	Flush(ctx context.Context) error
	Close()
	QueueExposure(a *assignment)
	PushExposure(ctx context.Context, a *assignment) error
	Refresh(ctx context.Context) error
	UnitContext(u Units) *UnitContext
	getExperiment(name string) (model.Experiment, bool)
}

type NilSDK struct{}

func (n *NilSDK) Flush(_ context.Context) error { return nil }

func (n *NilSDK) Close() {}

func (n *NilSDK) QueueExposure(_ *assignment) {}

func (n *NilSDK) PushExposure(_ context.Context, _ *assignment) error { return nil }

func (n *NilSDK) Refresh(_ context.Context) error { return nil }

func (n *NilSDK) UnitContext(u Units) *UnitContext {
	return &UnitContext{
		u:  u,
		ab: n,
	}
}

func (n *NilSDK) getExperiment(_ string) (model.Experiment, bool) {
	return model.Experiment{}, false
}

func Safe(sdk *ABSDK, err error) SDK {
	if err != nil {
		return &NilSDK{}
	}

	return sdk
}
