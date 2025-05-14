
CREATE TABLE users (
    user_id BIGINT PRIMARY KEY,             -- Telegram userId (bigint for large numbers)
    name VARCHAR(255) NOT NULL,            -- User's first name
    email VARCHAR(255),                      -- User's email (unique)
    phone VARCHAR(20),                     -- User's mobile number
    avatar VARCHAR(255) ,            
    status VARCHAR(50) DEFAULT 'active',  -- User's status (active, inactive, etc.)
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,  -- Creation timestamp
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP   -- Update timestamp
);
