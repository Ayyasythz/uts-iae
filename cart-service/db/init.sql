-- Create carts table
CREATE TABLE IF NOT EXISTS carts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER,  -- Can be NULL for guest carts
    session_id VARCHAR(255) NOT NULL UNIQUE, -- For guest users
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL
);

-- Create cart items table
CREATE TABLE IF NOT EXISTS cart_items (
    id SERIAL PRIMARY KEY,
    cart_id INTEGER NOT NULL,
    product_id INTEGER NOT NULL,
    quantity INTEGER NOT NULL,
    added_at TIMESTAMP NOT NULL,
    FOREIGN KEY (cart_id) REFERENCES carts(id) ON DELETE CASCADE
);

-- Create indexes for faster lookups
CREATE INDEX IF NOT EXISTS idx_carts_user_id ON carts(user_id);
CREATE INDEX IF NOT EXISTS idx_carts_session_id ON carts(session_id);
CREATE INDEX IF NOT EXISTS idx_cart_items_cart_id ON cart_items(cart_id);
CREATE INDEX IF NOT EXISTS idx_cart_items_product_id ON cart_items(product_id);

-- Insert sample data
INSERT INTO carts (user_id, session_id, created_at, updated_at, expires_at)
VALUES 
    (1, 'session-user1', NOW(), NOW(), NOW() + INTERVAL '7 days'),
    (NULL, 'session-guest1', NOW(), NOW(), NOW() + INTERVAL '7 days');

INSERT INTO cart_items (cart_id, product_id, quantity, added_at)
VALUES
    (1, 1, 2, NOW()),  -- User 1 has 2 of Product 1 in cart
    (1, 3, 1, NOW()),  -- User 1 has 1 of Product 3 in cart
    (2, 2, 1, NOW());  -- Guest cart has 1 of Product 2