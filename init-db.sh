#!/bin/bash
set -e

# Create databases if they don't exist
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
  CREATE DATABASE user_service;
  CREATE DATABASE product_service;
  CREATE DATABASE order_service;
  CREATE DATABASE cart_service;
EOSQL

echo "Databases created successfully."

# Apply user-service schema
echo "Initializing user_service database..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "user_service" -f /docker-entrypoint-initdb.d/user-service-init.sql
echo "user_service database initialized."

# Apply the enhanced product service features
echo "Enhancing product_service with additional features..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "product_service" -f /docker-entrypoint-initdb.d/product-service-init.sql
echo "product_service enhanced successfully."

# Apply order-service schema
echo "Initializing order_service database..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "order_service" -f /docker-entrypoint-initdb.d/order-service-init.sql
echo "order_service database initialized."

# Initialize cart service schema
echo "Initializing cart_service database..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "cart_service" -f /docker-entrypoint-initdb.d/cart-service-init.sql
echo "cart_service initialized successfully."

echo "All databases initialized successfully!"