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

-- Create product categories (for future categorization)
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