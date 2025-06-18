package broker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/avvvet/bingo-services/internal/gamesvc/service"
	"github.com/nats-io/nats.go"
	"github.com/shopspring/decimal"
	log "github.com/sirupsen/logrus"
)

type Broker struct {
	Conn              *nats.Conn
	UserService       *service.UserService
	BalanceService    *service.BalanceService
	GameService       *service.GameService
	GamePlayerService *service.GamePlayerService
	CardService       *service.CardService
}

func NewBroker(nc *nats.Conn, userService *service.UserService,
	balanceService *service.BalanceService, gameService *service.GameService,
	gamePlayerService *service.GamePlayerService, cardService *service.CardService) *Broker {
	return &Broker{
		Conn:              nc,
		UserService:       userService,
		BalanceService:    balanceService,
		GameService:       gameService,
		GamePlayerService: gamePlayerService,
		CardService:       cardService,
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
	case "get-balance":
		var request struct {
			UserID int64 `json:"userId"`
		}
		err := json.Unmarshal(msg.Data, &request)
		if err != nil {
			log.Errorf("Error %s", err)
		}

		// get user balance
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		balance, err := b.BalanceService.GetUserBalance(ctx, request.UserID)
		if err != nil {
			log.Errorf("Error getBalance %s", err)
		}

		playerData := comm.PlayerData{
			Balance: balance.StringFixed(2),
		}

		b.PublishBalance(playerData, msg.SocketId)
	case "check-active-game":

		var request struct {
			UserId int64 `json:"user_id"`
		}

		err := json.Unmarshal(msg.Data, &request)
		if err != nil {
			log.Errorf("Error decoding request: %s", err)
			break
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		activeGamePlayer, err := b.GamePlayerService.GetActiveGameForUser(ctx, request.UserId)
		if err != nil {
			log.Errorf("Error checking active game for user %d: %s", request.UserId, err)
			break
		}

		if activeGamePlayer != nil {
			// Fetch the game
			game, err := b.GameService.GetGameByID(ctx, int(activeGamePlayer.GameID))
			if err != nil {
				log.Errorf("Error retrieving game: %s", err)
				break
			}

			// Fetch the game players
			players, err := b.GamePlayerService.GetGamePlayers(ctx, int(activeGamePlayer.GameID))
			if err != nil {
				log.Errorf("Error retrieving players: %s", err)
				break
			}

			// Fetch the player's card
			gameCard, err := b.CardService.GetCardBySN(ctx, activeGamePlayer.CardSN)
			if err != nil {
				log.Errorf("Error retrieving card by SN: %s", err)
				break
			}

			card := comm.GameCard{
				CardSN: gameCard.CardSN,
				Data:   gameCard.Data,
			}

			// this for web clients as gametype fee is know example id 1 -> 10, 2 -> 20 etc
			gameFeeType, err := b.GameService.ConvertGameTypeIDToFee(int(game.GameTypeID))
			if err != nil {
				log.Errorf("Error converting ConvertGameTypeIDToFee for game.GameTypeID %v : %s", int(game.GameTypeID), err)
			}
			gameData := comm.GameData{
				Gid:     int(game.ID),
				Game:    game,
				Players: players,
				Card:    &card,
				Gtype:   gameFeeType,
			}

			b.PublishActiveGameResponse(gameData, msg.SocketId)
		}
	case "get-wait-game":
		var request struct {
			UserId int64 `json:"user_id"`
			Gtype  int   `json:"gtype"`
			GameId int   `json:"game_id"`
		}

		err := json.Unmarshal(msg.Data, &request)
		if err != nil {
			log.Errorf("Error %s", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		//get a game for game type
		game, err := b.GameService.GetGameByFeeAndStatus(ctx, request.Gtype, "waiting")
		if err != nil {
			log.Errorf("Error [UserService.GetOrCreateUser] %s", err)
		}

		//get players
		players, err := b.GamePlayerService.GetGamePlayers(ctx, int(game.ID))
		if err != nil {
			log.Errorf("Error [UserService.GetOrCreateUser] %s", err)
		}

		gameData := comm.GameData{
			Gid:     request.GameId,
			Game:    game,
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
			log.Errorf("Error unmarshalling player-select-card: %s", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 1. Check user balance before proceeding
		balance, err := b.BalanceService.GetUserBalance(ctx, request.UserId)
		if err != nil {
			log.Errorf("Error [BalanceService.GetUserBalance]: %s", err)
			return
		}

		gameFee := request.Gtype
		if balance.LessThan(decimal.NewFromInt(int64(gameFee))) {
			log.Infof("User %d has insufficient balance: %s", request.UserId, balance.StringFixed(2))

			// Publish insufficient balance response
			resp := comm.BalanceStatus{
				Status:    false,
				Timestamp: time.Now().UnixMilli(),
			}

			b.PublishInsufficientBalance(resp, msg.SocketId)
			return
		}

		// 2. Proceed to create game player if card is available
		gamePlayer, err := b.GamePlayerService.CreateGamePlayerIfAvailable(ctx, request.GameId, request.UserId, request.CardSN)
		if err != nil {
			log.Errorf("Error [GamePlayerService.CreateGamePlayerIfAvailable]: %s", err)
			return
		}

		if gamePlayer == nil {
			return
		}

		// 3. Get updated game and players info Gtype is fee and it will be converted to game type id
		game, err := b.GameService.GetGameByFeeAndStatus(ctx, request.Gtype, "waiting")
		if err != nil {
			log.Errorf("Error [GameService.GetGameByFeeAndStatus]: %s", err)
		}

		players, err := b.GamePlayerService.GetGamePlayers(ctx, int(request.GameId))
		if err != nil {
			log.Errorf("Error [GamePlayerService.GetGamePlayers]: %s", err)
		}

		gameData := comm.GameData{
			Game:    game,
			Players: players,
			Gtype:   request.Gtype,
			Gid:     request.GameId,
		}
		b.PublishWaitGameResponseToAll(gameData, msg.SocketId)

		// 4. Publish the selected card detail to the player
		gameCard, err := b.CardService.GetCardBySN(ctx, gamePlayer.CardSN)
		if err != nil {
			log.Errorf("Error [CardService.GetCardBySN]: %s", err)
		}

		card := comm.GameCard{
			CardSN: gameCard.CardSN,
			Data:   gameCard.Data,
		}
		b.PublishSelectCardResponse(card, msg.SocketId)

	case "player-cancel-card":

		var request struct {
			UserId int64 `json:"user_id"`
			GameId int   `json:"game_id"`
			Gtype  int   `json:"gtype"`
		}

		err := json.Unmarshal(msg.Data, &request)
		if err != nil {
			log.Errorf("Error unmarshalling player-select-card: %s", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err = b.GamePlayerService.DeleteGamePlayerCard(ctx, request.GameId, request.UserId)
		if err != nil {
			log.Errorf("Error DeleteGamePlayerCard: %s", err)
			return
		}

		r := comm.Res{
			Status: true,
		}

		b.CancelCardResponse(r, msg.SocketId)
	default:
		log.Error("Unknown message")
		return
	}
}

func (b *Broker) PublishBalance(p comm.PlayerData, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("publishBalance unable to marsahl playerData")
	}

	msg := &comm.WSMessage{
		Type:     "balance-resp",
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

func (b *Broker) PublishSelectCardResponse(p comm.GameCard, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("[PublishSelectCardResponse] unable to marsahl playerData")
	}

	msg := &comm.WSMessage{
		Type:     "player-select-card-response",
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

func (b *Broker) CancelCardResponse(p comm.Res, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("PublishDeleteCardResponse unable to marsahl playerData")
	}

	msg := &comm.WSMessage{
		Type:     "player-cancel-card-response",
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

func (b *Broker) PublishInsufficientBalance(p comm.BalanceStatus, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("[insufficient_balance] unable to marsahl playerData")
	}

	msg := &comm.WSMessage{
		Type:     "insufficient-balance-response",
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

func (b *Broker) PublishActiveGameResponse(gdata comm.GameData, socketId string) {
	data, err := json.Marshal(gdata)
	if err != nil {
		log.Errorf("error [PublishActiveGameResponse] unable to marsahl game data %d %s", gdata.Game.ID, socketId)
	}

	msg := &comm.WSMessage{
		Type:     "check-active-game-response",
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

func (b *Broker) PublishWaitGameResponseToAll(gdata comm.GameData, socketId string) {
	data, err := json.Marshal(gdata)
	if err != nil {
		log.Errorf("error [PublishWaitGameResponse] unable to marsahl game data %d %s", gdata.Game.ID, socketId)
	}

	msg := &comm.WSMessage{
		Type:     "get-wait-game-response-broadcast",
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
