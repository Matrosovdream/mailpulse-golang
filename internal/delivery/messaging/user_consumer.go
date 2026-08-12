package messaging

import (
	"encoding/json"
	"mailpulse/internal/model"

	"github.com/IBM/sarama"
	"github.com/sirupsen/logrus"
)

type UserConsumer struct {
	Log *logrus.Logger
}

func NewUserConsumer(log *logrus.Logger) *UserConsumer {
	return &UserConsumer{
		Log: log,
	}
}

func (c UserConsumer) Consume(message *sarama.ConsumerMessage) error {
	userEvent := new(model.UserEvent)
	if err := json.Unmarshal(message.Value, userEvent); err != nil {
		c.Log.WithError(err).Error("error unmarshalling User event")
		return err
	}

	// TODO process event
	c.Log.Infof("Received topic users with event: %v from partition %d", userEvent, message.Partition)
	return nil
}
