package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	config "github.com/avvvet/bingo-services/configs"
	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/gamesvc/db"
	natscli "github.com/avvvet/bingo-services/internal/nats"

	"github.com/jackc/pgx/v5/pgxpool"
)

const SERVICE_NAME = "ctl"

var instanceId string

func init() {
	instanceId = config.CreateUniqueInstance(SERVICE_NAME)
	config.Logging(SERVICE_NAME + "_service_" + instanceId)
	config.LoadEnv(SERVICE_NAME)
}

type gameInfo struct {
	ID         int
	GameTypeID int
	TotPrize   int
	GNumber    int
}

func main() {
	// pg connection
	dbpool, err := db.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.ClosePool()
	log.Printf("pg connection established successfully")

	// Connect to NATS
	n, err := natscli.Connect()
	if err != nil {
		log.Errorf("Error: unable to connect to NATS server %v", err)
		os.Exit(1)
	}
	defer n.Conn.Close()
	log.Printf("NATS connection established successfully %s", n.Url)

	ctx := context.Background()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		startedGames, err := processWaitingGames(ctx, dbpool)
		if err != nil {
			log.Printf("processWaitingGames error: %v", err)
			continue
		}

		for _, g := range startedGames {
			gt := comm.GameType{Gtype: g.GameTypeID, Gid: g.ID, Gnum: g.GNumber}
			PublishGameStarted(n, gt)
		}
	}
}

func processWaitingGames(ctx context.Context, pool *pgxpool.Pool) ([]gameInfo, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
        SELECT id, game_type_id, tot_priz, game_no
        FROM games
        WHERE status = 'waiting'
          AND created_at < now() - interval '30 seconds'
        FOR UPDATE SKIP LOCKED
    `)
	if err != nil {
		return nil, fmt.Errorf("select waiting games: %w", err)
	}
	defer rows.Close()

	var candidates []gameInfo
	for rows.Next() {
		var g gameInfo
		if err := rows.Scan(&g.ID, &g.GameTypeID, &g.TotPrize, &g.GNumber); err != nil {
			return nil, fmt.Errorf("scan game row: %w", err)
		}
		candidates = append(candidates, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	var started []gameInfo
	for _, g := range candidates {
		var count int
		if err := tx.QueryRow(ctx, `
            SELECT COUNT(*) FROM game_players WHERE game_id = $1
        `, g.ID).Scan(&count); err != nil {
			return nil, fmt.Errorf("count players for game %d: %w", g.ID, err)
		}

		// only start if more than one player joined
		if count > 1 {
			if _, err := tx.Exec(ctx, `
                UPDATE games
                SET status = 'started', updated_at = now()
                WHERE id = $1
            `, g.ID); err != nil {
				return nil, fmt.Errorf("update game %d: %w", g.ID, err)
			}

			if _, err := tx.Exec(ctx, `
                INSERT INTO games (
                    game_type_id,
                    tot_priz,
                    status,
                    created_at,
                    updated_at
                ) VALUES ($1,$2,'waiting',now(),now())
            `, g.GameTypeID, g.TotPrize); err != nil {
				return nil, fmt.Errorf("insert next game for type %d: %w", g.GameTypeID, err)
			}

			started = append(started, g)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return started, nil
}

// now publishes only the GameType payload
func PublishGameStarted(n *natscli.Nats, gt comm.GameType) {
	data, err := json.Marshal(gt)
	if err != nil {
		log.Errorf("error [PublishGameStarted] marshaling GameType %d: %v", gt.Gtype, err)
		return
	}

	msg := &comm.WSMessage{
		Type:     "game-started",
		Data:     data,
		SocketId: "",
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("error [PublishGameStarted] marshaling WSMessage: %v", err)
		return
	}

	topic := "game.service"
	if err := n.Conn.Publish(topic, payload); err != nil {
		log.Errorf("error publishing game-started for gtype %d: %v", gt.Gtype, err)
	}
}
