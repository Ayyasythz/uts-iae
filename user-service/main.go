package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v4/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	PORT                = 8081
	POSTGRES_URI        = "postgres://postgres:postgres@postgres:5432/user_service" // Changed localhost to postgres
	RABBITMQ_URI        = "amqp://guest:guest@rabbitmq:5672/"                       // Changed localhost to rabbitmq
	ORDER_UPDATES_QUEUE = "order_updates"
)

type User struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type OrderHistory struct {
	UserID    int       `json:"user_id"`
	OrderID   int       `json:"order_id"`
	Total     float64   `json:"total"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type App struct {
	Router   *mux.Router
	DB       *pgxpool.Pool
	RabbitMQ *amqp.Connection
	RabbitCh *amqp.Channel
}

func (a *App) Initialize() error {
	var err error

	a.DB, err = pgxpool.Connect(context.Background(), POSTGRES_URI)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %v", err)
	}

	if err = a.DB.Ping(context.Background()); err != nil {
		return fmt.Errorf("unable to ping database: %v", err)
	}

	a.RabbitMQ, err = amqp.Dial(RABBITMQ_URI)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %v", err)
	}

	a.RabbitCh, err = a.RabbitMQ.Channel()
	if err != nil {
		return fmt.Errorf("failed to open a channel: %v", err)
	}

	_, err = a.RabbitCh.QueueDeclare(
		ORDER_UPDATES_QUEUE, // name
		true,                // durable
		false,               // delete when unused
		false,               // exclusive
		false,               // no-wait
		nil,                 // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare a queue: %v", err)
	}

	go a.consumeOrderUpdates()

	a.Router = mux.NewRouter()
	a.initializeRoutes()

	return nil
}

func (a *App) initializeRoutes() {
	a.Router.HandleFunc("/health", a.healthCheck).Methods("GET")

	a.Router.HandleFunc("/users", a.getUsers).Methods("GET")
	a.Router.HandleFunc("/users/{id:[0-9]+}", a.getUser).Methods("GET")
	a.Router.HandleFunc("/users", a.createUser).Methods("POST")
	a.Router.HandleFunc("/users/{id:[0-9]+}", a.updateUser).Methods("PUT")
	a.Router.HandleFunc("/users/{id:[0-9]+}", a.deleteUser).Methods("DELETE")

	a.Router.HandleFunc("/users/{id:[0-9]+}/orders", a.getUserOrders).Methods("GET")
}

// consumeOrderUpdates listens for order updates from the Order Service
func (a *App) consumeOrderUpdates() {
	msgs, err := a.RabbitCh.Consume(
		ORDER_UPDATES_QUEUE, // queue
		"",                  // consumer
		true,                // auto-ack
		false,               // exclusive
		false,               // no-local
		false,               // no-wait
		nil,                 // args
	)
	if err != nil {
		log.Printf("Failed to register a consumer: %v", err)
		return
	}

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			var orderHistory OrderHistory
			if err := json.Unmarshal(d.Body, &orderHistory); err != nil {
				log.Printf("Error parsing order update: %v", err)
				continue
			}

			_, err := a.DB.Exec(context.Background(),
				"INSERT INTO order_history (user_id, order_id, total, status, created_at) VALUES ($1, $2, $3, $4, $5)",
				orderHistory.UserID, orderHistory.OrderID, orderHistory.Total, orderHistory.Status, orderHistory.CreatedAt)

			if err != nil {
				log.Printf("Error storing order history: %v", err)
			} else {
				log.Printf("Stored order history for user %d, order %d", orderHistory.UserID, orderHistory.OrderID)
			}
		}
	}()

	<-forever
}

func (a *App) Run() {
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", PORT),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
		IdleTimeout:  60 * time.Second,
		Handler:      a.Router,
	}

	go func() {
		log.Printf("User Service listening on port %d...", PORT)
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := a.RabbitCh.Close(); err != nil {
		log.Printf("Error closing RabbitMQ channel: %v", err)
	}
	if err := a.RabbitMQ.Close(); err != nil {
		log.Printf("Error closing RabbitMQ connection: %v", err)
	}

	a.DB.Close()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}

func (a *App) healthCheck(w http.ResponseWriter, r *http.Request) {
	err := a.DB.Ping(context.Background())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Database connection failure")
		return
	}

	if a.RabbitMQ.IsClosed() {
		respondWithError(w, http.StatusInternalServerError, "RabbitMQ connection failure")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (a *App) getUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(context.Background(), "SELECT id, username, email, created_at, updated_at FROM users")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.CreatedAt, &u.UpdatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		users = append(users, u)
	}

	respondWithJSON(w, http.StatusOK, users)
}

func (a *App) getUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var u User
	err := a.DB.QueryRow(context.Background(),
		"SELECT id, username, email, created_at, updated_at FROM users WHERE id = $1",
		id).Scan(&u.ID, &u.Username, &u.Email, &u.CreatedAt, &u.UpdatedAt)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "User not found")
		return
	}

	respondWithJSON(w, http.StatusOK, u)
}

func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	var u User
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&u); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()

	err := a.DB.QueryRow(context.Background(),
		"INSERT INTO users (username, email, created_at, updated_at) VALUES ($1, $2, $3, $4) RETURNING id",
		u.Username, u.Email, u.CreatedAt, u.UpdatedAt).Scan(&u.ID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusCreated, u)
}

func (a *App) updateUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var u User
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&u); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	u.UpdatedAt = time.Now()

	_, err := a.DB.Exec(context.Background(),
		"UPDATE users SET username = $1, email = $2, updated_at = $3 WHERE id = $4",
		u.Username, u.Email, u.UpdatedAt, id)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	u.ID = parseInt(id)
	respondWithJSON(w, http.StatusOK, u)
}

// deleteUser removes a user
func (a *App) deleteUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := a.DB.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"result": "success"})
}

func (a *App) getUserOrders(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	rows, err := a.DB.Query(context.Background(),
		"SELECT user_id, order_id, total, status, created_at FROM order_history WHERE user_id = $1 ORDER BY created_at DESC",
		id)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	orders := []OrderHistory{}
	for rows.Next() {
		var o OrderHistory
		if err := rows.Scan(&o.UserID, &o.OrderID, &o.Total, &o.Status, &o.CreatedAt); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		orders = append(orders, o)
	}

	respondWithJSON(w, http.StatusOK, orders)
}

func parseInt(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

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
