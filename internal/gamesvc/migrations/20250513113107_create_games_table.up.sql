-- Create a custom sequence for game_no if not already created
CREATE SEQUENCE IF NOT EXISTS game_no_seq START 1;

-- Updated games table
CREATE TABLE games (
    id SERIAL PRIMARY KEY,
    game_no BIGINT NOT NULL DEFAULT nextval('game_no_seq') UNIQUE,
    game_type_id INTEGER NOT NULL REFERENCES game_types(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(user_id) ON DELETE SET NULL,  -- winner user_id; nullable
    tot_priz NUMERIC(12, 2) DEFAULT 0,                             -- total prize; defaults to 0
    status VARCHAR(20) CHECK (status IN ('waiting', 'started', 'ended', 'canceled')) DEFAULT 'waiting',
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
