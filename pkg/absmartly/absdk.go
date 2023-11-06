package absmartly

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/absmartly/go-sdk/internal/api"
	"github.com/absmartly/go-sdk/internal/model"
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
	// data holds last successful loaded context info with type map[string]model.Experiment
	data atomic.Value

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

func (ab *ABSDK) QueueExposure(a *assignment) {
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

func (ab *ABSDK) PushExposure(ctx context.Context, a *assignment) error {
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
	// log.Println("Refresh")
	// debug.PrintStack()
	data, err := ab.loader.GetContext(ctx)
	if err != nil {
		return err
	}
	dataMap := make(map[string]model.Experiment, len(data.Experiments))
	for _, exp := range data.Experiments {
		dataMap[exp.Name] = exp
	}
	ab.data.Store(dataMap)

	return nil
}

func (ab *ABSDK) getExperiment(name string) (model.Experiment, bool) {
	dataMap := ab.data.Load().(map[string]model.Experiment)
	exp, found := dataMap[name]
	return exp, found
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

type Config struct {
	Endpoint    string
	ApiKey      string
	Application string
	Environment string

	BatchSize     uint
	BatchInterval time.Duration

	RefreshInterval time.Duration
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Endpoint:    os.Getenv("ABSMARTLY_ENDPOINT"),
		ApiKey:      os.Getenv("ABSMARTLY_APIKEY"),
		Application: os.Getenv("ABSMARTLY_APPLICATION"),
		Environment: os.Getenv("ABSMARTLY_ENVIRONMENT"),
	}
	if cfg.Endpoint == "" {
		return Config{}, fmt.Errorf("env ABSMARTLY_ENDPOINT is unset or empty")
	}
	if cfg.ApiKey == "" {
		return Config{}, fmt.Errorf("env ABSMARTLY_APIKEY is unset or empty")
	}
	if cfg.Application == "" {
		return Config{}, fmt.Errorf("env ABSMARTLY_APPLICATION is unset or empty")
	}
	if cfg.Environment == "" {
		return Config{}, fmt.Errorf("env ABSMARTLY_ENVIRONMENT is unset or empty")
	}

	return cfg, nil
}

func ConfigFromEnvMust() Config {
	cfg, err := ConfigFromEnv()
	if err != nil {
		panic(err)
	}

	return cfg
}
