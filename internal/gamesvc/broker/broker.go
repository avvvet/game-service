package broker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/avvvet/bingo-services/internal/gamesvc/service"
	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
)

type Broker struct {
	Conn              *nats.Conn
	UserService       *service.UserService
	BalanceService    *service.BalanceService
	GameService       *service.GameService
	GamePlayerService *service.GamePlayerService
}

func NewBroker(nc *nats.Conn, userService *service.UserService, balanceService *service.BalanceService, gameService *service.GameService, gamePlayerService *service.GamePlayerService) *Broker {
	return &Broker{
		Conn:              nc,
		UserService:       userService,
		BalanceService:    balanceService,
		GameService:       gameService,
		GamePlayerService: gamePlayerService,
	}
}

// handles message coming from socket
func (b *Broker) handleMessage(msgNat *nats.Msg) {
	//unmarshal nats message
	msg := &comm.WSMessage{}
	err := json.Unmarshal(msgNat.Data, &msg)
	if err != nil {
		log.Errorf("Error nats message %s", err)
	}

	switch msg.Type {
	case "init":
		// unmarshal socket message
		userInfo := models.User{}
		err := json.Unmarshal(msg.Data, &userInfo)
		if err != nil {
			log.Errorf("Error %s", err)
		}

		user, err := b.UserService.GetOrCreateUser(userInfo)
		if err != nil {
			log.Errorf("Error [UserService.GetOrCreateUser] %s", err)
		}

		// get user balance
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		balance, err := b.BalanceService.GetUserBalance(ctx, user.UserId)
		if err != nil {
			log.Errorf("Error [BalanceService.GetUserBalance] %s", err)
		}

		playerData := comm.PlayerData{
			Name:    user.Name,
			Balance: balance.StringFixed(2),
			UserId:  user.UserId,
		}

		// publish to socket service
		//balance := decimal.NewFromFloat(40000.34)
		b.PublishInitResponse(playerData, msg.SocketId)
	case "get-wait-game":
		var request struct {
			UserId int64 `json:"user_id"`
			Gtype  int   `json:"gtype"`
		}

		err := json.Unmarshal(msg.Data, &request)
		if err != nil {
			log.Errorf("Error %s", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		//get a game for game type
		game, err := b.GameService.GetGameByTypeAndStatus(ctx, request.Gtype, "waiting")
		if err != nil {
			log.Errorf("Error [UserService.GetOrCreateUser] %s", err)
		}

		//get a game for game type
		players, err := b.GamePlayerService.GetGamePlayers(ctx, int(game.ID))
		if err != nil {
			log.Errorf("Error [UserService.GetOrCreateUser] %s", err)
		}

		gameData := comm.GameData{
			Game:    *game,
			Players: players,
		}

		b.PublishWaitGameResponse(gameData, msg.SocketId)
	case "player-select-card":
		var request struct {
			UserId int64  `json:"user_id"`
			GameId int    `json:"game_id"`
			CardSN string `json:"card_sn"`
			Gtype  int    `json:"gtype"`
		}

		err := json.Unmarshal(msg.Data, &request)
		if err != nil {
			log.Errorf("Error %s", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		//create game player if card is available
		gamePlayer, err := b.GamePlayerService.CreateGamePlayerIfAvailable(ctx, request.GameId, request.UserId, request.CardSN)
		if err != nil {
			log.Errorf("Error [GamePlayerService.CreateGamePlayerIfAvailable] %s", err)
			return
		}

		if gamePlayer == nil {
			return
		}

		// lets send new data

		//get a game for game type
		game, err := b.GameService.GetGameByTypeAndStatus(ctx, request.Gtype, "waiting")
		if err != nil {
			log.Errorf("Error [UserService.GetOrCreateUser] %s", err)
		}

		//get a game for game type
		players, err := b.GamePlayerService.GetGamePlayers(ctx, int(request.GameId))
		if err != nil {
			log.Errorf("Error [UserService.GetOrCreateUser] %s", err)
		}

		gameData := comm.GameData{
			Game:    *game,
			Players: players,
		}
		b.PublishWaitGameResponse(gameData, msg.SocketId)
	default:
		log.Error("Unknown message")
		return
	}
}

func (b *Broker) PublishInitResponse(p comm.PlayerData, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("unable to marsahl playerData %d %s", p.UserId, socketId)
	}

	msg := &comm.WSMessage{
		Type:     "init-response",
		Data:     data,
		SocketId: socketId,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error %s", err)
	}

	topic := "game.service"
	b.Publish(topic, payload)
}

func (b *Broker) PublishWaitGameResponse(gdata comm.GameData, socketId string) {
	data, err := json.Marshal(gdata)
	if err != nil {
		log.Errorf("error [PublishWaitGameResponse] unable to marsahl game data %d %s", gdata.Game.ID, socketId)
	}

	msg := &comm.WSMessage{
		Type:     "get-wait-game-response",
		Data:     data,
		SocketId: socketId,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error %s", err)
	}

	topic := "game.service"
	b.Publish(topic, payload)
}

// consume message from signal (Queue)
func (b *Broker) QueueSubscribSignal(topic, queueGroup string) (*nats.Subscription, error) {
	sub, err := b.Conn.QueueSubscribe(topic, queueGroup, b.handleMessage)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

// consume message from socket service
func (b *Broker) SubscribSocketService(topic string) (*nats.Subscription, error) {
	sub, err := b.Conn.Subscribe(topic, b.handleMessage)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

// relay service publish message for signal service to consume
func (b *Broker) Publish(topic string, payload []byte) error {
	err := b.Conn.Publish(topic, payload)
	if err != nil {
		log.Errorf("Error publishing to topic %s: %s", topic, err)
		return err
	}

	return nil
}
