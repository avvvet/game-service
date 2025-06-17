// cmd/callersvc/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"

	config "github.com/avvvet/bingo-services/configs"
	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/gamesvc/db"
	natscli "github.com/avvvet/bingo-services/internal/nats"
	"github.com/nats-io/nats.go"
)

const SERVICE_NAME = "caller"

var instanceId string

func init() {
	instanceId = "001"
	config.Logging(SERVICE_NAME + "_service_" + instanceId)
	config.LoadEnv(SERVICE_NAME)
	rand.Seed(time.Now().UnixNano())
}

func main() {
	// pg connection
	dbpool, err := db.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.ClosePool()
	log.Printf("pg connection established successfully")

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
		go startCaller(n, gt.Gtype, gt.Gid, 5*time.Second, dbpool)
	})
	if err != nil {
		log.Fatalf("subscribe error: %v", err)
	}

	select {} // block forever
}

func startCaller(n *natscli.Nats, gtype int, gid int, interval time.Duration, dbPool *pgxpool.Pool) {
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
			time.Sleep(5 * time.Second)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := expireGameIfUnended(ctx, dbPool, gid); err != nil {
				log.Errorf("expireGameIfUnended: %v", err)
			}

			return
		}
		num := deck[cursor]
		cursor++

		history = append(history, num)

		// 3) include history in the payload
		c := comm.CallMessage{
			Gtype:   gtype,
			Gid:     gid,
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

func expireGameIfUnended(ctx context.Context, pool *pgxpool.Pool, gid int) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM games WHERE id = $1 FOR UPDATE`, gid).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows {
			log.Warnf("expireGameIfUnended: game gid=%d not found", gid)
			return nil
		}
		return fmt.Errorf("query game status: %w", err)
	}

	if status != "ended" {
		_, err = tx.Exec(ctx, `UPDATE games SET status = 'canceled' WHERE id = $1`, gid)
		if err != nil {
			return fmt.Errorf("update game status: %w", err)
		}
		log.Infof("Game gid=%d marked as canceled", gid)
	}

	return tx.Commit(ctx)
}
