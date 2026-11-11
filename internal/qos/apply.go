package qos

import (
	"context"
	"encoding/json"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type Applier struct {
	Store  *core.Store
	Engine *Engine
}
type Status struct {
	Enabled bool     `json:"enabled"`
	Active  bool     `json:"active"`
	Reason  string   `json:"reason,omitempty"`
	Pending []string `json:"pending,omitempty"`
}

func (a Applier) Load() (Config, error) {
	d, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(d)
}
func (a Applier) Save(c Config) error {
	c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	d, err := a.Store.Load()
	if err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	d.Set(ModuleName, b)
	return a.Store.Save(d)
}
func (a Applier) Status() Status {
	c, err := a.Load()
	if err != nil {
		return Status{Reason: err.Error()}
	}
	if a.Engine == nil {
		if !c.Requested() {
			return Status{Reason: "No client policies configured"}
		}
		return Status{Enabled: true, Reason: "Traffic control backend is unavailable"}
	}
	return a.Engine.Status(context.Background(), c)
}

func (a Applier) Reconcile(ctx context.Context) error {
	c, err := a.Load()
	if err != nil {
		return err
	}
	if a.Engine == nil {
		return nil
	}
	return a.Engine.Apply(ctx, c)
}
