// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import (
	"cmp"
	"context"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
	"golang.org/x/sync/errgroup"
)

type BindingInterface struct {
	Name      string           `json:"name"`
	Value     string           `json:"value"`
	Available bool             `json:"available"`
	Addresses []BindingAddress `json:"addresses"`
}

type BindingAddress struct {
	Address   string `json:"address"`
	Available bool   `json:"available"`
}

func NetworkBinding(ctx context.Context, client *qbt.Client, prefs qbt.AppPreferences) ([]BindingInterface, error) {
	listed, err := client.GetNetworkInterfacesCtx(ctx)
	if err != nil {
		return nil, err
	}

	interfaces := make([]BindingInterface, 0, len(listed)+2)
	interfaces = append(interfaces, BindingInterface{Value: "", Available: true})
	for _, iface := range listed {
		interfaces = append(interfaces, BindingInterface{Name: iface.Name, Value: iface.Value, Available: true})
	}

	group, groupCtx := errgroup.WithContext(ctx)
	for i := range interfaces {
		group.Go(func() error {
			addresses, err := client.GetNetworkInterfaceAddressesCtx(groupCtx, interfaces[i].Value)
			interfaces[i].Addresses = make([]BindingAddress, len(addresses))
			for j, address := range addresses {
				interfaces[i].Addresses[j] = BindingAddress{Address: address, Available: true}
			}
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	savedInterface := prefs.CurrentNetworkInterface
	i := slices.IndexFunc(interfaces, func(iface BindingInterface) bool { return iface.Value == savedInterface })
	if i < 0 {
		interfaces = append(interfaces, BindingInterface{Name: cmp.Or(prefs.CurrentInterfaceName, savedInterface), Value: savedInterface, Addresses: []BindingAddress{}})
		i = len(interfaces) - 1
	}

	savedAddress := prefs.CurrentInterfaceAddress
	isAddress := savedAddress != "" && savedAddress != "0.0.0.0" && savedAddress != "::"
	if isAddress && !slices.ContainsFunc(interfaces[i].Addresses, func(address BindingAddress) bool { return address.Address == savedAddress }) {
		interfaces[i].Addresses = append(interfaces[i].Addresses, BindingAddress{Address: savedAddress})
	}
	return interfaces, nil
}
