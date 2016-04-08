/*
// Copyright (c) 2016 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
*/

package main

import (
	"sync"

	"github.com/01org/ciao/networking/libsnnet"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/network"
	"github.com/golang/glog"
	"golang.org/x/net/context"
)

type dockerNetworkState struct {
	done chan struct{}
	err  error
}

var dockerNetworkMap struct {
	sync.Mutex
	networks map[string]*dockerNetworkState
}

func init() {
	dockerNetworkMap.networks = make(map[string]*dockerNetworkState)
}

func createDockerVnicV2(vnicCfg *libsnnet.VnicConfig) (*libsnnet.Vnic, *libsnnet.SsntpEventInfo, *libsnnet.ContainerInfo, error) {
	dockerNetworkMap.Lock()
	state := dockerNetworkMap.networks[vnicCfg.SubnetID]
	if state != nil {
		dockerNetworkMap.Unlock()
		glog.Info("Waiting for Docker network creation")
		<-state.done
		if state.err != nil {
			return nil, nil, nil, state.err
		}

		return cnNet.CreateVnicV2(vnicCfg)
	}
	ch := make(chan struct{})
	defer close(ch)
	state = &dockerNetworkState{done: ch}
	dockerNetworkMap.networks[vnicCfg.SubnetID] = state
	dockerNetworkMap.Unlock()
	vnic, event, info, err := cnNet.CreateVnicV2(vnicCfg)
	state.err = err
	if err != nil {
		return vnic, event, info, err
	}

	if event == nil {
		glog.Warning("EVENT information expected")
		return vnic, event, info, err
	}

	state.err = createDockerNetwork(context.Background(), info)
	return vnic, event, info, state.err
}

func destroyDockerVnicV2(vnicCfg *libsnnet.VnicConfig) (*libsnnet.SsntpEventInfo, error) {
	// BUG(markus): We need to pass in a context to destroyVnic

	event, info, err := cnNet.DestroyVnicV2(vnicCfg)
	if err != nil {
		glog.Errorf("cn.DestroyVnic failed %v", err)
		return event, err
	}

	if info != nil {
		destroyDockerNetwork(context.Background(), info.SubnetID)
		dockerNetworkMap.Lock()
		delete(dockerNetworkMap.networks, vnicCfg.SubnetID)
		dockerNetworkMap.Unlock()
	}

	return event, err
}

func createDockerNetwork(ctx context.Context, info *libsnnet.ContainerInfo) error {
	cli, err := getDockerClient()
	if err != nil {
		return err
	}

	_, err = cli.NetworkCreate(ctx, types.NetworkCreate{
		Name:   info.SubnetID,
		Driver: "ciao",
		IPAM: network.IPAM{
			Driver: "ciao",
			Config: []network.IPAMConfig{{
				Subnet:  info.Subnet.String(),
				Gateway: info.Gateway.String(),
			}}},
		Options: map[string]string{
			"bridge": info.Bridge,
		}})

	if err != nil {
		glog.Errorf("Unable to create docker network %s: %v", info.SubnetID, err)
	}

	return err
}

func destroyDockerNetwork(ctx context.Context, bridge string) error {
	cli, err := getDockerClient()
	if err != nil {
		return err
	}

	err = cli.NetworkRemove(ctx, bridge)
	if err != nil {
		glog.Errorf("Unable to remove docker network %s: %v", bridge, err)
	}

	return err
}
