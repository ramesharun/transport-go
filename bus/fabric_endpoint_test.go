// Copyright 2019 VMware, Inc. All rights reserved. -- VMware Confidential

package bus

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "go-bifrost/stompserver"
    "sync"
    "encoding/json"
    "errors"
    "go-bifrost/model"
    "github.com/google/uuid"
)

type MockStompServerMessage struct {
    Destination string `json:"destination"`
    Payload []byte `json:"payload"`
    conId string
}

type MockStompServer struct {
    started bool
    sentMessages []MockStompServerMessage
    subscribeHandlerFunction stompserver.SubscribeHandlerFunction
    unsubscribeHandlerFunction stompserver.UnsubscribeHandlerFunction
    applicationRequestHandlerFunction stompserver.ApplicationRequestHandlerFunction
    wg *sync.WaitGroup
}

func(s *MockStompServer) Start() {
    s.started = true
}

func(s *MockStompServer) Stop() {
    s.started = false
}

func(s *MockStompServer) SendMessage(destination string, messageBody []byte) {
    s.sentMessages = append(s.sentMessages,
        MockStompServerMessage{Destination: destination, Payload: messageBody})

    if s.wg != nil {
        s.wg.Done()
    }
}

func(s *MockStompServer) SendMessageToClient(conId string, destination string, messageBody []byte) {
    s.sentMessages = append(s.sentMessages,
        MockStompServerMessage{Destination: destination, Payload: messageBody, conId: conId})

    if s.wg != nil {
        s.wg.Done()
    }
}

func(s *MockStompServer) OnUnsubscribeEvent(callback stompserver.UnsubscribeHandlerFunction) {
    s.unsubscribeHandlerFunction = callback
}

func(s *MockStompServer) OnApplicationRequest(callback stompserver.ApplicationRequestHandlerFunction) {
    s.applicationRequestHandlerFunction = callback
}

func(s *MockStompServer) OnSubscribeEvent(callback stompserver.SubscribeHandlerFunction) {
    s.subscribeHandlerFunction = callback
}

func newTestFabricEndpoint(bus EventBus, config EndpointConfig) (*fabricEndpoint, *MockStompServer) {

    fe := newFabricEndpoint(bus, nil, config).(*fabricEndpoint)
    ms := &MockStompServer{}

    fe.server = ms
    fe.initHandlers()

    return fe, ms
}

func TestFabricEndpoint_newFabricEndpoint(t *testing.T) {
    fe, _ := newTestFabricEndpoint(nil, EndpointConfig{
        TopicPrefix: "/topic",
        AppRequestPrefix: "/pub",
        Heartbeat: 0,
    })

    assert.NotNil(t, fe)
    assert.Equal(t, fe.config.TopicPrefix, "/topic/")
    assert.Equal(t, fe.config.AppRequestPrefix, "/pub/")

    fe, _ = newTestFabricEndpoint(nil, EndpointConfig{
        TopicPrefix: "/topic/",
        AppRequestPrefix: "",
        Heartbeat: 0,
    })

    assert.Equal(t, fe.config.TopicPrefix, "/topic/")
    assert.Equal(t, fe.config.AppRequestPrefix, "")
}

func TestFabricEndpoint_StartAndStop(t *testing.T) {
    fe, mockServer := newTestFabricEndpoint(nil, EndpointConfig{})
    assert.Equal(t, mockServer.started, false)
    fe.Start()
    assert.Equal(t, mockServer.started, true)
    fe.Stop()
    assert.Equal(t, mockServer.started, false)
}

func TestFabricEndpoint_SubscribeEvent(t *testing.T) {

    bus := newTestEventBus()
    fe, mockServer := newTestFabricEndpoint(bus,
        EndpointConfig{TopicPrefix: "/topic", UserQueuePrefix:"/user/queue"})

    // subscribe to non-existing channel
    mockServer.subscribeHandlerFunction("con1", "sub1", "/topic/test-service", nil)
    assert.Equal(t, len(fe.chanMappings), 0)

    bus.GetChannelManager().CreateChannel("test-service")

    // subscribe to invalid topic
    mockServer.subscribeHandlerFunction("con1", "sub1", "/topic2/test-service", nil)
    assert.Equal(t, len(fe.chanMappings), 0)

    bus.SendResponseMessage("test-service", "test-message", nil)
    assert.Equal(t, len(mockServer.sentMessages), 0)

    // subscribe to valid channel
    mockServer.subscribeHandlerFunction("con1", "sub1", "/topic/test-service", nil)
    assert.Equal(t, len(fe.chanMappings), 1)
    assert.Equal(t, len(fe.chanMappings["test-service"].subs), 1)
    assert.Equal(t, fe.chanMappings["test-service"].subs["con1#sub1"], true)

    // subscribe again to the same channel
    mockServer.subscribeHandlerFunction("con1", "sub2", "/topic/test-service", nil)
    assert.Equal(t, len(fe.chanMappings), 1)
    assert.Equal(t, len(fe.chanMappings["test-service"].subs), 2)
    assert.Equal(t, fe.chanMappings["test-service"].subs["con1#sub2"], true)

    // subscribe to queue channel
    mockServer.subscribeHandlerFunction("con1", "sub3", "/user/queue/test-service", nil)
    assert.Equal(t, len(fe.chanMappings), 1)
    assert.Equal(t, len(fe.chanMappings["test-service"].subs), 3)
    assert.Equal(t, fe.chanMappings["test-service"].subs["con1#sub3"], true)

    mockServer.wg = &sync.WaitGroup{}
    mockServer.wg.Add(1)

    bus.SendResponseMessage("test-service", "test-message", nil)

    mockServer.wg.Wait()

    mockServer.wg.Add(1)
    bus.SendResponseMessage("test-service", []byte{1,2,3}, nil)
    mockServer.wg.Wait()

    mockServer.wg.Add(1)
    msg := MockStompServerMessage{Destination: "test", Payload: []byte("test-message")}
    bus.SendResponseMessage("test-service", msg, nil)
    mockServer.wg.Wait()

    mockServer.wg.Add(1)
    bus.SendErrorMessage("test-service", errors.New("test-error"), nil)
    mockServer.wg.Wait()

    assert.Equal(t, len(mockServer.sentMessages), 4)
    assert.Equal(t, mockServer.sentMessages[0].Destination, "/topic/test-service")
    assert.Equal(t, string(mockServer.sentMessages[0].Payload), "test-message")
    assert.Equal(t, mockServer.sentMessages[1].Payload, []byte{1,2,3})

    var sentMsg MockStompServerMessage
    json.Unmarshal(mockServer.sentMessages[2].Payload, &sentMsg)
    assert.Equal(t, msg, sentMsg )

    assert.Equal(t, string(mockServer.sentMessages[3].Payload), "test-error")

    mockServer.wg.Add(1)
    bus.SendResponseMessage("test-service", model.Response{
        BrokerDestination: &model.BrokerDestinationConfig{
            Destination: "/user/queue/test-service",
            ConnectionId: "con1",
        },
        Payload: "test-private-message",
    }, nil)

    mockServer.wg.Wait()

    assert.Equal(t, len(mockServer.sentMessages), 5)
    assert.Equal(t, mockServer.sentMessages[4].Destination, "/user/queue/test-service")
    var sentResponse model.Response
    json.Unmarshal(mockServer.sentMessages[4].Payload, &sentResponse)
    assert.Equal(t, sentResponse.Payload, "test-private-message")

    mockServer.wg.Add(1)
    bus.SendResponseMessage("test-service", &model.Response{
        BrokerDestination: &model.BrokerDestinationConfig{
            Destination: "/user/queue/test-service",
            ConnectionId: "con1",
        },
        Payload: "test-private-message-ptr",
    }, nil)

    mockServer.wg.Wait()

    assert.Equal(t, len(mockServer.sentMessages), 6)
    assert.Equal(t, mockServer.sentMessages[5].Destination, "/user/queue/test-service")
    json.Unmarshal(mockServer.sentMessages[5].Payload, &sentResponse)
    assert.Equal(t, sentResponse.Payload, "test-private-message-ptr")
}

func TestFabricEndpoint_UnsubscribeEvent(t *testing.T) {
    bus := newTestEventBus()
    fe, mockServer := newTestFabricEndpoint(bus, EndpointConfig{TopicPrefix: "/topic"})

    bus.GetChannelManager().CreateChannel("test-service")

    // subscribe to valid channel
    mockServer.subscribeHandlerFunction("con1", "sub1", "/topic/test-service", nil)
    mockServer.subscribeHandlerFunction("con1", "sub2", "/topic/test-service", nil)

    assert.Equal(t, len(fe.chanMappings), 1)
    assert.Equal(t, len(fe.chanMappings["test-service"].subs), 2)

    mockServer.wg = &sync.WaitGroup{}
    mockServer.wg.Add(1)
    bus.SendResponseMessage("test-service", "test-message", nil)
    mockServer.wg.Wait()
    assert.Equal(t, len(mockServer.sentMessages), 1)


    mockServer.unsubscribeHandlerFunction("con1", "sub2", "/invalid-topic/test-service")
    assert.Equal(t, len(fe.chanMappings), 1)
    assert.Equal(t, len(fe.chanMappings["test-service"].subs), 2)

    mockServer.unsubscribeHandlerFunction("invalid-con1", "sub2", "/topic/test-service")
    assert.Equal(t, len(fe.chanMappings), 1)
    assert.Equal(t, len(fe.chanMappings["test-service"].subs), 2)

    mockServer.unsubscribeHandlerFunction("con1", "sub2", "/topic/test-service")
    assert.Equal(t, len(fe.chanMappings), 1)
    assert.Equal(t, len(fe.chanMappings["test-service"].subs), 1)

    mockServer.wg = &sync.WaitGroup{}
    mockServer.wg.Add(1)
    bus.SendResponseMessage("test-service", "test-message", nil)
    mockServer.wg.Wait()
    assert.Equal(t, len(mockServer.sentMessages), 2)

    mockServer.unsubscribeHandlerFunction("con1", "sub1", "/topic/test-service")
    assert.Equal(t, len(fe.chanMappings), 0)
    bus.SendResponseMessage("test-service", "test-message", nil)
}

func TestFabricEndpoint_BridgeMessage(t *testing.T) {
    bus := newTestEventBus()
    _, mockServer := newTestFabricEndpoint(bus, EndpointConfig{TopicPrefix: "/topic", AppRequestPrefix:"/pub",
            AppRequestQueuePrefix: "/pub/queue", UserQueuePrefix:"/user/queue" })

    bus.GetChannelManager().CreateChannel("request-channel")
    mh, _ := bus.ListenRequestStream("request-channel")
    assert.NotNil(t, mh)

    wg := sync.WaitGroup{}

    var messages []*model.Message

    mh.Handle(func(message *model.Message) {
        messages = append(messages, message)
        wg.Done()
    }, func(e error) {
        assert.Fail(t, "unexpected error")
    })

    id1 := uuid.New()
    req1, _ := json.Marshal(model.Request{
        Request: "test-request",
        Payload: "test-rq",
        Id: &id1,
    })

    wg.Add(1)

    mockServer.applicationRequestHandlerFunction("/pub/request-channel", req1, "con1")

    mockServer.applicationRequestHandlerFunction("/pub2/request-channel", req1, "con1")
    mockServer.applicationRequestHandlerFunction("/pub/request-channel-2", req1, "con1")

    mockServer.applicationRequestHandlerFunction("/pub/request-channel", []byte("invalid-request-json"), "con1")

    id2 := uuid.New()
    req2, _ := json.Marshal(model.Request{
        Request: "test-request2",
        Payload: "test-rq2",
        Id: &id2,
    })

    wg.Wait()

    wg.Add(1)
    mockServer.applicationRequestHandlerFunction("/pub/queue/request-channel", req2, "con2")
    wg.Wait()


    assert.Equal(t, len(messages), 2)

    receivedReq := messages[0].Payload.(model.Request)

    assert.Equal(t, receivedReq.Request, "test-request")
    assert.Equal(t, receivedReq.Payload, "test-rq")
    assert.Equal(t, *receivedReq.Id, id1)
    assert.Nil(t, receivedReq.BrokerDestination)

    receivedReq2 := messages[1].Payload.(model.Request)

    assert.Equal(t, receivedReq2.Request, "test-request2")
    assert.Equal(t, receivedReq2.Payload, "test-rq2")
    assert.Equal(t, *receivedReq2.Id, id2)
    assert.Equal(t, receivedReq2.BrokerDestination.ConnectionId, "con2")
    assert.Equal(t, receivedReq2.BrokerDestination.Destination, "/user/queue/request-channel")
}