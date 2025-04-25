#!/bin/bash
set -e

# Create databases if they don't exist
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
  CREATE DATABASE user_service;
  CREATE DATABASE product_service;
  CREATE DATABASE order_service;
EOSQL

echo "Databases created successfully."

# Apply user-service schema
echo "Initializing user_service database..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "user_service" <<-EOSQL
  -- Create users table
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
EOSQL

echo "user_service database initialized."

# Apply product-service schema
echo "Initializing product_service database..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "product_service" <<-EOSQL
  -- Create products table
  CREATE TABLE IF NOT EXISTS products (
      id SERIAL PRIMARY KEY,
      name VARCHAR(255) NOT NULL,
      description TEXT,
      price DECIMAL(10, 2) NOT NULL,
      inventory INTEGER NOT NULL DEFAULT 0,
      created_at TIMESTAMP NOT NULL,
      updated_at TIMESTAMP NOT NULL
  );

  -- Create product categories
  CREATE TABLE IF NOT EXISTS product_categories (
      id SERIAL PRIMARY KEY,
      name VARCHAR(100) NOT NULL,
      description TEXT
  );

  -- Create product category relationships
  CREATE TABLE IF NOT EXISTS product_category_map (
      product_id INTEGER NOT NULL,
      category_id INTEGER NOT NULL,
      PRIMARY KEY (product_id, category_id),
      FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE,
      FOREIGN KEY (category_id) REFERENCES product_categories(id) ON DELETE CASCADE
  );

  -- Insert sample data
  INSERT INTO products (name, description, price, inventory, created_at, updated_at)
  VALUES
      ('Smartphone X', 'Latest generation smartphone with advanced features', 999.99, 50, NOW(), NOW()),
      ('Laptop Pro', 'Professional laptop for developers and designers', 1499.99, 25, NOW(), NOW()),
      ('Wireless Headphones', 'Noise-cancelling wireless headphones', 199.99, 100, NOW(), NOW()),
      ('Smart Watch', 'Fitness and health tracking smart watch', 249.99, 75, NOW(), NOW()),
      ('Ultra HD TV', '65-inch 4K Ultra HD Smart TV', 799.99, 20, NOW(), NOW());

  INSERT INTO product_categories (name, description)
  VALUES
      ('Electronics', 'Electronic devices and gadgets'),
      ('Computers', 'Laptops, desktops, and computer accessories'),
      ('Audio', 'Headphones, speakers, and audio equipment'),
      ('Wearables', 'Smart watches and wearable technology'),
      ('Home', 'Home appliances and entertainment systems');

  INSERT INTO product_category_map (product_id, category_id)
  VALUES
      (1, 1), -- Smartphone in Electronics
      (2, 1), -- Laptop in Electronics
      (2, 2), -- Laptop in Computers
      (3, 1), -- Headphones in Electronics
      (3, 3), -- Headphones in Audio
      (4, 1), -- Smart Watch in Electronics
      (4, 4), -- Smart Watch in Wearables
      (5, 1), -- TV in Electronics
      (5, 5); -- TV in Home
EOSQL

echo "product_service database initialized."

# Apply order-service schema
echo "Initializing order_service database..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "order_service" <<-EOSQL
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
EOSQL

echo "order_service database initialized."

echo "All databases initialized successfully!"