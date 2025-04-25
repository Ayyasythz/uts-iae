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
	PORT                    = 8082
	POSTGRES_URI            = "postgres://postgres:postgres@postgres:5432/product_service" // Changed localhost to postgres
	RABBITMQ_URI            = "amqp://guest:guest@rabbitmq:5672/"                          // Changed localhost to rabbitmq
	INVENTORY_UPDATES_QUEUE = "inventory_updates"
	USER_SERVICE_URL        = "http://user-service:8081" // Changed localhost to user-service
)

// Product represents a product in the system
type Product struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       float64   `json:"price"`
	Inventory   int       `json:"inventory"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// InventoryUpdate represents an inventory update
type InventoryUpdate struct {
	ProductID  int  `json:"product_id"`
	Quantity   int  `json:"quantity"`
	IsIncrease bool `json:"is_increase"`
}

// User represents a user from the User Service
type User struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Recommendation represents a product recommendation
type Recommendation struct {
	ProductID int     `json:"product_id"`
	Score     float64 `json:"score"`
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
	_, err = a.RabbitCh.QueueDeclare(
		INVENTORY_UPDATES_QUEUE, // name
		true,                    // durable
		false,                   // delete when unused
		false,                   // exclusive
		false,                   // no-wait
		nil,                     // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare a queue: %v", err)
	}

	// Start consuming messages
	go a.consumeInventoryUpdates()

	// Initialize router
	a.Router = mux.NewRouter()
	a.initializeRoutes()

	return nil
}

// initializeRoutes sets up the API routes
func (a *App) initializeRoutes() {
	// Health check
	a.Router.HandleFunc("/health", a.healthCheck).Methods("GET")

	// Product CRUD operations
	a.Router.HandleFunc("/products", a.getProducts).Methods("GET")
	a.Router.HandleFunc("/products/{id:[0-9]+}", a.getProduct).Methods("GET")
	a.Router.HandleFunc("/products", a.createProduct).Methods("POST")
	a.Router.HandleFunc("/products/{id:[0-9]+}", a.updateProduct).Methods("PUT")
	a.Router.HandleFunc("/products/{id:[0-9]+}", a.deleteProduct).Methods("DELETE")

	// Inventory management
	a.Router.HandleFunc("/products/{id:[0-9]+}/inventory", a.updateInventory).Methods("PATCH")

	// AI-powered recommendations
	a.Router.HandleFunc("/recommendations/user/{user_id:[0-9]+}", a.getRecommendations).Methods("GET")
}

// consumeInventoryUpdates listens for inventory updates
func (a *App) consumeInventoryUpdates() {
	msgs, err := a.RabbitCh.Consume(
		INVENTORY_UPDATES_QUEUE, // queue
		"",                      // consumer
		true,                    // auto-ack
		false,                   // exclusive
		false,                   // no-local
		false,                   // no-wait
		nil,                     // args
	)
	if err != nil {
		log.Printf("Failed to register a consumer: %v", err)
		return
	}

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			var update InventoryUpdate
			if err := json.Unmarshal(d.Body, &update); err != nil {
				log.Printf("Error parsing inventory update: %v", err)
				continue
			}

			// Update inventory in the database
			var query string
			if update.IsIncrease {
				query = "UPDATE products SET inventory = inventory + $1, updated_at = NOW() WHERE id = $2"
			} else {
				query = "UPDATE products SET inventory = inventory - $1, updated_at = NOW() WHERE id = $2"
			}

			_, err := a.DB.Exec(context.Background(), query, update.Quantity, update.ProductID)
			if err != nil {
				log.Printf("Error updating inventory: %v", err)
			} else {
				log.Printf("Updated inventory for product %d by %d (%v)",
					update.ProductID, update.Quantity, update.IsIncrease)
			}
		}
	}()

	<-forever
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
		log.Printf("Product Service listening on port %d...", PORT)
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

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

// getProducts returns all products
func (a *App) getProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(context.Background(),
		"SELECT id, name, description, price, inventory, created_at, updated_at FROM products")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Inventory,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		products = append(products, p)
	}

	respondWithJSON(w, http.StatusOK, products)
}

// getProduct returns a specific product
func (a *App) getProduct(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var p Product
	err := a.DB.QueryRow(context.Background(),
		"SELECT id, name, description, price, inventory, created_at, updated_at FROM products WHERE id = $1",
		id).Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Inventory, &p.CreatedAt, &p.UpdatedAt)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "Product not found")
		return
	}

	respondWithJSON(w, http.StatusOK, p)
}

// createProduct adds a new product
func (a *App) createProduct(w http.ResponseWriter, r *http.Request) {
	var p Product
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&p); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()

	err := a.DB.QueryRow(context.Background(),
		"INSERT INTO products (name, description, price, inventory, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
		p.Name, p.Description, p.Price, p.Inventory, p.CreatedAt, p.UpdatedAt).Scan(&p.ID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusCreated, p)
}

// updateProduct updates an existing product
func (a *App) updateProduct(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var p Product
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&p); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	p.UpdatedAt = time.Now()

	_, err := a.DB.Exec(context.Background(),
		"UPDATE products SET name = $1, description = $2, price = $3, inventory = $4, updated_at = $5 WHERE id = $6",
		p.Name, p.Description, p.Price, p.Inventory, p.UpdatedAt, id)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	p.ID = parseInt(id)
	respondWithJSON(w, http.StatusOK, p)
}

// deleteProduct removes a product
func (a *App) deleteProduct(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := a.DB.Exec(context.Background(), "DELETE FROM products WHERE id = $1", id)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"result": "success"})
}

// updateInventory updates product inventory
func (a *App) updateInventory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var update InventoryUpdate
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&update); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Set the product ID from the URL
	update.ProductID = parseInt(id)

	// Update inventory in the database
	var query string
	if update.IsIncrease {
		query = "UPDATE products SET inventory = inventory + $1, updated_at = NOW() WHERE id = $2"
	} else {
		query = "UPDATE products SET inventory = inventory - $1, updated_at = NOW() WHERE id = $2"
	}

	_, err := a.DB.Exec(context.Background(), query, update.Quantity, update.ProductID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get the updated product
	var p Product
	err = a.DB.QueryRow(context.Background(),
		"SELECT id, name, description, price, inventory, created_at, updated_at FROM products WHERE id = $1",
		id).Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Inventory, &p.CreatedAt, &p.UpdatedAt)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, p)
}

// getRecommendations returns AI-powered product recommendations for a user
func (a *App) getRecommendations(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]

	// First, validate the user exists by calling the User Service
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

	// Parse the user response
	userBody, err := ioutil.ReadAll(userResp.Body)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error reading User Service response")
		return
	}

	var user User
	if err := json.Unmarshal(userBody, &user); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error parsing User Service response")
		return
	}

	// Get the user's order history to base recommendations on
	orderHistoryResp, err := http.Get(fmt.Sprintf("%s/users/%s/orders", USER_SERVICE_URL, userID))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get user order history")
		return
	}
	defer orderHistoryResp.Body.Close()

	// For simplicity, we'll use a basic recommendation algorithm here
	// In a real system, this would be more sophisticated, potentially using AI

	// Get all products
	rows, err := a.DB.Query(context.Background(),
		"SELECT id, name, description, price, inventory, created_at, updated_at FROM products WHERE inventory > 0")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Inventory,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		products = append(products, p)
	}

	// Here's where we would normally apply machine learning or sophisticated algorithms
	// For demo purposes, we'll just recommend based on inventory level and price
	recommendations := []Recommendation{}
	for _, p := range products {
		// Basic "AI" scoring - products with more inventory and lower price get higher scores
		// In a real system, this would be based on user behavior, preferences, etc.
		score := 100.0 / p.Price * float64(p.Inventory) / 100

		recommendations = append(recommendations, Recommendation{
			ProductID: p.ID,
			Score:     score,
		})
	}

	// Sort recommendations by score (in a real system)
	// For brevity, we'll skip the sorting here

	// Return top recommendations with product details
	type RecommendationResponse struct {
		ProductID   int     `json:"product_id"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Price       float64 `json:"price"`
		Score       float64 `json:"recommendation_score"`
	}

	response := []RecommendationResponse{}
	for i, rec := range recommendations {
		if i >= 5 { // Return top 5 recommendations
			break
		}

		// Find the product
		var product Product
		for _, p := range products {
			if p.ID == rec.ProductID {
				product = p
				break
			}
		}

		response = append(response, RecommendationResponse{
			ProductID:   rec.ProductID,
			Name:        product.Name,
			Description: product.Description,
			Price:       product.Price,
			Score:       rec.Score,
		})
	}

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
