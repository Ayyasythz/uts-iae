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

CREATE TABLE IF NOT EXISTS product_reviews (
    id SERIAL PRIMARY KEY,
    product_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    rating INTEGER NOT NULL CHECK (rating >= 1 AND rating <= 5),
    review_text TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE
    );

CREATE TABLE IF NOT EXISTS product_images (
    id SERIAL PRIMARY KEY,
    product_id INTEGER NOT NULL,
    image_url VARCHAR(255) NOT NULL,
    is_primary BOOLEAN DEFAULT false,
    display_order INTEGER DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE
);

ALTER TABLE product_categories
    ADD COLUMN IF NOT EXISTS parent_id INTEGER,
    ADD COLUMN IF NOT EXISTS image_url VARCHAR(255),
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMP,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP,
    ADD CONSTRAINT fk_parent FOREIGN KEY (parent_id) REFERENCES product_categories(id);

CREATE INDEX IF NOT EXISTS idx_product_reviews_product_id ON product_reviews(product_id);
CREATE INDEX IF NOT EXISTS idx_product_reviews_user_id ON product_reviews(user_id);
CREATE INDEX IF NOT EXISTS idx_product_images_product_id ON product_images(product_id);
CREATE INDEX IF NOT EXISTS idx_product_categories_parent_id ON product_categories(parent_id);


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

INSERT INTO product_images (product_id, image_url, is_primary, display_order, created_at)
VALUES
    (1, 'https://example.com/images/smartphone1.jpg', true, 1, NOW()),
    (1, 'https://example.com/images/smartphone2.jpg', false, 2, NOW()),
    (1, 'https://example.com/images/smartphone3.jpg', false, 3, NOW()),
    (2, 'https://example.com/images/laptop1.jpg', true, 1, NOW()),
    (2, 'https://example.com/images/laptop2.jpg', false, 2, NOW()),
    (3, 'https://example.com/images/headphones1.jpg', true, 1, NOW()),
    (4, 'https://example.com/images/smartwatch1.jpg', true, 1, NOW()),
    (5, 'https://example.com/images/tv1.jpg', true, 1, NOW());

INSERT INTO product_reviews (product_id, user_id, rating, review_text, created_at, updated_at)
VALUES
    (1, 1, 5, 'Great smartphone, excellent camera quality and battery life!', NOW(), NOW()),
    (1, 2, 4, 'Good phone overall, but a bit expensive.', NOW(), NOW()),
    (2, 1, 5, 'Perfect laptop for development work. Fast and reliable.', NOW(), NOW()),
    (3, 3, 3, 'Decent headphones, but the noise cancellation could be better.', NOW(), NOW()),
    (4, 2, 5, 'Love this smartwatch! Battery lasts for days.', NOW(), NOW()),
    (5, 1, 4, 'Excellent picture quality, but the smart TV interface is a bit slow.', NOW(), NOW());


UPDATE product_categories SET parent_id = NULL, created_at = NOW(), updated_at = NOW() WHERE id = 1;
UPDATE product_categories SET parent_id = 1, created_at = NOW(), updated_at = NOW() WHERE id = 2;
UPDATE product_categories SET parent_id = 1, created_at = NOW(), updated_at = NOW() WHERE id = 3;
UPDATE product_categories SET parent_id = 1, created_at = NOW(), updated_at = NOW() WHERE id = 4;
UPDATE product_categories SET parent_id = 1, created_at = NOW(), updated_at = NOW() WHERE id = 5;

INSERT INTO product_categories (name, description, parent_id, created_at, updated_at)
VALUES
    ('Smartphones', 'Mobile phones and smartphones', 1, NOW(), NOW()),
    ('Tablets', 'Tablet computers', 1, NOW(), NOW()),
    ('Gaming Laptops', 'Laptops optimized for gaming', 2, NOW(), NOW()),
    ('Business Laptops', 'Laptops for professional use', 2, NOW(), NOW()),
    ('Wireless Earbuds', 'Compact wireless earphones', 3, NOW(), NOW()),
    ('Over-ear Headphones', 'Full-sized headphones', 3, NOW(), NOW()),
    ('Smart Home', 'Smart home automation devices', 5, NOW(), NOW()),
    ('Kitchen Appliances', 'Smart kitchen devices', 5, NOW(), NOW());