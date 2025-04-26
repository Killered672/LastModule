package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"calc_service/internal/auth"
)

type Config struct {
	Addr                string
	TimeAddition        int
	TimeSubtraction     int
	TimeMultiplications int
	TimeDivisions       int
}

type Orchestrator1 struct {
	Config  *Config
	Storage *storage.Storage
	mu      sync.RWMutex
}

func Configuration() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ta, _ := strconv.Atoi(os.Getenv("TIME_ADDITION_MS"))

	if ta == 0 {
		ta = 100
	}

	ts, _ := strconv.Atoi(os.Getenv("TIME_SUBTRACTION_MS"))
	if ts == 0 {
		ts = 100
	}

	tm, _ := strconv.Atoi(os.Getenv("TIME_MULTIPLICATIONS_MS"))
	if tm == 0 {
		tm = 100
	}

	td, _ := strconv.Atoi(os.Getenv("TIME_DIVISIONS_MS"))
	if td == 0 {
		td = 100
	}

	return &Config{
		Addr:                port,
		TimeAddition:        ta,
		TimeSubtraction:     ts,
		TimeMultiplications: tm,
		TimeDivisions:       td,
	}
}

type Orchestrator struct {
	Config      *Config
	exprStore   map[string]*Expression
	taskStore   map[string]*Task
	taskQueue   []*Task
	mu          sync.Mutex
	exprCounter int64
	taskCounter int64
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		Config:    Configuration(),
		exprStore: make(map[string]*Expression),
		taskStore: make(map[string]*Task),
		taskQueue: make([]*Task, 0),
	}
}

func NewOrchestrator1() *Orchestrator {
	storage, err := storage.NewStorage("calc_service.db")
	if err != nil {
		log.Fatal(err)
	}

	err = storage.Init()
	if err != nil {
		log.Fatal(err)
	}

	return &Orchestrator{
		Config:  Configuration(),
		Storage: storage,
	}
}

func (o *Orchestrator) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Login == "" || req.Password == "" {
		http.Error(w, `{"error":"Login and password are required"}`, http.StatusBadRequest)
		return
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		log.Printf("Failed to hash password: %v", err)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	userID, err := o.Storage.CreateUser(req.Login, hashedPassword)
	if err != nil {
		if errors.Is(err, storage.ErrAlreadyExists) {
			http.Error(w, `{"error":"User already exists"}`, http.StatusConflict)
			return
		}
		log.Printf("Failed to create user: %v", err)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":    userID,
		"login": req.Login,
	})
}

func (o *Orchestrator) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request body"}`, http.StatusBadRequest)
		return
	}

	user, err := o.Storage.GetUserByLogin(req.Login)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
			return
		}
		log.Printf("Failed to get user: %v", err)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	if !auth.CheckPasswordHash(req.Password, user.Password) {
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	token, err := auth.GenerateJWT(user.ID)
	if err != nil {
		log.Printf("Failed to generate token: %v", err)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":    user.ID,
			"login": user.Login,
		},
	})
}

func (o *Orchestrator) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/login" || r.URL.Path == "/api/v1/register" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"error":"Authorization header is required"}`, http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == "" {
			http.Error(w, `{"error":"Invalid authorization header format"}`, http.StatusUnauthorized)
			return
		}

		userID, err := auth.ParseJWT(tokenString)
		if err != nil {
			http.Error(w, `{"error":"Invalid token"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), "userID", userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type Expression struct {
	ID     string   `json:"id"`
	Expr   string   `json:"expression"`
	Status string   `json:"status"`
	Result *float64 `json:"result,omitempty"`
	AST    *ASTNode `json:"-"`
}

type Task struct {
	ID            string   `json:"id"`
	ExprID        string   `json:"-"`
	Arg1          float64  `json:"arg1"`
	Arg2          float64  `json:"arg2"`
	Operation     string   `json:"operation"`
	OperationTime int      `json:"operation_time"`
	Node          *ASTNode `json:"-"`
}

func (o *Orchestrator) calculateHandler(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserIDFromContext(r.Context())

	var req struct {
		Expression string `json:"expression"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid Body"}`, http.StatusUnprocessableEntity)
		return
	}

	expr, err := o.Storage.CreateExpression(userID, req.Expression)
	if err != nil {
		http.Error(w, `{"error":"Failed to create expression"}`, http.StatusInternalServerError)
		return
	}

	ast, err := ParseAST(req.Expression)
	if err != nil {
		o.Storage.UpdateExpression(&storage.Expression{
			ID:     expr.ID,
			Status: "error",
		})
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusUnprocessableEntity)
		return
	}

	expr.AST = ast
	o.Tasks(expr)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": expr.ID})
}

func (o *Orchestrator) expressionsHandler(w http.ResponseWriter, r *http.Request) {
	userID := auth.GetUserIDFromContext(r.Context())

	exprs, err := o.Storage.GetExpressions(userID)
	if err != nil {
		http.Error(w, `{"error":"Failed to get expressions"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"expressions": exprs})
}
func (o *Orchestrator) expressionIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Wrong Method"}`, http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Path[len("/api/v1/expressions/"):]
	o.mu.Lock()
	expr, ok := o.exprStore[id]
	o.mu.Unlock()

	if !ok {
		http.Error(w, `{"error":"Expression not found"}`, http.StatusNotFound)
		return
	}

	if expr.AST != nil && expr.AST.IsLeaf {
		expr.Status = "completed"
		expr.Result = &expr.AST.Value
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"expression": expr})
}

func (o *Orchestrator) getTaskHandler(w http.ResponseWriter, r *http.Request) {
	task, err := o.Storage.GetPendingTask()
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.Error(w, `{"error":"No task available"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"Internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"task": task})
}

func (o *Orchestrator) postTaskHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string  `json:"id"`
		Result float64 `json:"result"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid Body"}`, http.StatusUnprocessableEntity)
		return
	}

	if err := o.Storage.CompleteTask(req.ID, req.Result); err != nil {
		http.Error(w, `{"error":"Failed to complete task"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"result accepted"}`))
}

func (o *Orchestrator) Tasks(expr *Expression) {
	var traverse func(node *ASTNode)
	traverse = func(node *ASTNode) {

		if node == nil || node.IsLeaf {
			return
		}

		traverse(node.Left)
		traverse(node.Right)
		if node.Left != nil && node.Right != nil && node.Left.IsLeaf && node.Right.IsLeaf {
			if !node.TaskScheduled {
				o.taskCounter++
				taskID := fmt.Sprintf("%d", o.taskCounter)
				var opTime int
				switch node.Operator {
				case "+":
					opTime = o.Config.TimeAddition
				case "-":
					opTime = o.Config.TimeSubtraction
				case "*":
					opTime = o.Config.TimeMultiplications
				case "/":
					opTime = o.Config.TimeDivisions
				default:
					opTime = 100
				}

				task := &Task{
					ID:            taskID,
					ExprID:        expr.ID,
					Arg1:          node.Left.Value,
					Arg2:          node.Right.Value,
					Operation:     node.Operator,
					OperationTime: opTime,
					Node:          node,
				}
				node.TaskScheduled = true
				o.taskStore[taskID] = task
				o.taskQueue = append(o.taskQueue, task)
			}
		}
	}
	traverse(expr.AST)
}

func (o *Orchestrator) RunServer() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/register", o.registerHandler)
	mux.HandleFunc("/api/v1/login", o.loginHandler)

	protected := http.NewServeMux()
	protected.HandleFunc("/api/v1/calculate", o.calculateHandler)
	protected.HandleFunc("/api/v1/expressions", o.expressionsHandler)
	protected.HandleFunc("/api/v1/expressions/", o.expressionIDHandler)
	protected.HandleFunc("/internal/task", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			o.getTaskHandler(w, r)
		} else if r.Method == http.MethodPost {
			o.postTaskHandler(w, r)
		} else {
			http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})

	mux.Handle("/", o.authMiddleware(protected))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Not Found"}`, http.StatusNotFound)
	})

	go func() {
		for {
			time.Sleep(2 * time.Second)
			o.mu.Lock()
			if len(o.taskQueue) > 0 {
				log.Printf("Pending tasks in queue: %d", len(o.taskQueue))
			}
			o.mu.Unlock()
		}
	}()
	return http.ListenAndServe(":"+o.Config.Addr, mux)
}
