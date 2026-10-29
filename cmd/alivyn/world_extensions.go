package main

import (
	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/persona"
	"github.com/AsCat/alivyn/internal/world"
	"github.com/AsCat/alivyn/pkg/extension"
)

func worldExtensions(c config.Config, client model.Client) (*extension.Registry, error) {
	if c.EffectiveEngine() != "character_v2" || c.EffectiveWorldPack() != "tidehaven" {
		return nil, nil
	}
	pack, _ := persona.Get(c.EffectivePersonaPack())
	if pack.Standalone {
		return nil, nil
	}
	zone, err := c.Location()
	if err != nil {
		return nil, err
	}
	r := extension.NewRegistry()
	if err := r.Register(extension.Registration{
		Manifest:  extension.Manifest{ID: "alivyn.tidehaven", Version: "0.1.0", APIVersion: extension.APIVersion, Capabilities: []extension.Capability{extension.Simulate}},
		Simulator: world.SceneSimulator{Client: client, Model: c.Model, Zone: zone},
	}); err != nil {
		return nil, err
	}
	if err := r.Use("world.tidehaven", extension.Simulate, "alivyn.tidehaven"); err != nil {
		return nil, err
	}
	return r, nil
}
