// Copyright (C) 2017 Jan Delgado

// +build integration

package main

// cmd_{exchangeCreate, sub, queueCreate, queueBind, queueDelete}
// integration test

import (
	"context"
	"crypto/tls"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	rabtap "github.com/jandelgado/rabtap/pkg"
	"github.com/jandelgado/rabtap/pkg/testcommon"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmdSubFailsEarlyWhenBrokerIsNotAvailable(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool)
	go func() {
		// we expect cmdSubscribe to return
		cmdSubscribe(ctx, CmdSubscribeArg{
			amqpURI:            "invalid uri",
			queue:              "queue",
			tlsConfig:          &tls.Config{},
			messageReceiveFunc: func(rabtap.TapMessage) error { return nil },
		})
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(time.Second * 2):
		assert.Fail(t, "cmdSubscribe did not fail on initial connection error")
	}
	cancel()
}

func TestCmdSub(t *testing.T) {
	const testMessage = "SubHello"
	const testQueue = "sub-queue-test"
	testKey := testQueue

	testExchange := ""
	//	testExchange := "sub-exchange-test"
	tlsConfig := &tls.Config{}
	amqpURI := testcommon.IntegrationURIFromEnv()

	done := make(chan bool)
	receiveFunc := func(message rabtap.TapMessage) error {
		log.Debug("test: received message: #+v", message)
		if string(message.AmqpMessage.Body) == testMessage {
			done <- true
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	// create and bind queue
	cmdQueueCreate(CmdQueueCreateArg{amqpURI: amqpURI,
		queue: testQueue, tlsConfig: tlsConfig})
	defer cmdQueueRemove(amqpURI, testQueue, tlsConfig)

	// subscribe to testQueue
	go cmdSubscribe(ctx, CmdSubscribeArg{
		amqpURI:            amqpURI,
		queue:              testQueue,
		tlsConfig:          tlsConfig,
		messageReceiveFunc: receiveFunc})

	time.Sleep(time.Second * 1)

	messageCount := 0

	// TODO test without cmdPublish
	cmdPublish(
		ctx,
		CmdPublishArg{
			amqpURI:    amqpURI,
			exchange:   &testExchange,
			routingKey: &testKey,
			tlsConfig:  tlsConfig,
			readerFunc: func() (RabtapPersistentMessage, bool, error) {
				// provide exactly one message
				if messageCount > 0 {
					return RabtapPersistentMessage{}, false, io.EOF
				}
				messageCount++
				return RabtapPersistentMessage{
					Body:         []byte(testMessage),
					ContentType:  "text/plain",
					DeliveryMode: amqp.Transient,
				}, true, nil
			}})

	// test if we received the message
	select {
	case <-done:
	case <-time.After(time.Second * 2):
		assert.Fail(t, "did not receive message within expected time")
	}
	cancel() // stop cmdSubscribe()
}

func TestCmdSubIntegration(t *testing.T) {
	const testMessage = "SubHello"
	const testQueue = "sub-queue-test"
	testKey := testQueue
	testExchange := "" // default exchange

	tlsConfig := &tls.Config{}
	amqpURI := testcommon.IntegrationURIFromEnv()

	cmdQueueCreate(CmdQueueCreateArg{amqpURI: amqpURI,
		queue: testQueue, tlsConfig: tlsConfig})
	defer cmdQueueRemove(amqpURI, testQueue, tlsConfig)

	_, ch := testcommon.IntegrationTestConnection(t, "", "", 0, false)
	err := ch.Publish(
		testExchange,
		testKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			Body:         []byte("Hello"),
			ContentType:  "text/plain",
			DeliveryMode: amqp.Transient,
			Headers:      amqp.Table{},
		})
	require.Nil(t, err)

	go func() {
		time.Sleep(time.Second * 2)
		syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	}()

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"rabtap", "sub",
		"--uri", testcommon.IntegrationURIFromEnv(),
		testQueue,
		"--format=raw",
		"--no-color"}
	output := testcommon.CaptureOutput(main)

	assert.Regexp(t, "(?s).*message received.*\nroutingkey.....: sub-queue-test\n.*Hello", output)
}
