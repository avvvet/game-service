// cmd/callersvc/main.go
package main

import (
	"encoding/json"
	"math/rand"
	"time"

	log "github.com/sirupsen/logrus"

	config "github.com/avvvet/bingo-services/configs"
	"github.com/avvvet/bingo-services/internal/comm"
	natscli "github.com/avvvet/bingo-services/internal/nats"
	"github.com/nats-io/nats.go"
)

const SERVICE_NAME = "caller"

var instanceId string

func init() {
	instanceId = config.CreateUniqueInstance(SERVICE_NAME)
	config.Logging(SERVICE_NAME + "_service_" + instanceId)
	config.LoadEnv(SERVICE_NAME)
	rand.Seed(time.Now().UnixNano())
}

func main() {
	// connect to NATS
	n, err := natscli.Connect()
	if err != nil {
		log.Fatalf("unable to connect to NATS: %v", err)
	}
	defer n.Conn.Close()
	log.Infof("NATS connected at %s", n.Url)

	// subscribe to game-started events
	_, err = n.Conn.Subscribe("game.service", func(msg *nats.Msg) {
		var ws comm.WSMessage
		if err := json.Unmarshal(msg.Data, &ws); err != nil {
			log.Errorf("invalid WSMessage: %v", err)
			return
		}
		if ws.Type != "game-started" {
			return
		}
		var gt comm.GameType
		if err := json.Unmarshal(ws.Data, &gt); err != nil {
			log.Errorf("invalid GameType payload: %v", err)
			return
		}
		log.Infof("starting caller for gtype %d", gt.Gtype)
		go startCaller(n, gt.Gtype, gt.Gnum, 5*time.Second)
	})
	if err != nil {
		log.Fatalf("subscribe error: %v", err)
	}

	select {} // block forever
}

func startCaller(n *natscli.Nats, gtype int, gnum int, interval time.Duration) {
	// 1) prepare a shuffled deck 1–75
	deck := rand.Perm(75)
	for i := range deck {
		deck[i]++
	}

	// 2) maintain history of calls
	history := make([]int, 0, len(deck))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	cursor := 0
	for range ticker.C {
		if cursor >= len(deck) {
			log.Infof("caller done for gtype %d", gtype)
			return
		}
		num := deck[cursor]
		cursor++

		history = append(history, num)

		// 3) include history in the payload
		c := comm.CallMessage{
			Gtype:   gtype,
			Gnum:    gnum,
			Number:  num,
			History: append([]int(nil), history...), // copy to avoid mutation

		}
		PublishBingoCall(n, c)
	}
}

func PublishBingoCall(n *natscli.Nats, c comm.CallMessage) {
	data, err := json.Marshal(c)
	if err != nil {
		log.Errorf("error [PublishBingoCall] marshaling call: %v", err)
		return
	}

	msg := &comm.WSMessage{
		Type:     "bingo-call",
		Data:     data,
		SocketId: "",
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("error [PublishBingoCall] marshaling WSMessage: %v", err)
		return
	}

	if err := n.Conn.Publish("game.service", payload); err != nil {
		log.Errorf("error publishing game.service for gtype %d: %v", c.Gtype, err)
	}
}
