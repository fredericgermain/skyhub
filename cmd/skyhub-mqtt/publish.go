package main

import (
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Publisher is the minimal MQTT surface the poller needs; tests use a
// recording implementation.
type Publisher interface {
	Publish(topic, payload string, retain bool) error
	Close()
}

type pahoPublisher struct {
	c       mqtt.Client
	timeout time.Duration
}

// connectPaho connects to the broker with a retained LWT on statusTopic.
func connectPaho(cfg Config, statusTopic string, onConnect func()) (*pahoPublisher, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetUsername(cfg.MQTTUser).
		SetPassword(cfg.MQTTPassword).
		SetWill(statusTopic, "offline", 1, true).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetCleanSession(false).
		SetOrderMatters(false).
		SetConnectTimeout(10 * time.Second).
		SetKeepAlive(30 * time.Second)
	if onConnect != nil {
		opts.SetOnConnectHandler(func(mqtt.Client) { onConnect() })
	}
	c := mqtt.NewClient(opts)
	tok := c.Connect()
	if !tok.WaitTimeout(30 * time.Second) {
		return nil, fmt.Errorf("mqtt connect to %s: timeout", cfg.Broker)
	}
	if err := tok.Error(); err != nil {
		return nil, fmt.Errorf("mqtt connect to %s: %w", cfg.Broker, err)
	}
	return &pahoPublisher{c: c, timeout: 5 * time.Second}, nil
}

func (p *pahoPublisher) Publish(topic, payload string, retain bool) error {
	tok := p.c.Publish(topic, 1, retain, payload)
	if !tok.WaitTimeout(p.timeout) {
		return fmt.Errorf("publish %s: timeout", topic)
	}
	return tok.Error()
}

func (p *pahoPublisher) Close() {
	p.c.Disconnect(500)
}
