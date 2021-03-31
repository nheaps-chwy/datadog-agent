// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package sender

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/DataDog/datadog-agent/pkg/logs/message"
)

// newBatchStrategyWithLimits returns a new batchStrategy.
func newBatchStrategyWithLimits(serializer Serializer, batchSize int, contentSize int, batchWait time.Duration, maxConcurrent int) Strategy {
	var climit chan struct{}
	if maxConcurrent > 1 {
		climit = make(chan struct{}, maxConcurrent)
	}
	return &batchStrategy{
		buffer:     NewMessageBuffer(batchSize, contentSize),
		serializer: serializer,
		batchWait:  batchWait,
		climit:     climit,
	}
}

func TestBatchStrategySendsPayloadWhenBufferIsFull(t *testing.T) {
	input := make(chan *message.Message)
	output := make(chan *message.Message)

	var mu sync.Mutex

	var content []byte
	success := func(payload []byte) error {
		assert.Equal(t, content, payload)
		return nil
	}

	go newBatchStrategyWithLimits(LineSerializer, 2, 2, 100*time.Millisecond, 0).Send(input, output, success, &mu)

	content = []byte("a\nb")

	message1 := message.NewMessage([]byte("a"), nil, "", 0)
	input <- message1

	message2 := message.NewMessage([]byte("b"), nil, "", 0)
	input <- message2

	// expect payload to be sent because buffer is full
	assert.Equal(t, message1, <-output)
	assert.Equal(t, message2, <-output)
}

// func TestBatchStrategySendsPayloadWhenBufferIsOutdated(t *testing.T) {
// 	input := make(chan *message.Message)
// 	output := make(chan *message.Message)

// 	var content []byte
// 	success := func(payload []byte) error {
// 		assert.Equal(t, content, payload)
// 		return nil
// 	}

// 	go newBatchStrategyWithLimits(LineSerializer, 2, 2, 100*time.Millisecond).Send(input, output, success)

// 	content = []byte("a")

// 	message1 := message.NewMessage([]byte(content), nil, "")
// 	input <- message1

// 	// expect payload to be sent after timer
// 	start := time.Now()
// 	assert.Equal(t, message1, <-output)
// 	end := start.Add(100 * time.Millisecond)
// 	now := time.Now()
// 	assert.True(t, now.After(end) || now.Equal(end))

// 	content = []byte("b\nc")

// 	message2 := message.NewMessage([]byte("b"), nil, "")
// 	input <- message2

// 	message3 := message.NewMessage([]byte("c"), nil, "")
// 	input <- message3

// 	// expect payload to be sent because buffer is full
// 	assert.Equal(t, message2, <-output)
// 	assert.Equal(t, message3, <-output)
// }

func TestBatchStrategySendsPayloadWhenClosingInput(t *testing.T) {
	input := make(chan *message.Message)
	output := make(chan *message.Message)

	var mu sync.Mutex

	var content []byte
	success := func(payload []byte) error {
		assert.Equal(t, content, payload)
		return nil
	}

	go newBatchStrategyWithLimits(LineSerializer, 2, 2, 100*time.Millisecond, 0).Send(input, output, success, &mu)

	content = []byte("a")

	message := message.NewMessage(content, nil, "", 0)
	input <- message

	start := time.Now()
	close(input)

	// expect payload to be sent before timer
	assert.Equal(t, message, <-output)
	end := start.Add(100 * time.Millisecond)
	now := time.Now()
	assert.True(t, now.Before(end) || now.Equal(end))
}

func TestBatchStrategyShouldNotBlockWhenForceStopping(t *testing.T) {
	input := make(chan *message.Message)
	output := make(chan *message.Message)

	var mu sync.Mutex

	var content []byte
	success := func(payload []byte) error {
		return context.Canceled
	}

	message := message.NewMessage(content, nil, "", 0)
	go func() {
		input <- message
		close(input)
	}()

	newBatchStrategyWithLimits(LineSerializer, 2, 2, 100*time.Millisecond, 0).Send(input, output, success, &mu)
}

func TestBatchStrategyShouldNotBlockWhenStoppingGracefully(t *testing.T) {
	input := make(chan *message.Message)
	output := make(chan *message.Message)

	var mu sync.Mutex

	var content []byte
	success := func(payload []byte) error {
		return nil
	}

	message := message.NewMessage(content, nil, "", 0)
	go func() {
		input <- message
		close(input)
		assert.Equal(t, message, <-output)
	}()

	newBatchStrategyWithLimits(LineSerializer, 2, 2, 100*time.Millisecond, 0).Send(input, output, success, &mu)
}

func TestBatchStrategyConcurrentSends(t *testing.T) {
	input := make(chan *message.Message)
	output := make(chan *message.Message)
	waitChan := make(chan bool)
	var mu sync.Mutex

	// payload sends are blocked until we've confirmed that the we buffer the correct number of pending payloads
	stuckSend := func(payload []byte) error {
		<-waitChan
		return nil
	}

	go newBatchStrategyWithLimits(LineSerializer, 1, 100, 100*time.Millisecond, 2).Send(input, output, stuckSend, &mu)

	messages := []*message.Message{
		// the first two messages will be blocked in concurrent send goroutines
		message.NewMessage([]byte("a"), nil, "", 0),
		message.NewMessage([]byte("b"), nil, "", 0),
		// this message will be read out by the main batch sender loop and will be blocked waiting for one of the
		// first two concurrent sends to complete
		message.NewMessage([]byte("c"), nil, "", 0),
	}

	for _, m := range messages {
		input <- m
	}

	select {
	case input <- message.NewMessage([]byte("c"), nil, "", 0):
		assert.Fail(t, "should not have been able to write into the channel as the input channel is expected to be backed up due to reaching max concurrent sends")
	default:
	}

	// unblock the sends so the messages get processed and sent to output channel
	close(waitChan)

	var receivedMessages []*message.Message
	for _, m := range messages {
		receivedMessages = append(receivedMessages, m)
	}

	// order in which messages are received here is not deterministic so compare values
	assert.EqualValues(t, messages, receivedMessages)
}
