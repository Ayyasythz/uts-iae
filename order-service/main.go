package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v4/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	PORT                    = 8083
	POSTGRES_URI            = "postgres://postgres:postgres@postgres:5432/order_service" // Changed localhost to postgres
	RABBITMQ_URI            = "amqp://guest:guest@rabbitmq:5672/"                        // Changed localhost to rabbitmq
	ORDER_UPDATES_QUEUE     = "order_updates"
	INVENTORY_UPDATES_QUEUE = "inventory_updates"
	USER_SERVICE_URL        = "http://user-service:8081"    // Changed localhost to user-service
	PRODUCT_SERVICE_URL     = "http://product-service:8082" // Changed localhost to product-service
)

// Order represents an order in the system
type Order struct {
	ID         int         `json:"id"`
	UserID     int         `json:"user_id"`
	TotalPrice float64     `json:"total_price"`
	Status     string      `json:"status"`
	Items      []OrderItem `json:"items"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// OrderItem represents an item in an order
type OrderItem struct {
	ID        int     `json:"id"`
	OrderID   int     `json:"order_id"`
	ProductID int     `json:"product_id"`
	Name      string  `json:"name,omitempty"` // Populated from product service
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"` // Price at time of order
}

// OrderRequest represents the request to create a new order
type OrderRequest struct {
	UserID int              `json:"user_id"`
	Items  []OrderItemInput `json:"items"`
}

// OrderItemInput represents an input item for order creation
type OrderItemInput struct {
	ProductID int `json:"product_id"`
	Quantity  int `json:"quantity"`
}

// User represents a user from the User Service
type User struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Product represents a product from the Product Service
type Product struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       float64   `json:"price"`
	Inventory   int       `json:"inventory"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// OrderHistory represents a user's order history sent to the User Service
type OrderHistory struct {
	UserID    int       `json:"user_id"`
	OrderID   int       `json:"order_id"`
	Total     float64   `json:"total"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// InventoryUpdate represents a product inventory update sent to the Product Service
type InventoryUpdate struct {
	ProductID  int  `json:"product_id"`
	Quantity   int  `json:"quantity"`
	IsIncrease bool `json:"is_increase"`
}

// App represents the application
type App struct {
	Router   *mux.Router
	DB       *pgxpool.Pool
	RabbitMQ *amqp.Connection
	RabbitCh *amqp.Channel
}

// Initialize sets up the database connection and router
func (a *App) Initialize() error {
	var err error

	// Initialize PostgreSQL connection
	a.DB, err = pgxpool.Connect(context.Background(), POSTGRES_URI)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %v", err)
	}

	// Verify database connection
	if err = a.DB.Ping(context.Background()); err != nil {
		return fmt.Errorf("unable to ping database: %v", err)
	}

	// Initialize RabbitMQ connection
	a.RabbitMQ, err = amqp.Dial(RABBITMQ_URI)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %v", err)
	}

	// Create a channel
	a.RabbitCh, err = a.RabbitMQ.Channel()
	if err != nil {
		return fmt.Errorf("failed to open a channel: %v", err)
	}

	// Declare the queues we'll be using
	queues := []string{ORDER_UPDATES_QUEUE, INVENTORY_UPDATES_QUEUE}
	for _, queue := range queues {
		_, err = a.RabbitCh.QueueDeclare(
			queue, // name
			true,  // durable
			false, // delete when unused
			false, // exclusive
			false, // no-wait
			nil,   // arguments
		)
		if err != nil {
			return fmt.Errorf("failed to declare queue %s: %v", queue, err)
		}
	}

	// Initialize router
	a.Router = mux.NewRouter()
	a.initializeRoutes()

	return nil
}

// initializeRoutes sets up the API routes
func (a *App) initializeRoutes() {
	// Health check
	a.Router.HandleFunc("/health", a.healthCheck).Methods("GET")

	a.Router.HandleFunc("/test-rabbitmq", a.testRabbitMQConnection).Methods("GET")

	// Order operations
	a.Router.HandleFunc("/orders", a.getOrders).Methods("GET")
	a.Router.HandleFunc("/orders/{id:[0-9]+}", a.getOrder).Methods("GET")
	a.Router.HandleFunc("/orders", a.createOrder).Methods("POST")
	a.Router.HandleFunc("/orders/{id:[0-9]+}/status", a.updateOrderStatus).Methods("PATCH")

	// User-specific orders
	a.Router.HandleFunc("/users/{user_id:[0-9]+}/orders", a.getUserOrders).Methods("GET")

	// Order statistics and analytics (could be expanded for AI analysis)
	a.Router.HandleFunc("/analytics/sales", a.getSalesAnalytics).Methods("GET")
}

// Run starts the HTTP server
func (a *App) Run() {
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", PORT),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
		IdleTimeout:  60 * time.Second,
		Handler:      a.Router,
	}

	// Run server in a goroutine so it doesn't block
	go func() {
		log.Printf("Order Service listening on port %d...", PORT)
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	c := make(chan os.Signal, 1)
	// Accept graceful shutdowns when quit via SIGINT (Ctrl+C) or SIGTERM
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Block until signal is received
	<-c

	log.Println("Shutting down server...")

	// Create a deadline to wait for
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Close RabbitMQ connection
	if err := a.RabbitCh.Close(); err != nil {
		log.Printf("Error closing RabbitMQ channel: %v", err)
	}
	if err := a.RabbitMQ.Close(); err != nil {
		log.Printf("Error closing RabbitMQ connection: %v", err)
	}

	// Close DB connection
	a.DB.Close()

	// Shutdown server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}

// healthCheck is a simple health check endpoint
func (a *App) healthCheck(w http.ResponseWriter, r *http.Request) {
	// Check database connection
	err := a.DB.Ping(context.Background())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Database connection failure")
		return
	}

	// Check RabbitMQ connection
	if a.RabbitMQ.IsClosed() {
		respondWithError(w, http.StatusInternalServerError, "RabbitMQ connection failure")
		return
	}

	// Check User Service
	_, err = http.Get(fmt.Sprintf("%s/health", USER_SERVICE_URL))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "User Service connection failure")
		return
	}

	// Check Product Service
	_, err = http.Get(fmt.Sprintf("%s/health", PRODUCT_SERVICE_URL))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Product Service connection failure")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

// getOrders returns all orders
func (a *App) getOrders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(context.Background(),
		"SELECT id, user_id, total_price, status, created_at, updated_at FROM orders")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	orders := []Order{}
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.TotalPrice, &o.Status,
			&o.CreatedAt, &o.UpdatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get order items
		items, err := a.getOrderItems(o.ID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		o.Items = items

		orders = append(orders, o)
	}

	respondWithJSON(w, http.StatusOK, orders)
}

// getOrder returns a specific order
func (a *App) getOrder(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var o Order
	err := a.DB.QueryRow(context.Background(),
		"SELECT id, user_id, total_price, status, created_at, updated_at FROM orders WHERE id = $1",
		id).Scan(&o.ID, &o.UserID, &o.TotalPrice, &o.Status, &o.CreatedAt, &o.UpdatedAt)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "Order not found")
		return
	}

	// Get order items
	items, err := a.getOrderItems(o.ID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	o.Items = items

	respondWithJSON(w, http.StatusOK, o)
}

// getUserOrders returns all orders for a specific user
func (a *App) getUserOrders(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]

	// Verify user exists
	userResp, err := http.Get(fmt.Sprintf("%s/users/%s", USER_SERVICE_URL, userID))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to contact User Service")
		return
	}
	defer userResp.Body.Close()

	if userResp.StatusCode != http.StatusOK {
		respondWithError(w, http.StatusNotFound, "User not found")
		return
	}

	rows, err := a.DB.Query(context.Background(),
		"SELECT id, user_id, total_price, status, created_at, updated_at FROM orders WHERE user_id = $1 ORDER BY created_at DESC",
		userID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	orders := []Order{}
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.TotalPrice, &o.Status,
			&o.CreatedAt, &o.UpdatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get order items
		items, err := a.getOrderItems(o.ID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		o.Items = items

		orders = append(orders, o)
	}

	respondWithJSON(w, http.StatusOK, orders)
}

// getOrderItems returns all items for a specific order
func (a *App) getOrderItems(orderID int) ([]OrderItem, error) {
	rows, err := a.DB.Query(context.Background(),
		"SELECT id, order_id, product_id, quantity, price FROM order_items WHERE order_id = $1",
		orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []OrderItem{}
	for rows.Next() {
		var i OrderItem
		if err := rows.Scan(&i.ID, &i.OrderID, &i.ProductID, &i.Quantity, &i.Price); err != nil {
			return nil, err
		}

		// Get product name from Product Service
		productResp, err := http.Get(fmt.Sprintf("%s/products/%d", PRODUCT_SERVICE_URL, i.ProductID))
		if err == nil && productResp.StatusCode == http.StatusOK {
			defer productResp.Body.Close()
			productBody, _ := ioutil.ReadAll(productResp.Body)
			var product Product
			if json.Unmarshal(productBody, &product) == nil {
				i.Name = product.Name
			}
		}

		items = append(items, i)
	}

	return items, nil
}

// createOrder creates a new order
func (a *App) createOrder(w http.ResponseWriter, r *http.Request) {
	var req OrderRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Validate user exists
	userResp, err := http.Get(fmt.Sprintf("%s/users/%d", USER_SERVICE_URL, req.UserID))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to contact User Service")
		return
	}
	defer userResp.Body.Close()

	if userResp.StatusCode != http.StatusOK {
		respondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Start a transaction
	tx, err := a.DB.Begin(context.Background())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback(context.Background())

	// Create the order
	order := Order{
		UserID:    req.UserID,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Calculate total price and validate products
	var totalPrice float64 = 0
	productErrs := make([]string, 0)
	processedItems := make([]OrderItem, 0)

	for _, item := range req.Items {
		// Get product from Product Service
		productResp, err := http.Get(fmt.Sprintf("%s/products/%d", PRODUCT_SERVICE_URL, item.ProductID))
		if err != nil {
			productErrs = append(productErrs, fmt.Sprintf("Unable to contact Product Service for product %d", item.ProductID))
			continue
		}

		if productResp.StatusCode != http.StatusOK {
			productErrs = append(productErrs, fmt.Sprintf("Product with ID %d not found", item.ProductID))
			productResp.Body.Close()
			continue
		}

		productBody, err := ioutil.ReadAll(productResp.Body)
		productResp.Body.Close()
		if err != nil {
			productErrs = append(productErrs, fmt.Sprintf("Error reading product data for ID %d", item.ProductID))
			continue
		}

		var product Product
		if err := json.Unmarshal(productBody, &product); err != nil {
			productErrs = append(productErrs, fmt.Sprintf("Error parsing product data for ID %d", item.ProductID))
			continue
		}

		// Check inventory
		if product.Inventory < item.Quantity {
			productErrs = append(productErrs, fmt.Sprintf("Insufficient inventory for product %s (ID: %d)", product.Name, item.ProductID))
			continue
		}

		// Add item to order
		orderItem := OrderItem{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			Price:     product.Price,
			Name:      product.Name,
		}
		processedItems = append(processedItems, orderItem)

		// Update total price
		totalPrice += product.Price * float64(item.Quantity)
	}

	// Check if we had any product errors
	if len(productErrs) > 0 {
		errorMessage := "The following errors occurred while processing your order:\n"
		for _, errMsg := range productErrs {
			errorMessage += "- " + errMsg + "\n"
		}
		respondWithError(w, http.StatusBadRequest, errorMessage)
		return
	}

	// Set the total price
	order.TotalPrice = totalPrice

	// Insert order into database
	err = tx.QueryRow(context.Background(),
		"INSERT INTO orders (user_id, total_price, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		order.UserID, order.TotalPrice, order.Status, order.CreatedAt, order.UpdatedAt).Scan(&order.ID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Insert order items
	for _, item := range processedItems {

		_, err := tx.Exec(context.Background(),
			"INSERT INTO order_items (order_id, product_id, quantity, price) VALUES ($1, $2, $3, $4)",
			order.ID, item.ProductID, item.Quantity, item.Price)

		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Commit transaction
	if err := tx.Commit(context.Background()); err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update inventory for each product (async via RabbitMQ)
	for _, item := range processedItems {
		inventoryUpdate := InventoryUpdate{
			ProductID:  item.ProductID,
			Quantity:   item.Quantity,
			IsIncrease: false, // Decrease inventory
		}

		inventoryUpdateJSON, _ := json.Marshal(inventoryUpdate)
		log.Printf("PUBLISHING TO RABBITMQ: inventory_updates queue with payload: %s", string(inventoryUpdateJSON))

		err = a.RabbitCh.Publish(
			"",                      // exchange
			INVENTORY_UPDATES_QUEUE, // routing key
			false,                   // mandatory
			false,                   // immediate
			amqp.Publishing{
				ContentType: "application/json",
				Body:        inventoryUpdateJSON,
			})

		if err != nil {
			log.Printf("ERROR PUBLISHING TO RABBITMQ: %v", err)
		} else {
			log.Printf("Successfully published inventory update for product %d", item.ProductID)
		}
	}

	// Send order history to User Service (async via RabbitMQ)
	orderHistory := OrderHistory{
		UserID:    order.UserID,
		OrderID:   order.ID,
		Total:     order.TotalPrice,
		Status:    order.Status,
		CreatedAt: order.CreatedAt,
	}

	orderHistoryJSON, _ := json.Marshal(orderHistory)
	log.Printf("PUBLISHING TO RABBITMQ: order_updates queue with payload: %s", string(orderHistoryJSON))

	err = a.RabbitCh.Publish(
		"",                  // exchange
		ORDER_UPDATES_QUEUE, // routing key
		false,               // mandatory
		false,               // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        orderHistoryJSON,
		})

	if err != nil {
		log.Printf("ERROR PUBLISHING ORDER HISTORY TO RABBITMQ: %v", err)
	} else {
		log.Printf("Successfully published order history for user %d, order %d", order.UserID, order.ID)
	}

	// Get the complete order with items
	order.Items = processedItems
	respondWithJSON(w, http.StatusCreated, order)
}

func (a *App) testRabbitMQConnection(w http.ResponseWriter, r *http.Request) {
	log.Println("Testing RabbitMQ connection...")

	// Test message
	testMessage := map[string]string{
		"message": "This is a test message",
		"time":    time.Now().String(),
	}

	messageJSON, _ := json.Marshal(testMessage)

	// Try publishing to inventory updates queue
	err := a.RabbitCh.Publish(
		"",                      // exchange
		INVENTORY_UPDATES_QUEUE, // routing key
		false,                   // mandatory
		false,                   // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        messageJSON,
		})

	if err != nil {
		log.Printf("RABBITMQ TEST ERROR (inventory): %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to publish test message to inventory queue: "+err.Error())
		return
	}

	// Try publishing to order updates queue
	err = a.RabbitCh.Publish(
		"",                  // exchange
		ORDER_UPDATES_QUEUE, // routing key
		false,               // mandatory
		false,               // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        messageJSON,
		})

	if err != nil {
		log.Printf("RABBITMQ TEST ERROR (orders): %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to publish test message to orders queue: "+err.Error())
		return
	}

	log.Println("Successfully published test messages to both queues!")
	respondWithJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Test messages published to RabbitMQ queues",
	})
}

// updateOrderStatus updates the status of an order
func (a *App) updateOrderStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var statusUpdate struct {
		Status string `json:"status"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&statusUpdate); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Validate status
	validStatuses := map[string]bool{
		"pending":    true,
		"processing": true,
		"shipped":    true,
		"delivered":  true,
		"cancelled":  true,
	}

	if !validStatuses[statusUpdate.Status] {
		respondWithError(w, http.StatusBadRequest, "Invalid status value")
		return
	}

	// Update order status
	_, err := a.DB.Exec(context.Background(),
		"UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2",
		statusUpdate.Status, id)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get updated order
	var order Order
	err = a.DB.QueryRow(context.Background(),
		"SELECT id, user_id, total_price, status, created_at, updated_at FROM orders WHERE id = $1",
		id).Scan(&order.ID, &order.UserID, &order.TotalPrice, &order.Status, &order.CreatedAt, &order.UpdatedAt)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get order items
	items, err := a.getOrderItems(order.ID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	order.Items = items

	// If order was cancelled, return inventory (async via RabbitMQ)
	if statusUpdate.Status == "cancelled" {
		for _, item := range items {
			inventoryUpdate := InventoryUpdate{
				ProductID:  item.ProductID,
				Quantity:   item.Quantity,
				IsIncrease: true, // Increase inventory (return items)
			}

			inventoryUpdateJSON, _ := json.Marshal(inventoryUpdate)
			err = a.RabbitCh.Publish(
				"",                      // exchange
				INVENTORY_UPDATES_QUEUE, // routing key
				false,                   // mandatory
				false,                   // immediate
				amqp.Publishing{
					ContentType: "application/json",
					Body:        inventoryUpdateJSON,
				})

			if err != nil {
				log.Printf("Error publishing inventory update: %v", err)
			}
		}
	}

	// Send updated order history to User Service (async via RabbitMQ)
	orderHistory := OrderHistory{
		UserID:    order.UserID,
		OrderID:   order.ID,
		Total:     order.TotalPrice,
		Status:    order.Status,
		CreatedAt: time.Now(), // Use current time for the update
	}

	orderHistoryJSON, _ := json.Marshal(orderHistory)
	err = a.RabbitCh.Publish(
		"",                  // exchange
		ORDER_UPDATES_QUEUE, // routing key
		false,               // mandatory
		false,               // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        orderHistoryJSON,
		})

	if err != nil {
		log.Printf("Error publishing order history: %v", err)
	}

	respondWithJSON(w, http.StatusOK, order)
}

// getSalesAnalytics returns sales analytics data (with AI-based insights)
func (a *App) getSalesAnalytics(w http.ResponseWriter, r *http.Request) {
	// Get sales data by day for the last 30 days
	rows, err := a.DB.Query(context.Background(), `
		SELECT 
			DATE(created_at) as order_date,
			COUNT(*) as order_count,
			SUM(total_price) as total_sales
		FROM 
			orders
		WHERE 
			created_at >= NOW() - INTERVAL '30 days'
		GROUP BY 
			DATE(created_at)
		ORDER BY 
			order_date
	`)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type DailySales struct {
		Date       string  `json:"date"`
		OrderCount int     `json:"order_count"`
		TotalSales float64 `json:"total_sales"`
	}

	salesData := []DailySales{}
	for rows.Next() {
		var s DailySales
		var orderDate time.Time
		if err := rows.Scan(&orderDate, &s.OrderCount, &s.TotalSales); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.Date = orderDate.Format("2006-01-02")
		salesData = append(salesData, s)
	}

	// Get top 5 selling products
	rows, err = a.DB.Query(context.Background(), `
		SELECT 
			oi.product_id,
			SUM(oi.quantity) as total_quantity,
			SUM(oi.quantity * oi.price) as total_sales
		FROM 
			order_items oi
		JOIN 
			orders o ON oi.order_id = o.id
		WHERE 
			o.created_at >= NOW() - INTERVAL '30 days'
		GROUP BY 
			oi.product_id
		ORDER BY 
			total_quantity DESC
		LIMIT 5
	`)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type TopProduct struct {
		ProductID     int     `json:"product_id"`
		Name          string  `json:"name,omitempty"`
		TotalQuantity int     `json:"total_quantity"`
		TotalSales    float64 `json:"total_sales"`
	}

	topProducts := []TopProduct{}
	for rows.Next() {
		var p TopProduct
		if err := rows.Scan(&p.ProductID, &p.TotalQuantity, &p.TotalSales); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get product name from Product Service
		productResp, err := http.Get(fmt.Sprintf("%s/products/%d", PRODUCT_SERVICE_URL, p.ProductID))
		if err == nil && productResp.StatusCode == http.StatusOK {
			defer productResp.Body.Close()
			productBody, _ := ioutil.ReadAll(productResp.Body)
			var product Product
			if json.Unmarshal(productBody, &product) == nil {
				p.Name = product.Name
			}
		}

		topProducts = append(topProducts, p)
	}

	// Generate AI insights (simulated - in a real system, this would use ML)
	// Calculate simple statistics for demonstration purposes
	var totalSales float64 = 0
	var totalOrders int = 0
	for _, s := range salesData {
		totalSales += s.TotalSales
		totalOrders += s.OrderCount
	}

	averageOrderValue := 0.0
	if totalOrders > 0 {
		averageOrderValue = totalSales / float64(totalOrders)
	}

	// Simulate trend detection
	salesTrend := "stable"
	if len(salesData) > 7 {
		recentSales := 0.0
		olderSales := 0.0

		for i, s := range salesData {
			if i >= len(salesData)-7 {
				recentSales += s.TotalSales
			} else if i >= len(salesData)-14 && i < len(salesData)-7 {
				olderSales += s.TotalSales
			}
		}

		if recentSales > olderSales*1.1 {
			salesTrend = "increasing"
		} else if recentSales < olderSales*0.9 {
			salesTrend = "decreasing"
		}
	}

	// Prepare the response
	response := struct {
		DailySales        []DailySales `json:"daily_sales"`
		TopProducts       []TopProduct `json:"top_products"`
		TotalSales        float64      `json:"total_sales"`
		AverageOrderValue float64      `json:"average_order_value"`
		SalesTrend        string       `json:"sales_trend"`
		AIInsights        []string     `json:"ai_insights"`
	}{
		DailySales:        salesData,
		TopProducts:       topProducts,
		TotalSales:        totalSales,
		AverageOrderValue: averageOrderValue,
		SalesTrend:        salesTrend,
		AIInsights:        []string{},
	}

	// Generate some example AI insights
	if len(topProducts) > 0 {
		response.AIInsights = append(response.AIInsights,
			fmt.Sprintf("Top selling product is %s with %d units sold in the last 30 days.",
				topProducts[0].Name, topProducts[0].TotalQuantity))
	}

	response.AIInsights = append(response.AIInsights,
		fmt.Sprintf("The average order value is $%.2f, which is %s average.",
			averageOrderValue,
			func() string {
				if averageOrderValue > 100 {
					return "above"
				}
				return "below"
			}()))

	response.AIInsights = append(response.AIInsights,
		fmt.Sprintf("Sales are currently %s compared to the previous week.",
			salesTrend))

	respondWithJSON(w, http.StatusOK, response)
}

// Helper function to parse ID from string to int
func parseInt(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}

// respondWithError responds with an error message
func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

// respondWithJSON responds with a JSON payload
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func main() {
	a := App{}
	if err := a.Initialize(); err != nil {
		log.Fatal(err)
	}
	a.Run()
}
