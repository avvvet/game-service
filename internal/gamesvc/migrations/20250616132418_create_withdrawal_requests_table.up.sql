CREATE TABLE withdrawal_requests (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    amount NUMERIC(20, 2) NOT NULL CHECK (amount > 0), -- Withdrawal amount with 2 decimal places
    balance_snapshot NUMERIC(20, 2) NOT NULL, -- User's balance at time of request
    account_type VARCHAR(20) NOT NULL CHECK (account_type IN ('cbe', 'abyssinia', 'telebirr')), -- Bank/payment method
    account_no VARCHAR(100) NOT NULL, -- Account number for the specified account type
    name VARCHAR(255) NOT NULL, -- Account holder name
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'processing', 'completed', 'rejected', 'cancelled')), -- Request status
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);