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

package datastore

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/01org/ciao/ciao-controller/types"
	"github.com/01org/ciao/ciao-storage"
	"github.com/01org/ciao/payloads"
	"github.com/01org/ciao/ssntp/uuid"
)

func newTenantHardwareAddr(ip net.IP) (hw net.HardwareAddr) {
	buf := make([]byte, 6)
	ipBytes := ip.To4()
	buf[0] |= 2
	buf[1] = 0
	copy(buf[2:6], ipBytes)
	hw = net.HardwareAddr(buf)
	return
}

func addTestInstance(tenant *types.Tenant, workload *types.Workload) (instance *types.Instance, err error) {
	id := uuid.Generate()

	ip, err := ds.AllocateTenantIP(tenant.ID)
	if err != nil {
		return
	}

	mac := newTenantHardwareAddr(ip)

	resources := make(map[string]int)
	rr := workload.Defaults

	for i := range rr {
		resources[string(rr[i].Type)] = rr[i].Value
	}

	instance = &types.Instance{
		TenantID:   tenant.ID,
		WorkloadID: workload.ID,
		State:      payloads.Pending,
		ID:         id.String(),
		CNCI:       false,
		IPAddress:  ip.String(),
		MACAddress: mac.String(),
		Usage:      resources,
	}

	err = ds.AddInstance(instance)
	if err != nil {
		return
	}

	return
}

func addTestTenant() (tenant *types.Tenant, err error) {
	/* add a new tenant */
	tuuid := uuid.Generate()
	tenant, err = ds.AddTenant(tuuid.String())
	if err != nil {
		return
	}

	// Add fake CNCI
	err = ds.AddTenantCNCI(tuuid.String(), uuid.Generate().String(), tenant.CNCIMAC)
	if err != nil {
		return
	}
	err = ds.AddCNCIIP(tenant.CNCIMAC, "192.168.0.1")
	if err != nil {
		return
	}
	return
}

func addTestInstanceStats(t *testing.T) ([]*types.Instance, payloads.Stat) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	var instances []*types.Instance

	for i := 0; i < 10; i++ {
		instance, err := addTestInstance(tenant, wls[0])
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, instance)
	}

	var stats []payloads.InstanceStat

	for i := range instances {
		stat := payloads.InstanceStat{
			InstanceUUID:  instances[i].ID,
			State:         payloads.ComputeStatusRunning,
			SSHIP:         "192.168.0.1",
			SSHPort:       34567,
			MemoryUsageMB: 0,
			DiskUsageMB:   0,
			CPUUsage:      0,
		}
		stats = append(stats, stat)
	}
	stat := payloads.Stat{
		NodeUUID:        uuid.Generate().String(),
		MemTotalMB:      256,
		MemAvailableMB:  256,
		DiskTotalMB:     1024,
		DiskAvailableMB: 1024,
		Load:            20,
		CpusOnline:      4,
		NodeHostName:    "test",
		Instances:       stats,
	}

	err = ds.addNodeStat(stat)
	if err != nil {
		t.Fatal(err)
	}

	err = ds.addInstanceStats(stats, stat.NodeUUID)
	if err != nil {
		t.Fatal(err)
	}

	return instances, stat
}

func BenchmarkGetTenantNoCache(b *testing.B) {
	/* add a new tenant */
	tuuid := uuid.Generate().String()
	_, err := ds.AddTenant(tuuid)
	if err != nil {
		b.Error(err)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err = ds.db.getTenantNoCache(tuuid)
		if err != nil {
			b.Error(err)
		}
	}
}

func BenchmarkAllocateTenantIP(b *testing.B) {
	/* add a new tenant */
	tuuid := uuid.Generate().String()
	_, err := ds.AddTenant(tuuid)
	if err != nil {
		b.Error(err)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err = ds.AllocateTenantIP(tuuid)
		if err != nil {
			b.Error(err)
		}
	}
}

func BenchmarkGetAllInstances(b *testing.B) {
	for n := 0; n < b.N; n++ {
		_, err := ds.GetAllInstances()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestTenantCreate(t *testing.T) {
	var err error

	/* add a new tenant */
	tuuid := uuid.Generate()
	tenant, err := ds.AddTenant(tuuid.String())
	if err != nil {
		t.Fatal(err)
	}
	tenant, err = ds.GetTenant(tuuid.String())
	if err != nil {
		t.Fatal(err)
	}
	if tenant == nil {
		t.Fatal(err)
	}
}

func TestGetWorkloads(t *testing.T) {
	wls, err := ds.getWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}
}

func TestAddInstance(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	_, err = addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteInstanceResources(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	// update tenant Info
	tenantBefore, err := ds.getTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	resourcesBefore := make(map[string]int)
	for i := range tenantBefore.Resources {
		r := tenantBefore.Resources[i]
		resourcesBefore[r.Rname] = r.Usage
	}

	time.Sleep(1 * time.Second)

	err = ds.DeleteInstance(instance.ID)
	if err != nil {
		t.Fatal(err)
	}

	tenantAfter, err := ds.getTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	defaults := wls[0].Defaults

	usage := make(map[string]int)
	for i := range defaults {
		usage[string(defaults[i].Type)] = defaults[i].Value
	}

	resourcesAfter := make(map[string]int)
	for i := range tenantAfter.Resources {
		r := tenantAfter.Resources[i]
		resourcesAfter[r.Rname] = r.Usage
	}

	// make sure usage was reduced by workload defaults values
	for name, val := range resourcesAfter {
		before := resourcesBefore[name]
		delta := usage[name]

		if name == "instances" {
			if val != before-1 {
				t.Fatal("instances not decremented")
			}
		} else if val != before-delta {
			t.Fatal("usage not reduced")
		}
	}
}

func TestDeleteInstanceNetwork(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	err = ds.DeleteInstance(instance.ID)
	if err != nil {
		t.Fatal(err)
	}

	tenantAfter, err := ds.getTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	ip := net.ParseIP(instance.IPAddress)

	ipBytes := ip.To4()
	if ipBytes == nil {
		t.Fatal(errors.New("Unable to convert ip to bytes"))
	}

	subnetInt := binary.BigEndian.Uint16(ipBytes[1:3])

	// confirm that tenant map shows it not used.
	if tenantAfter.network[int(subnetInt)][int(ipBytes[3])] != false {
		t.Fatal("IP Address not released from cache")
	}

	time.Sleep(1 * time.Second)

	// clear tenant from cache
	ds.tenantsLock.Lock()
	delete(ds.tenants, tenant.ID)
	ds.tenantsLock.Unlock()

	// get updated tenant info - should hit database
	newTenant, err := ds.getTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	// confirm that tenant map shows it not used.
	if newTenant.network[int(subnetInt)][int(ipBytes[3])] != false {
		t.Fatal("IP Address not released from database")
	}
}

func TestGetAllInstances(t *testing.T) {
	instancesBefore, err := ds.GetAllInstances()
	if err != nil {
		t.Fatal(err)
	}

	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	for i := 0; i < 10; i++ {
		_, err = addTestInstance(tenant, wls[0])
		if err != nil {
			t.Fatal(err)
		}
	}

	instances, err := ds.GetAllInstances()
	if err != nil {
		t.Fatal(err)
	}

	if len(instances) != (len(instancesBefore) + 10) {
		t.Fatal(err)
	}
}

func TestGetAllInstancesFromTenant(t *testing.T) {
	var err error

	/* add a new tenant */
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	for i := 0; i < 10; i++ {
		_, err = addTestInstance(tenant, wls[0])
		if err != nil {
			t.Fatal(err)
		}
	}

	// if we don't get 10 eventually, the test will timeout and fail
	instances, err := ds.GetAllInstancesFromTenant(tenant.ID)
	for len(instances) < 10 {
		time.Sleep(1 * time.Second)
		instances, err = ds.GetAllInstancesFromTenant(tenant.ID)
	}

	if err != nil {
		t.Fatal(err)
	}

	if len(instances) < 10 {
		t.Fatal("Didn't get right number of instances")
	}
}

func TestGetAllInstancesByNode(t *testing.T) {
	instances, stat := addTestInstanceStats(t)
	newInstances, err := ds.GetAllInstancesByNode(stat.NodeUUID)
	if err != nil {
		t.Fatal(err)
	}

	retry := 5
	for len(newInstances) < len(instances) && retry > 0 {
		retry--
		time.Sleep(1 * time.Second)
		newInstances, err = ds.GetAllInstancesByNode(stat.NodeUUID)
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(newInstances) != len(instances) {
		msg := fmt.Sprintf("expected %d instances, got %d", len(instances), len(newInstances))
		t.Fatal(msg)
	}
}

func TestGetInstance(t *testing.T) {
	instances, stat := addTestInstanceStats(t)
	instance, err := ds.GetInstance(instances[0].ID)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}

	for instance == nil {
		time.Sleep(1 * time.Second)
		instance, err = ds.GetInstance(instances[0].ID)
		if err != nil && err != sql.ErrNoRows {
			t.Fatal(err)
		}
	}

	if instance.NodeID != stat.NodeUUID {
		t.Fatal("retrieved incorrect NodeID")
	}

	if instance.State != payloads.ComputeStatusRunning {
		t.Fatal("retrieved incorrect state")
	}
}

func TestHandleStats(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	var instances []*types.Instance

	for i := 0; i < 10; i++ {
		instance, err := addTestInstance(tenant, wls[0])
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, instance)
	}

	var stats []payloads.InstanceStat

	for i := range instances {
		stat := payloads.InstanceStat{
			InstanceUUID:  instances[i].ID,
			State:         payloads.ComputeStatusRunning,
			SSHIP:         "192.168.0.1",
			SSHPort:       34567,
			MemoryUsageMB: 0,
			DiskUsageMB:   0,
			CPUUsage:      0,
		}
		stats = append(stats, stat)
	}
	stat := payloads.Stat{
		NodeUUID:        uuid.Generate().String(),
		MemTotalMB:      256,
		MemAvailableMB:  256,
		DiskTotalMB:     1024,
		DiskAvailableMB: 1024,
		Load:            20,
		CpusOnline:      4,
		NodeHostName:    "test",
		Instances:       stats,
	}

	err = ds.HandleStats(stat)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	// check instance stats recorded
	for i := range stats {
		id := stats[i].InstanceUUID
		instance, err := ds.GetInstance(id)
		if err != nil {
			t.Fatal(err)
		}

		if instance.NodeID != stat.NodeUUID {
			t.Fatal("Incorrect NodeID in stats table")
		}

		if instance.State != payloads.ComputeStatusRunning {
			t.Fatal("state not updated")
		}
	}
}

func TestGetInstanceLastStats(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	var instances []*types.Instance

	for i := 0; i < 10; i++ {
		instance, err := addTestInstance(tenant, wls[0])
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, instance)
	}

	var stats []payloads.InstanceStat

	for i := range instances {
		stat := payloads.InstanceStat{
			InstanceUUID:  instances[i].ID,
			State:         payloads.ComputeStatusRunning,
			SSHIP:         "192.168.0.1",
			SSHPort:       34567,
			MemoryUsageMB: 0,
			DiskUsageMB:   0,
			CPUUsage:      0,
		}
		stats = append(stats, stat)
	}
	stat := payloads.Stat{
		NodeUUID:        uuid.Generate().String(),
		MemTotalMB:      256,
		MemAvailableMB:  256,
		DiskTotalMB:     1024,
		DiskAvailableMB: 1024,
		Load:            20,
		CpusOnline:      4,
		NodeHostName:    "test",
		Instances:       stats,
	}

	err = ds.HandleStats(stat)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	serverStats := ds.GetInstanceLastStats(stat.NodeUUID)

	if len(serverStats.Servers) != len(instances) {
		t.Fatal("Not enough instance stats retrieved")
	}
}

func TestGetNodeLastStats(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	var instances []*types.Instance

	for i := 0; i < 10; i++ {
		instance, err := addTestInstance(tenant, wls[0])
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, instance)
	}

	var stats []payloads.InstanceStat

	for i := range instances {
		stat := payloads.InstanceStat{
			InstanceUUID:  instances[i].ID,
			State:         payloads.ComputeStatusRunning,
			SSHIP:         "192.168.0.1",
			SSHPort:       34567,
			MemoryUsageMB: 0,
			DiskUsageMB:   0,
			CPUUsage:      0,
		}
		stats = append(stats, stat)
	}
	stat := payloads.Stat{
		NodeUUID:        uuid.Generate().String(),
		MemTotalMB:      256,
		MemAvailableMB:  256,
		DiskTotalMB:     1024,
		DiskAvailableMB: 1024,
		Load:            20,
		CpusOnline:      4,
		NodeHostName:    "test",
		Instances:       stats,
	}

	err = ds.HandleStats(stat)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	computeNodes := ds.GetNodeLastStats()

	// how many compute Nodes should be here?  If we want to
	// control we need to clear out previous test stats
	if len(computeNodes.Nodes) == 0 {
		t.Fatal("Not enough compute Nodes found")
	}
}

func TestGetBatchFrameStatistics(t *testing.T) {
	var nodes []payloads.SSNTPNode
	for i := 0; i < 3; i++ {
		node := payloads.SSNTPNode{
			SSNTPUUID:   uuid.Generate().String(),
			SSNTPRole:   "test",
			TxTimestamp: time.Now().Format(time.RFC3339Nano),
			RxTimestamp: time.Now().Format(time.RFC3339Nano),
		}
		nodes = append(nodes, node)
	}

	var frames []payloads.FrameTrace
	for i := 0; i < 3; i++ {
		stat := payloads.FrameTrace{
			Label:          "batch_frame_test",
			Type:           "type",
			Operand:        "operand",
			StartTimestamp: time.Now().Format(time.RFC3339Nano),
			EndTimestamp:   time.Now().Format(time.RFC3339Nano),
			Nodes:          nodes,
		}
		frames = append(frames, stat)
	}

	trace := payloads.Trace{
		Frames: frames,
	}

	err := ds.HandleTraceReport(trace)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.db.getBatchFrameStatistics("batch_frame_test")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetBatchFrameSummary(t *testing.T) {
	var nodes []payloads.SSNTPNode
	for i := 0; i < 3; i++ {
		node := payloads.SSNTPNode{
			SSNTPUUID:   uuid.Generate().String(),
			SSNTPRole:   "test",
			TxTimestamp: time.Now().Format(time.RFC3339Nano),
			RxTimestamp: time.Now().Format(time.RFC3339Nano),
		}
		nodes = append(nodes, node)
	}

	var frames []payloads.FrameTrace
	for i := 0; i < 3; i++ {
		stat := payloads.FrameTrace{
			Label:          "batch_summary_test",
			Type:           "type",
			Operand:        "operand",
			StartTimestamp: time.Now().Format(time.RFC3339Nano),
			EndTimestamp:   time.Now().Format(time.RFC3339Nano),
			Nodes:          nodes,
		}
		frames = append(frames, stat)
	}

	trace := payloads.Trace{
		Frames: frames,
	}

	err := ds.HandleTraceReport(trace)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.db.getBatchFrameSummary()
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetNodeSummary(t *testing.T) {
	_, err := ds.db.getNodeSummary()
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetEventLog(t *testing.T) {
	err := ds.db.logEvent("test-tenantID", "info", "this is a test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.db.getEventLog()
	if err != nil {
		t.Fatal(err)
	}
}

func TestLogEvent(t *testing.T) {
	err := ds.db.logEvent("test-tenantID", "info", "this is a test")
	if err != nil {
		t.Fatal(err)
	}
}

func TestClearLog(t *testing.T) {
	err := ds.db.clearLog()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAddFrameStat(t *testing.T) {
	var nodes []payloads.SSNTPNode
	for i := 0; i < 3; i++ {
		node := payloads.SSNTPNode{
			SSNTPUUID:   uuid.Generate().String(),
			SSNTPRole:   "test",
			TxTimestamp: time.Now().Format(time.RFC3339Nano),
			RxTimestamp: time.Now().Format(time.RFC3339Nano),
		}
		nodes = append(nodes, node)
	}

	stat := payloads.FrameTrace{
		Label:          "test",
		Type:           "type",
		Operand:        "operand",
		StartTimestamp: time.Now().Format(time.RFC3339Nano),
		EndTimestamp:   time.Now().Format(time.RFC3339Nano),
		Nodes:          nodes,
	}
	err := ds.db.addFrameStat(stat)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAddInstanceStats(t *testing.T) {
	var stats []payloads.InstanceStat

	for i := 0; i < 3; i++ {
		stat := payloads.InstanceStat{
			InstanceUUID:  uuid.Generate().String(),
			State:         payloads.ComputeStatusRunning,
			SSHIP:         "192.168.0.1",
			SSHPort:       34567,
			MemoryUsageMB: 0,
			DiskUsageMB:   0,
			CPUUsage:      0,
		}
		stats = append(stats, stat)
	}

	nodeID := uuid.Generate().String()

	err := ds.addInstanceStats(stats, nodeID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAddNodeStats(t *testing.T) {
	var stats []payloads.InstanceStat

	for i := 0; i < 3; i++ {
		stat := payloads.InstanceStat{
			InstanceUUID:  uuid.Generate().String(),
			State:         payloads.ComputeStatusRunning,
			SSHIP:         "192.168.0.1",
			SSHPort:       34567,
			MemoryUsageMB: 0,
			DiskUsageMB:   0,
			CPUUsage:      0,
		}
		stats = append(stats, stat)
	}
	stat := payloads.Stat{
		NodeUUID:        uuid.Generate().String(),
		MemTotalMB:      256,
		MemAvailableMB:  256,
		DiskTotalMB:     1024,
		DiskAvailableMB: 1024,
		Load:            20,
		CpusOnline:      4,
		NodeHostName:    "test",
		Instances:       stats,
	}

	err := ds.addNodeStat(stat)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAllocateTenantIP(t *testing.T) {
	/* add a new tenant */
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	ip, err := ds.AllocateTenantIP(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	// this should hit cache
	newTenant, err := ds.getTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	ipBytes := ip.To4()
	if ipBytes == nil {
		t.Fatal(errors.New("Unable to convert ip to bytes"))
	}

	subnetInt := int(binary.BigEndian.Uint16(ipBytes[1:3]))
	host := int(ipBytes[3])

	if newTenant.network[subnetInt][host] != true {
		t.Fatal("IP Address not claimed in cache")
	}

	time.Sleep(5 * time.Second)

	// clear out cache
	ds.tenantsLock.Lock()
	delete(ds.tenants, tenant.ID)
	ds.tenantsLock.Unlock()

	// this should not hit cache
	newTenant, err = ds.getTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	if newTenant.network[subnetInt][host] != true {
		t.Fatal("IP Address not claimed in database")
	}
}

func TestNonOverlappingTenantIP(t *testing.T) {
	/* add a new tenant */
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	ip1, err := ds.AllocateTenantIP(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	tenant, err = addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	ip2, err := ds.AllocateTenantIP(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	// make sure the subnet for ip1 and ip2 don't match
	b1 := ip1.To4()
	subnetInt1 := binary.BigEndian.Uint16(b1[1:3])
	b2 := ip2.To4()
	subnetInt2 := binary.BigEndian.Uint16(b2[1:3])
	if subnetInt1 == subnetInt2 {
		t.Fatal(errors.New("Tenant subnets must not overlap"))
	}
}

func TestGetCNCIWorkloadID(t *testing.T) {
	_, err := ds.db.getCNCIWorkloadID()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAddLimit(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	/* put tenant limit of 1 instance */
	err = ds.AddLimit(tenant.ID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}

	// make sure cache was updated
	ds.tenantsLock.Lock()
	t2 := ds.tenants[tenant.ID]
	delete(ds.tenants, tenant.ID)
	ds.tenantsLock.Unlock()

	for i := range t2.Resources {
		if t2.Resources[i].Rtype == 1 {
			if t2.Resources[i].Limit != 1 {
				t.Fatal(err)
			}
		}
	}

	// make sure datastore was updated
	t3, err := ds.GetTenant(tenant.ID)
	for i := range t3.Resources {
		if t3.Resources[i].Rtype == 1 {
			if t3.Resources[i].Limit != 1 {
				t.Fatal(err)
			}
		}
	}
}

func TestRemoveTenantCNCI(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	err = ds.removeTenantCNCI(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	// make sure cache was updated
	ds.tenantsLock.Lock()
	t2 := ds.tenants[tenant.ID]
	delete(ds.tenants, tenant.ID)
	ds.tenantsLock.Unlock()

	if t2.CNCIID != "" || t2.CNCIIP != "" {
		t.Fatal("Cache Not Updated")
	}

	// check database was updated
	testTenant, err := ds.GetTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if testTenant.CNCIID != "" || testTenant.CNCIIP != "" {
		t.Fatal("Database not updated")
	}
}

func TestGetTenant(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	testTenant, err := ds.GetTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if testTenant.ID != tenant.ID {
		t.Fatal(err)
	}
}

func TestGetAllTenants(t *testing.T) {
	_, err := ds.GetAllTenants()
	if err != nil {
		t.Fatal(err)
	}
	// for now, just check that the query has no
	// errors.
}

func TestAddCNCIIP(t *testing.T) {
	/* add a new tenant */
	tuuid := uuid.Generate()
	tenant, err := ds.AddTenant(tuuid.String())
	if err != nil {
		t.Fatal(err)
	}

	// Add fake CNCI
	err = ds.AddTenantCNCI(tenant.ID, uuid.Generate().String(), tenant.CNCIMAC)
	if err != nil {
		t.Fatal(err)
	}

	// make sure that AddCNCIIP signals the channel it's supposed to
	c := make(chan bool)
	ds.cnciAddedLock.Lock()
	ds.cnciAddedChans[tenant.ID] = c
	ds.cnciAddedLock.Unlock()

	go func() {
		err := ds.AddCNCIIP(tenant.CNCIMAC, "192.168.0.1")
		if err != nil {
			t.Fatal(err)
		}
	}()

	success := <-c
	if !success {
		t.Fatal(err)
	}

	// confirm that the channel was cleared
	ds.cnciAddedLock.Lock()
	c = ds.cnciAddedChans[tenant.ID]
	ds.cnciAddedLock.Unlock()
	if c != nil {
		t.Fatal(err)
	}
}

func TestHandleTraceReport(t *testing.T) {
	var nodes []payloads.SSNTPNode
	for i := 0; i < 3; i++ {
		node := payloads.SSNTPNode{
			SSNTPUUID:   uuid.Generate().String(),
			SSNTPRole:   "test",
			TxTimestamp: time.Now().Format(time.RFC3339Nano),
			RxTimestamp: time.Now().Format(time.RFC3339Nano),
		}
		nodes = append(nodes, node)
	}

	var frames []payloads.FrameTrace
	for i := 0; i < 3; i++ {
		stat := payloads.FrameTrace{
			Label:          "test",
			Type:           "type",
			Operand:        "operand",
			StartTimestamp: time.Now().Format(time.RFC3339Nano),
			EndTimestamp:   time.Now().Format(time.RFC3339Nano),
			Nodes:          nodes,
		}
		frames = append(frames, stat)
	}

	trace := payloads.Trace{
		Frames: frames,
	}

	err := ds.HandleTraceReport(trace)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetCNCISummary(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	// test without null cnciid
	_, err = ds.GetTenantCNCISummary(tenant.CNCIID)
	if err != nil {
		t.Fatal(err)
	}

	// test with null cnciid
	_, err = ds.GetTenantCNCISummary("")
	if err != nil {
		t.Fatal(err)
	}

}

func TestReleaseTenantIP(t *testing.T) {
	/* add a new tenant */
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	ip, err := ds.AllocateTenantIP(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	ipBytes := ip.To4()
	if ipBytes == nil {
		t.Fatal(errors.New("Unable to convert ip to bytes"))
	}
	subnetInt := binary.BigEndian.Uint16(ipBytes[1:3])

	// get updated tenant info
	newTenant, err := ds.getTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	// confirm that tenant map shows it used.
	if newTenant.network[int(subnetInt)][int(ipBytes[3])] != true {
		t.Fatal("IP Address not marked Used")
	}

	time.Sleep(1 * time.Second)

	err = ds.ReleaseTenantIP(tenant.ID, ip.String())
	if err != nil {
		t.Fatal(err)
	}

	// get updated tenant info - should hit cache
	newTenant, err = ds.getTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	// confirm that tenant map shows it not used.
	if newTenant.network[int(subnetInt)][int(ipBytes[3])] != false {
		t.Fatal("IP Address not released from cache")
	}

	time.Sleep(1 * time.Second)

	// clear tenant from cache
	ds.tenantsLock.Lock()
	delete(ds.tenants, tenant.ID)
	ds.tenantsLock.Unlock()

	// get updated tenant info - should hit database
	newTenant, err = ds.getTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	// confirm that tenant map shows it not used.
	if newTenant.network[int(subnetInt)][int(ipBytes[3])] != false {
		t.Fatal("IP Address not released from database")
	}
}

func TestAddTenantChan(t *testing.T) {
	c := make(chan bool)

	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	ds.AddTenantChan(c, tenant.ID)

	// check cncisAddedChans
	ds.cnciAddedLock.Lock()
	c1 := ds.cnciAddedChans[tenant.ID]
	delete(ds.cnciAddedChans, tenant.ID)
	ds.cnciAddedLock.Unlock()

	if c1 != c {
		t.Fatal("Did not update Added Chans properly")
	}
}

func TestGetWorkload(t *testing.T) {
	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	wl, err := ds.GetWorkload(wls[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if wl != wls[0] {
		t.Fatal("Did not get correct workload")
	}

	// clear cache to exercise sql
	// clear out of cache to exercise sql
	ds.workloadsLock.Lock()
	delete(ds.workloads, wl.ID)
	ds.workloadsLock.Unlock()

	wl2, err := ds.GetWorkload(wls[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if wl2.ID != wl.ID {
		t.Fatal("Did not get correct workload from db")
	}

	// put it back in the cache
	work, err := ds.getWorkload(wl.ID)
	if err != nil {
		t.Fatal(err)
	}

	ds.workloadsLock.Lock()
	ds.workloads[wl.ID] = work
	ds.workloadsLock.Unlock()
}

func TestRestartFailure(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)
	reason := payloads.RestartNoInstance

	err = ds.RestartFailure(instance.ID, reason)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStopFailure(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)
	reason := payloads.StopNoInstance

	err = ds.StopFailure(instance.ID, reason)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStartFailureFullCloud(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	tenantBefore, err := ds.GetTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	resourcesBefore := make(map[string]int)
	for i := range tenantBefore.Resources {
		r := tenantBefore.Resources[i]
		resourcesBefore[r.Rname] = r.Usage
	}

	reason := payloads.FullCloud

	err = ds.StartFailure(instance.ID, reason)
	if err != nil {
		t.Fatal(err)
	}

	tenantAfter, err := ds.GetTenant(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	defaults := wls[0].Defaults

	usage := make(map[string]int)
	for i := range defaults {
		usage[string(defaults[i].Type)] = defaults[i].Value
	}

	resourcesAfter := make(map[string]int)
	for i := range tenantAfter.Resources {
		r := tenantAfter.Resources[i]
		resourcesAfter[r.Rname] = r.Usage
	}

	// make sure usage was reduced by workload defaults values
	for name, val := range resourcesAfter {
		before := resourcesBefore[name]
		delta := usage[name]

		if name == "instances" {
			if val != before-1 {
				t.Fatal("instances not decremented")
			}
		} else if val != before-delta {
			t.Fatal("usage not reduced")
		}
	}
}

func TestAttachVolumeFailure(t *testing.T) {
	newTenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	// add test instances
	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	instance, err := addTestInstance(newTenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	// add test block data
	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    newTenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	// update block data to indicate it is attaching
	data.State = types.Attaching

	err = ds.UpdateBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	// pretend we got a failure to attach.
	ds.AttachVolumeFailure(instance.ID, data.ID, payloads.AttachVolumeAlreadyAttached)

	// make sure state has been switched to Available again.
	bd, err := ds.GetBlockDevice(data.ID)
	if err != nil {
		t.Fatal(err)
	}

	if bd.State != types.Available {
		t.Fatalf("expected state: %s, got %s\n", types.Available, bd.State)
	}
}

func TestDetachVolumeFailure(t *testing.T) {
	newTenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	// add test instances
	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	instance, err := addTestInstance(newTenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	// add test block data
	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    newTenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	// update block data to indicate it is detaching
	data.State = types.Detaching

	err = ds.UpdateBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	// pretend we got a failure to attach.
	ds.DetachVolumeFailure(instance.ID, data.ID, payloads.DetachVolumeNotAttached)

	// make sure state has been switched to InUse again.
	bd, err := ds.GetBlockDevice(data.ID)
	if err != nil {
		t.Fatal(err)
	}

	if bd.State != types.InUse {
		t.Fatalf("expected state: %s, got %s\n", types.InUse, bd.State)
	}
}

func testAllocateTenantIPs(t *testing.T, nIPs int) {
	nIPsPerSubnet := 253

	newTenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	// make this tenant have some network hosts assigned to them.
	for n := 0; n < nIPs; n++ {
		_, err = ds.AllocateTenantIP(newTenant.ID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// get private tenant type
	tenant, err := ds.getTenant(newTenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(tenant.subnets) != (nIPs/nIPsPerSubnet)+1 {
		t.Fatal("Too many subnets created")
	}

	for i, subnet := range tenant.subnets {
		if ((i + 1) * nIPsPerSubnet) < nIPs {
			if len(tenant.network[subnet]) != nIPsPerSubnet {
				t.Fatal("Missing IPs")
			}
		} else {
			if len(tenant.network[subnet]) != nIPs%nIPsPerSubnet {
				t.Fatal("Missing IPs")
			}
		}
	}
}

func TestAllocate100IPs(t *testing.T) {
	testAllocateTenantIPs(t, 100)
}

func TestAllocate1024IPs(t *testing.T) {
	testAllocateTenantIPs(t, 1024)
}

func TestAddBlockDevice(t *testing.T) {
	newTenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    newTenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	// confirm that we can retrieve the block data.
	_, err = ds.GetBlockDevice("validID")
	if err != nil {
		t.Fatal(err)
	}

	// confirm that this is associate with our tenant.
	devices, err := ds.GetBlockDevices(newTenant.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, d := range devices {
		if d.ID != "validID" {
			t.Fatal(err)
		}
	}
}

func TestDeleteBlockDevice(t *testing.T) {
	newTenant, err := addTestTenant()
	if err != nil {
		t.Error(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    newTenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	// confirm that we can retrieve the block data.
	_, err = ds.GetBlockDevice("validID")
	if err != nil {
		t.Fatal(err)
	}

	// remove the block device
	err = ds.DeleteBlockDevice(data.ID)
	if err != nil {
		t.Fatal(err)
	}

	// confirm that it is no longer there.
	_, err = ds.GetBlockDevice(data.ID)
	if err == nil {
		t.Fatal("Did not delete block device")
	}

	// attempt to delete a non-existing block device
	err = ds.DeleteBlockDevice("unknownID")
	if err != ErrNoBlockData {
		t.Fatalf("expecting %s error, received %s\n", ErrNoBlockData, err)
	}
}

func TestUpdateBlockDevice(t *testing.T) {
	newTenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: uuid.Generate().String(),
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    newTenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	// confirm that we can retrieve the block data.
	_, err = ds.GetBlockDevice(blockDevice.ID)
	if err != nil {
		t.Fatal(err)
	}

	// update the state of the block device.
	data.State = types.Attaching

	err = ds.UpdateBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	// confirm that we can retrieve the block data.
	d, err := ds.GetBlockDevice(blockDevice.ID)
	if err != nil {
		t.Fatal(err)
	}

	if d.State != types.Attaching {
		t.Fatalf("expected State == %s, got %s\n", types.Attaching, d.State)
	}
}

func TestGetBlockDevicesErr(t *testing.T) {
	// confirm that sending a bad tenant id results in error
	_, err := ds.GetBlockDevices("badID")
	if err != ErrNoTenant {
		t.Fatal(err)
	}
}

func TestGetBlockDeviceErr(t *testing.T) {
	// confirm that we get the correct error for missing id
	_, err := ds.GetBlockDevice("badID")
	if err != ErrNoBlockData {
		t.Fatal(err)
	}
}

func TestUpdateBlockDeviceErr(t *testing.T) {
	newTenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: uuid.Generate().String(),
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    newTenant.ID,
		CreateTime:  time.Now(),
	}

	// confirm that we get the correct error for missing id
	err = ds.UpdateBlockDevice(data)
	if err != ErrNoBlockData {
		t.Fatal(err)
	}
}

func TestCreateStorageAttachment(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    tenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.createStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	// get the attachments associated with this instance
	a1, err := ds.GetStorageAttachments(instance.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(a1) != 1 {
		t.Fatal(err)
	}

	if a1[0].InstanceID != instance.ID {
		t.Fatal(err)
	}
}

func TestUpdateStorageAttachmentExisting(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    tenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.createStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	// get the attachments associated with this instance
	a1, err := ds.GetStorageAttachments(instance.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(a1) != 1 {
		t.Fatal(err)
	}

	if a1[0].InstanceID != instance.ID {
		t.Fatal(err)
	}

	attachments := []string{data.ID}

	// this doesn't return an error, but we can still exercise
	// the code to see if anything panics.
	ds.updateStorageAttachments(instance.ID, attachments)
}

func TestUpdateStorageAttachmentNotExisting(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    tenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	attachments := []string{data.ID}

	// this doesn't return an error, but we can still exercise
	// the code to see if anything panics.
	ds.updateStorageAttachments(instance.ID, attachments)
}

func TestUpdateStorageAttachmentDeleted(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    tenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.createStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	attachments := []string{}

	// this doesn't return an error, but we can still exercise
	// the code to see if anything panics.
	// send in an empty list.
	ds.updateStorageAttachments(instance.ID, attachments)
}

func TestGetStorageAttachment(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    tenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.createStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	a, err := ds.getStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	if a.InstanceID != instance.ID || a.BlockID != data.ID {
		t.Fatal(err)
	}
}

func TestGetStorageAttachmentError(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    tenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.getStorageAttachment(instance.ID, data.ID)
	if err != ErrNoStorageAttachment {
		t.Fatal(err)
	}
}

func TestDeleteStorageAttachment(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    tenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.createStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	a, err := ds.getStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	if a.InstanceID != instance.ID || a.BlockID != data.ID {
		t.Fatal(err)
	}

	err = ds.deleteStorageAttachment(a.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.getStorageAttachment(instance.ID, data.ID)
	if err != ErrNoStorageAttachment {
		t.Fatal(err)
	}
}

func TestDeleteStorageAttachmentError(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: "validID",
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    tenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.createStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	a, err := ds.getStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	if a.InstanceID != instance.ID || a.BlockID != data.ID {
		t.Fatal(err)
	}

	err = ds.deleteStorageAttachment(a.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.getStorageAttachment(instance.ID, data.ID)
	if err != ErrNoStorageAttachment {
		t.Fatal(err)
	}

	err = ds.deleteStorageAttachment(a.ID)
	if err != ErrNoStorageAttachment {
		t.Fatal(err)
	}
}

func TestGetVolumeAttachments(t *testing.T) {
	tenant, err := addTestTenant()
	if err != nil {
		t.Fatal(err)
	}

	blockDevice := storage.BlockDevice{
		ID: uuid.Generate().String(),
	}

	data := types.BlockData{
		BlockDevice: blockDevice,
		Size:        0,
		State:       types.Available,
		TenantID:    tenant.ID,
		CreateTime:  time.Now(),
	}

	err = ds.AddBlockDevice(data)
	if err != nil {
		t.Fatal(err)
	}

	wls, err := ds.GetWorkloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(wls) == 0 {
		t.Fatal("No Workloads Found")
	}

	instance, err := addTestInstance(tenant, wls[0])
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.createStorageAttachment(instance.ID, data.ID)
	if err != nil {
		t.Fatal(err)
	}

	attachments, err := ds.GetVolumeAttachments(data.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, found %d", len(attachments))
	}

	for _, a := range attachments {
		if a.InstanceID != instance.ID || a.BlockID != data.ID {
			t.Fatal("Returned incorrect attachment")
		}
	}
}

var ds *Datastore

var tablesInitPath = flag.String("tables_init_path", "../../tables", "path to csv files")
var workloadsPath = flag.String("workloads_path", "../../workloads", "path to yaml files")

func TestMain(m *testing.M) {
	flag.Parse()

	ds = new(Datastore)

	dsConfig := Config{
		PersistentURI:     "file:memdb1?mode=memory&cache=shared",
		TransientURI:      "file:memdb2?mode=memory&cache=shared",
		InitTablesPath:    *tablesInitPath,
		InitWorkloadsPath: *workloadsPath,
	}

	err := ds.Init(dsConfig)
	if err != nil {
		os.Exit(1)
	}

	code := m.Run()

	ds.Exit()

	os.Exit(code)
}
