CREATE TABLE IF NOT EXISTS users (
                                     id SERIAL PRIMARY KEY,
                                     username VARCHAR(100) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
    );

-- Create order history table
CREATE TABLE IF NOT EXISTS order_history (
                                             id SERIAL PRIMARY KEY,
                                             user_id INTEGER NOT NULL,
                                             order_id INTEGER NOT NULL,
                                             total DECIMAL(10, 2) NOT NULL,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
    );

-- Insert sample data
INSERT INTO users (username, email, created_at, updated_at)
VALUES
    ('john_doe', 'john@example.com', NOW(), NOW()),
    ('jane_smith', 'jane@example.com', NOW(), NOW()),
    ('bob_johnson', 'bob@example.com', NOW(), NOW());