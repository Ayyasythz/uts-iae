# E-commerce Microservices Platform

This repository contains a complete e-commerce backend platform built using microservices architecture. Each service is independently deployable and focuses on a specific business domain.

## Architecture Overview

The system consists of the following microservices:

- **User Service**: Manages user accounts and authentication
- **Product Service**: Handles product catalog, inventory, and categories
- **Order Service**: Processes orders and provides analytics
- **Cart Service**: Manages shopping carts and checkout process

These services communicate with each other through:
- **REST APIs**: For synchronous communication
- **RabbitMQ**: For asynchronous event-based communication

## Technology Stack

- **Backend**: Go (v1.24)
- **Database**: PostgreSQL
- **Message Queue**: RabbitMQ
- **Containerization**: Docker and Docker Compose
- **API**: RESTful JSON APIs

## Services Overview

### User Service (Port 8081)

Manages user accounts, profiles, and their order history.

#### Database Models
- `users`: Stores user information
- `order_history`: Tracks users' order history

#### Endpoints

| Method | Endpoint                | Description                      |
|--------|-------------------------|----------------------------------|
| GET    | /health                 | Health check                     |
| GET    | /users                  | Get all users                    |
| GET    | /users/{id}             | Get user by ID                   |
| POST   | /users                  | Create a new user                |
| PUT    | /users/{id}             | Update a user                    |
| DELETE | /users/{id}             | Delete a user                    |
| GET    | /users/{id}/orders      | Get order history for a user     |

---

### Product Service (Port 8082)

Manages products, categories, inventory, and product reviews.

#### Database Models
- `products`: Stores product information
- `product_categories`: Stores product categories
- `product_category_map`: Maps products to categories
- `product_reviews`: Stores product reviews
- `product_images`: Stores product images

#### Endpoints

| Method | Endpoint                                  | Description                        |
|--------|-------------------------------------------|------------------------------------|
| GET    | /health                                   | Health check                       |
| GET    | /products                                 | Get all products                   |
| GET    | /products/{id}                            | Get product by ID                  |
| POST   | /products                                 | Create a new product               |
| PUT    | /products/{id}                            | Update a product                   |
| DELETE | /products/{id}                            | Delete a product                   |
| PATCH  | /products/{id}/inventory                  | Update product inventory           |
| GET    | /recommendations/user/{user_id}           | Get product recommendations        |
| GET    | /products/{id}/images                     | Get product images                 |
| POST   | /products/{id}/images                     | Add product image                  |
| PUT    | /products/{id}/images/{image_id}          | Update product image               |
| DELETE | /products/{id}/images/{image_id}          | Delete product image               |
| GET    | /products/{id}/reviews                    | Get product reviews                |
| POST   | /products/{id}/reviews                    | Add product review                 |
| PUT    | /products/{id}/reviews/{review_id}        | Update product review              |
| DELETE | /products/{id}/reviews/{review_id}        | Delete product review              |
| GET    | /categories                               | Get all categories                 |
| GET    | /categories/{id}                          | Get category by ID                 |
| GET    | /categories/{id}/products                 | Get products in a category         |
| POST   | /categories                               | Create a new category              |
| PUT    | /categories/{id}                          | Update a category                  |
| DELETE | /categories/{id}                          | Delete a category                  |
| GET    | /products/search                          | Search products                    |
| GET    | /products/top-rated                       | Get top-rated products             |

---

### Order Service (Port 8083)

Processes orders, maintains order history, and provides sales analytics.

#### Database Models
- `orders`: Stores order information
- `order_items`: Stores items in orders

#### Endpoints

| Method | Endpoint                    | Description                      |
|--------|----------------------------|----------------------------------|
| GET    | /health                     | Health check                     |
| GET    | /orders                     | Get all orders                   |
| GET    | /orders/{id}                | Get order by ID                  |
| POST   | /orders                     | Create a new order               |
| PATCH  | /orders/{id}/status         | Update order status              |
| GET    | /users/{user_id}/orders     | Get orders for a user            |
| GET    | /analytics/sales            | Get sales analytics              |
| GET    | /test-rabbitmq              | Test RabbitMQ connection         |

---

### Cart Service (Port 8085)

Manages shopping carts, cart items, and checkout process.

#### Database Models
- `carts`: Stores cart information
- `cart_items`: Stores items in carts

#### Endpoints

| Method | Endpoint                           | Description                      |
|--------|-----------------------------------|----------------------------------|
| GET    | /health                            | Health check                     |
| POST   | /carts                             | Create a new cart                |
| GET    | /carts/{id}                        | Get cart by ID                   |
| GET    | /carts/session/{session_id}        | Get cart by session ID           |
| GET    | /carts/user/{user_id}              | Get cart by user ID              |
| DELETE | /carts/{id}                        | Delete a cart                    |
| PUT    | /carts/{id}/user/{user_id}         | Associate cart with user         |
| POST   | /carts/{id}/items                  | Add item to cart                 |
| PUT    | /carts/{id}/items/{item_id}        | Update cart item                 |
| DELETE | /carts/{id}/items/{item_id}        | Remove item from cart            |
| POST   | /carts/{id}/checkout               | Checkout cart                    |

## API Details

### User Service API

#### Create a User
```
POST /users
```
Request body:
```json
{
  "username": "john_doe",
  "email": "john@example.com"
}
```
Response body:
```json
{
  "id": 1,
  "username": "john_doe",
  "email": "john@example.com",
  "created_at": "2025-04-28T12:00:00Z",
  "updated_at": "2025-04-28T12:00:00Z"
}
```

#### Get User by ID
```
GET /users/{id}
```
Response body:
```json
{
  "id": 1,
  "username": "john_doe",
  "email": "john@example.com",
  "created_at": "2025-04-28T12:00:00Z",
  "updated_at": "2025-04-28T12:00:00Z"
}
```

#### Get User's Order History
```
GET /users/{id}/orders
```
Response body:
```json
[
  {
    "user_id": 1,
    "order_id": 123,
    "total": 1499.99,
    "status": "delivered",
    "created_at": "2025-04-20T10:30:00Z"
  },
  {
    "user_id": 1,
    "order_id": 124,
    "total": 199.99,
    "status": "pending",
    "created_at": "2025-04-28T09:15:00Z"
  }
]
```

### Product Service API

#### Create a Product
```
POST /products
```
Request body:
```json
{
  "name": "Smartphone X",
  "description": "Latest generation smartphone with advanced features",
  "price": 999.99,
  "inventory": 50
}
```
Response body:
```json
{
  "id": 1,
  "name": "Smartphone X",
  "description": "Latest generation smartphone with advanced features",
  "price": 999.99,
  "inventory": 50,
  "created_at": "2025-04-28T12:00:00Z",
  "updated_at": "2025-04-28T12:00:00Z"
}
```

#### Get Product by ID
```
GET /products/{id}
```
Response body:
```json
{
  "id": 1,
  "name": "Smartphone X",
  "description": "Latest generation smartphone with advanced features",
  "price": 999.99,
  "inventory": 50,
  "images": [
    {
      "id": 1,
      "product_id": 1,
      "image_url": "https://example.com/images/smartphone1.jpg",
      "is_primary": true,
      "display_order": 1,
      "created_at": "2025-04-28T12:00:00Z"
    }
  ],
  "reviews": [
    {
      "id": 1,
      "product_id": 1,
      "user_id": 1,
      "username": "john_doe",
      "rating": 5,
      "review_text": "Great smartphone, excellent camera quality and battery life!",
      "created_at": "2025-04-28T14:30:00Z",
      "updated_at": "2025-04-28T14:30:00Z"
    }
  ],
  "categories": [
    {
      "id": 1,
      "name": "Electronics",
      "description": "Electronic devices and gadgets",
      "parent_id": null,
      "created_at": "2025-04-28T12:00:00Z",
      "updated_at": "2025-04-28T12:00:00Z"
    }
  ],
  "avg_rating": 5.0,
  "created_at": "2025-04-28T12:00:00Z",
  "updated_at": "2025-04-28T12:00:00Z"
}
```

#### Search Products
```
GET /products/search?q=smartphone&category=1&min_price=500&max_price=1000&min_rating=4&sort=price_asc
```
Response body:
```json
[
  {
    "id": 1,
    "name": "Smartphone X",
    "description": "Latest generation smartphone with advanced features",
    "price": 999.99,
    "inventory": 50,
    "images": [
      {
        "image_url": "https://example.com/images/smartphone1.jpg",
        "is_primary": true
      }
    ],
    "avg_rating": 5.0,
    "created_at": "2025-04-28T12:00:00Z",
    "updated_at": "2025-04-28T12:00:00Z"
  }
]
```

### Order Service API

#### Create an Order
```
POST /orders
```
Request body:
```json
{
  "user_id": 1,
  "items": [
    {
      "product_id": 1,
      "quantity": 1
    },
    {
      "product_id": 3,
      "quantity": 2
    }
  ]
}
```
Response body:
```json
{
  "id": 123,
  "user_id": 1,
  "total_price": 1399.97,
  "status": "pending",
  "items": [
    {
      "id": 1,
      "order_id": 123,
      "product_id": 1,
      "name": "Smartphone X",
      "quantity": 1,
      "price": 999.99
    },
    {
      "id": 2,
      "order_id": 123,
      "product_id": 3,
      "name": "Wireless Headphones",
      "quantity": 2,
      "price": 199.99
    }
  ],
  "created_at": "2025-04-28T12:00:00Z",
  "updated_at": "2025-04-28T12:00:00Z"
}
```

#### Update Order Status
```
PATCH /orders/{id}/status
```
Request body:
```json
{
  "status": "shipped"
}
```
Response body:
```json
{
  "id": 123,
  "user_id": 1,
  "total_price": 1399.97,
  "status": "shipped",
  "items": [
    {
      "id": 1,
      "order_id": 123,
      "product_id": 1,
      "name": "Smartphone X",
      "quantity": 1,
      "price": 999.99
    },
    {
      "id": 2,
      "order_id": 123,
      "product_id": 3,
      "name": "Wireless Headphones",
      "quantity": 2,
      "price": 199.99
    }
  ],
  "created_at": "2025-04-28T12:00:00Z",
  "updated_at": "2025-04-28T12:05:00Z"
}
```

#### Get Sales Analytics
```
GET /analytics/sales
```
Response body:
```json
{
  "daily_sales": [
    {
      "date": "2025-04-27",
      "order_count": 5,
      "total_sales": 2500.75
    },
    {
      "date": "2025-04-28",
      "order_count": 7,
      "total_sales": 3200.50
    }
  ],
  "top_products": [
    {
      "product_id": 1,
      "name": "Smartphone X",
      "total_quantity": 12,
      "total_sales": 11999.88
    },
    {
      "product_id": 2,
      "name": "Laptop Pro",
      "total_quantity": 8,
      "total_sales": 11999.92
    }
  ],
  "total_sales": 35000.75,
  "average_order_value": 583.34,
  "sales_trend": "increasing",
  "ai_insights": [
    "Top selling product is Smartphone X with 12 units sold in the last 30 days.",
    "The average order value is $583.34, which is above average.",
    "Sales are currently increasing compared to the previous week."
  ]
}
```

### Cart Service API

#### Create a Cart
```
POST /carts
```
Request body:
```json
{
  "user_id": null,
  "session_id": "session-guest123"
}
```
Response body:
```json
{
  "id": 1,
  "user_id": null,
  "session_id": "session-guest123",
  "created_at": "2025-04-28T12:00:00Z",
  "updated_at": "2025-04-28T12:00:00Z",
  "expires_at": "2025-05-05T12:00:00Z"
}
```

#### Add Item to Cart
```
POST /carts/{id}/items
```
Request body:
```json
{
  "product_id": 1,
  "quantity": 1
}
```
Response body:
```json
{
  "id": 1,
  "user_id": null,
  "session_id": "session-guest123",
  "items": [
    {
      "id": 1,
      "cart_id": 1,
      "product_id": 1,
      "name": "Smartphone X",
      "price": 999.99,
      "quantity": 1,
      "added_at": "2025-04-28T12:05:00Z"
    }
  ],
  "total": 999.99,
  "created_at": "2025-04-28T12:00:00Z",
  "updated_at": "2025-04-28T12:05:00Z",
  "expires_at": "2025-05-05T12:00:00Z"
}
```

#### Checkout Cart
```
POST /carts/{id}/checkout
```
Request body:
```json
{
  "shipping_address": "123 Main St, Anytown, USA",
  "payment_method": "credit_card"
}
```
Response body:
```json
{
  "message": "Order created successfully",
  "order": {
    "id": 123,
    "user_id": 1,
    "total_price": 999.99,
    "status": "pending",
    "items": [
      {
        "product_id": 1,
        "quantity": 1
      }
    ],
    "created_at": "2025-04-28T12:10:00Z"
  }
}
```

## Inter-Service Communication

The microservices communicate with each other in the following ways:

1. **User Service ↔ Order Service**:
    - Order Service calls User Service to verify user existence
    - Order Service publishes order updates to RabbitMQ, consumed by User Service

2. **Product Service ↔ Order Service**:
    - Order Service calls Product Service to get product details and verify inventory
    - Order Service publishes inventory updates to RabbitMQ, consumed by Product Service

3. **User Service ↔ Product Service**:
    - Product Service calls User Service to get user details for recommendations and reviews

4. **Cart Service ↔ Product Service**:
    - Cart Service calls Product Service to get product details and verify inventory

5. **Cart Service ↔ Order Service**:
    - Cart Service calls Order Service to create an order during checkout

6. **Cart Service ↔ User Service**:
    - Cart Service calls User Service to verify user existence

## Setup and Deployment

The entire application can be deployed using Docker Compose:

```bash
# Build and start all services
docker-compose up -d

# To stop all services
docker-compose down
```

Each service is also individually deployable. For example:

```bash
# Start just the product service
docker-compose up -d product-service
```

## Environment Variables

Each service supports the following environment variables:

- `POSTGRES_URI`: PostgreSQL connection string
- `RABBITMQ_URI`: RabbitMQ connection string
- `USER_SERVICE_URL`: URL of the User Service
- `PRODUCT_SERVICE_URL`: URL of the Product Service
- `ORDER_SERVICE_URL`: URL of the Order Service

## Database Initialization

The `init-db.sh` script creates the necessary databases and populates them with initial data when the application is first deployed.

## Message Queue Information

The following RabbitMQ queues are used for asynchronous communication:

- `order_updates`: Order status updates (Order Service to User Service)
- `inventory_updates`: Inventory updates (Order Service to Product Service)
- `cart_events`: Cart events like creation, item added, checkout (Cart Service to Analytics)
