package absmartly

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/absmartly/go-sdk/internal/api"
	"github.com/absmartly/go-sdk/internal/experiment"
	"github.com/absmartly/go-sdk/internal/model"
	"github.com/absmartly/go-sdk/pkg/absmartly/types"
)

type dataLoader interface {
	GetContext(ctx context.Context) (*model.Context, error)
}

type eventPusher interface {
	PutEvents(ctx context.Context, events []json.RawMessage) error
	Flush(ctx context.Context) error
}

type ABSDK struct {
	loader dataLoader
	// data holds last successful loaded context info with type map[string]experiment.Experiment
	data atomic.Value
	// varIndex holds index of variable name to experiment name with type map[string]string
	varIndex atomic.Value

	pusher eventPusher

	done chan<- struct{}

	queue     chan json.RawMessage
	queueDone bool
	queueMu   sync.RWMutex
}

func New(ctx context.Context, cfg Config) (*ABSDK, error) {
	cli := api.NewClient(cfg.Endpoint, cfg.ApiKey, cfg.Application, cfg.Environment, nil)
	ab := &ABSDK{
		loader: cli,
		pusher: cli,
		done:   make(chan struct{}),
		queue:  make(chan json.RawMessage, 16),
	}
	if cfg.BatchInterval < 1*time.Millisecond {
		cfg.BatchInterval = 1 * time.Millisecond
	}
	if cfg.BatchSize > 1 {
		batch := api.NewBatcher(cfg.BatchSize, cfg.BatchInterval, cli)
		ab.pusher = batch
	}
	err := ab.Refresh(ctx)
	if err != nil {
		return nil, err
	}
	go ab.queueFetch()
	return ab, nil
}

func (ab *ABSDK) Flush(ctx context.Context) error {
	return ab.pusher.Flush(ctx)
}

func (ab *ABSDK) Close() {
	ab.queueMu.Lock()
	ab.queueDone = true
	ab.queueMu.Unlock()

	close(ab.done)
	close(ab.queue)
}

func (ab *ABSDK) queueFetch() {
	for {
		msg, ok := <-ab.queue
		if !ok {
			return
		}
		err := ab.pushExposureMsg(context.Background(), msg)
		if err != nil {
			// todo log
		}
	}
}

func (ab *ABSDK) QueueExposure(a Assignment) {
	msg, err := a.encode()
	if err != nil {
		// todo log
	}
	ab.queueMu.RLock()
	if ab.queueDone {
		ab.queueMu.RUnlock()
		err = ab.pushExposureMsg(context.TODO(), msg)
		if err != nil {
			// todo log
		}
		return
	}
	ab.queue <- msg
	ab.queueMu.RUnlock()
}

func (ab *ABSDK) PushExposure(ctx context.Context, a Assignment) error {
	msg, err := a.encode()
	if err != nil {
		return err
	}
	return ab.pushExposureMsg(ctx, msg)
}

func (ab *ABSDK) pushExposureMsg(ctx context.Context, msg json.RawMessage) error {
	return ab.pusher.PutEvents(ctx, []json.RawMessage{msg})
}

func (ab *ABSDK) Refresh(ctx context.Context) error {
	data, err := ab.loader.GetContext(ctx)
	if err != nil {
		return err
	}
	dataMap := make(map[string]experiment.Experiment, len(data.Experiments))
	varIndex := make(map[string]string)
	for _, exp := range data.Experiments {
		e := experiment.New(exp)
		dataMap[exp.Name] = e
		for _, vars := range e.Variables {
			for key := range vars {
				if _, ok := varIndex[key]; ok {
					// todo log overwrite var index
				}
				varIndex[key] = exp.Name
			}
		}
	}
	ab.varIndex.Store(varIndex)
	ab.data.Store(dataMap)

	return nil
}

func (ab *ABSDK) experimentNameByVariable(variable string) (string, bool) {
	varIndex := ab.varIndex.Load().(map[string]string)
	name, ok := varIndex[variable]
	return name, ok
}

func (ab *ABSDK) experiment(name string) (experiment.Experiment, bool) {
	dataMap := ab.data.Load().(map[string]experiment.Experiment)
	exp, found := dataMap[name]
	return exp, found
}

func (ab *ABSDK) CustomFieldValue(experiment string, key string) (types.Field, bool) {
	exp, found := ab.experiment(experiment)
	if !found {
		return types.EmptyField(), false
	}
	f, found := exp.CustomFields[key]

	return f, found
}

func (ab *ABSDK) UnitContext(u Units) *UnitContext {
	uc := &UnitContext{
		u:  u,
		ab: ab,
	}
	return uc
}

type Units map[string]string

func NewUnitsMust(key, value string, others ...string) Units {
	if len(others)%2 != 0 {
		panic("even number of arguments required")
	}
	u := make(Units, len(others)/2+1)
	u[key] = value
	for i := 0; i < len(others); i += 2 {
		u[others[i]] = others[i+1]
	}

	return u
}
