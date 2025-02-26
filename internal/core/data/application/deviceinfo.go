//
// Copyright (C) 2025 IOTech Ltd
//
// SPDX-License-Identifier: Apache-2.0

package application

import (
	"github.com/edgexfoundry/edgex-go/internal/core/data/container"
	bootstrapContainer "github.com/edgexfoundry/go-mod-bootstrap/v4/bootstrap/container"
	"github.com/edgexfoundry/go-mod-bootstrap/v4/di"
	"github.com/edgexfoundry/go-mod-core-contracts/v4/errors"
)

func (a *CoreDataApp) RemoveDeviceInfoByDeviceName(deviceName string, dic *di.Container) errors.EdgeX {
	dbClient := container.DBClientFrom(dic.Get)
	lc := bootstrapContainer.LoggingClientFrom(dic.Get)

	err := dbClient.RemoveDeviceInfoByDeviceName(deviceName)
	if err != nil {
		lc.Errorf("Delete deviceInfo by deviceName failed: %v", err)
	}
	return nil
}
