package application

import (
	"fmt"
	"time"

	"github.com/edgexfoundry/edgex-go/internal/pkg/common"
	"github.com/edgexfoundry/edgex-go/internal/pkg/v2/utils"
	notificationContainer "github.com/edgexfoundry/edgex-go/internal/support/notifications/container"
	v2NotificationsContainer "github.com/edgexfoundry/edgex-go/internal/support/notifications/v2/bootstrap/container"

	"github.com/edgexfoundry/go-mod-bootstrap/v2/bootstrap/container"
	"github.com/edgexfoundry/go-mod-bootstrap/v2/di"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/clients"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/clients/logger"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/errors"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/v2"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/v2/models"
)

// sendNotificationViaChannel sends notification via address and return the transmission record. The record status should be SENT or FAILED.
func sendNotificationViaChannel(lc logger.LoggingClient, n models.Notification, channel models.Address) (transRecord models.TransmissionRecord, err errors.EdgeX) {
	transRecord.Status = models.Sent
	switch channel.GetBaseAddress().Type {
	case v2.REST:
		restAddress, ok := channel.(models.RESTAddress)
		if !ok {
			return transRecord, errors.NewCommonEdgeX(errors.KindContractInvalid, "fail to cast Address to RESTAddress", nil)
		}
		transRecord.Response, err = utils.SendRequestWithRESTAddress(lc, n.Content, n.ContentType, restAddress)
		if err != nil {
			transRecord.Status = models.Failed
			transRecord.Response = err.Error()
		}
		transRecord.Sent = common.MakeTimestamp()
	case v2.EMAIL:
		emailAddress, ok := channel.(models.EmailAddress)
		if !ok {
			return transRecord, errors.NewCommonEdgeX(errors.KindContractInvalid, "fail to cast Address to EmailAddress", nil)
		}
		transRecord.Response, err = utils.SendEmailWithAddress(lc, n.Content, n.ContentType, emailAddress)
		if err != nil {
			transRecord.Status = models.Failed
			transRecord.Response = err.Error()
		}
		transRecord.Sent = common.MakeTimestamp()
	default:
		transRecord.Response = fmt.Sprintf("unsupported address type: %s", channel.GetBaseAddress().Type)
	}
	return transRecord, nil
}

// normalSend handles the notification transmission
func normalSend(dic *di.Container, n models.Notification, trans models.Transmission) (models.Transmission, errors.EdgeX) {
	dbClient := v2NotificationsContainer.DBClientFrom(dic.Get)
	lc := container.LoggingClientFrom(dic.Get)

	record, err := sendNotificationViaChannel(lc, n, trans.Channel)
	if err != nil {
		return trans, errors.NewCommonEdgeXWrapper(err)
	}
	trans.Records = append(trans.Records, record)
	trans.Status = record.Status
	trans, err = dbClient.AddTransmission(trans)
	if err != nil {
		return trans, errors.NewCommonEdgeXWrapper(err)
	}
	lc.Debugf("success to send the notification to %s with address %v.", trans.SubscriptionName, trans.Channel.GetBaseAddress())
	return trans, nil
}

// criticalSend handles the Critical notification Transmission
func criticalSend(dic *di.Container, n models.Notification, trans models.Transmission) (models.Transmission, errors.EdgeX) {
	dbClient := v2NotificationsContainer.DBClientFrom(dic.Get)
	lc := container.LoggingClientFrom(dic.Get)
	config := notificationContainer.ConfigurationFrom(dic.Get)

	for i := 1; i <= config.ResendLimit; i++ {
		time.Sleep(time.Duration(config.ResendDelayTime) * time.Second)

		record, err := sendNotificationViaChannel(lc, n, trans.Channel)
		if err != nil {
			return trans, errors.NewCommonEdgeXWrapper(err)
		}
		trans.ResendCount = trans.ResendCount + 1
		trans.Status = record.Status
		trans.Records = append(trans.Records, record)
		err = dbClient.UpdateTransmission(trans)
		if err != nil {
			return trans, errors.NewCommonEdgeXWrapper(err)
		}
		if trans.Status == models.Failed {
			lc.Warn("fail to send the critical notification. Retry to send again...")
			continue
		}
		lc.Debugf("success to send the critical notification to %s with address %v.", trans.SubscriptionName, trans.Channel.GetBaseAddress())
		return trans, nil
	}

	lc.Warn("Resend count exceeds the configurable limit, escalate the transmission.")
	trans.Status = models.Escalated
	err := dbClient.UpdateTransmission(trans)
	if err != nil {
		return trans, errors.NewCommonEdgeXWrapper(err)
	}
	return trans, nil
}

// escalatedSend
func escalatedSend(dic *di.Container, n models.Notification, trans models.Transmission) errors.EdgeX {
	dbClient := v2NotificationsContainer.DBClientFrom(dic.Get)

	sub, err := dbClient.SubscriptionByName(models.EscalationSubscriptionName)
	if err != nil {
		return errors.NewCommonEdgeX(errors.Kind(err), fmt.Sprintf("subscription %s does not exists", models.EscalationSubscriptionName), err)
	}

	escalated := escalatedNotification(n, trans)
	escalated, err = dbClient.AddNotification(escalated)
	if err != nil {
		return errors.NewCommonEdgeX(errors.Kind(err), "fail to create the escalated notification", err)
	}

	for _, address := range sub.Channels {
		go asyncHandleNotification(dic, escalated, sub, address)
	}
	return nil
}

func escalatedNotification(n models.Notification, trans models.Transmission) models.Notification {
	n.Id = ""
	n.Created = 0
	n.Content = fmt.Sprintf("[%s %s] %s", models.EscalatedContentNotice, trans.Id, n.Content)
	n.ContentType = clients.ContentTypeText
	n.Status = models.Escalated
	return n
}
