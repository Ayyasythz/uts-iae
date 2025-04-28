package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Price       float64    `json:"price"`
	Inventory   int        `json:"inventory"`
	Images      []Image    `json:"images,omitempty"`
	Reviews     []Review   `json:"reviews,omitempty"`
	Categories  []Category `json:"categories,omitempty"`
	AvgRating   float64    `json:"avg_rating,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Review struct {
	ID         int       `json:"id"`
	ProductID  int       `json:"product_id"`
	UserID     int       `json:"user_id"`
	Username   string    `json:"username,omitempty"`
	Rating     int       `json:"rating"`
	ReviewText string    `json:"review_text"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Image struct {
	ID           int       `json:"id"`
	ProductID    int       `json:"product_id"`
	ImageURL     string    `json:"image_url"`
	IsPrimary    bool      `json:"is_primary"`
	DisplayOrder int       `json:"display_order"`
	CreatedAt    time.Time `json:"created_at"`
}

type Category struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	ParentID      *int       `json:"parent_id"`
	ImageURL      string     `json:"image_url,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	SubCategories []Category `json:"sub_categories,omitempty"`
	Products      []Product  `json:"products,omitempty"`
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

	a.Router.HandleFunc("/products/{id:[0-9]+}/images", a.getProductImages).Methods("GET")
	a.Router.HandleFunc("/products/{id:[0-9]+}/images", a.addProductImage).Methods("POST")
	a.Router.HandleFunc("/products/{id:[0-9]+}/images/{image_id:[0-9]+}", a.updateProductImage).Methods("PUT")
	a.Router.HandleFunc("/products/{id:[0-9]+}/images/{image_id:[0-9]+}", a.deleteProductImage).Methods("DELETE")

	a.Router.HandleFunc("/products/{id:[0-9]+}/reviews", a.getProductReviews).Methods("GET")
	a.Router.HandleFunc("/products/{id:[0-9]+}/reviews", a.addProductReview).Methods("POST")
	a.Router.HandleFunc("/products/{id:[0-9]+}/reviews/{review_id:[0-9]+}", a.updateProductReview).Methods("PUT")
	a.Router.HandleFunc("/products/{id:[0-9]+}/reviews/{review_id:[0-9]+}", a.deleteProductReview).Methods("DELETE")

	a.Router.HandleFunc("/categories", a.getCategories).Methods("GET")
	a.Router.HandleFunc("/categories/{id:[0-9]+}", a.getCategory).Methods("GET")
	a.Router.HandleFunc("/categories/{id:[0-9]+}/products", a.getCategoryProducts).Methods("GET")
	a.Router.HandleFunc("/categories", a.createCategory).Methods("POST")
	a.Router.HandleFunc("/categories/{id:[0-9]+}", a.updateCategory).Methods("PUT")
	a.Router.HandleFunc("/categories/{id:[0-9]+}", a.deleteCategory).Methods("DELETE")

	a.Router.HandleFunc("/products/search", a.searchProducts).Methods("GET")
	a.Router.HandleFunc("/products/top-rated", a.getTopRatedProducts).Methods("GET")

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

// getProduct returns a specific product with all details
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

	// Get product images
	rows, err := a.DB.Query(context.Background(),
		"SELECT id, product_id, image_url, is_primary, display_order, created_at FROM product_images WHERE product_id = $1 ORDER BY display_order",
		id)
	if err == nil {
		defer rows.Close()
		p.Images = []Image{}

		for rows.Next() {
			var img Image
			if err := rows.Scan(&img.ID, &img.ProductID, &img.ImageURL, &img.IsPrimary, &img.DisplayOrder, &img.CreatedAt); err != nil {
				log.Printf("Error scanning image: %v", err)
				continue
			}
			p.Images = append(p.Images, img)
		}
	}

	// Get product categories
	rows, err = a.DB.Query(context.Background(),
		`SELECT pc.id, pc.name, pc.description, pc.parent_id, pc.image_url, pc.created_at, pc.updated_at
         FROM product_categories pc
         JOIN product_category_map pcm ON pc.id = pcm.category_id
         WHERE pcm.product_id = $1`,
		id)
	if err == nil {
		defer rows.Close()
		p.Categories = []Category{}

		for rows.Next() {
			var cat Category
			if err := rows.Scan(&cat.ID, &cat.Name, &cat.Description, &cat.ParentID, &cat.ImageURL, &cat.CreatedAt, &cat.UpdatedAt); err != nil {
				log.Printf("Error scanning category: %v", err)
				continue
			}
			p.Categories = append(p.Categories, cat)
		}
	}

	// Get product reviews summary
	var reviewCount int
	err = a.DB.QueryRow(context.Background(),
		"SELECT COALESCE(AVG(rating), 0), COUNT(*) FROM product_reviews WHERE product_id = $1",
		id).Scan(&p.AvgRating, &reviewCount)
	if err != nil {
		p.AvgRating = 0
	}

	// Get a few recent reviews (limit to 3)
	rows, err = a.DB.Query(context.Background(),
		"SELECT id, user_id, rating, review_text, created_at, updated_at FROM product_reviews WHERE product_id = $1 ORDER BY created_at DESC LIMIT 3",
		id)
	if err == nil {
		defer rows.Close()
		p.Reviews = []Review{}

		for rows.Next() {
			var review Review
			if err := rows.Scan(&review.ID, &review.UserID, &review.Rating, &review.ReviewText, &review.CreatedAt, &review.UpdatedAt); err != nil {
				log.Printf("Error scanning review: %v", err)
				continue
			}

			review.ProductID = p.ID

			// Get username from User Service
			userResp, err := http.Get(fmt.Sprintf("%s/users/%d", USER_SERVICE_URL, review.UserID))
			if err == nil && userResp.StatusCode == http.StatusOK {
				var user struct {
					Username string `json:"username"`
				}
				body, _ := ioutil.ReadAll(userResp.Body)
				userResp.Body.Close()

				if json.Unmarshal(body, &user) == nil {
					review.Username = user.Username
				}
			}

			p.Reviews = append(p.Reviews, review)
		}
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

func (a *App) getProductImages(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID := vars["id"]

	// Verify product exists
	var exists bool
	err := a.DB.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)", productID).Scan(&exists)
	if err != nil || !exists {
		respondWithError(w, http.StatusNotFound, "Product not found")
		return
	}

	rows, err := a.DB.Query(context.Background(),
		"SELECT id, product_id, image_url, is_primary, display_order, created_at FROM product_images WHERE product_id = $1 ORDER BY display_order",
		productID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	images := []Image{}
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.ProductID, &img.ImageURL, &img.IsPrimary, &img.DisplayOrder, &img.CreatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		images = append(images, img)
	}

	respondWithJSON(w, http.StatusOK, images)
}

// addProductImage adds an image to a product
func (a *App) addProductImage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid product ID")
		return
	}

	// Verify product exists
	var exists bool
	err = a.DB.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)", productID).Scan(&exists)
	if err != nil || !exists {
		respondWithError(w, http.StatusNotFound, "Product not found")
		return
	}

	var img Image
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&img); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	img.ProductID = productID
	img.CreatedAt = time.Now()

	// If this is set as primary, update all other images to be non-primary
	if img.IsPrimary {
		_, err = a.DB.Exec(context.Background(),
			"UPDATE product_images SET is_primary = false WHERE product_id = $1",
			productID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Get the highest display order and increment
	var maxOrder int
	err = a.DB.QueryRow(context.Background(),
		"SELECT COALESCE(MAX(display_order), 0) FROM product_images WHERE product_id = $1",
		productID).Scan(&maxOrder)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	img.DisplayOrder = maxOrder + 1

	err = a.DB.QueryRow(context.Background(),
		"INSERT INTO product_images (product_id, image_url, is_primary, display_order, created_at) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		img.ProductID, img.ImageURL, img.IsPrimary, img.DisplayOrder, img.CreatedAt).Scan(&img.ID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusCreated, img)
}

// updateProductImage updates an existing product image
func (a *App) updateProductImage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid product ID")
		return
	}

	imageID, err := strconv.Atoi(vars["image_id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid image ID")
		return
	}

	var img Image
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&img); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Verify image exists and belongs to product
	var exists bool
	err = a.DB.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM product_images WHERE id = $1 AND product_id = $2)",
		imageID, productID).Scan(&exists)
	if err != nil || !exists {
		respondWithError(w, http.StatusNotFound, "Image not found or doesn't belong to this product")
		return
	}

	// If setting as primary, update all others
	if img.IsPrimary {
		_, err = a.DB.Exec(context.Background(),
			"UPDATE product_images SET is_primary = false WHERE product_id = $1 AND id != $2",
			productID, imageID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	_, err = a.DB.Exec(context.Background(),
		"UPDATE product_images SET image_url = $1, is_primary = $2, display_order = $3 WHERE id = $4 AND product_id = $5",
		img.ImageURL, img.IsPrimary, img.DisplayOrder, imageID, productID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get updated image
	err = a.DB.QueryRow(context.Background(),
		"SELECT id, product_id, image_url, is_primary, display_order, created_at FROM product_images WHERE id = $1",
		imageID).Scan(&img.ID, &img.ProductID, &img.ImageURL, &img.IsPrimary, &img.DisplayOrder, &img.CreatedAt)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, img)
}

// deleteProductImage removes an image from a product
func (a *App) deleteProductImage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid product ID")
		return
	}

	imageID, err := strconv.Atoi(vars["image_id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid image ID")
		return
	}

	// Check if the image is primary
	var isPrimary bool
	err = a.DB.QueryRow(context.Background(),
		"SELECT is_primary FROM product_images WHERE id = $1 AND product_id = $2",
		imageID, productID).Scan(&isPrimary)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "Image not found")
		return
	}

	// Delete the image
	_, err = a.DB.Exec(context.Background(),
		"DELETE FROM product_images WHERE id = $1 AND product_id = $2",
		imageID, productID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// If it was primary, set another image as primary
	if isPrimary {
		_, err = a.DB.Exec(context.Background(),
			"UPDATE product_images SET is_primary = true WHERE product_id = $1 ORDER BY display_order LIMIT 1",
			productID)
		if err != nil {
			log.Printf("Error setting new primary image: %v", err)
		}
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"result": "success"})
}

func (a *App) getProductReviews(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID := vars["id"]

	// Verify product exists
	var exists bool
	err := a.DB.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)", productID).Scan(&exists)
	if err != nil || !exists {
		respondWithError(w, http.StatusNotFound, "Product not found")
		return
	}

	rows, err := a.DB.Query(context.Background(),
		"SELECT id, product_id, user_id, rating, review_text, created_at, updated_at FROM product_reviews WHERE product_id = $1 ORDER BY created_at DESC",
		productID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	reviews := []Review{}
	for rows.Next() {
		var review Review
		if err := rows.Scan(&review.ID, &review.ProductID, &review.UserID, &review.Rating, &review.ReviewText, &review.CreatedAt, &review.UpdatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get username from User Service
		userResp, err := http.Get(fmt.Sprintf("%s/users/%d", USER_SERVICE_URL, review.UserID))
		if err == nil && userResp.StatusCode == http.StatusOK {
			var user struct {
				Username string `json:"username"`
			}
			body, _ := ioutil.ReadAll(userResp.Body)
			userResp.Body.Close()

			if json.Unmarshal(body, &user) == nil {
				review.Username = user.Username
			}
		}

		reviews = append(reviews, review)
	}

	respondWithJSON(w, http.StatusOK, reviews)
}

// addProductReview adds a review for a product
func (a *App) addProductReview(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid product ID")
		return
	}

	// Verify product exists
	var exists bool
	err = a.DB.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)", productID).Scan(&exists)
	if err != nil || !exists {
		respondWithError(w, http.StatusNotFound, "Product not found")
		return
	}

	var review Review
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&review); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	if review.Rating < 1 || review.Rating > 5 {
		respondWithError(w, http.StatusBadRequest, "Rating must be between 1 and 5")
		return
	}

	// Verify user exists
	userResp, err := http.Get(fmt.Sprintf("%s/users/%d", USER_SERVICE_URL, review.UserID))
	if err != nil || userResp.StatusCode != http.StatusOK {
		respondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}
	userResp.Body.Close()

	// Check if user has already reviewed this product
	var existingReviewID int
	err = a.DB.QueryRow(context.Background(),
		"SELECT id FROM product_reviews WHERE product_id = $1 AND user_id = $2",
		productID, review.UserID).Scan(&existingReviewID)

	if err == nil {
		respondWithError(w, http.StatusConflict, "User has already reviewed this product")
		return
	}

	review.ProductID = productID
	review.CreatedAt = time.Now()
	review.UpdatedAt = time.Now()

	err = a.DB.QueryRow(context.Background(),
		"INSERT INTO product_reviews (product_id, user_id, rating, review_text, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
		review.ProductID, review.UserID, review.Rating, review.ReviewText, review.CreatedAt, review.UpdatedAt).Scan(&review.ID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update average rating for the product
	_, err = a.DB.Exec(context.Background(),
		"UPDATE products SET updated_at = NOW() WHERE id = $1",
		productID)
	if err != nil {
		log.Printf("Error updating product timestamp: %v", err)
	}

	respondWithJSON(w, http.StatusCreated, review)
}

// updateProductReview updates an existing product review
func (a *App) updateProductReview(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid product ID")
		return
	}

	reviewID, err := strconv.Atoi(vars["review_id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid review ID")
		return
	}

	var review Review
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&review); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	if review.Rating < 1 || review.Rating > 5 {
		respondWithError(w, http.StatusBadRequest, "Rating must be between 1 and 5")
		return
	}

	// Verify review exists and belongs to the specified product
	var userID int
	err = a.DB.QueryRow(context.Background(),
		"SELECT user_id FROM product_reviews WHERE id = $1 AND product_id = $2",
		reviewID, productID).Scan(&userID)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "Review not found")
		return
	}

	// Verify user is the owner of the review
	if userID != review.UserID {
		respondWithError(w, http.StatusForbidden, "You can only update your own reviews")
		return
	}

	review.UpdatedAt = time.Now()

	_, err = a.DB.Exec(context.Background(),
		"UPDATE product_reviews SET rating = $1, review_text = $2, updated_at = $3 WHERE id = $4 AND product_id = $5",
		review.Rating, review.ReviewText, review.UpdatedAt, reviewID, productID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get updated review
	err = a.DB.QueryRow(context.Background(),
		"SELECT id, product_id, user_id, rating, review_text, created_at, updated_at FROM product_reviews WHERE id = $1",
		reviewID).Scan(&review.ID, &review.ProductID, &review.UserID, &review.Rating, &review.ReviewText, &review.CreatedAt, &review.UpdatedAt)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, review)
}

// deleteProductReview removes a review from a product
func (a *App) deleteProductReview(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid product ID")
		return
	}

	reviewID, err := strconv.Atoi(vars["review_id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid review ID")
		return
	}

	// Delete the review
	_, err = a.DB.Exec(context.Background(),
		"DELETE FROM product_reviews WHERE id = $1 AND product_id = $2",
		reviewID, productID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"result": "success"})
}

func (a *App) getCategories(w http.ResponseWriter, r *http.Request) {
	// Get only top-level categories (parent_id is NULL)
	rows, err := a.DB.Query(context.Background(),
		"SELECT id, name, description, parent_id, image_url, created_at, updated_at FROM product_categories WHERE parent_id IS NULL ORDER BY name")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	categories := []Category{}
	for rows.Next() {
		var cat Category
		if err := rows.Scan(&cat.ID, &cat.Name, &cat.Description, &cat.ParentID, &cat.ImageURL, &cat.CreatedAt, &cat.UpdatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get subcategories for this category
		cat.SubCategories = a.getSubcategories(cat.ID)

		categories = append(categories, cat)
	}

	respondWithJSON(w, http.StatusOK, categories)
}

// getSubcategories fetches subcategories for a given parent category
func (a *App) getSubcategories(parentID int) []Category {
	rows, err := a.DB.Query(context.Background(),
		"SELECT id, name, description, parent_id, image_url, created_at, updated_at FROM product_categories WHERE parent_id = $1 ORDER BY name",
		parentID)
	if err != nil {
		log.Printf("Error fetching subcategories: %v", err)
		return []Category{}
	}
	defer rows.Close()

	subcategories := []Category{}
	for rows.Next() {
		var cat Category
		if err := rows.Scan(&cat.ID, &cat.Name, &cat.Description, &cat.ParentID, &cat.ImageURL, &cat.CreatedAt, &cat.UpdatedAt); err != nil {
			log.Printf("Error scanning subcategory: %v", err)
			continue
		}

		// Recursively get subcategories
		cat.SubCategories = a.getSubcategories(cat.ID)

		subcategories = append(subcategories, cat)
	}

	return subcategories
}

// getCategory returns a specific category with its products
func (a *App) getCategory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	categoryID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid category ID")
		return
	}

	var cat Category
	err = a.DB.QueryRow(context.Background(),
		"SELECT id, name, description, parent_id, image_url, created_at, updated_at FROM product_categories WHERE id = $1",
		categoryID).Scan(&cat.ID, &cat.Name, &cat.Description, &cat.ParentID, &cat.ImageURL, &cat.CreatedAt, &cat.UpdatedAt)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "Category not found")
		return
	}

	// Get subcategories
	cat.SubCategories = a.getSubcategories(cat.ID)

	// Get products in this category
	rows, err := a.DB.Query(context.Background(),
		`SELECT p.id, p.name, p.description, p.price, p.inventory, p.created_at, p.updated_at
         FROM products p
         JOIN product_category_map pcm ON p.id = pcm.product_id
         WHERE pcm.category_id = $1
         ORDER BY p.name`,
		categoryID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	cat.Products = []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Inventory, &p.CreatedAt, &p.UpdatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get primary image
		var imageURL string
		err = a.DB.QueryRow(context.Background(),
			"SELECT image_url FROM product_images WHERE product_id = $1 AND is_primary = true LIMIT 1",
			p.ID).Scan(&imageURL)
		if err == nil {
			p.Images = []Image{{ImageURL: imageURL, IsPrimary: true}}
		}

		// Get average rating
		err = a.DB.QueryRow(context.Background(),
			"SELECT COALESCE(AVG(rating), 0) FROM product_reviews WHERE product_id = $1",
			p.ID).Scan(&p.AvgRating)
		if err != nil {
			p.AvgRating = 0
		}

		cat.Products = append(cat.Products, p)
	}

	respondWithJSON(w, http.StatusOK, cat)
}

// getCategoryProducts returns products in a specific category
func (a *App) getCategoryProducts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	categoryID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid category ID")
		return
	}

	// Include products from subcategories
	includeSubcategories := r.URL.Query().Get("include_subcategories") == "true"

	// Verify category exists
	var exists bool
	err = a.DB.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM product_categories WHERE id = $1)", categoryID).Scan(&exists)
	if err != nil || !exists {
		respondWithError(w, http.StatusNotFound, "Category not found")
		return
	}

	var rows pgx.Rows
	if includeSubcategories {
		// Get all subcategory IDs recursively
		categoryIDs := a.getAllSubcategoryIDs(categoryID)
		categoryIDs = append(categoryIDs, categoryID) // Include the parent category

		// Convert to string for SQL IN clause
		var categoryIDsList string
		for i, id := range categoryIDs {
			if i > 0 {
				categoryIDsList += ","
			}
			categoryIDsList += fmt.Sprintf("%d", id)
		}

		// Query products in all categories
		rows, err = a.DB.Query(context.Background(),
			fmt.Sprintf(`SELECT DISTINCT p.id, p.name, p.description, p.price, p.inventory, p.created_at, p.updated_at
                FROM products p
                JOIN product_category_map pcm ON p.id = pcm.product_id
                WHERE pcm.category_id IN (%s)
                ORDER BY p.name`, categoryIDsList))
	} else {
		// Query products only in this category
		rows, err = a.DB.Query(context.Background(),
			`SELECT p.id, p.name, p.description, p.price, p.inventory, p.created_at, p.updated_at
             FROM products p
             JOIN product_category_map pcm ON p.id = pcm.product_id
             WHERE pcm.category_id = $1
             ORDER BY p.name`,
			categoryID)
	}

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Inventory, &p.CreatedAt, &p.UpdatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get primary image
		var imageURL string
		err = a.DB.QueryRow(context.Background(),
			"SELECT image_url FROM product_images WHERE product_id = $1 AND is_primary = true LIMIT 1",
			p.ID).Scan(&imageURL)
		if err == nil {
			p.Images = []Image{{ImageURL: imageURL, IsPrimary: true}}
		}

		// Get average rating
		err = a.DB.QueryRow(context.Background(),
			"SELECT COALESCE(AVG(rating), 0) FROM product_reviews WHERE product_id = $1",
			p.ID).Scan(&p.AvgRating)
		if err != nil {
			p.AvgRating = 0
		}

		products = append(products, p)
	}

	respondWithJSON(w, http.StatusOK, products)
}

// getAllSubcategoryIDs recursively fetches all subcategory IDs
func (a *App) getAllSubcategoryIDs(categoryID int) []int {
	rows, err := a.DB.Query(context.Background(),
		"SELECT id FROM product_categories WHERE parent_id = $1",
		categoryID)
	if err != nil {
		log.Printf("Error fetching subcategory IDs: %v", err)
		return []int{}
	}
	defer rows.Close()

	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			log.Printf("Error scanning subcategory ID: %v", err)
			continue
		}

		// Add this ID
		ids = append(ids, id)

		// Recursively add children
		childIDs := a.getAllSubcategoryIDs(id)
		ids = append(ids, childIDs...)
	}

	return ids
}

// createCategory adds a new category
func (a *App) createCategory(w http.ResponseWriter, r *http.Request) {
	var cat Category
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&cat); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Validate parent category if provided
	if cat.ParentID != nil {
		var exists bool
		err := a.DB.QueryRow(context.Background(),
			"SELECT EXISTS(SELECT 1 FROM product_categories WHERE id = $1)",
			*cat.ParentID).Scan(&exists)
		if err != nil || !exists {
			respondWithError(w, http.StatusBadRequest, "Parent category not found")
			return
		}
	}

	cat.CreatedAt = time.Now()
	cat.UpdatedAt = time.Now()

	err := a.DB.QueryRow(context.Background(),
		"INSERT INTO product_categories (name, description, parent_id, image_url, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
		cat.Name, cat.Description, cat.ParentID, cat.ImageURL, cat.CreatedAt, cat.UpdatedAt).Scan(&cat.ID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusCreated, cat)
}

// updateCategory updates an existing category
func (a *App) updateCategory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	categoryID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid category ID")
		return
	}

	var cat Category
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&cat); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Prevent circular references - make sure parent is not a subcategory of this category
	if cat.ParentID != nil {
		if *cat.ParentID == categoryID {
			respondWithError(w, http.StatusBadRequest, "Category cannot be its own parent")
			return
		}

		subcatIDs := a.getAllSubcategoryIDs(categoryID)
		for _, id := range subcatIDs {
			if id == *cat.ParentID {
				respondWithError(w, http.StatusBadRequest, "Parent cannot be a subcategory of this category")
				return
			}
		}
	}

	cat.UpdatedAt = time.Now()

	_, err = a.DB.Exec(context.Background(),
		"UPDATE product_categories SET name = $1, description = $2, parent_id = $3, image_url = $4, updated_at = $5 WHERE id = $6",
		cat.Name, cat.Description, cat.ParentID, cat.ImageURL, cat.UpdatedAt, categoryID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cat.ID = categoryID
	respondWithJSON(w, http.StatusOK, cat)
}

// deleteCategory removes a category
func (a *App) deleteCategory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	categoryID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid category ID")
		return
	}

	// Check if category has subcategories
	var hasSubcategories bool
	err = a.DB.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM product_categories WHERE parent_id = $1)",
		categoryID).Scan(&hasSubcategories)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if hasSubcategories {
		respondWithError(w, http.StatusConflict, "Cannot delete category with subcategories")
		return
	}

	// Remove category mappings first
	_, err = a.DB.Exec(context.Background(),
		"DELETE FROM product_category_map WHERE category_id = $1",
		categoryID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Delete the category
	_, err = a.DB.Exec(context.Background(),
		"DELETE FROM product_categories WHERE id = $1",
		categoryID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"result": "success"})
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

func (a *App) searchProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Get query parameters
	searchTerm := q.Get("q")
	categoryID := q.Get("category")
	minPrice := q.Get("min_price")
	maxPrice := q.Get("max_price")
	minRating := q.Get("min_rating")
	sortBy := q.Get("sort")

	// Build query
	query := `
        SELECT DISTINCT p.id, p.name, p.description, p.price, p.inventory, p.created_at, p.updated_at,
            COALESCE(AVG(pr.rating), 0) as avg_rating
        FROM products p
        LEFT JOIN product_reviews pr ON p.id = pr.product_id
    `

	// Add category filter if provided
	if categoryID != "" {
		query += `
            JOIN product_category_map pcm ON p.id = pcm.product_id
            WHERE pcm.category_id = ` + categoryID
	} else {
		query += " WHERE 1=1"
	}

	// Add search term filter
	if searchTerm != "" {
		query += fmt.Sprintf(" AND (p.name ILIKE '%%%s%%' OR p.description ILIKE '%%%s%%')",
			searchTerm, searchTerm)
	}

	// Add price filters
	if minPrice != "" {
		query += " AND p.price >= " + minPrice
	}

	if maxPrice != "" {
		query += " AND p.price <= " + maxPrice
	}

	// Group by for aggregations
	query += " GROUP BY p.id"

	// Add rating filter (applies after grouping)
	if minRating != "" {
		query += " HAVING COALESCE(AVG(pr.rating), 0) >= " + minRating
	}

	// Add sorting
	switch sortBy {
	case "price_asc":
		query += " ORDER BY p.price ASC"
	case "price_desc":
		query += " ORDER BY p.price DESC"
	case "rating_desc":
		query += " ORDER BY avg_rating DESC"
	case "newest":
		query += " ORDER BY p.created_at DESC"
	default:
		query += " ORDER BY p.name ASC"
	}

	// Execute query
	rows, err := a.DB.Query(context.Background(), query)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Inventory, &p.CreatedAt, &p.UpdatedAt, &p.AvgRating); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get primary image
		var imageURL string
		err = a.DB.QueryRow(context.Background(),
			"SELECT image_url FROM product_images WHERE product_id = $1 AND is_primary = true LIMIT 1",
			p.ID).Scan(&imageURL)
		if err == nil {
			p.Images = []Image{{ImageURL: imageURL, IsPrimary: true}}
		}

		products = append(products, p)
	}

	respondWithJSON(w, http.StatusOK, products)
}

// getTopRatedProducts returns the top rated products
func (a *App) getTopRatedProducts(w http.ResponseWriter, r *http.Request) {
	// Parse limit parameter (default to 10)
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		var err error
		limit, err = strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 10
		}
	}

	// Only include products with at least 3 reviews
	minReviews := 3
	minReviewsStr := r.URL.Query().Get("min_reviews")
	if minReviewsStr != "" {
		var err error
		minReviews, err = strconv.Atoi(minReviewsStr)
		if err != nil || minReviews < 1 {
			minReviews = 3
		}
	}

	// Query top rated products
	rows, err := a.DB.Query(context.Background(), `
        SELECT p.id, p.name, p.description, p.price, p.inventory, p.created_at, p.updated_at,
            AVG(pr.rating) as avg_rating, COUNT(pr.id) as review_count
        FROM products p
        JOIN product_reviews pr ON p.id = pr.product_id
        GROUP BY p.id
        HAVING COUNT(pr.id) >= $1
        ORDER BY avg_rating DESC, review_count DESC
        LIMIT $2
    `, minReviews, limit)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		var p Product
		var reviewCount int
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Inventory, &p.CreatedAt, &p.UpdatedAt, &p.AvgRating, &reviewCount); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get primary image
		var imageURL string
		err = a.DB.QueryRow(context.Background(),
			"SELECT image_url FROM product_images WHERE product_id = $1 AND is_primary = true LIMIT 1",
			p.ID).Scan(&imageURL)
		if err == nil {
			p.Images = []Image{{ImageURL: imageURL, IsPrimary: true}}
		}

		products = append(products, p)
	}

	respondWithJSON(w, http.StatusOK, products)
}

func main() {
	a := App{}
	if err := a.Initialize(); err != nil {
		log.Fatal(err)
	}
	a.Run()
}
