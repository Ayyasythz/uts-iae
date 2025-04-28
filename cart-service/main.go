package main

import (
    "bytes"
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
    "strconv"
    "syscall"
    "time"
)

const (
	PORT                = 8085
	POSTGRES_URI        = "postgres://postgres:postgres@postgres:5432/cart_service"
	RABBITMQ_URI        = "amqp://guest:guest@rabbitmq:5672/"
	CART_EVENTS_QUEUE   = "cart_events"
	PRODUCT_SERVICE_URL = "http://product-service:8082"
	ORDER_SERVICE_URL   = "http://order-service:8083"
	USER_SERVICE_URL    = "http://user-service:8081"
	CART_EXPIRY_DAYS    = 7
)

// Cart represents a shopping cart
type Cart struct {
	ID        int       `json:"id"`
	UserID    *int      `json:"user_id"`
	SessionID string    `json:"session_id"`
	Items     []CartItem `json:"items,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Total     float64   `json:"total,omitempty"`
}

// CartItem represents an item in a cart
type CartItem struct {
	ID        int       `json:"id"`
	CartID    int       `json:"cart_id"`
	ProductID int       `json:"product_id"`
	Name      string    `json:"name,omitempty"`
	Price     float64   `json:"price,omitempty"`
	Quantity  int       `json:"quantity"`
	AddedAt   time.Time `json:"added_at"`
}

// Product represents a product from the Product Service
type Product struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       float64   `json:"price"`
	Inventory   int       `json:"inventory"`
	Images      []Image   `json:"images,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Image represents a product image
type Image struct {
	ID          int    `json:"id"`
	ProductID   int    `json:"product_id"`
	ImageURL    string `json:"image_url"`
	IsPrimary   bool   `json:"is_primary"`
	DisplayOrder int   `json:"display_order"`
}

// User represents a user from the User Service
type User struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CartEvent represents a cart event for the message queue
type CartEvent struct {
	EventType  string    `json:"event_type"` // created, updated, item_added, item_removed, checkout
	CartID     int       `json:"cart_id"`
	UserID     *int      `json:"user_id"`
	SessionID  string    `json:"session_id"`
	ProductID  int       `json:"product_id,omitempty"`
	Quantity   int       `json:"quantity,omitempty"`
	EventTime  time.Time `json:"event_time"`
}

// CheckoutRequest represents the data needed to convert a cart to an order
type CheckoutRequest struct {
	ShippingAddress string `json:"shipping_address"`
	PaymentMethod   string `json:"payment_method"`
}

// App represents the application
type App struct {
	Router   *mux.Router
	DB       *pgxpool.Pool
	RabbitMQ *amqp.Connection
	RabbitCh *amqp.Channel
}

// Initialize sets up the database connection, message queue, and router
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

	// Declare the queue
	_, err = a.RabbitCh.QueueDeclare(
		CART_EVENTS_QUEUE, // name
		true,              // durable
		false,             // delete when unused
		false,             // exclusive
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare a queue: %v", err)
	}

	// Initialize router
	a.Router = mux.NewRouter()
	a.initializeRoutes()

	// Start cleanup routine for expired carts
	go a.cleanupExpiredCarts()

	return nil
}

// initializeRoutes sets up the API routes
func (a *App) initializeRoutes() {
	// Health check
	a.Router.HandleFunc("/health", a.healthCheck).Methods("GET")

	// Cart operations
	a.Router.HandleFunc("/carts", a.createCart).Methods("POST")
	a.Router.HandleFunc("/carts/{id:[0-9]+}", a.getCart).Methods("GET")
	a.Router.HandleFunc("/carts/session/{session_id}", a.getCartBySession).Methods("GET")
	a.Router.HandleFunc("/carts/user/{user_id:[0-9]+}", a.getCartByUser).Methods("GET")
	a.Router.HandleFunc("/carts/{id:[0-9]+}", a.deleteCart).Methods("DELETE")
	a.Router.HandleFunc("/carts/{id:[0-9]+}/user/{user_id:[0-9]+}", a.associateCartWithUser).Methods("PUT")

	// Cart item operations
	a.Router.HandleFunc("/carts/{id:[0-9]+}/items", a.addCartItem).Methods("POST")
	a.Router.HandleFunc("/carts/{id:[0-9]+}/items/{item_id:[0-9]+}", a.updateCartItem).Methods("PUT")
	a.Router.HandleFunc("/carts/{id:[0-9]+}/items/{item_id:[0-9]+}", a.removeCartItem).Methods("DELETE")
	
	// Checkout
	a.Router.HandleFunc("/carts/{id:[0-9]+}/checkout", a.checkoutCart).Methods("POST")
}

// cleanupExpiredCarts periodically removes expired carts
func (a *App) cleanupExpiredCarts() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("Running cart cleanup process...")
			result, err := a.DB.Exec(context.Background(),
				"DELETE FROM carts WHERE expires_at < NOW()")
			if err != nil {
				log.Printf("Error cleaning up expired carts: %v", err)
				continue
			}
			
			rowsAffected := result.RowsAffected()
			log.Printf("Cleaned up %d expired carts", rowsAffected)
		}
	}
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
		log.Printf("Cart Service listening on port %d...", PORT)
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

// createCart creates a new cart
func (a *App) createCart(w http.ResponseWriter, r *http.Request) {
	var cart Cart
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&cart); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Generate a session ID if not provided
	if cart.SessionID == "" {
		cart.SessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	cart.CreatedAt = time.Now()
	cart.UpdatedAt = time.Now()
	cart.ExpiresAt = time.Now().AddDate(0, 0, CART_EXPIRY_DAYS)

	// Insert cart into database
	err := a.DB.QueryRow(context.Background(),
		"INSERT INTO carts (user_id, session_id, created_at, updated_at, expires_at) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		cart.UserID, cart.SessionID, cart.CreatedAt, cart.UpdatedAt, cart.ExpiresAt).Scan(&cart.ID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Publish cart created event
	cartEvent := CartEvent{
		EventType: "created",
		CartID:    cart.ID,
		UserID:    cart.UserID,
		SessionID: cart.SessionID,
		EventTime: time.Now(),
	}
	a.publishCartEvent(cartEvent)

	respondWithJSON(w, http.StatusCreated, cart)
}

// getCart returns a cart by ID with its items
func (a *App) getCart(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid cart ID")
		return
	}

	cart, err := a.fetchCartWithItems(id)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Cart not found")
		return
	}

	respondWithJSON(w, http.StatusOK, cart)
}

// getCartBySession returns a cart by session ID
func (a *App) getCartBySession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	var cartID int
	err := a.DB.QueryRow(context.Background(),
		"SELECT id FROM carts WHERE session_id = $1 AND expires_at > NOW()",
		sessionID).Scan(&cartID)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "Cart not found")
		return
	}

	cart, err := a.fetchCartWithItems(cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, cart)
}

// getCartByUser returns a cart for a user
func (a *App) getCartByUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var cartID int
	err = a.DB.QueryRow(context.Background(),
		"SELECT id FROM carts WHERE user_id = $1 AND expires_at > NOW() ORDER BY updated_at DESC LIMIT 1",
		userID).Scan(&cartID)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "Cart not found")
		return
	}

	cart, err := a.fetchCartWithItems(cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, cart)
}

// deleteCart removes a cart
func (a *App) deleteCart(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid cart ID")
		return
	}

	// Get cart info before deleting for event
	var cart Cart
	err = a.DB.QueryRow(context.Background(),
		"SELECT id, user_id, session_id FROM carts WHERE id = $1",
		id).Scan(&cart.ID, &cart.UserID, &cart.SessionID)
	
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Cart not found")
		return
	}

	// Delete the cart
	_, err = a.DB.Exec(context.Background(), "DELETE FROM carts WHERE id = $1", id)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Publish cart deleted event
	cartEvent := CartEvent{
		EventType: "deleted",
		CartID:    cart.ID,
		UserID:    cart.UserID,
		SessionID: cart.SessionID,
		EventTime: time.Now(),
	}
	a.publishCartEvent(cartEvent)

	respondWithJSON(w, http.StatusOK, map[string]string{"result": "success"})
}

// associateCartWithUser links a cart to a user (e.g., after login)
func (a *App) associateCartWithUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cartID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid cart ID")
		return
	}

	userID, err := strconv.Atoi(vars["user_id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Verify user exists by calling the User Service
	resp, err := http.Get(fmt.Sprintf("%s/users/%d", USER_SERVICE_URL, userID))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to verify user")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respondWithError(w, http.StatusBadRequest, "User does not exist")
		return
	}

	// Check if user already has a cart
	var existingCartID int
	err = a.DB.QueryRow(context.Background(),
		"SELECT id FROM carts WHERE user_id = $1 AND expires_at > NOW()",
		userID).Scan(&existingCartID)

	if err == nil {
		// User already has a cart, merge items from the guest cart
		err = a.mergeGuestCart(existingCartID, cartID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Delete the guest cart
		_, err = a.DB.Exec(context.Background(), "DELETE FROM carts WHERE id = $1", cartID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		cart, err := a.fetchCartWithItems(existingCartID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		respondWithJSON(w, http.StatusOK, cart)
		return
	}

	// Update the cart with user ID
	_, err = a.DB.Exec(context.Background(),
		"UPDATE carts SET user_id = $1, updated_at = NOW() WHERE id = $2",
		userID, cartID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Publish cart updated event
	cartEvent := CartEvent{
		EventType: "updated",
		CartID:    cartID,
		UserID:    &userID,
		EventTime: time.Now(),
	}
	a.publishCartEvent(cartEvent)

	cart, err := a.fetchCartWithItems(cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, cart)
}

// mergeGuestCart merges items from a guest cart into a user cart
func (a *App) mergeGuestCart(userCartID, guestCartID int) error {
	// Get items from guest cart
	rows, err := a.DB.Query(context.Background(),
		"SELECT product_id, quantity FROM cart_items WHERE cart_id = $1",
		guestCartID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var productID, quantity int
		if err := rows.Scan(&productID, &quantity); err != nil {
			return err
		}

		// Check if the item already exists in the user cart
		var existingItemID, existingQuantity int
		err = a.DB.QueryRow(context.Background(),
			"SELECT id, quantity FROM cart_items WHERE cart_id = $1 AND product_id = $2",
			userCartID, productID).Scan(&existingItemID, &existingQuantity)

		if err == nil {
			// Item exists, update quantity
			_, err = a.DB.Exec(context.Background(),
				"UPDATE cart_items SET quantity = $1 WHERE id = $2",
				existingQuantity+quantity, existingItemID)
			if err != nil {
				return err
			}
		} else {
			// Item doesn't exist, add it
			_, err = a.DB.Exec(context.Background(),
				"INSERT INTO cart_items (cart_id, product_id, quantity, added_at) VALUES ($1, $2, $3, NOW())",
				userCartID, productID, quantity)
			if err != nil {
				return err
			}
		}
	}

	// Update cart timestamp
	_, err = a.DB.Exec(context.Background(),
		"UPDATE carts SET updated_at = NOW() WHERE id = $1", userCartID)
	
	return err
}

// addCartItem adds an item to a cart
func (a *App) addCartItem(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cartID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid cart ID")
		return
	}

	var item CartItem
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&item); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Set cart ID and added time
	item.CartID = cartID
	item.AddedAt = time.Now()

	// Verify product exists and has sufficient inventory
	product, err := a.getProductInfo(item.ProductID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Product not found")
		return
	}

	if product.Inventory < item.Quantity {
		respondWithError(w, http.StatusBadRequest, "Insufficient inventory")
		return
	}

	// Check if the item already exists in the cart
	var existingItemID, existingQuantity int
	err = a.DB.QueryRow(context.Background(),
		"SELECT id, quantity FROM cart_items WHERE cart_id = $1 AND product_id = $2",
		cartID, item.ProductID).Scan(&existingItemID, &existingQuantity)

	var itemID int
	if err == nil {
		// Item exists, update quantity
		_, err = a.DB.Exec(context.Background(),
			"UPDATE cart_items SET quantity = $1 WHERE id = $2",
			existingQuantity+item.Quantity, existingItemID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		itemID = existingItemID
	} else {
		// Item doesn't exist, insert it
		err = a.DB.QueryRow(context.Background(),
			"INSERT INTO cart_items (cart_id, product_id, quantity, added_at) VALUES ($1, $2, $3, $4) RETURNING id",
			item.CartID, item.ProductID, item.Quantity, item.AddedAt).Scan(&itemID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Update cart timestamp
	_, err = a.DB.Exec(context.Background(),
		"UPDATE carts SET updated_at = NOW() WHERE id = $1", cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get cart info for event
	var cart Cart
	err = a.DB.QueryRow(context.Background(),
		"SELECT user_id, session_id FROM carts WHERE id = $1", cartID).Scan(&cart.UserID, &cart.SessionID)
	if err != nil {
		log.Printf("Error getting cart info for event: %v", err)
	} else {
		// Publish item added event
		cartEvent := CartEvent{
			EventType: "item_added",
			CartID:    cartID,
			UserID:    cart.UserID,
			SessionID: cart.SessionID,
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			EventTime: time.Now(),
		}
		a.publishCartEvent(cartEvent)
	}

	// Return updated cart
	updatedCart, err := a.fetchCartWithItems(cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, updatedCart)
}

// updateCartItem updates the quantity of an item in a cart
func (a *App) updateCartItem(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cartID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid cart ID")
		return
	}

	itemID, err := strconv.Atoi(vars["item_id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid item ID")
		return
	}

	var update struct {
		Quantity int `json:"quantity"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&update); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	if update.Quantity <= 0 {
		respondWithError(w, http.StatusBadRequest, "Quantity must be positive")
		return
	}

	// Get product ID for the cart item
	var productID int
	err = a.DB.QueryRow(context.Background(),
		"SELECT product_id FROM cart_items WHERE id = $1 AND cart_id = $2",
		itemID, cartID).Scan(&productID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Cart item not found")
		return
	}

	// Verify product has sufficient inventory
	product, err := a.getProductInfo(productID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error verifying product")
		return
	}

	if product.Inventory < update.Quantity {
		respondWithError(w, http.StatusBadRequest, "Insufficient inventory")
		return
	}

	// Update the item quantity
	_, err = a.DB.Exec(context.Background(),
		"UPDATE cart_items SET quantity = $1 WHERE id = $2 AND cart_id = $3",
		update.Quantity, itemID, cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update cart timestamp
	_, err = a.DB.Exec(context.Background(),
		"UPDATE carts SET updated_at = NOW() WHERE id = $1", cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return updated cart
	updatedCart, err := a.fetchCartWithItems(cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, updatedCart)
}

// removeCartItem removes an item from a cart
func (a *App) removeCartItem(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cartID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid cart ID")
		return
	}

	itemID, err := strconv.Atoi(vars["item_id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid item ID")
		return
	}

	// Get product ID for the cart item for event
	var productID int
	err = a.DB.QueryRow(context.Background(),
		"SELECT product_id FROM cart_items WHERE id = $1 AND cart_id = $2",
		itemID, cartID).Scan(&productID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Cart item not found")
		return
	}

	// Delete the item
	_, err = a.DB.Exec(context.Background(),
		"DELETE FROM cart_items WHERE id = $1 AND cart_id = $2",
		itemID, cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update cart timestamp
	_, err = a.DB.Exec(context.Background(),
		"UPDATE carts SET updated_at = NOW() WHERE id = $1", cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get cart info for event
	var cart Cart
	err = a.DB.QueryRow(context.Background(),
		"SELECT user_id, session_id FROM carts WHERE id = $1", cartID).Scan(&cart.UserID, &cart.SessionID)
	if err == nil {
		// Publish item removed event
		cartEvent := CartEvent{
			EventType: "item_removed",
			CartID:    cartID,
			UserID:    cart.UserID,
			SessionID: cart.SessionID,
			ProductID: productID,
			EventTime: time.Now(),
		}
		a.publishCartEvent(cartEvent)
	}

	// Return updated cart
	updatedCart, err := a.fetchCartWithItems(cartID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, updatedCart)
}

// checkoutCart converts a cart to an order
func (a *App) checkoutCart(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cartID, err := strconv.Atoi(vars["id"])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid cart ID")
		return
	}

	var checkout CheckoutRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&checkout); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Get cart with items
	cart, err := a.fetchCartWithItems(cartID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Cart not found")
		return
	}

	if len(cart.Items) == 0 {
		respondWithError(w, http.StatusBadRequest, "Cart is empty")
		return
	}

	// Verify user ID exists for the cart
	if cart.UserID == nil {
		respondWithError(w, http.StatusBadRequest, "Cart must be associated with a user to checkout")
		return
	}

	// Prepare order request
	type OrderItemInput struct {
		ProductID int `json:"product_id"`
		Quantity  int `json:"quantity"`
	}
	
	orderRequest := struct {
		UserID int             `json:"user_id"`
		Items  []OrderItemInput `json:"items"`
	}{
		UserID: *cart.UserID,
		Items:  make([]OrderItemInput, 0, len(cart.Items)),
	}

	for _, item := range cart.Items {
		orderRequest.Items = append(orderRequest.Items, OrderItemInput{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
		})
	}

	// Send order to Order Service
	orderJSON, err := json.Marshal(orderRequest)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error preparing order")
		return
	}


	resp, err := http.Post(fmt.Sprintf("%s/orders", ORDER_SERVICE_URL), 
    "application/json", 
    bytes.NewBuffer(orderJSON))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error communicating with Order Service")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := ioutil.ReadAll(resp.Body)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Error creating order: %s", string(body)))
		return
	}

	// Parse order response
	var orderResponse map[string]interface{}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error reading order response")
		return
	}

	err = json.Unmarshal(body, &orderResponse)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error parsing order response")
		return
	}

	// Clear the cart
	_, err = a.DB.Exec(context.Background(), "DELETE FROM cart_items WHERE cart_id = $1", cartID)
	if err != nil {
		log.Printf("Error clearing cart items: %v", err) // Log but don't fail
	}

	// Delete the cart
	_, err = a.DB.Exec(context.Background(), "DELETE FROM carts WHERE id = $1", cartID)
	if err != nil {
		log.Printf("Error deleting cart: %v", err) // Log but don't fail
	}

	// Publish checkout event
	cartEvent := CartEvent{
		EventType: "checkout",
		CartID:    cartID,
		UserID:    cart.UserID,
		SessionID: cart.SessionID,
		EventTime: time.Now(),
	}
	a.publishCartEvent(cartEvent)

	// Return order information
	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Order created successfully",
		"order":   orderResponse,
	})
}

// Helper function to fetch a cart with its items
func (a *App) fetchCartWithItems(cartID int) (Cart, error) {
	var cart Cart
	err := a.DB.QueryRow(context.Background(),
		"SELECT id, user_id, session_id, created_at, updated_at, expires_at FROM carts WHERE id = $1",
		cartID).Scan(&cart.ID, &cart.UserID, &cart.SessionID, &cart.CreatedAt, &cart.UpdatedAt, &cart.ExpiresAt)

	if err != nil {
		return cart, err
	}

	rows, err := a.DB.Query(context.Background(),
		"SELECT id, cart_id, product_id, quantity, added_at FROM cart_items WHERE cart_id = $1",
		cartID)
	if err != nil {
		return cart, err
	}
	defer rows.Close()

	cart.Items = []CartItem{}
	cart.Total = 0

	for rows.Next() {
		var item CartItem
		if err := rows.Scan(&item.ID, &item.CartID, &item.ProductID, &item.Quantity, &item.AddedAt); err != nil {
			return cart, err
		}

		// Get product info
		product, err := a.getProductInfo(item.ProductID)
		if err == nil {
			item.Name = product.Name
			item.Price = product.Price
			cart.Total += product.Price * float64(item.Quantity)
		}

		cart.Items = append(cart.Items, item)
	}

	return cart, nil
}

// getProductInfo fetches product information from the Product Service
func (a *App) getProductInfo(productID int) (Product, error) {
	var product Product
	resp, err := http.Get(fmt.Sprintf("%s/products/%d", PRODUCT_SERVICE_URL, productID))
	if err != nil {
		return product, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return product, fmt.Errorf("product not found")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return product, err
	}

	err = json.Unmarshal(body, &product)
	return product, err
}

// publishCartEvent publishes a cart event to RabbitMQ
func (a *App) publishCartEvent(event CartEvent) {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error serializing cart event: %v", err)
		return
	}

	err = a.RabbitCh.Publish(
		"",                 // exchange
		CART_EVENTS_QUEUE,  // routing key
		false,              // mandatory
		false,              // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        eventJSON,
		})

	if err != nil {
		log.Printf("Error publishing cart event: %v", err)
	}
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