//
// Copyright (C) 2020-2024 IOTech Ltd
//
// SPDX-License-Identifier: Apache-2.0

package redis

import (
	"encoding/json"
	"fmt"
	"math"

	pkgCommon "github.com/edgexfoundry/edgex-go/internal/pkg/common"

	"github.com/edgexfoundry/go-mod-core-contracts/v4/common"
	"github.com/edgexfoundry/go-mod-core-contracts/v4/errors"
	"github.com/edgexfoundry/go-mod-core-contracts/v4/models"

	"github.com/gomodule/redigo/redis"
)

const (
	DeviceCollection            = "md|dv"
	DeviceCollectionName        = DeviceCollection + DBKeySeparator + common.Name
	DeviceCollectionLabel       = DeviceCollection + DBKeySeparator + common.Label
	DeviceCollectionParent      = DeviceCollection + DBKeySeparator + "parent"
	DeviceCollectionServiceName = DeviceCollection + DBKeySeparator + common.Service + DBKeySeparator + common.Name
	DeviceCollectionProfileName = DeviceCollection + DBKeySeparator + common.Profile + DBKeySeparator + common.Name
)

// deviceStoredKey return the device's stored key which combines the collection name and object id
func deviceStoredKey(id string) string {
	return CreateKey(DeviceCollection, id)
}

// deviceNameExists whether the device exists by name
func deviceNameExists(conn redis.Conn, name string) (bool, errors.EdgeX) {
	exists, err := objectNameExists(conn, DeviceCollectionName, name)
	if err != nil {
		return false, errors.NewCommonEdgeX(errors.KindDatabaseError, "device existence check by name failed", err)
	}
	return exists, nil
}

// deviceIdExists checks whether the device exists by id
func deviceIdExists(conn redis.Conn, id string) (bool, errors.EdgeX) {
	exists, err := objectIdExists(conn, deviceStoredKey(id))
	if err != nil {
		return false, errors.NewCommonEdgeX(errors.KindDatabaseError, "device existence check by id failed", err)
	}
	return exists, nil
}

// sendAddDeviceCmd send redis command for adding device
func sendAddDeviceCmd(conn redis.Conn, storedKey string, d models.Device) errors.EdgeX {
	dsJSONBytes, err := json.Marshal(d)
	if err != nil {
		return errors.NewCommonEdgeX(errors.KindContractInvalid, "unable to JSON marshal device for Redis persistence", err)
	}
	_ = conn.Send(SET, storedKey, dsJSONBytes)
	_ = conn.Send(ZADD, DeviceCollection, 0, storedKey)
	_ = conn.Send(HSET, DeviceCollectionName, d.Name, storedKey)
	_ = conn.Send(ZADD, CreateKey(DeviceCollectionServiceName, d.ServiceName), d.Modified, storedKey)
	_ = conn.Send(ZADD, CreateKey(DeviceCollectionProfileName, d.ProfileName), d.Modified, storedKey)
	for _, label := range d.Labels {
		_ = conn.Send(ZADD, CreateKey(DeviceCollectionLabel, label), d.Modified, storedKey)
	}
	if d.Parent != "" {
		_ = conn.Send(ZADD, CreateKey(DeviceCollectionParent, d.Parent), d.Modified, storedKey)
	}
	return nil
}

// addDevice adds a new device into DB
func addDevice(conn redis.Conn, d models.Device) (models.Device, errors.EdgeX) {
	var exists bool
	var edgeXerr errors.EdgeX
	if d.ProfileName != "" {
		exists, edgeXerr = deviceProfileNameExists(conn, d.ProfileName)
		if edgeXerr != nil {
			return d, errors.NewCommonEdgeXWrapper(edgeXerr)
		}
		if !exists {
			return d, errors.NewCommonEdgeX(errors.KindEntityDoesNotExist, fmt.Sprintf("device profile '%s' does not exists", d.ProfileName), nil)
		}
	}

	exists, edgeXerr = deviceIdExists(conn, d.Id)
	if edgeXerr != nil {
		return d, errors.NewCommonEdgeXWrapper(edgeXerr)
	} else if exists {
		return d, errors.NewCommonEdgeX(errors.KindDuplicateName, fmt.Sprintf("device id %s already exists", d.Id), edgeXerr)
	}

	exists, edgeXerr = deviceNameExists(conn, d.Name)
	if edgeXerr != nil {
		return d, errors.NewCommonEdgeXWrapper(edgeXerr)
	} else if exists {
		return d, errors.NewCommonEdgeX(errors.KindDuplicateName, fmt.Sprintf("device name %s already exists", d.Name), edgeXerr)
	}

	ts := pkgCommon.MakeTimestamp()
	if d.Created == 0 {
		d.Created = ts
	}
	d.Modified = ts

	storedKey := deviceStoredKey(d.Id)
	_ = conn.Send(MULTI)
	edgeXerr = sendAddDeviceCmd(conn, storedKey, d)
	if edgeXerr != nil {
		return d, errors.NewCommonEdgeXWrapper(edgeXerr)
	}
	_, err := conn.Do(EXEC)
	if err != nil {
		edgeXerr = errors.NewCommonEdgeX(errors.KindDatabaseError, "device creation failed", err)
	}

	return d, edgeXerr
}

// deviceById query device by id from DB
func deviceById(conn redis.Conn, id string) (device models.Device, edgeXerr errors.EdgeX) {
	edgeXerr = getObjectById(conn, deviceStoredKey(id), &device)
	if edgeXerr != nil {
		return device, errors.NewCommonEdgeXWrapper(edgeXerr)
	}
	return
}

// deviceByName query device by name from DB
func deviceByName(conn redis.Conn, name string) (device models.Device, edgeXerr errors.EdgeX) {
	edgeXerr = getObjectByHash(conn, DeviceCollectionName, name, &device)
	if edgeXerr != nil {
		return device, errors.NewCommonEdgeXWrapper(edgeXerr)
	}
	return
}

// deleteDeviceById deletes the device by id
func deleteDeviceById(conn redis.Conn, id string) errors.EdgeX {
	device, err := deviceById(conn, id)
	if err != nil {
		return errors.NewCommonEdgeXWrapper(err)
	}
	err = deleteDevice(conn, device)
	if err != nil {
		return errors.NewCommonEdgeXWrapper(err)
	}
	return nil
}

// deleteDeviceByName deletes the device by name
func deleteDeviceByName(conn redis.Conn, name string) errors.EdgeX {
	device, err := deviceByName(conn, name)
	if err != nil {
		return errors.NewCommonEdgeXWrapper(err)
	}
	err = deleteDevice(conn, device)
	if err != nil {
		return errors.NewCommonEdgeXWrapper(err)
	}
	return nil
}

// sendDeleteDeviceCmd send redis command for deleting device
func sendDeleteDeviceCmd(conn redis.Conn, storedKey string, device models.Device) {
	_ = conn.Send(DEL, storedKey)
	_ = conn.Send(ZREM, DeviceCollection, storedKey)
	_ = conn.Send(HDEL, DeviceCollectionName, device.Name)
	_ = conn.Send(ZREM, CreateKey(DeviceCollectionServiceName, device.ServiceName), storedKey)
	_ = conn.Send(ZREM, CreateKey(DeviceCollectionProfileName, device.ProfileName), storedKey)
	for _, label := range device.Labels {
		_ = conn.Send(ZREM, CreateKey(DeviceCollectionLabel, label), storedKey)
	}
	if device.Parent != "" {
		_ = conn.Send(ZREM, CreateKey(DeviceCollectionParent, device.Parent), storedKey)
	}
}

// deleteDevice deletes a device
func deleteDevice(conn redis.Conn, device models.Device) errors.EdgeX {
	numChildren, edgexErr := getMemberNumber(conn, ZCARD, CreateKey(DeviceCollectionParent, device.Name))
	if edgexErr != nil {
		return errors.NewCommonEdgeX(errors.KindDatabaseError, "Could not determine if device had any children", edgexErr)
	}
	if numChildren > 0 {
		return errors.NewCommonEdgeX(errors.KindStatusConflict, "Cannot delete device, it has child devices", nil)
	}
	storedKey := deviceStoredKey(device.Id)
	_ = conn.Send(MULTI)
	sendDeleteDeviceCmd(conn, storedKey, device)
	_, err := conn.Do(EXEC)
	if err != nil {
		return errors.NewCommonEdgeX(errors.KindDatabaseError, "device deletion failed", err)
	}
	return nil
}

// devicesByServiceName query devices by offset, limit and name
func devicesByServiceName(conn redis.Conn, offset int, limit int, name string) (devices []models.Device, edgeXerr errors.EdgeX) {
	objects, err := getObjectsByRevRange(conn, CreateKey(DeviceCollectionServiceName, name), offset, limit)
	if err != nil {
		return devices, errors.NewCommonEdgeXWrapper(err)
	}

	devices = make([]models.Device, len(objects))
	for i, in := range objects {
		s := models.Device{}
		err := json.Unmarshal(in, &s)
		if err != nil {
			return []models.Device{}, errors.NewCommonEdgeX(errors.KindDatabaseError, "device format parsing failed from the database", err)
		}
		devices[i] = s
	}
	return devices, nil
}

// devicesByLabels query devices with offset, limit and labels
func devicesByLabels(conn redis.Conn, offset int, limit int, labels []string) (devices []models.Device, edgeXerr errors.EdgeX) {
	objects, edgeXerr := getObjectsByLabelsAndSomeRange(conn, ZREVRANGE, DeviceCollection, labels, offset, limit)
	if edgeXerr != nil {
		return devices, errors.NewCommonEdgeXWrapper(edgeXerr)
	}

	devices = make([]models.Device, len(objects))
	for i, in := range objects {
		dp := models.Device{}
		err := json.Unmarshal(in, &dp)
		if err != nil {
			return []models.Device{}, errors.NewCommonEdgeX(errors.KindDatabaseError, "device format parsing failed from the database", err)
		}
		devices[i] = dp
	}
	return devices, nil
}

// devicesByProfileName query devices by offset, limit and profile name
func devicesByProfileName(conn redis.Conn, offset int, limit int, profileName string) (devices []models.Device, edgeXerr errors.EdgeX) {
	objects, err := getObjectsByRevRange(conn, CreateKey(DeviceCollectionProfileName, profileName), offset, limit)
	if err != nil {
		return devices, errors.NewCommonEdgeXWrapper(err)
	}

	devices = make([]models.Device, len(objects))
	for i, in := range objects {
		s := models.Device{}
		err := json.Unmarshal(in, &s)
		if err != nil {
			return []models.Device{}, errors.NewCommonEdgeX(errors.KindDatabaseError, "device format parsing failed from the database", err)
		}
		devices[i] = s
	}
	return devices, nil
}

func updateDevice(conn redis.Conn, d models.Device) errors.EdgeX {
	if d.ProfileName != "" {
		exists, edgeXerr := deviceProfileNameExists(conn, d.ProfileName)
		if edgeXerr != nil {
			return errors.NewCommonEdgeX(errors.Kind(edgeXerr), fmt.Sprintf("device profile '%s' existence check failed", d.ProfileName), edgeXerr)
		} else if !exists {
			return errors.NewCommonEdgeX(errors.KindEntityDoesNotExist, fmt.Sprintf("device profile '%s' does not exists", d.ProfileName), nil)
		}
	}

	oldDevice, edgexErr := deviceByName(conn, d.Name)
	if edgexErr != nil {
		return errors.NewCommonEdgeXWrapper(edgexErr)
	}

	ts := pkgCommon.MakeTimestamp()
	d.Modified = ts

	storedKey := deviceStoredKey(d.Id)
	_ = conn.Send(MULTI)
	sendDeleteDeviceCmd(conn, storedKey, oldDevice)
	edgexErr = sendAddDeviceCmd(conn, storedKey, d)
	if edgexErr != nil {
		return errors.NewCommonEdgeXWrapper(edgexErr)
	}
	_, err := conn.Do(EXEC)
	if err != nil {
		return errors.NewCommonEdgeX(errors.KindDatabaseError, "device update failed", err)
	}

	return nil
}

// Return all devices with the given parent and labels (one level of the tree).
func deviceTreeLevel(conn redis.Conn, parent string, labels []string) ([]models.Device, errors.EdgeX) {
	queryList := []string{CreateKey(DeviceCollectionParent, parent)}
	for l := range labels {
		queryList = append(queryList, CreateKey(DeviceCollectionLabel, labels[l]))
	}
	objects, err := intersectionObjectsByKeys(conn, 0, -1, queryList...)
	if err != nil {
		return []models.Device{}, errors.NewCommonEdgeXWrapper(err)
	}
	devices := make([]models.Device, len(objects))
	for i, in := range objects {
		s := models.Device{}
		err := json.Unmarshal(in, &s)
		if err != nil {
			return []models.Device{}, errors.NewCommonEdgeX(errors.KindDatabaseError, "device format parsing failed from the database", err)
		}
		if s.Name == s.Parent {
			message := "Device " + s.Name + " is its own parent, stopping this query"
			return []models.Device{}, errors.NewCommonEdgeX(errors.KindDatabaseError, message, nil)
		}
		devices[i] = s
	}
	return devices, nil
}

// Get the entire subtree starting with the given parent, descending at most the given number of levels.
func deviceSubTree(conn redis.Conn, parent string, levels int, labels []string) ([]models.Device, errors.EdgeX) {
	if levels == 0 {
		return []models.Device{}, nil
	}
	devices, err := deviceTreeLevel(conn, parent, labels)
	if err != nil {
		return []models.Device{}, errors.NewCommonEdgeXWrapper(err)
	}
	if levels == 1 {
		return devices, nil
	}
	for i := range devices {
		subDevices, err := deviceSubTree(conn, devices[i].Name, levels-1, labels)
		if err != nil {
			return []models.Device{}, errors.NewCommonEdgeXWrapper(err)
		}
		devices = append(devices, subDevices...)
	}
	return devices, nil
}

// Get the full result-set since that's the only way to correctly get totalCount.
// Then return the subset of the result-set that corresponds to the requested offset and limit.
func deviceTree(conn redis.Conn, parent string, levels int, offset int, limit int, labels []string) (uint32, []models.Device, errors.EdgeX) {
	var maxLevels int
	var emptyList = []models.Device{}
	if levels <= 0 {
		maxLevels = math.MaxInt
	} else {
		maxLevels = levels
	}
	all_devices, err := deviceSubTree(conn, parent, maxLevels, labels)
	if err != nil {
		return 0, emptyList, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(all_devices) {
		return uint32(len(all_devices)), emptyList, nil
	}
	numToReturn := len(all_devices) - offset
	if limit > 0 && limit < numToReturn {
		numToReturn = limit
	}
	return uint32(len(all_devices)), all_devices[offset : offset+numToReturn], nil
}
