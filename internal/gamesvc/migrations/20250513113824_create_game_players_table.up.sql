CREATE TABLE game_players (
    id SERIAL PRIMARY KEY,
    game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    card_sn VARCHAR(100) NOT NULL REFERENCES cards(card_sn) ON DELETE CASCADE,
    status VARCHAR(20) CHECK (status IN ('pending', 'win', 'loose', 'canceled')) DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

    -- A user can join a game only once
    CONSTRAINT unique_game_user UNIQUE (game_id, user_id),

    -- A card_sn can only be used once per game
    CONSTRAINT unique_game_card UNIQUE (game_id, card_sn)
);
