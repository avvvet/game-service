CREATE TABLE balances (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    ttype VARCHAR(50) NOT NULL,              -- Transaction type: deposit, withdraw, etc.
    dr NUMERIC(20, 2) NOT NULL,              -- debit postive amount with 2 decimal places
    cr NUMERIC(20, 2) NOT NULL,              -- credit negative amount with 2 decimal places
    tref VARCHAR(50)  UNIQUE,                        
    status VARCHAR(50) DEFAULT 'pending',    -- e.g., pending, verified
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
