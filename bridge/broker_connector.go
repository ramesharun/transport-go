// Copyright 2019 VMware Inc.
package bridge

import (
    "fmt"
    "github.com/go-stomp/stomp"
    "github.com/google/uuid"
    "net/url"
    "sync"
)

// BrokerConnector is used to connect to a message broker over TCP or WebSocket.
type BrokerConnector interface {
    Connect(config *BrokerConnectorConfig) (Connection, error)
}

type brokerConnector struct {
    c         Connection
    config    *BrokerConnectorConfig
    connected bool
}

// Create a new broker connector
func NewBrokerConnector() BrokerConnector {
    return &brokerConnector{connected: false}
}

func checkConfig(config *BrokerConnectorConfig) error {
    if config == nil {
        return fmt.Errorf("config is nil")
    }
    if config.ServerAddr == "" {
        return fmt.Errorf("config invalid, config missing server address")
    }
    if config.Username == "" {
        return fmt.Errorf("config invalid, config missing username")
    }
    if config.Password == "" {
        return fmt.Errorf("config invalid, config missing password")
    }
    return nil
}

// Connect to broker using supplied connector config.
func (bc *brokerConnector) Connect(config *BrokerConnectorConfig) (Connection, error) {

    err := checkConfig(config)
    if err != nil {
        return nil, err
    }

    // use different mechanism for WS connections.
    if config.UseWS {
        return bc.connectWs(config)
    }

    return bc.connectTCP(config, err)
}

func (bc *brokerConnector) connectTCP(config *BrokerConnectorConfig, err error) (Connection, error) {
    if config.HostHeader == "" {
        config.HostHeader = "/"
    }
    var options = []func(*stomp.Conn) error{
        stomp.ConnOpt.Login(config.Username, config.Password),
        stomp.ConnOpt.Host(config.HostHeader),
    }
    conn, err := stomp.Dial("tcp", config.ServerAddr, options...)
    if err != nil {
        return nil, err
    }
    id := uuid.New()
    bcConn := &connection{
        id:             &id,
        conn:           conn,
        subscriptions:  make(map[string]Subscription),
        useWs:          false,
        connLock:       sync.Mutex{},
        disconnectChan: make(chan bool)}
    bc.c = bcConn
    bc.connected = true
    bc.config = config
    return bcConn, nil
}

func (bc *brokerConnector) connectWs(config *BrokerConnectorConfig) (Connection, error) {

    u := url.URL{Scheme: "ws", Host: config.ServerAddr, Path: config.WSPath}
    c := NewBridgeWsClient()
    err := c.Connect(&u, nil)
    if err != nil {
        return nil, fmt.Errorf("cannot connect to host '%s' via path '%s', stopping", config.ServerAddr, config.WSPath)
    }
    id := uuid.New()
    bcConn := &connection{
        id:             &id,
        wsConn:         c,
        subscriptions:  make(map[string]Subscription),
        useWs:          true,
        connLock:       sync.Mutex{},
        disconnectChan: make(chan bool)}
    bc.c = bcConn
    bc.connected = true
    return bcConn, nil
}