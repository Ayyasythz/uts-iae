-- Create orders table
CREATE TABLE IF NOT EXISTS orders (
                                      id SERIAL PRIMARY KEY,
                                      user_id INTEGER NOT NULL,
                                      total_price DECIMAL(10, 2) NOT NULL,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
    );

-- Create order items table
CREATE TABLE IF NOT EXISTS order_items (
                                           id SERIAL PRIMARY KEY,
                                           order_id INTEGER NOT NULL,
                                           product_id INTEGER NOT NULL,
                                           quantity INTEGER NOT NULL,
                                           price DECIMAL(10, 2) NOT NULL,
    FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE
    );

-- Create indexes for faster lookups
CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_order_items_order_id ON order_items(order_id);
CREATE INDEX IF NOT EXISTS idx_order_items_product_id ON order_items(product_id);

-- Insert sample data
INSERT INTO orders (user_id, total_price, status, created_at, updated_at)
VALUES
    (1, 1499.99, 'delivered', NOW() - INTERVAL '15 days', NOW() - INTERVAL '10 days'),
    (1, 199.99, 'delivered', NOW() - INTERVAL '7 days', NOW() - INTERVAL '5 days'),
    (2, 2249.98, 'shipped', NOW() - INTERVAL '3 days', NOW() - INTERVAL '1 day'),
    (3, 799.99, 'pending', NOW(), NOW());

INSERT INTO order_items (order_id, product_id, quantity, price)
VALUES
    (1, 2, 1, 1499.99),
    (2, 3, 1, 199.99),
    (3, 1, 1, 999.99),
    (3, 2, 1, 1249.99),
    (4, 5, 1, 799.99);