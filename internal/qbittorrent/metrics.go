// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

type InstanceInfo struct {
	ID   int
	Name string
}

func (i *InstanceInfo) IDString() string {
	return fmt.Sprintf("%d", i.ID)
}

func (cp *ClientPool) GetAllInstances() []*InstanceInfo {
	instances, err := cp.instanceStore.List(false)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get instances for metrics")
		return nil
	}

	var result []*InstanceInfo
	for _, instance := range instances {
		result = append(result, &InstanceInfo{
			ID:   instance.ID,
			Name: instance.Name,
		})
	}

	return result
}

func (cp *ClientPool) IsHealthy(instanceID int) bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	client, exists := cp.clients[instanceID]
	if !exists {
		return false
	}

	return client.IsHealthy()
}
